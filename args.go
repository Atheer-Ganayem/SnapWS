package snapws

import "time"

const (
	defaultWriteWait       = time.Second * 5
	defaultReadWait        = time.Second * 5
	defaultPingEvery       = time.Second * 50
	DefaultPongWait        = time.Minute
	DefaultReadBufferSize  = 4096
	DefaultWriteBufferSize = 4096
)

type Args struct {
	OnConnect    func(id string, conn *Conn)
	OnDisconnect func(id string, conn *Conn)

	// If not set it will default to 5 seconds.
	WriteWait time.Duration
	// If not set it will default to 5 seconds.
	ReadWait time.Duration
	// If not set it will default to 50 seconds.
	PingEvery time.Duration
	// If not set it will default to 60 seconds.
	PongWait time.Duration

	// if not set it will default to 4096 bytes
	ReadBufferSize int
	// if not set it will default to 4096 bytes
	WriteBufferSize int
}

func (args *Args) WithDefault() {
	if args.WriteWait == 0 {
		args.WriteWait = defaultWriteWait
	}
	if args.ReadWait == 0 {
		args.ReadWait = defaultReadWait
	}
	if args.PingEvery == 0 {
		args.PingEvery = defaultPingEvery
	}
	if args.PongWait == 0 {
		args.PongWait = DefaultPongWait
	}
	if args.ReadBufferSize == 0 {
		args.ReadBufferSize = DefaultReadBufferSize
	}
	if args.WriteBufferSize == 0 {
		args.WriteBufferSize = DefaultWriteBufferSize
	}
}
