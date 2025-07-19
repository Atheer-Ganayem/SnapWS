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
	raw       net.Conn
	Manager   *Manager[KeyType]
	Key       KeyType
	isClosed  atomic.Bool
	closeOnce sync.Once
	MetaData  sync.Map
	// Empty string means its a raw websocket
	SubProtocol string

	// reading channels
	inboundFrames   chan *Frame
	inboundMessages chan FrameGroup

	// writing channels

	// used for sending a FrameGroup, text or binary only.
	outboundFrames chan *SendFrameRequest
	wLock          chan struct{}
	// used for sending outboundControl frames.
	outboundControl chan *SendFrameRequest
	// ticker for ping loop
	ticker *time.Ticker
}

type SendFrameRequest struct {
	frame *Frame
	errCh chan error
	ctx   context.Context
}

func (m *Manager[KeyType]) newConn(c net.Conn, key KeyType, subProtocol string) *Conn[KeyType] {
	return &Conn[KeyType]{
		raw:         c,
		Manager:     m,
		Key:         key,
		SubProtocol: subProtocol,
		ticker:      time.NewTicker(m.PingEvery),

		inboundFrames:   make(chan *Frame, 32),
		wLock:           make(chan struct{}, 1),
		inboundMessages: make(chan FrameGroup, 8),

		outboundFrames:  make(chan *SendFrameRequest, 16),
		outboundControl: make(chan *SendFrameRequest, 4),
	}
}

// Locks "wLock" indicating that a writer has been intiated.
// It tires to aquire the lock, if the provided context is done before
// succeeding to aquiring the lock, it return an error.
func (conn *Conn[KeyType]) lockW(ctx context.Context) error {
	select {
	case conn.wLock <- struct{}{}:
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
	case _, ok := <-conn.wLock:
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
				if req != nil && req.errCh != nil {
					req.errCh <- ErrChannelClosed
				}
				return
			}
			conn.sendFrame(req)

		case req, ok := <-conn.outboundFrames:
			if req == nil {
				break
			}
			// checking if chan is closes
			if !ok {
				trySendErr(req.errCh, ErrChannelClosed)
				return
			}
			// checking if ctx is done
			if req.ctx == nil {
				req.ctx = context.Background()
			}
			if req.ctx.Err() != nil {
				if req.errCh != nil {
					trySendErr(req.errCh, req.ctx.Err())
					break
				}
			}
			conn.sendFrame(req)
		}
	}
}

// A loop that runs as long as the connection is alive.
// Ping the client every "PingEvery" provided from the manager.
// If pinging fails the connection closes.
func (conn *Conn[KeyType]) pingLoop() {
	for range conn.ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), conn.Manager.WriteWait)
		err := conn.Ping(ctx)
		cancel()
		if err != nil {
			conn.closeWithCode(ClosePolicyViolation, "failed to ping")
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

		conn.raw.Close()
		conn.isClosed.Store(true)
		if conn.ticker != nil {
			conn.ticker.Stop()
		}
		close(conn.outboundFrames)
		close(conn.outboundControl)
		close(conn.inboundMessages)
		close(conn.inboundFrames)
		close(conn.wLock)
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
