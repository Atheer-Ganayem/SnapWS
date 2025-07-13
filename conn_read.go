package snapws

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/Atheer-Ganayem/SnapWS/internal"
)

// acceptFrame reads and parses a single WebSocket frame.
// Returs a websocket frame, uint16 representing close reason (if error), and an error.
// Use higher-level methods like AcceptString, AcceptJSON, or AcceptBytes for convenience.
func (conn *Conn[KeyType]) acceptFrame() (*internal.Frame, uint16, error) {
	data := make([]byte, 0, conn.Manager.ReadBufferSize)
	buf := make([]byte, conn.Manager.ReadBufferSize)

	for {
		err := conn.raw.SetReadDeadline(time.Now().Add(conn.Manager.ReadWait))
		if err != nil {
			return nil, internal.CloseInternalServerErr, err
		}

		n, err := conn.raw.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}

		if err == nil {
			ok, err := internal.IsCompleteFrame(data)
			if err != nil {
				return nil, internal.CloseProtocolError, err
			} else if ok {
				break
			}
			continue
		}

		if err == io.EOF {
			ok, err := internal.IsCompleteFrame(data)
			if err != nil {
				return nil, internal.CloseProtocolError, err
			}
			if ok {
				break
			} else {
				return nil, internal.CloseProtocolError, fmt.Errorf("incomplete frame at EOF")
			}
		} else if err != nil {
			return nil, internal.CloseAbnormalClosure, err
		}
	}

	frame, err := internal.ReadFrame(data)
	if err != nil {
		return nil, internal.CloseProtocolError, err
	} else if !frame.IsMasked {
		return nil, internal.CloseProtocolError, errNotMasked
	}

	if frame.IsControl() {
		if !frame.IsValidControl() {
			return nil, internal.CloseProtocolError, ErrInvalidControlFrame
		}
		switch frame.OPCODE {
		case internal.OpcodeClose:
			return &frame, internal.CloseNormalClosure, io.EOF

		case internal.OpcodePing:
			conn.pong(frame.Payload)
			return conn.acceptFrame()

		case internal.OpcodePong:
			return conn.acceptFrame()
		}
	}

	return &frame, 0, nil
}

// This is the method used for accepting a full websocket message (text or binary).
// This method will automatically close the connection with the appropriate close code on protocol errors or close frames.
// Control frames will be handled automatically by the accetFrame method, they wont be returned.
func (conn *Conn[KeyType]) acceptMessage() (internal.FrameGroup, error) {
	frames := make(internal.FrameGroup, 0, 1)
	frame, code, err := conn.acceptFrame()
	if code == internal.CloseNormalClosure && frame != nil {
		conn.closeWithPayload(frame.Payload)
		return nil, err
	} else if err != nil {
		conn.closeWithCode(code, err.Error())
		return nil, err
	}

	frames = append(frames, frame)

	if frame.OPCODE != internal.OpcodeText && frame.OPCODE != internal.OpcodeBinary {
		conn.closeWithCode(internal.CloseProtocolError, ErrInvalidOPCODE.Error())
		return nil, ErrInvalidOPCODE
	}
	if frame.FIN {
		return frames, nil
	}

	totalSize := len(frame.Payload)
	for {
		frame, code, err := conn.acceptFrame()
		if err != nil {
			conn.closeWithCode(code, err.Error())
			return nil, err
		}
		if frame.OPCODE != internal.OpcodeContinuation {
			conn.closeWithCode(internal.CloseProtocolError, ErrInvalidOPCODE.Error())
			return nil, ErrInvalidOPCODE
		}

		totalSize += len(frame.Payload)
		if conn.Manager.MaxMessageSize != -1 && totalSize > conn.Manager.MaxMessageSize {
			conn.closeWithCode(internal.CloseMessageTooBig, ErrMessageTooLarge.Error())
			return nil, ErrMessageTooLarge
		}
		frames = append(frames, frame)
		if frame.FIN {
			break
		}
	}

	if frames[0].IsText() && !frames.IsValidUTF8() {
		conn.closeWithCode(internal.CloseProtocolError, ErrInvalidUTF8.Error())
		return nil, ErrInvalidUTF8
	}

	return frames, nil
}

// ReadBinary returns the binary payload from a WebSocket binary message.
//
// If the received message is not of type binary, it returns snapws.ErrMessageTypeMismatch
// without closing the connection. All other errors indicate a protocol or I/O failure,
// and the connection will be closed automatically by acceptMessage.
//
// Note: This method returns the payload of a binary WebSocket message as a []byte slice.
func (conn *Conn[KeyType]) ReadBinary() (data []byte, err error) {
	frames, err := conn.acceptMessage()
	if err != nil {
		return nil, err // Connection already close by acceptMessage()
	}
	if !frames.IsBinary() {
		return nil, ErrMessageTypeMismatch
	}

	return frames.Payload(), nil
}

// ReadString returns the message payload as a UTF-8 string from a text WebSocket message.
//
// If the received message is not of type text, it returns snapws.ErrMessageTypeMismatch
// without closing the connection. All other errors indicate protocol or I/O issues,
// and the connection will be closed automatically by acceptMessage.
func (conn *Conn[KeyType]) ReadString() (string, error) {
	frames, err := conn.acceptMessage()
	if err != nil {
		return "", err // Connection already close by acceptMessage()
	} else if !frames.IsText() {
		return "", ErrMessageTypeMismatch
	}

	return string(frames.Payload()), nil
}

// ReadJSON reads a text WebSocket message and unmarshals its payload into the given value.
//
// This method expects the message to be of type text and contain valid UTF-8 encoded JSON.
// If the message is not of type text, it returns snapws.ErrMessageTypeMismatch without
// closing the connection.
//
// All other errors (such as protocol errors or connection issues) will cause the connection
// to be closed automatically by acceptMessage.
func (conn *Conn[KeyType]) ReadJSON(v any) error {
	frames, err := conn.acceptMessage()
	if err != nil {
		return err // Connection already close by acceptMessage()
	} else if !frames.IsText() {
		return ErrMessageTypeMismatch
	}

	return json.Unmarshal(frames.Payload(), v)
}
