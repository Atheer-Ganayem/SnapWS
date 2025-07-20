package snapws

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"
)

// ConnReader provides an io.Reader interface over a websocket message (frame group).
// It supports reading fragmented frames as a single continuous stream.
type ConnReader struct {
	message *Message // Group of frames making up the complete message
	eof     bool     // Indicates if all frames have been fully read
}

func (conn *Conn[KeyType]) readLoop() {
	for {
		frame, code, err := conn.acceptFrame()
		if code == CloseNormalClosure && frame != nil {
			conn.closeWithPayload(frame.Payload)
			return
		} else if err != nil {
			conn.closeWithCode(code, err.Error())
			return
		}

		select {
		case conn.inboundFrames <- frame:
		default:
			conn.closeWithCode(ClosePolicyViolation, "inboundFrames full — slow consumer")
			return
		}
	}
}

// acceptFrame reads and parses a single WebSocket frame.
// Returns a websocket frame, uint16 representing close reason (if error), and an error.
// Use higher-level methods like AcceptString, AcceptJSON, or AcceptBytes for convenience.
func (conn *Conn[KeyType]) acceptFrame() (*Frame, uint16, error) {
	buf := conn.readFrameBuf
	offset := 0
	var data []byte

	for {
		n, err := conn.raw.Read(buf[offset:])
		if err != nil && err != io.EOF {
			return nil, CloseProtocolError, err
		}
		offset += n
		if err == io.EOF {
			break
		}

		length, err := readLength(buf[:offset])
		if length == offset {
			break
		}
		if IsFatalErr(err) {
			return nil, CloseProtocolError, err
		} else if err == nil {
			if offset > length {
				return nil, CloseProtocolError, ErrInvalidPayloadLength
			}
			if length > len(buf) {
				data = make([]byte, length)
				copy(data, buf[:offset])
				n, err = io.ReadFull(conn.raw, data[offset:])
			} else {
				n, err = io.ReadFull(conn.raw, buf[offset:])
			}
			offset += n
			if err != nil {
				return nil, CloseProtocolError, err
			}
			break
		}
	}
	var frame *Frame
	var err error
	if data == nil {
		frame, err = ReadFrame(buf[:offset])
	} else {
		frame, err = ReadFrame(data[:offset])
	}
	if err != nil {
		return nil, CloseProtocolError, err
	} else if !frame.IsMasked {
		return nil, CloseProtocolError, errExpectedMaskedFrame
	}

	if frame.IsControl() {
		if !frame.IsValidControl() {
			return nil, CloseProtocolError, ErrInvalidControlFrame
		}
		switch frame.OPCODE {
		case OpcodeClose:
			return frame, CloseNormalClosure, io.EOF

		case OpcodePing:
			conn.Pong(frame.Payload)
			return conn.acceptFrame()

		case OpcodePong:
			conn.raw.SetReadDeadline(time.Now().Add(conn.Manager.ReadWait))
			return conn.acceptFrame()
		}
	}

	return frame, 0, nil
}

// This is the method used for accepting a full websocket message (text or binary).
// This method will automatically close the connection with the appropriate close code on protocol errors or close frames.
// Control frames will be handled automatically by the accetFrame method, they wont be returned.
// the length of returned frames group is always >= 1.
func (conn *Conn[KeyType]) acceptMessage() {
	for {
		if err := conn.raw.SetReadDeadline(time.Now().Add(conn.Manager.ReadWait)); err != nil {
			conn.closeWithCode(ClosePolicyViolation, "failed to set a deadline")
			return
		}

		frame, ok := <-conn.inboundFrames
		if !ok {
			return
		}
		if frame.OPCODE != OpcodeText && frame.OPCODE != OpcodeBinary {
			conn.closeWithCode(CloseProtocolError, ErrInvalidOPCODE.Error())
			return
		}

		message := &Message{OPCODE: frame.OPCODE}
		if frame.FIN {
			message.Payload = bytes.NewBuffer(frame.Payload)
			conn.inboundMessages <- message
			continue
		}
		message.Payload = bytes.NewBuffer(make([]byte, 0, conn.Manager.ReadBufferSize))
		_, err := message.Payload.Write(frame.Payload)
		if err != nil {
			conn.closeWithCode(CloseInternalServerErr, ErrBufferWriteFaild.Error())
			return
		}

		frameCount := 1
		for {
			frame, ok := <-conn.inboundFrames
			if !ok {
				return
			}
			if frame.OPCODE != OpcodeContinuation {
				conn.closeWithCode(CloseProtocolError, ErrInvalidOPCODE.Error())
				return
			}

			frameCount++
			if conn.Manager.MaxMessageSize != -1 && message.Payload.Len()+frame.PayloadLength > conn.Manager.MaxMessageSize {
				conn.closeWithCode(CloseMessageTooBig, ErrMessageTooLarge.Error())
				return
			} else if conn.Manager.ReaderMaxFragments > 0 && frameCount > conn.Manager.ReaderMaxFragments {
				conn.closeWithCode(CloseMessageTooBig, ErrTooMuchFragments.Error())
				return
			}
			_, err = message.Payload.Write(frame.Payload)
			if err != nil {
				return
			}

			if frame.FIN {
				break
			}
		}

		if message.IsText() && !message.IsValidUTF8() {
			conn.closeWithCode(CloseProtocolError, ErrInvalidUTF8.Error())
			return
		}

		conn.inboundMessages <- message
	}
}

