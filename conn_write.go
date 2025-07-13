package snapws

import "github.com/Atheer-Ganayem/SnapWS/internal"

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
