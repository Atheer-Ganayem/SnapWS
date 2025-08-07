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
)

type Options struct {
	// Ran before finalizing and accepting the handshake.
	Middlwares []Middlware
	// Ran when the connection finalizes.
	OnConnect func(conn *Conn)
	// // Ran when connection closes.
	OnDisconnect func(conn *Conn)

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
	// if not set it will use the default http buffer (4kb)
	ReadBufferSize int
	// if not set it will use the default http buffer (4kb)
	WriteBufferSize int

	//Buffer pooling can reduce GC pressure in workloads with large messages and very high throughput,
	// but may increase latency in some scenarios. Enabled by default.
	DisableWriteBuffersPooling bool

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

func (opt *Options) WithDefault() {
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

type Middlwares []Middlware

// A function representing a middleware that will be ran after validating the websocket upgrade request
// and before switching protocols.
// If an error returns the connection wont be accepted.
// It is prefered to return an error of type snapws.MiddlewareErr.
type Middlware func(w http.ResponseWriter, r *http.Request) error

type MiddlewareErr struct {
	Code    int
	Message string
}

func AsMiddlewareErr(err error) (*MiddlewareErr, bool) {
	e, ok := err.(*MiddlewareErr)
	return e, ok
}

func (err *MiddlewareErr) Error() string {
	return err.Message
}

func NewMiddlewareErr(code int, message string) *MiddlewareErr {
	return &MiddlewareErr{Code: code, Message: message}
}
