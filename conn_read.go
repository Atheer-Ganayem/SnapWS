package snapws

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Atheer-Ganayem/SnapWS/internal"
)

type ConnReader struct {
	frames       internal.FrameGroup
	currentFrame int // index of the frame in frames
	offsest       int // offset of the payload in frame[currentFrame]
	eof          bool
}

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

	for ; r.currentFrame < len(r.frames); r.currentFrame++ {
		frame := r.frames[r.currentFrame]
		isLastFrame := r.currentFrame == len(r.frames)-1
		payload := frame.Payload

		for {
			if n >= len(p) {
				return n, nil
			}

			if r.offsest < len(payload) {
				p[n] = payload[r.offsest]
				n++
				r.offsest++
			} else if isLastFrame {
				break
			} else {
				r.offsest = 0
				break
			}
		}
	}

	r.eof = true
	if n > 0 {
		return n, nil
	}
	return 0, io.EOF
}

func (conn *Conn[KeyType]) NextReader(ctx context.Context) (io.Reader, error) {
	if ctx == nil {
		ctx = context.TODO()
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case frames, ok := <-conn.inboundMessages:
		if !ok {
			return nil, Fatal(ErrConnClosed)
		}
		return &ConnReader{frames: frames}, nil
	}
}

func (conn *Conn[KeyType]) readLoop() {
	for {
		frame, code, err := conn.acceptFrame()
		if code == internal.CloseNormalClosure && frame != nil {
			conn.closeWithPayload(frame.Payload)
			return
		} else if err != nil {
			conn.closeWithCode(code, err.Error())
			return
		}

		select {
		case conn.inboundFrames <- frame:
		default:
			conn.closeWithCode(internal.ClosePolicyViolation, "inboundFrames full — slow consumer")
			return
		}
	}
}

// acceptFrame reads and parses a single WebSocket frame.
// Returns a websocket frame, uint16 representing close reason (if error), and an error.
// Use higher-level methods like AcceptString, AcceptJSON, or AcceptBytes for convenience.
func (conn *Conn[KeyType]) acceptFrame() (*internal.Frame, uint16, error) {
	buf := make([]byte, conn.Manager.ReadBufferSize)
	var data bytes.Buffer

	for {
		n, err := conn.raw.Read(buf)
		if err != nil && err != io.EOF {
			return nil, internal.CloseProtocolError, err
		}
		if data.Len()+n > conn.Manager.MaxMessageSize {
			return nil, internal.CloseMessageTooBig, ErrMessageTooLarge
		}
		if n > 0 {
			_, err = data.Write(buf[:n])
			if err != nil {
				return nil, internal.CloseInternalServerErr, err
			}
		}

		ok, fErr := internal.IsCompleteFrame(data.Bytes())
		if fErr != nil {
			return nil, internal.CloseProtocolError, err
		}
		if ok {
			break
		}
		if !ok && err == io.EOF {
			return nil, internal.CloseProtocolError, fmt.Errorf("incomplete frame at EOF")
		}
	}

	frame, err := internal.ReadFrame(data.Bytes())
	if err != nil {
		return nil, internal.CloseProtocolError, err
	} else if !frame.IsMasked {
		return nil, internal.CloseProtocolError, errExpectedMaskedFrame
	}

	if frame.IsControl() {
		if !frame.IsValidControl() {
			return nil, internal.CloseProtocolError, ErrInvalidControlFrame
		}
		switch frame.OPCODE {
		case internal.OpcodeClose:
			return &frame, internal.CloseNormalClosure, io.EOF

		case internal.OpcodePing:
			conn.Pong(frame.Payload)
			return conn.acceptFrame()

		case internal.OpcodePong:
			conn.raw.SetReadDeadline(time.Now().Add(conn.Manager.ReadWait))
			return conn.acceptFrame()
		}
	}

	return &frame, 0, nil
}

