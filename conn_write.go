package snapws

import (
	"context"
	"encoding/json"
	"time"
	"unicode/utf8"

	"github.com/Atheer-Ganayem/SnapWS/internal"
)

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

func trySendErr(errCh chan error, err error) {
	if errCh != nil {
		select {
		case errCh <- err:
		default:
		}
	}
}

func (conn *Conn[KeyType]) sendFrame(req *SendFrameRequest) {
	if req.ctx != nil && req.ctx.Err() != nil {
		trySendErr(req.errCh, req.ctx.Err())
		return
	}

	err := conn.write(req.frame)
	trySendErr(req.errCh, err)
}

// Low level writing, not safe to use concurrently.
// Use SendString, SendJSON, SendBytes for safe writing.
func (conn *Conn[KeyType]) write(frame *internal.Frame) (err error) {
	err = conn.raw.SetWriteDeadline(time.Now().Add(conn.Manager.WriteWait))
	if err != nil {
		return err
	}

	_, err = conn.raw.Write(frame.Bytes())
	return err
}

////////////////////////////////////

func (conn *Conn[KeyType]) splitAndSend(ctx context.Context, frame *internal.Frame) error {
	if ctx == nil {
		ctx = context.Background()
	}

	frames, err := frame.SplitIntoGroup(conn.Manager.WriteBufferSize)
	if err != nil {
		return err
	}

	errCh := make(chan error)
	req := &SendMessageRequest{
		frames: frames,
		errCh:  errCh,
		ctx:    ctx,
	}

	if conn.isClosed.Load() {
		return errConnClosed
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case conn.message <- req:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (conn *Conn[KeyType]) SendString(ctx context.Context, str string) error {
	if str == "" {
		return ErrEmptyPayload
	}

	if ok := utf8.ValidString(str); !ok {
		return ErrInvalidUTF8
	}

	frame, err := internal.NewFrame(true, internal.OpcodeText, false, []byte(str))
	if err != nil {
		return err
	}

	err = conn.splitAndSend(ctx, &frame)

	return err
}

func (conn *Conn[KeyType]) SendJSON(ctx context.Context, v any) error {
	if v == nil {
		return ErrEmptyPayload
	}

	b, err := json.Marshal(v)
	if err != nil {
		return err
	}

	frame, err := internal.NewFrame(true, internal.OpcodeText, false, b)
	if err != nil {
		return err
	}

	err = conn.splitAndSend(ctx, &frame)

	return err
}

func (conn *Conn[KeyType]) SendBytes(ctx context.Context, b []byte) error {
	if len(b) == 0 {
		return ErrEmptyPayload
	}

	frame, err := internal.NewFrame(true, internal.OpcodeBinary, false, b)
	if err != nil {
		return err
	}

	err = conn.splitAndSend(ctx, &frame)

	return err
}

func (conn *Conn[Key]) Ping() error {
	frame, err := internal.NewFrame(true, internal.OpcodePing, false, []byte("test"))
	if err != nil {
		return err
	}

	if conn.isClosed.Load() {
		return errConnClosed
	}

	errCh := make(chan error)
	conn.control <- &SendFrameRequest{
		frame: &frame,
		errCh: errCh,
		ctx:   nil,
	}

	return <-errCh
}

func (conn *Conn[KeyType]) Pong(payload []byte) {
	frame, err := internal.NewFrame(true, internal.OpcodePong, false, payload)

	if err == nil && !conn.isClosed.Load() {
		conn.control <- &SendFrameRequest{
			frame: &frame,
			errCh: nil,
		}
	}
}
