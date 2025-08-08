package snapws

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"time"
	"unicode/utf8"
)

// ConnWriter is NOT safe for concurrent use.
// Only one goroutine may call Write/Flush/Close at a time.
// Use Conn.NextWriter to safely obtain exclusive access to a writer.
type ConnWriter struct {
	conn *Conn
	pb   *PooledBuf
	buf  []byte
	// The start of the frame in the buf. its used to save space for the frame header.
	start int
	// The end of the frame in the buf.
	used int
	// The message opcode. First frame will be of this opcode, proceeding one would be of OpcodeContinuation.
	opcode     uint8
	closed     bool
	flushCount int
	lock       *mu
	ctx        context.Context
}

// newWriter is a constructor for conn.writer, only to be used once.
// When request the next writer, reset(opcode uint8) should be the one to be called.
func (conn *Conn) newWriter(opcode uint8, b []byte) *ConnWriter {
	w := &ConnWriter{
		conn:   conn,
		opcode: opcode,
		lock:   newMu(conn),
		closed: true,
	}

	if cap(b) == conn.upgrader.WriteBufferSize {
		w.buf = b[:cap(b)]
	} else if conn.upgrader.DisableWriteBuffersPooling {
		w.buf = make([]byte, conn.upgrader.WriteBufferSize)
	} else {
		w.pb = conn.upgrader.writePool.Get().(*PooledBuf)
		w.buf = w.pb.buf
	}

	return w
}

// resets the writer to prepare it for the next write.
func (w *ConnWriter) reset(ctx context.Context, opcode uint8) {
	w.start = MaxHeaderSize
	w.used = MaxHeaderSize
	w.opcode = opcode
	w.closed = false
	w.flushCount = 0
	w.ctx = ctx
}

// NextWriter locks the data write stream and returns a new writer for the given message type.
// Must call Close() on the returned writer to release the lock.
// The context given, is to be used for all the writer's functions as long is its not closed,
// This mean its used when its trying to optain the next writer and flushing.
// After you close the writer and call NextWriter again, you must give it a new context.
func (conn *Conn) NextWriter(ctx context.Context, msgType uint8) (*ConnWriter, error) {
	if conn.writer == nil {
		return nil, ErrWriterUnintialized
	}
	if !isData(msgType) {
		return nil, ErrInvalidOPCODE
	}
	if !conn.writer.closed {
		return nil, ErrWriterNotClosed
	}

	err := conn.writer.lock.lockCtx(ctx)
	if err != nil {
		return nil, err
	}

	conn.writer.reset(ctx, msgType)

	return conn.writer, nil
}

// Write appends bytes to the writer buffer and flushes if full.
// Automatically handles splitting into multiple frames.
func (w *ConnWriter) Write(p []byte) (n int, err error) {
	if w == nil {
		return 0, ErrWriterUnintialized
	}
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
		toCopy := min(space, remain)

		copy(w.buf[w.used:], p[n:n+toCopy])
		w.used += toCopy
		n += toCopy
	}
	return n, nil
}

// Flush sends the current buffer as a WebSocket frame.
// If FIN is true, this frame is marked as the final one in the message.
//
// Return values:
//   - If it returns a **fatal error**, the connection is closed and cannot be reused.
//   - If it returns a **non-fatal, non-nil error**, the connection is still alive,
//     and you may attempt to flush again.
//     note: If err == context.Canceled or DeadlineExceeded, connection is alive but ctx is done.
//     No point in retrying.
//   - If it returns **nil**, the flush was successful, and the buffer's "start" and "used"
//     positions have been reset.
func (w *ConnWriter) Flush(FIN bool) error {
	if w == nil {
		return ErrWriterUnintialized
	}
	if w.closed {
		return ErrWriterClosed
	}

	opcode := w.opcode
	if w.flushCount > 0 {
		opcode = OpcodeContinuation
	}

	err := w.writeHeaders(FIN, opcode)
	if err != nil {
		return err
	}

	err = w.conn.writeLock.lockCtx(w.ctx)
	if err != nil {
		return err
	}
	defer w.conn.writeLock.unLock()

	if err = w.conn.raw.SetWriteDeadline(time.Now().Add(w.conn.upgrader.WriteWait)); err != nil {
		w.conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
		return fatal(err)
	}

	err = w.conn.sendFrame(w.buf[w.start:w.used])
	if err != nil {
		w.conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
		return err
	}

	w.flushCount++
	w.used = MaxHeaderSize
	w.start = MaxHeaderSize

	return nil
}

// Close flushes the final frame and releases the writer lock.
func (w *ConnWriter) Close() error {
	if !w.closed {
		defer w.lock.tryUnlock()
		defer func() { w.closed = true }()
		err := w.Flush(true)
		return err
	}
	return nil
}

// sendFrame writes a prepared frame to the underlying connection.
// This function is for internal library use.
func (conn *Conn) sendFrame(buf []byte) error {
	_, err := conn.raw.Write(buf)

	return fatal(err)
}

