package snapws

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"unicode/utf8"

	"github.com/Atheer-Ganayem/SnapWS/internal"
)

type Conn[KeyType comparable] struct {
	raw       net.Conn
	send      chan *internal.Frame
	Manager   *Manager[KeyType]
	Key       KeyType
	MetaData  sync.Map
	closeOnce sync.Once
	// Empty string means its a raw websocket
	SubProtocol string
}

func (m *Manager[KeyType]) newConn(c net.Conn, key KeyType, subProtocol string) *Conn[KeyType] {
	return &Conn[KeyType]{
		raw:         c,
		send:        make(chan *internal.Frame, 256),
		Manager:     m,
		Key:         key,
		SubProtocol: subProtocol,
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

// Used to trigger closeWithCode with the code and reason parsed from payload.
// The payload must be of at least length 2, first 2 bytes are uint16 represnting the close code,
// The rest of the payload is optional, represnting a UTF-8 reason.
// Any violations would cause a close with internal.CloseProtocolError with the apropiate reason.
func (conn *Conn[KeyType]) closeWithPayload(payload []byte) {
	if len(payload) < 2 {
		conn.closeWithCode(internal.CloseProtocolError, "invalid close frame payload")
		return
	}
	code := binary.BigEndian.Uint16(payload[:2])
	if !internal.IsValidCloseCode(code) {
		conn.closeWithCode(internal.CloseProtocolError, "invalid close code")
		return
	}

	if len(payload) > 2 {
		if !utf8.Valid(payload[2:]) {
			conn.closeWithCode(internal.CloseProtocolError, ErrInvalidUTF8.Error())
			return
		}
		conn.closeWithCode(code, string(payload[2:]))
	} else {
		conn.closeWithCode(code, "")
	}
}

// Closes the conn normaly.
func (conn *Conn[KeyType]) Close() {
	conn.closeWithCode(internal.CloseNormalClosure, "Normal close")
}
