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
		return Fatal(err)
	}

	_, err = conn.raw.Write(frame.Bytes())
	if err != nil {
		return Fatal(err)
	}

	return nil
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
		return Fatal(ErrConnClosed)
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

// SendBytes sends the given byte slice as a WebSocket binary message.
//
// The payload must be non-empty. If not, the method returns snapws.ErrEmptyPayload.
// The message will be split into fragments if needed based on WriteBufferSize.
//
// All errors except snapws.ErrEmptyPayload are of type snapws.FatalError,
// indicating that the connection was closed due to an I/O or protocol error.
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

// SendString sends the given string as a WebSocket text message.
//
// The string must be valid UTF-8 and non-empty. If it is not, the method returns
// snapws.ErrEmptyPayload or snapws.ErrInvalidUTF8. The message will be split
// into fragments if necessary based on WriteBufferSize.
//
// All returned errors except for the above are of type snapws.FatalError,
// indicating an I/O failure or protocol error. These errors will automatically
// close the connection.
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

// SendJSON sends the given value as a JSON-encoded WebSocket text message.
//
// The value must not be nil. If marshaling fails, the method returns the original
// marshaling error. The message will be split into fragments if necessary.
//
// All errors other than marshaling are of type snapws.FatalError, meaning the connection
// has been closed due to a protocol or I/O failure.
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

func (conn *Conn[Key]) Ping(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if conn.isClosed.Load() {
		return Fatal(ErrConnClosed)
	}

	frame, err := internal.NewFrame(true, internal.OpcodePing, false, []byte("test"))
	if err != nil {
		return err
	}

	errCh := make(chan error)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case conn.control <- &SendFrameRequest{
		frame: &frame,
		errCh: errCh,
		ctx:   ctx,
	}:
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err = <-errCh:
		return err
	}
}

func (conn *Conn[KeyType]) Pong(payload []byte) {
	if conn.isClosed.Load() {
		return
	}

	frame, err := internal.NewFrame(true, internal.OpcodePong, false, payload)
	if err != nil {
		conn.closeWithCode(internal.CloseInternalServerErr, "faild to create pong frame")
		return
	}

	ctx, cancel := context.WithTimeout(context.TODO(), conn.Manager.WriteWait)
	defer cancel()
	errCh := make(chan error)

	select {
	case <-ctx.Done():
		conn.closeWithCode(internal.ClosePolicyViolation, "pong enqueue timeout")
		return
	case conn.control <- &SendFrameRequest{frame: &frame, errCh: errCh, ctx: ctx}:
	}

	select {
	case <-ctx.Done():
		conn.closeWithCode(internal.ClosePolicyViolation, "pong enqueue timeout")
		return
	case err = <-errCh:
		if err != nil {
			conn.closeWithCode(internal.ClosePolicyViolation, "pong failed: "+err.Error())
			return
		}
	}
}
