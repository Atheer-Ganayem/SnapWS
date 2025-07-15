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

	"github.com/Atheer-Ganayem/SnapWS/internal"
)

type Conn[KeyType comparable] struct {
	raw     net.Conn
	Manager *Manager[KeyType]

	//
	isClosed atomic.Bool
	// used for sending a internal.FrameGroup, text or binary only.
	message chan *SendMessageRequest
	// used for sending a single frame from the internal.FrameGroup receive by the mesage channel.
	frame chan *SendFrameRequest
	// used for sending control frames.
	control chan *SendFrameRequest
	// ticker for ping loop
	ticker *time.Ticker

	Key       KeyType
	MetaData  sync.Map
	closeOnce sync.Once
	// Empty string means its a raw websocket
	SubProtocol string
}

func (m *Manager[KeyType]) newConn(c net.Conn, key KeyType, subProtocol string) *Conn[KeyType] {
	return &Conn[KeyType]{
		raw:         c,
		message:     make(chan *SendMessageRequest, 8),
		frame:       make(chan *SendFrameRequest, 32),
		control:     make(chan *SendFrameRequest, 4),
		Manager:     m,
		Key:         key,
		SubProtocol: subProtocol,
	}
}

func (conn *Conn[KeyType]) frameListener() {
	for {
		select {
		case req, ok := <-conn.control:
			if !ok {
				if req != nil && req.errCh != nil {
					req.errCh <- ErrChannelClosed
				}
				return
			}
			conn.sendFrame(req)

		case req, ok := <-conn.frame:
			if !ok {
				if req != nil && req.errCh != nil {
					req.errCh <- ErrChannelClosed
				}
				return
			}
			conn.sendFrame(req)
		}
	}
}

func (conn *Conn[KeyType]) messageListener() {
	for req := range conn.message {
		ctx := req.ctx
		if ctx == nil {
			ctx = context.Background()
		}

		if ctx.Err() != nil {
			if req.errCh != nil {
				req.errCh <- ctx.Err()
				close(req.errCh)
			}
			continue
		}

		var err error
		for _, frame := range *req.frames {
			errCh := make(chan error)
			freq := &SendFrameRequest{
				frame: frame,
				errCh: errCh,
				ctx:   ctx,
			}

			// Send the frame
			if conn.isClosed.Load() {
				return
			}
			select {
			case <-ctx.Done():
				err = ctx.Err()
				break
			case conn.frame <- freq:
			}

			// Wait for write result
			select {
			case <-ctx.Done():
				err = ctx.Err()
			case err = <-errCh:
			}

			close(errCh)

			if err != nil {
				break
			}
		}

		if req.errCh != nil {
			req.errCh <- err
		}
	}
}

func (conn *Conn[KeyType]) pingLoop() {
	conn.ticker = time.NewTicker(conn.Manager.PingEvery)

	for range conn.ticker.C {
		conn.Ping()
	}
}

func (conn *Conn[KeyType]) closeWithCode(code uint16, reason string) {
	conn.closeOnce.Do(func() {
		buf := new(bytes.Buffer)
		_ = binary.Write(buf, binary.BigEndian, code)
		buf.WriteString(reason)
		payload := buf.Bytes()

		frame, err := internal.NewFrame(true, internal.OpcodeClose, false, payload)

		if err == nil && !conn.isClosed.Load() {
			select {
			case conn.control <- &SendFrameRequest{
				frame: &frame,
				errCh: nil,
			}:
			default:
				// channel full => skip sending close frame
			}
		}

		conn.isClosed.Store(true)
		conn.ticker.Stop()
		close(conn.message)
		close(conn.control)
		close(conn.frame)
		conn.Manager.unregister(conn.Key)
	})
}

// Used to trigger closeWithCode with the code and reason parsed from payload.
// The payload must be of at least length 2, first 2 bytes are uint16 represnting the close code,
// The rest of the payload is optional, represnting a UTF-8 reason.
// Any violations would cause a close with internal.CloseProtocolError with the apropiate reason.
func (conn *Conn[KeyType]) closeWithPayload(payload []byte) {
	if len(payload) < 2 {
		conn.closeWithCode(internal.CloseProtocolError, "invalid close frame payload")
		return
	}
	code := binary.BigEndian.Uint16(payload[:2])
	if !internal.IsValidCloseCode(code) {
		conn.closeWithCode(internal.CloseProtocolError, "invalid close code")
		return
	}

	if len(payload) > 2 {
		if !utf8.Valid(payload[2:]) {
			conn.closeWithCode(internal.CloseProtocolError, ErrInvalidUTF8.Error())
			return
		}
		conn.closeWithCode(code, string(payload[2:]))
	} else {
		conn.closeWithCode(code, "")
	}
}

// Closes the conn normaly.
func (conn *Conn[KeyType]) Close() {
	conn.closeWithCode(internal.CloseNormalClosure, "Normal close")
}
