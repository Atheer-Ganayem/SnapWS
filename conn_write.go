package snapws

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"github.com/Atheer-Ganayem/SnapWS/internal"
)

type ConnWriter[KeyType comparable] struct {
	conn       *Conn[KeyType]
	buf        []byte
	used       int
	opcode     uint8
	closed     bool
	flushCount int
}

func (conn *Conn[KeyType]) NewWriter(opcode uint8) *ConnWriter[KeyType] {
	return &ConnWriter[KeyType]{
		conn:   conn,
		buf:    make([]byte, conn.Manager.WriteBufferSize),
		opcode: opcode,
	}
}

func (conn *Conn[KeyType]) NextWriter(ctx context.Context, msgType uint8) (io.WriteCloser, error) {
	if conn.isClosed.Load() {
		return nil, Fatal(ErrConnClosed)
	}
	if msgType != internal.OpcodeText && msgType != internal.OpcodeBinary {
		return nil, ErrInvalidOPCODE
	}
	if ctx == nil {
		ctx = context.TODO()
	}

	err := conn.lockW(ctx)
	if err != nil {
		return nil, err
	}

	return conn.NewWriter(msgType), nil
}

func (w *ConnWriter[KeyType]) Write(p []byte) (n int, err error) {
	if w.closed {
		return 0, ErrWriterClosed
	}
	for n < len(p) {
		if w.used == len(w.buf) {
			if err := w.Flush(false); err != nil {
				return n, err
			}
			w.used = 0
		}

		space := len(w.buf) - w.used
		remain := len(p) - n
		toCopy := space
		if remain < space {
			toCopy = remain
		}

		copy(w.buf[w.used:], p[n:n+toCopy])
		w.used += toCopy
		n += toCopy
	}
	return n, nil
}

func (w *ConnWriter[KeyType]) Flush(FIN bool) error {
	if w.closed {
		return ErrWriterClosed
	}
	if w.conn.isClosed.Load() {
		return Fatal(ErrChannelClosed)
	}

	opcdoe := w.opcode
	if w.flushCount > 0 {
		opcdoe = internal.OpcodeContinuation
	}

	frame, err := internal.NewFrame(FIN, opcdoe, false, w.buf[:w.used])
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), w.conn.Manager.WriterTimeout)
	defer cancel()
	errCh := make(chan error)
	req := &SendFrameRequest{
		frame: &frame,
		errCh: errCh,
		ctx:   ctx,
	}

	w.flushCount++
	select {
	case w.conn.outboundFrames <- req:
	case <-ctx.Done():
		return ErrWriteChanFull
	}

	select {
	case err, ok := <-errCh:
		if !ok {
			return Fatal(ErrChannelClosed)
		}
		fmt.Println(err)
		return err
	case <-ctx.Done():
		return ErrWriteChanFull
	}
}

func (w *ConnWriter[KeyType]) Close() error {
	defer w.conn.unlockW(context.TODO())
	err := w.Flush(true)
	w.closed = true
	return err
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

	err := conn.writeFrame(req.frame)
	trySendErr(req.errCh, err)
}

// Low level writing, not safe to use concurrently.
// Use SendString, SendJSON, SendBytes for safe writing.
func (conn *Conn[KeyType]) writeFrame(frame *internal.Frame) (err error) {
	err = conn.raw.SetWriteDeadline(time.Now().Add(conn.Manager.WriteWait))
	if err != nil {
		return Fatal(err)
	}

	if frame.OPCODE != internal.OpcodePing {
		fmt.Printf("Writing frame %v: %s\n", frame.OPCODE, frame.Payload)
	}
	_, err = conn.raw.Write(frame.Bytes())
	if err != nil {
		return Fatal(err)
	}

	return nil
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

	w, err := conn.NextWriter(ctx, internal.OpcodeBinary)
	if err != nil {
		return err
	}

	_, err = w.Write(b)
	if err != nil {
		return err
	}

	return w.Close()
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

	w, err := conn.NextWriter(ctx, internal.OpcodeText)
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, str)
	if err != nil {
		return err
	}

	return w.Close()
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

	w, err := conn.NextWriter(ctx, internal.OpcodeText)
	if err != nil {
		return err
	}

	err = json.NewEncoder(w).Encode(v)
	if err != nil {
		return err
	}

	return w.Close()
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
	case conn.outboundControl <- &SendFrameRequest{
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
	case conn.outboundControl <- &SendFrameRequest{frame: &frame, errCh: errCh, ctx: ctx}:
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