// Receives a context, opcode (text or binary), and a slice of bytes.
//   - Tries to optain the next writer
//   - sends a message of opcode (the given opcode) with the data of the given byte slice.
//
// It returns an error. Errors must be checked if fatal.
func (conn *Conn) SendMessage(ctx context.Context, opcode uint8, b []byte) error {
	if !isData(opcode) {
		return ErrInvalidOPCODE
	}

	if opcode == OpcodeText && !conn.upgrader.SkipUTF8Validation {
		if ok := utf8.Valid(b); !ok {
			return ErrInvalidUTF8
		}
	}

	w, err := conn.NextWriter(ctx, opcode)
	if err != nil {
		return err
	}

	_, err = w.Write(b)
	if err != nil {
		return err
	}

	return w.Close()
}

// SendBytes sends the given byte slice as a WebSocket binary message.
//
// This is a shorthand for SendMessage with OpcodeBinary.
// The returned error must be checked. If it's a snapws.FatalError,
// the connection was closed due to an I/O or protocol error.
// Other errors indicate the connection is still alive and can be reused.
func (conn *Conn) SendBytes(ctx context.Context, p []byte) error {
	return conn.SendMessage(ctx, OpcodeBinary, p)
}

// SendString sends the given byte slice as a WebSocket binary message.
//
// The byte slice must be valid UTF-8. If it is not, the method returns snapws.ErrInvalidUTF8.
//
// This is a shorthand for SendMessage with OpcodeText.
// The returned error must be checked. If it's a snapws.FatalError,
// the connection was closed due to an I/O or protocol error.
// Other errors indicate the connection is still alive and can be reused.
func (conn *Conn) SendString(ctx context.Context, p []byte) error {
	return conn.SendMessage(ctx, OpcodeText, p)
}

// SendJSON sends the given value as a JSON-encoded WebSocket text message.
//
// If marshaling fails, the method returns the original marshaling error.
// The message will be split into fragments if necessary.
//
// The returned error must be checked. If it's of type snapws.FatalError,
// that indicates the connection was closed due to an I/O or protocol error.
// Any other error means the connection is still open, and you may retry or continue using it.
func (conn *Conn) SendJSON(ctx context.Context, v any) error {
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
func (conn *Conn) Ping() error {
	return conn.controlWriter.writeControl(OpcodePing, 0, false)
}

// pong sends a pong control frame in response to a ping.
// Ping\pong frames are already handeled by the library, you dont need
// to habdle them manually.
func (conn *Conn) pong(n int, isMasked bool) error {
	err := conn.controlWriter.writeControl(OpcodePong, n, isMasked)
	if IsFatalErr(err) {
		conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
	}
	return err
}

// Writes control frame.
func (cw *ControlWriter) writeControl(opcode byte, n int, isMasked bool) error {
	if cw.t == nil {
		cw.t = time.NewTimer(cw.conn.upgrader.WriteWait)
	} else {
		cw.t.Reset(cw.conn.upgrader.WriteWait)
	}
	defer cw.t.Stop()

	err := cw.lock.lockTimer(cw.t)
	if err != nil {
		return err
	}
	defer cw.lock.unLock()

	// set deadline
	if err = cw.conn.raw.SetWriteDeadline(time.Now().Add(cw.conn.upgrader.WriteWait)); err != nil {
		return fatal(err)
	}

	// write header and payload
	cw.buf = cw.buf[:2]
	cw.buf[0] = 0x80 + opcode

	if isMasked {
		b, err := cw.conn.nRead(4)
		if err != nil {
			return fatal(ErrInternalServer)
		}
		copy(cw.maskKey[:], b)
	}
	if n > 0 && n <= MaxControlFramePayload {
		payload, err := cw.conn.nRead(n)
		if err != nil {
			return fatal(ErrConnClosed)
		}
		cw.buf[1] = byte(n)

		cw.unMask(payload)
		cw.buf = append(cw.buf, payload...)
	}

	if err := cw.conn.writeLock.lockTimer(cw.t); err != nil {
		return err
	}
	defer cw.conn.writeLock.unLock()

	return cw.conn.sendFrame(cw.buf)
}

// Writes close frame to the ControlWriter.
// Receive a uint16 closeCode and a string reason and return an error.
// It tries to write the control frame within the the duration given in: conn.upgrader.WriteWait.
func (cw *ControlWriter) writeClose(closeCode uint16, reason string) {
	if !isValidCloseCode(closeCode) {
		closeCode = CloseGoingAway
	}

	// lock without unlocking because this is the last write.
	cw.lock.forceLock()

	// set deadline
	err := cw.conn.raw.SetWriteDeadline(time.Now().Add(cw.conn.upgrader.WriteWait))
	if err != nil {
		return
	}

	cw.buf = cw.buf[:4]
	cw.buf[0] = 0x80 + OpcodeClose
	cw.buf[1] = 2
	binary.BigEndian.PutUint16(cw.buf[2:4], closeCode)
	if len(reason) > 0 && len(reason) <= MaxControlFramePayload-2 {
		cw.buf[1] += byte(len(reason))
		cw.buf = append(cw.buf, reason...)
	}

	// lock without unlocking because this is the last write.
	cw.conn.writeLock.forceLock()
	cw.conn.sendFrame(cw.buf)
	cw.conn.raw.Close()
}
