package snapws

import "errors"

var (
	ErrWrongMethod             = errors.New("Wrong method, the request method must be GET.")
	ErrMissingUpgradeHeader    = errors.New("Mssing Upgrade header.")
	ErrInvalidUpgradeHeader    = errors.New("Invalid Upgrade header.")
	ErrMissingConnectionHeader = errors.New("Mssing connection header.")
	ErrInvalidConnectionHeader = errors.New("Invalid connection header.")
	ErrMissingVersionHeader    = errors.New("Mssing version header.")
	ErrInvalidVersionHeader    = errors.New("Invalid version header.")
	ErrMissingSecKey           = errors.New("Mssing Sec-WebSocket-Key header.")
	ErrInvalidSecKey           = errors.New("Invalid Sec-WebSocket-Key header.")

	ErrHijackerNotSupported = errors.New("Connection doesnt support hijacking.")

	ErrConnNotFound = errors.New("Can't unregister a non existing connection.")
)
