package snapws

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"time"
	"unicode/utf8"
)

// ConnReader provides an io.Reader for a single WebSocket message.
//
// Concurrency:
//   - Only one goroutine may read from a Conn (and thus its ConnReader)
//     at a time. Concurrent reads on the same connection or the same
//     ConnReader are not supported.
type ConnReader struct {
	conn *Conn
	// message info
	totalSize int
	fragments int
	// current frame info
	fin       bool
	isMasked  bool
	maskKey   [4]byte
	maskPos   int
	remaining int
}

// acceptFrame reads and parses a single WebSocket frame.
// If the frame is a data frame it returns its opcode and an error. if the frame is a control frame,
// it handles it and keeps reading till it reads a data frame.
// Note: it resets the conn.reader (only the current frame info) if it reads a valid control frame,
// because of that, you should only call it when you are ready to reset it.
func (conn *Conn) acceptFrame() (uint8, error) {
	for {
		b, err := conn.nRead(2)
		if err != nil {
			conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
			return nilOpcode, fatal(err)
		}

		// reading header values
		fin := b[0]&0b10000000 == 128
		rsv1 := b[0] & 0b01000000
		rsv2 := b[0] & 0b00100000
		rsv3 := b[0] & 0b00010000
		opcode := b[0] & 0b00001111
		isMasked := b[1]&0b10000000 == 128
		lengthB := b[1] & 0b01111111

		if !isVaidOpcode(opcode) {
			conn.CloseWithCode(CloseProtocolError, ErrInvalidOPCODE.Error())
			return nilOpcode, ErrInvalidOPCODE
		}
		if (rsv1 | rsv2 | rsv3) != 0 {
			conn.CloseWithCode(CloseProtocolError, ErrUnnegotiatedRsvBits.Error())
			return nilOpcode, ErrUnnegotiatedRsvBits
		}
		if conn.isServer && !isMasked {
			conn.CloseWithCode(CloseProtocolError, errExpectedMaskedFrame.Error())
			return nilOpcode, errExpectedMaskedFrame
		}

		// reading length
		n := 0
		switch lengthB {
		case 127:
			b, err := conn.nRead(8)
			if err != nil {
				conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
				return nilOpcode, err
			}
			n64 := binary.BigEndian.Uint64(b)
			if n64 > math.MaxInt {
				conn.CloseWithCode(CloseMessageTooBig, ErrTooLargePayload.Error())
				return nilOpcode, ErrTooLargePayload
			}
			n = int(n64)
		case 126:
			b, err := conn.nRead(2)
			if err != nil {
				conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
				return nilOpcode, err
			}
			n = int(binary.BigEndian.Uint16(b))
		default:
			n = int(lengthB)
		}

		if n > conn.upgrader.MaxMessageSize {
			conn.CloseWithCode(CloseMessageTooBig, ErrTooLargePayload.Error())
			return nilOpcode, ErrTooLargePayload
		}

		// handling control frames
		if isControl(opcode) {
			if !fin || n > MaxControlFramePayload {
				conn.CloseWithCode(CloseProtocolError, ErrInvalidControlFrame.Error())
				return nilOpcode, ErrInvalidControlFrame
			}
			switch opcode {
			case OpcodeClose:
				conn.handleClose(n, isMasked)
				return nilOpcode, fatal(ErrConnClosed)
			case OpcodePing:
				if err := conn.pong(n, isMasked); err != nil {
					return 0, err
				}
			case OpcodePong:
				if err = conn.handlePong(n, isMasked); err != nil {
					return 0, err
				}
			}
			continue
		}

		// reseting conn.reader current frame info.
		conn.reader.fin = fin
		conn.reader.remaining = n
		conn.reader.maskPos = 0
		conn.reader.isMasked = isMasked
		conn.reader.fragments++
		if isMasked {
			b, err := conn.nRead(4)
			if err != nil {
				conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
				return nilOpcode, err
			}
			copy(conn.reader.maskKey[:], b)
		}

		return opcode, nil
	}
}

// NextReader returns an io.Reader for the next WebSocket message.
// It blocks until a data frame is available.
// The returned reader allows streaming the message payload frame-by-frame,
// and the second return value indicates the message type (e.g., Text or Binary).
// All errors return are of type snap.FatalError indicating that the error is fatal and the connection got closed.
func (conn *Conn) NextReader() (io.Reader, uint8, error) {
	select {
	case <-conn.done:
		return nil, 0, fatal(ErrConnClosed)
	default:
	}

	err := conn.raw.SetReadDeadline(time.Now().Add(conn.upgrader.ReadWait))
	if err != nil {
		conn.CloseWithCode(CloseInternalServerErr, "timeout")
		return nil, 0, fatal(err)
	}

	opcode, err := conn.acceptFrame()
	if err != nil {
		return nil, 0, fatal(err)
	}

	if !isData(opcode) {
		conn.CloseWithCode(CloseProtocolError, ErrInvalidOPCODE.Error())
		return nil, 0, fatal(ErrInvalidOPCODE)
	}

	conn.reader.fragments = 0
	conn.reader.totalSize = conn.reader.remaining

	ok, err := conn.allow()
	if err != nil {
		return nil, 0, fatal(err)
	} else if !ok {
		if err := conn.skipRestOfMessage(); err != nil {
			return nil, 0, fatal(err)
		}
		return nil, 0, errors.New("rate limited")
	}

	return &conn.reader, opcode, nil
}

