package snapws

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

type Conn[KeyType comparable] struct {
	raw         net.Conn
	Manager     *Manager[KeyType]
	SubProtocol string
	Key         KeyType
	MetaData    sync.Map
	// channel used to signal that the conn is closed.
	// when the conn closes, the channel closes, so any go routine trying to read from it,
	// would receive ok=false, indicating that the channel is closed => conn is closed.
	done      chan struct{}
	isClosed  atomic.Bool
	closeOnce sync.Once
	// Empty string means its a raw websocket

	inboundFrames   chan *Frame
	inboundMessages chan *Message
	// ticker for ping loop
	ticker *time.Ticker

	// used for sending control frames.
	outboundControl chan *SendFrameRequest

	writer *ConnWriter[KeyType]

	readFrameBuf []byte
}

type SendFrameRequest struct {
	frame *Frame
	errCh chan error
	ctx   context.Context
}

func (conn *Conn[KeyType]) sendMessageToChan(m *Message) {
	select {
	case <-conn.done:
		switch conn.Manager.BackpressureStrategy {
		case BackpressureClose:
			conn.closeWithCode(ClosePolicyViolation, ErrSlowConsumer.Error())
			return
		case BackpressureDrop:
			return
		case BackpressureWait:
			select {
			case <-conn.done:
			case conn.inboundMessages <- m:
			}
			return
		}
	case conn.inboundMessages <- m:
	}
}

func (m *Manager[KeyType]) newConn(c net.Conn, key KeyType, subProtocol string) *Conn[KeyType] {
	conn := &Conn[KeyType]{
		raw:         c,
		Manager:     m,
		Key:         key,
		SubProtocol: subProtocol,
		done:        make(chan struct{}),
		ticker:      time.NewTicker(m.PingEvery),

		inboundFrames:   make(chan *Frame, m.InboundFramesSize),
		inboundMessages: make(chan *Message, m.InboundMessagesSize),

		outboundControl: make(chan *SendFrameRequest, m.OutboundControlSize),

		readFrameBuf: make([]byte, m.ReadBufferSize),
	}

	conn.writer = conn.newWriter(OpcodeText)
	return conn
}

// Locks "wLock" indicating that a writer has been intiated.
// It tires to aquire the lock, if the provided context is done before
// succeeding to aquiring the lock, it return an error.
func (conn *Conn[KeyType]) lockW(ctx context.Context) error {
	select {
	case <-conn.done:
		return Fatal(ErrConnClosed)
	case conn.writer.lock <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Unlocks "wLock", indicating that the writer has finished.
// Returns a snapws.FatalError if the connection is closed.
// Returns nil if unlocking succeeds or if it was already unlocked.
func (conn *Conn[KeyType]) unlockW() error {
	select {
	case _, ok := <-conn.writer.lock:
		if !ok {
			return Fatal(ErrChannelClosed)
		}
		return nil
	default:
		// already unlocked
		return nil
	}
}

// A loop that runs as long as the connection is alive.
// Listens for outgoing frames.
// It gives priotiry to control frames.
func (conn *Conn[KeyType]) listen() {
	for {
		select {
		case req, ok := <-conn.outboundControl:
			if !ok {
				if req != nil {
					trySendErr(req.errCh, ErrChannelClosed)
				}
				return
			}
			if req == nil {
				break
			}
			trySendErr(req.errCh, conn.sendFrame(req.frame.Encoded))

		case _, ok := <-conn.writer.sig:
			if !ok {
				if conn.writer != nil {
					trySendErr(conn.writer.errCh, Fatal(ErrChannelClosed))
				}
				return
			}

			if conn.writer.ctx.Err() != nil {
				trySendErr(conn.writer.errCh, conn.writer.ctx.Err())
				break
			}

			err := conn.sendFrame(conn.writer.buf[conn.writer.start:conn.writer.used])
			if conn.writer.ctx.Err() == nil {
				trySendErr(conn.writer.errCh, err)
			}
		}
	}
}

// A loop that runs as long as the connection is alive.
// Ping the client every "PingEvery" provided from the manager.
// If pinging fails the connection closes.
func (conn *Conn[KeyType]) pingLoop() {
	for range conn.ticker.C {
		err := conn.Ping()
		if err != nil {
			conn.closeWithCode(ClosePolicyViolation, err.Error())
			return
		}
	}

}

// closeWithCode closes the connection with the given code and reason.
func (conn *Conn[KeyType]) closeWithCode(code uint16, reason string) {
	conn.closeOnce.Do(func() {
		buf := new(bytes.Buffer)
		_ = binary.Write(buf, binary.BigEndian, code)
		buf.WriteString(reason)
		payload := buf.Bytes()

		frame, err := NewFrame(true, OpcodeClose, false, payload)

		errCh := make(chan error)
		if err == nil && !conn.isClosed.Load() {
			select {
			case conn.outboundControl <- &SendFrameRequest{
				frame: &frame,
				errCh: errCh,
			}:
			default:
			}
		}

		select {
		case <-errCh:
		case <-time.After(conn.Manager.WriteWait):
		}

		conn.writer.Close()
		conn.raw.Close()
		conn.isClosed.Store(true)
		if conn.ticker != nil {
			conn.ticker.Stop()
		}
		close(conn.done)
		close(conn.writer.sig)
		close(conn.outboundControl)
		close(conn.inboundMessages)
		close(conn.inboundFrames)
		close(conn.writer.lock)
		conn.Manager.unregister(conn.Key)
	})
}

// Used to trigger closeWithCode with the code and reason parsed from payload.
// The payload must be of at least length 2, first 2 bytes are uint16 represnting the close code,
// The rest of the payload is optional, represnting a UTF-8 reason.
// Any violations would cause a close with CloseProtocolError with the apropiate reason.
func (conn *Conn[KeyType]) closeWithPayload(payload []byte) {
	if len(payload) < 2 {
		conn.closeWithCode(CloseProtocolError, "invalid close frame payload")
		return
	}
	code := binary.BigEndian.Uint16(payload[:2])
	if !IsValidCloseCode(code) {
		conn.closeWithCode(CloseProtocolError, "invalid close code")
		return
	}

	if len(payload) > 2 {
		if !utf8.Valid(payload[2:]) {
			conn.closeWithCode(CloseProtocolError, ErrInvalidUTF8.Error())
			return
		}
		conn.closeWithCode(code, string(payload[2:]))
	} else {
		conn.closeWithCode(code, "")
	}
}

// Closes the conn normaly.
func (conn *Conn[KeyType]) Close() {
	conn.closeWithCode(CloseNormalClosure, "Normal close")
}
