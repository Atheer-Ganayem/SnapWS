package snapws

import (
	"context"
	"encoding/json"
	"time"
	"unicode/utf8"
)

type ConnWriter[KeyType comparable] struct {
	conn       *Conn[KeyType]
	buf        []byte
	used       int
	opcode     uint8
	closed     bool
	flushCount int
	lock       chan struct{}
	ctx        context.Context
}

// newWriter is a constructor for conn.writer, only to be used once.
// When request the next writer, reset(opcode uint8) should be the one to be called.
func (conn *Conn[KeyType]) newWriter(opcode uint8) *ConnWriter[KeyType] {
	return &ConnWriter[KeyType]{
		conn:   conn,
		buf:    make([]byte, conn.Manager.WriteBufferSize),
		opcode: opcode,
		lock:   make(chan struct{}, 1),
		closed: true,
	}
}

// resets the writer to prepare it for the next write.
func (w *ConnWriter[KeyType]) reset(ctx context.Context, opcode uint8) {
	clear(w.buf[:w.used])
	w.used = 0
	w.opcode = opcode
	w.closed = false
	w.flushCount = 0
	w.ctx = ctx
}

// NextWriter locks the write stream and returns a new writer for the given message type.
// Must call Close() on the returned writer to release the lock.
func (conn *Conn[KeyType]) NextWriter(ctx context.Context, msgType uint8) (*ConnWriter[KeyType], error) {
	if conn.isClosed.Load() {
		return nil, Fatal(ErrConnClosed)
	}
	if msgType != OpcodeText && msgType != OpcodeBinary {
		return nil, ErrInvalidOPCODE
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	if !conn.writer.closed {
		return nil, ErrWriterNotClosed
	}

	err := conn.lockW(ctx)
	if err != nil {
		return nil, err
	}

	conn.writer.reset(ctx, msgType)

	return conn.writer, nil
}

// Write appends bytes to the writer buffer and flushes if full.
// Automatically handles splitting into multiple frames.
func (w *ConnWriter[KeyType]) Write(p []byte) (n int, err error) {
	if w.closed {
		return 0, ErrWriterClosed
	}
	for n < len(p) {
		if w.used == len(w.buf) {
			if err := w.Flush(false); err != nil {
				return n, err
			}
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

// Flush sends the current buffer as a WebSocket frame.
// If FIN is true, marks this as the final frame in the message.
func (w *ConnWriter[KeyType]) Flush(FIN bool) error {
	if w.closed {
		return ErrWriterClosed
	}
	if w.conn.isClosed.Load() {
		return Fatal(ErrChannelClosed)
	}

	opcode := w.opcode
	if w.flushCount > 0 {
		opcode = OpcodeContinuation
	}

	frame, err := NewFrame(FIN, opcode, false, w.buf[:w.used])
	if err != nil {
		return err
	}

	if w.ctx == nil {
		w.ctx = context.TODO()
	}
	errCh := make(chan error)
	req := &SendFrameRequest{
		frame: &frame,
		errCh: errCh,
		ctx:   w.ctx,
	}

	w.flushCount++
	w.used = 0
	select {
	case w.conn.outboundFrames <- req:
	case <-w.ctx.Done():
		return ErrWriteChanFull
	}

	select {
	case err, ok := <-errCh:
		if !ok {
			return Fatal(ErrChannelClosed)
		}
		return err
	case <-w.ctx.Done():
		return ErrWriteChanFull
	}
}

// Close flushes the final frame and releases the writer lock.
func (w *ConnWriter[KeyType]) Close() error {
	defer w.conn.unlockW()
	defer func() { w.closed = true }()
	err := w.Flush(true)
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

// sendFrame writes a prepared frame to the underlying connection.
// Used internally to send frames from the outbound queues.
func (conn *Conn[KeyType]) sendFrame(frame *Frame) error {
	err := conn.raw.SetWriteDeadline(time.Now().Add(conn.Manager.WriteWait))
	if err != nil {
		return Fatal(err)
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
// The returned error must be checked. If it's of type snapws.FatalError,
// that indicates the connection was closed due to an I/O or protocol error.
// Any other error means the connection is still open, and you may retry or continue using it.
func (conn *Conn[KeyType]) SendBytes(ctx context.Context, b []byte) error {
	if len(b) == 0 {
		return ErrEmptyPayload
	}

	w, err := conn.NextWriter(ctx, OpcodeBinary)
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
// The returned error must be checked. If it's of type snapws.FatalError,
// that indicates the connection was closed due to an I/O or protocol error.
// Any other error means the connection is still open, and you may retry or continue using it.
func (conn *Conn[KeyType]) SendString(ctx context.Context, data []byte) error {
	if len(data) == 0 {
		return ErrEmptyPayload
	}

	if ok := utf8.Valid(data); !ok {
		return ErrInvalidUTF8
	}

	w, err := conn.NextWriter(ctx, OpcodeText)
	if err != nil {
		return err
	}

	_, err = w.Write(data)
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
// The returned error must be checked. If it's of type snapws.FatalError,
// that indicates the connection was closed due to an I/O or protocol error.
// Any other error means the connection is still open, and you may retry or continue using it.
func (conn *Conn[KeyType]) SendJSON(ctx context.Context, v any) error {
	if v == nil {
		return ErrEmptyPayload
	}

	w, err := conn.NextWriter(ctx, OpcodeText)
	if err != nil {
		return err
	}

	err = json.NewEncoder(w).Encode(v)
	if err != nil {
		return err
	}

	return w.Close()
}

// Ping sends a WebSocket ping frame and waits for it to be sent.
// Ping\Pong frames are already handeled by the library, you dont need
// to habdle them manually.
func (conn *Conn[Key]) Ping(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if conn.isClosed.Load() {
		return Fatal(ErrConnClosed)
	}

	frame, err := NewFrame(true, OpcodePing, false, nil)
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

// Pong sends a pong control frame in response to a ping.
// Automatically closes the connection on failure.
// Ping\Pong frames are already handeled by the library, you dont need
// to habdle them manually.
func (conn *Conn[KeyType]) Pong(payload []byte) {
	if conn.isClosed.Load() {
		return
	}

	frame, err := NewFrame(true, OpcodePong, false, payload)
	if err != nil {
		conn.closeWithCode(CloseInternalServerErr, "faild to create pong frame")
		return
	}

	ctx, cancel := context.WithTimeout(context.TODO(), conn.Manager.WriteWait)
	defer cancel()
	errCh := make(chan error)

	select {
	case <-ctx.Done():
		conn.closeWithCode(ClosePolicyViolation, "pong enqueue timeout")
		return
	case conn.outboundControl <- &SendFrameRequest{frame: &frame, errCh: errCh, ctx: ctx}:
	}

	select {
	case <-ctx.Done():
		conn.closeWithCode(ClosePolicyViolation, "pong enqueue timeout")
		return
	case err = <-errCh:
		if err != nil {
			conn.closeWithCode(ClosePolicyViolation, "pong failed: "+err.Error())
			return
		}
	}
}