// Read implements the io.Reader interface for ConnReader.
// It reads WebSocket message payload data into p, handling continuation frames,
// unmasking (if required), and connection closure. This function allows the
// application to stream data frame-by-frame.
//
// Behavior:
//   - Returns io.EOF when the final frame of a message has been fully read.
//   - If additional frames are part of the same message (continuation frames),
//     it will transparently fetch and continue reading them.
//   - If a fatal error occurs (protocol violation, closed connection, etc.),
//     the connection will be closed and a fatal error will be returned.
func (r *ConnReader) Read(p []byte) (n int, err error) {
	// if we reached the end if the frame and a Continuation frame is expected.
	if r.remaining == 0 && !r.fin {
		// checking if we reached the ReaderMaxFragments before reading the next frame.
		if r.conn.upgrader.ReaderMaxFragments > 0 && r.fragments >= r.conn.upgrader.ReaderMaxFragments {
			r.conn.CloseWithCode(ClosePolicyViolation, ErrTooMuchFragments.Error())
			return 0, fatal(ErrTooMuchFragments)
		}

		opcode, err := r.conn.acceptFrame()
		if err != nil {
			return 0, fatal(err)
		}
		if opcode != OpcodeContinuation {
			r.conn.CloseWithCode(CloseProtocolError, ErrExpectedContinuation.Error())
			return 0, fatal(ErrExpectedContinuation)
		}
		r.totalSize += r.remaining
		if r.totalSize > r.conn.upgrader.MaxMessageSize {
			r.conn.CloseWithCode(CloseMessageTooBig, ErrMessageTooLarge.Error())
			return 0, fatal(ErrMessageTooLarge)
		}
	}

	for {
		if r.remaining == 0 && r.fin {
			return n, io.EOF
		} else if r.remaining == 0 {
			return n, nil
		}
		if n == len(p) {
			return n, nil
		}

		toRead := min(len(p)-n, r.remaining)
		rn, err := r.conn.readBuf.Read(p[n : n+toRead])
		n += rn
		r.remaining -= rn
		if err == io.EOF {
			// connection got closed
			r.conn.CloseWithCode(CloseNormalClosure, "")
			return n, fatal(ErrConnClosed)
		}
		if err != nil {
			r.conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
			return n, fatal(err)
		}

		if r.isMasked {
			r.mask(p[n-rn : n])
		}
	}
}

// ReadMessage reads the next complete WebSocket message into memory.
// It returns the message type (e.g., Text or Binary), the full payload, and any error encountered.
func (conn *Conn) ReadMessage() (msgType uint8, data []byte, err error) {
	reader, msgType, err := conn.NextReader()
	if err != nil {
		return nilOpcode, nil, err
	}

	data, err = io.ReadAll(reader)
	if err != nil {
		return nilOpcode, nil, err
	}

	if msgType == OpcodeText && !conn.upgrader.SkipUTF8Validation {
		if ok := utf8.Valid(data); !ok {
			conn.CloseWithCode(CloseInvalidFramePayloadData, ErrInvalidUTF8.Error())
			return nilOpcode, nil, fatal(ErrInvalidUTF8)
		}
	}

	return msgType, data, nil
}

// ReadBinary returns the binary payload from a WebSocket binary message.
//
// If the received message is not of type binary, it returns snapws.ErrMessageTypeMismatch
// without closing the connection.
// The returned error must be checked. If it's of type snapws.FatalError,
// that indicates the connection was closed due to an I/O or protocol error.
// Any other error means the connection is still open, and you may retry or continue using it.
func (conn *Conn) ReadBinary() (data []byte, err error) {
	msgType, payload, err := conn.ReadMessage()
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
func (conn *Conn) ReadString() ([]byte, error) {
	msgType, payload, err := conn.ReadMessage()
	if err != nil {
		return nil, err // Connection already close by acceptMessage()
	} else if msgType != OpcodeText {
		return nil, ErrMessageTypeMismatch
	}

	return payload, nil
}

// ReadJSON reads a text WebSocket message and unmarshals its payload into the given value.
//
// This method expects the message to be of type text and contain valid UTF-8 encoded JSON.
// If the message is not of type text, it returns snapws.ErrMessageTypeMismatch without
// closing the connection. if the text is not a valid json, an error will be returned without
// closing the connection.
// The returned error must be checked. If it's of type snapws.FatalError,
// that indicates the connection was closed due to an I/O or protocol error.
// Any other error means the connection is still open, and you may retry or continue using it.
func (conn *Conn) ReadJSON(v any) error {
	reader, msgType, err := conn.NextReader()
	if err != nil {
		return err
	}
	if msgType != OpcodeText {
		return ErrMessageTypeMismatch
	}

	return json.NewDecoder(reader).Decode(v)
}
