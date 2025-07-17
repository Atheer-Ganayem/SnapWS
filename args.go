package snapws

import "time"

const (
	defaultWriteWait       = time.Second * 5
	defaultReadWait        = time.Minute
	defaultPingEvery       = time.Second * 50
	defaultWriterTimeout   = time.Second * 5
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
	// Timeout for the writer trying to flush if the outbound channel is full.
	// if not set it will default to 5
	WriterTimeout time.Duration

	// This is the max size of message sent by the client. if not set it will default to 1MB
	// -1 means there is no max size.
	// PLEASE DONT USER -1 UNLESS YOU KNOW WHAT YOU ARE DOING
	MaxMessageSize int
	// if not set it will default to 4096 bytes
	ReadBufferSize int
	// if not set it will default to 4096 bytes
	WriteBufferSize int

	// subProtocols defines the list of supported WebSocket sub-protocols by the server.
	// During the handshake, the server will select the first matching protocol from the
	// client's Sec-WebSocket-Protocol header, based on the client's order of preference.
	// If no match is found, the behavior depends on the value of rejectRaw.
	SubProtocols []string
	// rejectRaw determines whether to reject clients that do not propose any matching
	// sub-protocols. If set to true, the connection will be rejected when:
	//   - The client does not include any Sec-WebSocket-Protocol header,
	//   - Or none of the client's protocols match the supported subProtocols list.
	//
	// If false, such connections will be accepted as raw WebSocket connections.
	RejectRaw bool

	// BroadcastWorkers is an optional function to customize the number of workers
	// used during broadcasting. It receives the number of connections and returns the desired worker count.
	// If nil, the default is (connsLength / 10) + 2.
	BroadcastWorkers func(connsLength int) int
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
	if args.WriterTimeout == 0 {
		args.WriterTimeout = defaultWriterTimeout
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
