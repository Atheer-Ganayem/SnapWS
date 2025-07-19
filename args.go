package snapws

import (
	"net/http"
	"time"
)

const (
	defaultWriteWait     = time.Second * 5
	defaultReadWait      = time.Minute
	defaultPingEvery     = time.Second * 50
	defaultWriterTimeout = time.Second * 5

	DefaultMaxMessageSize  = 1 << 20 // 1MB
	DefaultReadBufferSize  = 4096
	DefaultWriteBufferSize = 4096

	DefaultInboundFrames   = 32
	DefaultInboundMessages = 8

	DefaultOutboundFrames  = 16
	DefaultOutboundControl = 4
)

type Args[KeyType comparable] struct {
	// Ran before finalizing and accepting the handshake.
	Middlwares []Middlware
	// Ran when the connection finalizes.
	OnConnect func(id KeyType, conn *Conn[KeyType])
	// Ran when connection closes.
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
	// maximum fragremnt allowed per an incoming message.
	// if not set it will default to 0, which means there is no limit.
	ReaderMaxFragments int
	// if not set it will default to 4096 bytes
	ReadBufferSize int
	// if not set it will default to 4096 bytes
	WriteBufferSize int
	//

	// the size of iboundFrames chan buffer, if not set it will default to 32
	InboundFramesSize int
	// the size of iboundMessages chan buffer, if not set it will default to 8
	InboundMessagesSize int
	// the size of outboundFrames chan buffer, if not set it will default to 16
	OutboundFramesSize int
	// the size of outboundControl chan buffer, if not set it will default to 4
	OutboundControlSize int

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

	if args.InboundFramesSize == 0 {
		args.InboundFramesSize = DefaultInboundFrames
	}
	if args.InboundMessagesSize == 0 {
		args.InboundMessagesSize = DefaultInboundMessages
	}
	if args.OutboundFramesSize == 0 {
		args.OutboundFramesSize = DefaultOutboundFrames
	}
	if args.OutboundControlSize == 0 {
		args.OutboundControlSize = DefaultOutboundControl
	}
}

// If an error returns the connection wont be accepted.
// The functions are ran by their order in the slice.
type Middlwares []Middlware
type Middlware func(w http.ResponseWriter, r *http.Request) error

type HttpErr struct {
	Code    int
	Message string
	IsJson  bool
}

func AsHttpErr(err error) (*HttpErr, bool) {
	e, ok := err.(*HttpErr)
	return e, ok
}

func (err *HttpErr) Error() string {
	return err.Message
}

func NewHttpErr(code int, message string) *HttpErr {
	return &HttpErr{Code: code, Message: message, IsJson: false}
}

func NewHttpJSONErr(code int, message string) *HttpErr {
	return &HttpErr{Code: code, Message: message, IsJson: true}
}