// This is the method used for accepting a full websocket message (text or binary).
// This method will automatically close the connection with the appropriate close code on protocol errors or close frames.
// Control frames will be handled automatically by the accetFrame method, they wont be returned.
// the length of returned frames group is always >= 1.
func (conn *Conn[KeyType]) acceptMessage() {
	go conn.readLoop()
	for {
		if err := conn.raw.SetReadDeadline(time.Now().Add(conn.Manager.ReadWait)); err != nil {
			conn.closeWithCode(internal.ClosePolicyViolation, "failed to set a deadline")
			return
		}

		frames := make(internal.FrameGroup, 0, 1)
		frame, ok := <-conn.inboundFrames
		if !ok {
			return
		}
		frames = append(frames, frame)

		if frame.OPCODE != internal.OpcodeText && frame.OPCODE != internal.OpcodeBinary {
			conn.closeWithCode(internal.CloseProtocolError, ErrInvalidOPCODE.Error())
			return
		}
		if frame.FIN {
			conn.inboundMessages <- frames
			continue
		}

		totalSize := len(frame.Payload)
		for {
			frame, ok := <-conn.inboundFrames
			if !ok {
				return
			}
			if frame.OPCODE != internal.OpcodeContinuation {
				conn.closeWithCode(internal.CloseProtocolError, ErrInvalidOPCODE.Error())
				return
			}

			totalSize += len(frame.Payload)
			if conn.Manager.MaxMessageSize != -1 && totalSize > conn.Manager.MaxMessageSize {
				conn.closeWithCode(internal.CloseMessageTooBig, ErrMessageTooLarge.Error())
				return
			}
			frames = append(frames, frame)
			if frame.FIN {
				break
			}
		}

		if frames[0].IsText() && !frames.IsValidUTF8() {
			conn.closeWithCode(internal.CloseProtocolError, ErrInvalidUTF8.Error())
			return
		}

		conn.inboundMessages <- frames
	}
}

// ReadBinary returns the payload from a WebSocket message (binary or text).
// If the receive message was binary, msgType would be equal to internal.OpcodeBinary,
// if the receive message was binary, msgType would be equal to internal.OpcodeText.
//
// All errors are of type snapws.FatalError, indicating a protocol or I/O failure, and the connection will be
// closed automatically by acceptMessage, andthe msgType would be -1.
func (conn *Conn[KeyType]) Read() (msgType int8, data []byte, err error) {
	if conn.isClosed.Load() {
		return -1, nil, Fatal(ErrConnClosed)
	}
	frames, ok := <-conn.inboundMessages
	if !ok {
		return 0, nil, Fatal(ErrConnClosed)
	}

	return int8(frames[0].OPCODE), frames.Payload(), nil
}

// ReadBinary returns the binary payload from a WebSocket binary message.
//
// If the received message is not of type binary, it returns snapws.ErrMessageTypeMismatch
// without closing the connection. All other errors are of type snapws.FatalError, indicating a protocol
// or I/O failure, and the connection will be closed automatically by acceptMessage.
//
// Note: This method returns the payload of a binary WebSocket message as a []byte slice.
func (conn *Conn[KeyType]) ReadBinary() (data []byte, err error) {
	msgType, payload, err := conn.Read()
	if err != nil {
		return nil, err
	} else if msgType != internal.OpcodeBinary {
		return nil, ErrMessageTypeMismatch
	}

	return payload, nil
}

// ReadString returns the message payload as a UTF-8 string from a text WebSocket message.
//
// If the received message is not of type text, it returns snapws.ErrMessageTypeMismatch
// without closing the connection. All other errors are of type snapws.FatalError, indicating a protocol
// or I/O failure, and the connection will be closed automatically by acceptMessage.
func (conn *Conn[KeyType]) ReadString() (string, error) {
	msgType, payload, err := conn.Read()
	if err != nil {
		return "", err // Connection already close by acceptMessage()
	} else if msgType != internal.OpcodeText {
		return "", ErrMessageTypeMismatch
	}

	return string(payload), nil
}

// ReadJSON reads a text WebSocket message and unmarshals its payload into the given value.
//
// This method expects the message to be of type text and contain valid UTF-8 encoded JSON.
// If the message is not of type text, it returns snapws.ErrMessageTypeMismatch without
// closing the connection.
//
// All other errors (such as protocol errors or connection issues) will cause the connection
// to be closed automatically by acceptMessage.
// func (conn *Conn[KeyType]) ReadJSON(v any) error {
// 	msgType, payload, err := conn.Read()
// 	if err != nil {
// 		return err // Connection already close by acceptMessage()
// 	} else if msgType != internal.OpcodeText {
// 		return ErrMessageTypeMismatch
// 	}

// 	return json.Unmarshal(payload, v)
// }
