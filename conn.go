package snapws

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"sync"

	"github.com/Atheer-Ganayem/SnapWS/internal"
)

type Conn[KeyType comparable] struct {
	raw       net.Conn
	send      chan *internal.Frame
	Manager   *Manager[KeyType]
	Key       KeyType
	MetaData  sync.Map
	closeOnce sync.Once
}

func (m *Manager[KeyType]) newConn(c net.Conn, key KeyType) *Conn[KeyType] {
	return &Conn[KeyType]{
		raw:     c,
		send:    make(chan *internal.Frame, 256),
		Manager: m,
		Key:     key,
	}
}

func (conn *Conn[KeyType]) listen() {
	for frame := range conn.send {
		fmt.Printf("Receive %d byte from send channel\n", frame.PayloadLength)
		// sending logic later
	}
}

// Low level writing, not safe to use concurrently.
// Use SendString, SendJSON, SendBytes for safe writing.
// func (conn *Conn) Write(b []byte) (n int, err error)

// func (conn *Conn) SendString(str string) error
// func (conn *Conn) SendJSON(val map[string]interface{}) error
// func (conn *Conn) SendBytes(b []byte) error
// func (conn *Conn) Ping() error

func (conn *Conn[KeyType]) pong(payload []byte) {
	frame := internal.Frame{
		FIN:           true,
		OPCODE:        internal.OpcodePong,
		PayloadLength: len(payload),
		Payload:       payload,
	}

	conn.send <- &frame
}

func (conn *Conn[KeyType]) Close(code uint16, reason string) {
	conn.closeOnce.Do(func() {
		buf := new(bytes.Buffer)
		_ = binary.Write(buf, binary.BigEndian, code)
		buf.WriteString(reason)
		payload := buf.Bytes()

		frame, err := internal.NewFrame(true, internal.OpcodeClose, false, payload)
		if err == nil {
			select {
			case conn.send <- &frame:
			default:
				// channel full, skip sending close frame
			}
		}

		close(conn.send)
		conn.Manager.unregister(conn.Key)
	})
}
