package snapws

import "time"

const (
	defaultWriteWait       = time.Second * 5
	defaultReadWait        = time.Minute
	defaultPingEvery       = time.Second * 50
	DefaultMaxMessageSize  = 1 << 20 // 1MB
	DefaultReadBufferSize  = 4096
	DefaultWriteBufferSize = 4096
)

type Args[KeyType comparable] struct {
	OnConnect    func(id KeyType, conn *Conn[KeyType])
	OnDisconnect func(id KeyType, conn *Conn[KeyType])

	// If not set it will default to 5 seconds.
	WriteWait time.Duration
	// Should be larger than PingEvery. If not set it will default to 60 seconds.
	ReadWait time.Duration
	// If not set it will default to 50 seconds.
	PingEvery time.Duration

	// This is the max size of message sent by the client. if not set it will default to 1MB
	// -1 means there is no max size.
	// PLEASE DONT USER -1 UNLESS YOU KNOW WHAT YOU ARE DOING
	MaxMessageSize int
	// if not set it will default to 4096 bytes
	ReadBufferSize int
	// if not set it will default to 4096 bytes
	WriteBufferSize int
}

func (args *Args[KeyType]) WithDefault() {
	if args.WriteWait == 0 {
		args.WriteWait = defaultWriteWait
	}
	if args.ReadWait == 0 {
		args.ReadWait = defaultReadWait
	}
	if args.PingEvery == 0 {
		args.PingEvery = defaultPingEvery
	}
	if args.MaxMessageSize == 0 {
		args.MaxMessageSize = DefaultMaxMessageSize
	}
	if args.ReadBufferSize == 0 {
		args.ReadBufferSize = DefaultReadBufferSize
	}
	if args.WriteBufferSize == 0 {
		args.WriteBufferSize = DefaultWriteBufferSize
	}
}
