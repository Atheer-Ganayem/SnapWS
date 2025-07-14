package snapws

import (
	"context"
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

// Low level writing, not safe to use concurrently.
// Use SendString, SendJSON, SendBytes for safe writing.
func (conn *Conn[KeyType]) sendFrame(frame *internal.Frame) (err error) {
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
	defer close(errCh)

	select {
	case conn.message <- req:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (conn *Conn[KeyType]) SendString(ctx context.Context, str string) error {
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

// func (conn *Conn) SendJSON(val map[string]interface{}) error
// func (conn *Conn) SendBytes(b []byte) error
// func (conn *Conn) Ping() error

func (conn *Conn[KeyType]) Pong(payload []byte) {
	frame := internal.Frame{
		FIN:           true,
		OPCODE:        internal.OpcodePong,
		PayloadLength: len(payload),
		Payload:       payload,
	}

	conn.control <- &SendFrameRequest{
		frame: &frame,
		errCh: nil,
	}
}
