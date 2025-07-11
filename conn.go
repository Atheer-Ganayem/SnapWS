package snapws

import (
	"fmt"
	"io"
	"net"
)

type Conn struct {
	raw             net.Conn
	send            chan []byte
	readBufferSize  int
	writeBufferSize int
}

func (m *Manager[KeyType]) NewConn(c net.Conn) *Conn {
	return &Conn{
		raw:             c,
		send:            make(chan []byte, 256),
		readBufferSize:  m.ReadBufferSize,
		writeBufferSize: m.WriteBufferSize,
	}
}

func (conn *Conn) listen() {
	for {
		select {
		case b, ok := <-conn.send:
			if !ok {
				return
			}
			fmt.Printf("Receive %d byte from send channel\n", len(b))
		}
	}
}

// This is a low level method for accepting raw frames
// Use AcceptString, AcceptJSON or AcceptBytes.
func (conn *Conn) Accept() ([]byte, error) {
	data := make([]byte, 0, conn.readBufferSize)
	buf := make([]byte, conn.readBufferSize)

	for {
		n, err := conn.raw.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err == io.EOF || n < conn.readBufferSize {
			break
		} else if err != nil {
			return nil, err
		}
	}

	return data, nil
}

func (conn *Conn) ReadString() (string, error)
func (conn *Conn) ReadJSON(x struct{}) error
func (conn *Conn) ReadBytes(x interface{}) error

// Low level writing, not safe to use concurrently.
// Use SendString, SendJSON, SendBytes for safe writing.
func (conn *Conn) Write(b []byte) (n int, err error)

func (conn *Conn) SendString(str string) error
func (conn *Conn) SendJSON(val map[string]interface{}) error
func (conn *Conn) SendBytes(b []byte) error
func (conn *Conn) Ping() error
