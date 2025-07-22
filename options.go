package snapws

import (
	"net/http"
	"time"
)

type BackpressureStrategy int

const (
	BackpressureClose BackpressureStrategy = iota
	BackpressureDrop
	BackpressureWait
)

const (
	defaultWriteWait = time.Second * 5
	defaultReadWait  = time.Minute
	defaultPingEvery = time.Second * 50

	DefaultMaxMessageSize  = 1 << 20 // 1MB
	DefaultReadBufferSize  = 4096
	DefaultWriteBufferSize = 4096

	DefaultInboundFrames   = 32
	DefaultInboundMessages = 8

	DefaultOutboundControl = 4
)

type Options[KeyType comparable] struct {
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

	// This is the max size of message sent by the client. if not set it will default to 1MB
	// -1 means there is no max size.
	// Note: the size includes the frame/frames header/header and masking key/keys.
	// PLEASE DONT USER -1 UNLESS YOU KNOW WHAT YOU ARE DOING
	MaxMessageSize int
	// maximum fragremnt allowed per an incoming message.
	// if not set it will default to 0, which means there is no limit.
	ReaderMaxFragments int
	// if not set it will default to 4096 bytes
	ReadBufferSize int
	// if not set it will default to 4096 bytes
	WriteBufferSize int

	// the size of iboundFrames chan buffer, if not set it will default to 32
	// Note: if the consumer is too slow and the channel gets full, the connection will be droped.
	InboundFramesSize int
	// the size of iboundMessages chan buffer, if not set it will default to 8
	InboundMessagesSize int
	// the size of outboundControl chan buffer, if not set it will default to 4
	OutboundControlSize int

	// subProtocols defines the list of supported WebSocket sub-protocols by the server.
	// During the handshake, the server will select the first matching protocol from the
	// client's Sec-WebSocket-Protocol header, based on the client's order of preference.
	// If no match is found, the behavior depends on the value of rejectRaw.
	SubProtocols []string
	// rejectRaw determines whether to reject clients that do not propose any matching
	// sub-protocols. If set to true, the connection will be rejected when:
	//   - The client does not include any Sec-WebSocket-Protocol header.
	//   - Or none of the client's protocols match the supported subProtocols list.
	//
	// If false, such connections will be accepted as raw WebSocket connections.
	RejectRaw bool

	// BroadcastWorkers is an optional function to customize the number of workers
	// used during broadcasting. It receives the number of connections and returns the desired worker count.
	// If nil, the default is (connsLength / 10) + 2.
	BroadcastWorkers func(connsLength int) int

	// BackpressureStrategy controls the behavior when the messages channel is full:
	// 	- snapws.BackpressureClose (default): when the messages channel is full the connection will close.
	// 	- snapws.BackpressureDrop: when the messages channel is full the message will be droped.
	// 	- snapws.BackpressureWait: when the messages channel is full the reading loop will block
	// 	until it succeeds to send the message.
	//
	// Note: when the inobundFrames channel is full, the connection will be closed.
	BackpressureStrategy BackpressureStrategy
}

func (opt *Options[KeyType]) WithDefault() {
	if opt.WriteWait == 0 {
		opt.WriteWait = defaultWriteWait
	}
	if opt.ReadWait == 0 {
		opt.ReadWait = defaultReadWait
	}
	if opt.PingEvery == 0 {
		opt.PingEvery = defaultPingEvery
	}
	if opt.MaxMessageSize == 0 {
		opt.MaxMessageSize = DefaultMaxMessageSize
	}
	if opt.ReadBufferSize == 0 {
		opt.ReadBufferSize = DefaultReadBufferSize
	}
	if opt.WriteBufferSize == 0 {
		opt.WriteBufferSize = DefaultWriteBufferSize
	}

	if opt.InboundFramesSize == 0 {
		opt.InboundFramesSize = DefaultInboundFrames
	}
	if opt.InboundMessagesSize == 0 {
		opt.InboundMessagesSize = DefaultInboundMessages
	}
	if opt.OutboundControlSize == 0 {
		opt.OutboundControlSize = DefaultOutboundControl
	}

	if !opt.BackpressureStrategy.Valid() {
		opt.BackpressureStrategy = BackpressureClose
	}
}

func (s BackpressureStrategy) Valid() bool {
	switch s {
	case BackpressureClose:
		return true
	case BackpressureDrop:
		return true
	case BackpressureWait:
		return true
	default:
		return false
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
