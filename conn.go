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

func (conn *Conn[KeyType]) closeWithCode(code uint16, reason string) {
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
				// channel full => skip sending close frame
			}
		}

		close(conn.send)
		conn.Manager.unregister(conn.Key)
	})
}

func (conn *Conn[KeyType]) closeWithPayload(payload []byte) {
	conn.closeOnce.Do(func() {
		frame, err := internal.NewFrame(true, internal.OpcodeClose, false, payload)
		if err == nil {
			select {
			case conn.send <- &frame:
			default:
				// channel full => skip sending close frame
			}
		}
		close(conn.send)
		conn.Manager.unregister(conn.Key)
	})
}

func (conn *Conn[KeyType]) Close() {
	conn.closeWithCode(internal.CloseNormalClosure, "Normal close")
}
