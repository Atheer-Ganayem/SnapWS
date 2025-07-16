package snapws

import (
	"errors"
)

type FatalError struct {
	Err error
}

func (e *FatalError) Error() string {
	return e.Err.Error()
}

func IsFatalErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.As(err, &FatalError{})
}

func Fatal(err error) error {
	if err == nil {
		return nil
	}
	return &FatalError{Err: err}
}

var (
	ErrTest                    = errors.New("this is just an error for testing")
	ErrWrongMethod             = errors.New("wrong method, the request method must be GET")
	ErrMissingUpgradeHeader    = errors.New("mssing Upgrade header")
	ErrInvalidUpgradeHeader    = errors.New("invalid Upgrade header")
	ErrMissingConnectionHeader = errors.New("mssing connection header")
	ErrInvalidConnectionHeader = errors.New("invalid connection header")
	ErrMissingVersionHeader    = errors.New("mssing version header")
	ErrInvalidVersionHeader    = errors.New("invalid version header")
	ErrMissingSecKey           = errors.New("mssing Sec-WebSocket-Key header")
	ErrInvalidSecKey           = errors.New("invalid Sec-WebSocket-Key header")
	ErrHijackerNotSupported    = errors.New("connection doesnt support hijacking")
	ErrConnNotFound            = errors.New("can't unregister a non existing connection")
	ErrInvalidOPCODE           = errors.New("invalid OPCODE")
	ErrTooLargePayload         = errors.New("payload length too large")
	ErrInvalidFrameSeq         = errors.New("invalid frame sequence: expected continuation")
	ErrInvalidControlFrame     = errors.New("invalid control frame")
	errExpectedMaskedFrame     = errors.New("received unmasked frame, all frames from the client must be masked")
	ErrMessageTooLarge         = errors.New("message received from client was too large")
	ErrInvalidUTF8             = errors.New("invalid utf8 data")
	// ErrMessageTypeMismatch is returned when the received WebSocket message type
	// does not match the expected type (e.g., expecting text but received binary).
	ErrMessageTypeMismatch      = errors.New("websocket message type did not match expected type")
	ErrNotSupportedSubProtocols = errors.New("unsupported Sec-WebSocket-Protocol")
	ErrChannelClosed            = errors.New("channel is closed")
	ErrEmptyPayload             = errors.New("cannot send empty payload")
	ErrConnClosed               = errors.New("connection is closed")

	ErrNilBuf = errors.New("cannot read into a nil buffer")
)
