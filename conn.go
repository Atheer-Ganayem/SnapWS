package snapws

import "net"

type Conn struct {
	raw net.Conn
	send chan<- []byte // later
}

func NewConn(c net.Conn) *Conn {
	return &Conn{
		raw: c,
		send: make(chan<- []byte),
	}
}
