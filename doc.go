// Package snapws is a minimal WebSocket library for Go.
//
// SnapWS makes WebSockets effortless: you connect, read, and write,
// while the library takes care of everything else — ping/pong, safe
// concurrent access, connection lifecycle management, rate-limiting, and many other things.
//
// # Philosophy
//
// No boilerplate, no juggling goroutines, no protocol details.
// You write application logic; SnapWS handles the hard parts.
//
// # Features
//
// - Minimal and easy to use API.
// - Fully passes the autobahn-testsuite
// - Automatic handling of ping/pong and close frames.
// - Connection management built-in (useful when communicating between different clients like chat apps).
// - Built-in easy to use rate limiter.
// - Written completely in standard library amd Go offical libraries (golang.org/x), no external libraries imported.
// - Support for middlewares and connect/disconnect hooks.
//
// SnapWS is for developers who want WebSockets to
// “just work” with minimal effort and minimal code.
package snapws
