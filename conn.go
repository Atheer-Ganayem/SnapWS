package snapws

import (
	"fmt"
	"net"
)

type Conn struct {
	raw  net.Conn
	send chan []byte
}

func NewConn(c net.Conn) *Conn {
	return &Conn{
		raw:  c,
		send: make(chan []byte, 256),
	}
}

func (conn *Conn) Listen(broadCast <-chan []byte) {
	for {
		select {
		case b, ok := <-conn.send:
			if !ok {
				return
			}
			fmt.Printf("Receive %d byte from send channel\n", len(b))
		case b, ok := <-broadCast:
			if !ok {
				return
			}
			fmt.Printf("Receive %d byte from broadcast channel\n", len(b))
		}
	}
}

// Low level writing, not safe to use concurrently.
// Use SendString, SendJSON, SendBytes for safe writing.
func (conn *Conn) Write(b []byte) (n int, err error) 
func (conn *Conn) SendString(str string) error
func (conn *Conn) SendJSON(val map[string]interface{}) error
func (conn *Conn) SendBytes(b []byte) error
func (conn *Conn) Ping() error