// Read reads data from the current frame group (message) into the provided byte slice `p`.
// It reads sequentially across multiple frames if needed, until `p` is full or EOF is reached.
// Returns the number of bytes read and any error encountered.
// When all frames are fully consumed, it returns io.EOF.
func (r *ConnReader) Read(p []byte) (n int, err error) {
	if r.eof {
		return 0, io.EOF
	}
	if p == nil {
		return 0, ErrNilBuf
	}
	if len(p) == 0 {
		return 0, nil
	}

	for n < len(p) {
		rn, err := r.message.Payload.Read(p[n:])
		n += rn
		if err == io.EOF {
			r.eof = true
			if n == 0 {
				return 0, io.EOF
			}
			return n, nil
		} else if err != nil {
			return n, err
		}
	}

	return n, nil
}

func (r *ConnReader) Payload() []byte {
	return r.message.Payload.Bytes()
}

// NextReader returns an io.Reader for the next complete WebSocket message.
// It blocks until a full message is available or the context is canceled.
// The returned reader allows streaming the message payload frame-by-frame,
// and the second return value indicates the message type (e.g., Text or Binary).
// If the connection is closed or the context expires, it returns a non-nil error.
func (conn *Conn[KeyType]) NextReader(ctx context.Context) (*ConnReader, int8, error) {
	if ctx == nil {
		ctx = context.TODO()
	}

	select {
	case <-ctx.Done():
		return nil, -1, ctx.Err()
	case message, ok := <-conn.inboundMessages:
		if !ok {
			return nil, -1, Fatal(ErrConnClosed)
		}
		// there is alway at least 1 element in inbound messages
		return &ConnReader{message: message}, int8(message.OPCODE), nil
	}
}

// ReadMessage reads the next complete WebSocket message into memory.
// It returns the message type (e.g., Text or Binary), the full payload, and any error encountered.
// The context controls cancellation or timeout. If the connection is closed or the context expires,
// it returns an appropriate error.
func (conn *Conn[KeyType]) ReadMessage(ctx context.Context) (msgType int8, data []byte, err error) {
	if conn.isClosed.Load() {
		return -1, nil, Fatal(ErrConnClosed)
	}
	if ctx == nil {
		ctx = context.TODO()
	}

	reader, msgType, err := conn.NextReader(ctx)
	if err != nil {
		return -1, nil, err
	}

	// payload, err := io.ReadAll(reader)
	// if err != nil {
	// 	return -1, nil, err
	// }

	return msgType, reader.Payload(), nil
}

// ReadBinary returns the binary payload from a WebSocket binary message.
//
// If the received message is not of type binary, it returns snapws.ErrMessageTypeMismatch
// without closing the connection.
// The returned error must be checked. If it's of type snapws.FatalError,
// that indicates the connection was closed due to an I/O or protocol error.
// Any other error means the connection is still open, and you may retry or continue using it.
func (conn *Conn[KeyType]) ReadBinary(ctx context.Context) (data []byte, err error) {
	msgType, payload, err := conn.ReadMessage(ctx)
	if err != nil {
		return nil, err
	} else if msgType != OpcodeBinary {
		return nil, ErrMessageTypeMismatch
	}

	return payload, nil
}

// ReadString returns the message payload as a UTF-8 byte slice from a text WebSocket message.
//
// If the received message is not of type text, it returns snapws.ErrMessageTypeMismatch
// without closing the connection.
// The returned error must be checked. If it's of type snapws.FatalError,
// that indicates the connection was closed due to an I/O or protocol error.
// Any other error means the connection is still open, and you may retry or continue using it.
func (conn *Conn[KeyType]) ReadString(ctx context.Context) ([]byte, error) {
	msgType, payload, err := conn.ReadMessage(ctx)
	if err != nil {
		return nil, err // Connection already close by acceptMessage()
	} else if msgType != OpcodeText {
		return nil, ErrMessageTypeMismatch
	}

	return payload, nil
}

// ReadJSON reads a text WebSocket message and unmarshals its payload into the given value.

// This method expects the message to be of type text and contain valid UTF-8 encoded JSON.
// If the message is not of type text, it returns snapws.ErrMessageTypeMismatch without
// closing the connection. if the text is not a valid json, an error will be returned without
// closing the connection.
// The returned error must be checked. If it's of type snapws.FatalError,
// that indicates the connection was closed due to an I/O or protocol error.
// Any other error means the connection is still open, and you may retry or continue using it.
func (conn *Conn[KeyType]) ReadJSON(ctx context.Context, v any) error {
	reader, msgType, err := conn.NextReader(ctx)
	if err != nil {
		return err
	}
	if msgType != OpcodeText {
		return ErrMessageTypeMismatch
	}

	return json.NewDecoder(reader).Decode(v)
}
