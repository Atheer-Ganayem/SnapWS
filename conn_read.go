package snapws

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

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
			return nil, internal.CloseNormalClosure, io.EOF

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
// Control frames will be handled automatically, they wont be returned.
func (conn *Conn[KeyType]) accept() (internal.FrameGroup, error) {
	frames := make(internal.FrameGroup, 0, 1)
	frame, code, err := conn.acceptFrame()
	if err != nil {
		conn.Close(code, err.Error())
		return nil, err
	}

	frames = append(frames, frame)

	if !frame.IsValidMessageFrame() {
		conn.Close(internal.CloseProtocolError, ErrInvalidOPCODE.Error())
		return nil, ErrInvalidOPCODE
	}
	if frame.FIN {
		return frames, nil
	}

	totalSize := len(frame.Payload)
	for {
		frame, code, err := conn.acceptFrame()
		if err != nil {
			conn.Close(code, err.Error())
			return nil, err
		}
		if frame.OPCODE != internal.OpcodeContinuation {
			conn.Close(internal.CloseProtocolError, ErrInvalidOPCODE.Error())
			return nil, ErrInvalidOPCODE
		}
		frames = append(frames, frame)
		totalSize += len(frame.Payload)

		if conn.Manager.MaxMessageSize != -1 && totalSize > conn.Manager.MaxMessageSize {
			conn.Close(internal.CloseMessageTooBig, ErrMessageTooLarge.Error())
			return nil, ErrMessageTooLarge
		}
		if frame.FIN {
			break
		}
	}

	return frames, nil
}

// Returns the payload as a slice of bytes.
func (conn *Conn[KeyType]) ReadBytes() (data []byte, err error) {
	frames, err := conn.accept()
	if err != nil {
		return nil, err
	}

	return frames.Payload(), nil
}

func (conn *Conn[KeyType]) ReadString() (string, error) {
	frames, err := conn.accept()
	if err != nil {
		return "", err
	} else if !frames[0].IsText() {
		return "", ErrInvalidOPCODE
	}
	if ok := utf8.Valid(frames.Payload()); !ok {
		return "", ErrInvalidUTF8
	}

	return string(frames.Payload()), nil
}

func (conn *Conn[KeyType]) ReadJSON(v any) error {
	frames, err := conn.accept()
	if err != nil {
		return err
	} else if !frames[0].IsText() {
		return ErrInvalidOPCODE
	}
	if ok := utf8.Valid(frames.Payload()); !ok {
		return ErrInvalidUTF8
	}

	return json.Unmarshal(frames.Payload(), v)
}
