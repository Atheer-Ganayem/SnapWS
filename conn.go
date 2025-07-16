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
	raw       net.Conn
	Manager   *Manager[KeyType]
	Key       KeyType
	isClosed  atomic.Bool
	closeOnce sync.Once
	MetaData  sync.Map
	// Empty string means its a raw websocket
	SubProtocol string

	// reading channels
	inboundFrames   chan *internal.Frame
	inboundMessages chan *internal.FrameGroup

	// writing channels

	// used for sending a internal.FrameGroup, text or binary only.
	outboundMessage chan *SendMessageRequest
	// used for sending outboundControl frames.
	outboundControl chan *SendFrameRequest
	// ticker for ping loop
	ticker *time.Ticker
}

type SendMessageRequest struct {
	frames *internal.FrameGroup
	errCh  chan error
	ctx    context.Context
}

type SendFrameRequest struct {
	frame *internal.Frame
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

		inboundFrames:   make(chan *internal.Frame, 32),
		inboundMessages: make(chan *internal.FrameGroup, 8),

		outboundMessage: make(chan *SendMessageRequest, 8),
		outboundControl: make(chan *SendFrameRequest, 4),
	}
}

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

		case req, ok := <-conn.outboundMessage:
			// checking if chan is closes
			if !ok {
				if req != nil {
					trySendErr(req.errCh, ErrChannelClosed)
				}
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

		messageLoop:
			for _, frame := range *req.frames {
				if conn.isClosed.Load() {
					return
				}
				if req.ctx.Err() != nil {
					trySendErr(req.errCh, req.ctx.Err())
					break
				}

				select {
				case creq, ok := <-conn.outboundControl:
					if !ok {
						if creq != nil && creq.errCh != nil {
							trySendErr(creq.errCh, ErrChannelClosed)
						}
						return
					}
					conn.sendFrame(creq)

				default:
					conn.sendFrame(&SendFrameRequest{frame: frame, errCh: req.errCh, ctx: req.ctx})
					select {
					case <-req.ctx.Done():
						trySendErr(req.errCh, req.ctx.Err())
						break messageLoop
					default:
					}
				}
			}
		}
	}
}

func (conn *Conn[KeyType]) pingLoop() {
	for range conn.ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), conn.Manager.WriteWait)
		defer cancel()

		if err := conn.Ping(ctx); err != nil {
			conn.closeWithCode(internal.ClosePolicyViolation, "failed to ping")
			return
		}
	}
}

func (conn *Conn[KeyType]) closeWithCode(code uint16, reason string) {
	conn.closeOnce.Do(func() {
		buf := new(bytes.Buffer)
		_ = binary.Write(buf, binary.BigEndian, code)
		buf.WriteString(reason)
		payload := buf.Bytes()

		frame, err := internal.NewFrame(true, internal.OpcodeClose, false, payload)

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
		close(conn.outboundMessage)
		close(conn.outboundControl)
		close(conn.inboundMessages)
		close(conn.inboundFrames)
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
