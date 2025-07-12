package snapws

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/Atheer-Ganayem/SnapWS/internal"
)

type Conn struct {
	raw      net.Conn
	send     chan internal.Frame
	MetaData sync.Map

	readBufferSize  int
	writeBufferSize int
	WriteWait       time.Duration
	ReadWait        time.Duration
	PingEvery       time.Duration
}

func (m *Manager[KeyType]) NewConn(c net.Conn) *Conn {
	return &Conn{
		raw:             c,
		send:            make(chan internal.Frame, 256),
		readBufferSize:  m.ReadBufferSize,
		writeBufferSize: m.WriteBufferSize,
		WriteWait:       m.WriteWait,
		ReadWait:        m.ReadWait,
		PingEvery:       m.PingEvery,
	}
}

func (conn *Conn) listen() {
	for {
		select {
		case frame, ok := <-conn.send:
			if !ok {
				return
			}
			fmt.Printf("Receive %d byte from send channel\n", frame.PayloadLength)
			// sending logic later
		}
	}
}

// AcceptFrame reads and parses a single WebSocket frame.
// Use higher-level methods like AcceptString, AcceptJSON, or AcceptBytes for convenience.
func (conn *Conn) AcceptFrame() (*internal.Frame, error) {
	data := make([]byte, 0, conn.readBufferSize)
	buf := make([]byte, conn.readBufferSize)

	for {
		err := conn.raw.SetReadDeadline(time.Now().Add(conn.ReadWait))
		if err != nil {
			return nil, err
		}

		n, err := conn.raw.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}

		if err == nil {
			ok, err := internal.IsCompleteFrame(data)
			if err != nil {
				return nil, err
			} else if ok {
				break
			}
			continue
		}

		if err == io.EOF {
			ok, err := internal.IsCompleteFrame(data)
			if err != nil {
				return nil, err
			}
			if ok {
				break
			} else {
				return nil, fmt.Errorf("incomplete frame at EOF")
			}
		} else if err != nil {
			return nil, err
		}
	}

	frame, err := internal.ReadFrame(data)
	if err != nil {
		return nil, err
	}

	switch frame.OPCODE {
	case internal.OpcodeClose:
		conn.raw.Close()
		close(conn.send)
		return nil, io.EOF

	case internal.OpcodePing:
		err = conn.Pong(frame.Payload)
		if err != nil {
			return nil, err
		}
		return conn.AcceptFrame()

	case internal.OpcodePong:
		return conn.AcceptFrame()
	}

	return &frame, nil
}

// This is the "true" accept method,
func (conn *Conn) Accept() (internal.FrameGroup, error) {
	frames := make(internal.FrameGroup, 0, 1)
	frame, err := conn.AcceptFrame()
	if err != nil {
		return nil, err
	}
	frames = append(frames, frame)
	if frame.FIN || (frame.OPCODE != internal.OpcodeBinary && frame.OPCODE != internal.OpcodeText) {
		return frames, nil
	}

	for {
		frame, err := conn.AcceptFrame()
		if err != nil {
			return nil, err
		}
		if frame.OPCODE != internal.OpcodeContinuation {
			return nil, ErrInvalidFrameSeq
		}
		frames = append(frames, frame)
		if frame.FIN {
			break
		}
	}

	return frames, nil
}

// Reads from conn and returns the payload of a single websocket frame.
func (conn *Conn) ReadBytes() (data []byte, err error) {
	frames, err := conn.Accept()
	if err != nil {
		return nil, err
	}

	return frames.Payload(), nil
}

func (conn *Conn) ReadString() (string, error) {
	frames, err := conn.Accept()
	if err != nil {
		return "", err
	} else if !frames[0].IsText() {
		return "", ErrInvalidOPCODE
	}

	return string(frames.Payload()), nil
}

func (conn *Conn) ReadJSON(v any) error {
	frames, err := conn.Accept()
	if err != nil {
		return err
	} else if !frames[0].IsText() {
		return ErrInvalidOPCODE
	}

	return json.Unmarshal(frames.Payload(), v)
}

// Low level writing, not safe to use concurrently.
// Use SendString, SendJSON, SendBytes for safe writing.
// func (conn *Conn) Write(b []byte) (n int, err error)

// func (conn *Conn) SendString(str string) error
// func (conn *Conn) SendJSON(val map[string]interface{}) error
// func (conn *Conn) SendBytes(b []byte) error
// func (conn *Conn) Ping() error

func (conn *Conn) Pong(payload []byte) error {
	frame := internal.Frame{
		FIN:           true,
		OPCODE:        internal.OpcodePong,
		PayloadLength: len(payload),
		Payload:       payload,
	}

	conn.send <- frame
	return nil
}
