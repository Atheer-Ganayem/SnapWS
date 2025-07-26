// Package snapws is a minimal WebSocket library for Go.
//
// SnapWS makes WebSockets effortless: you connect, read, and write,
// while the library takes care of everything else — ping/pong, safe
// concurrent access, and connection lifecycle management.
//
// # Philosophy
//
// No boilerplate, no juggling goroutines, no protocol details.
// You write application logic; SnapWS handles the hard parts.
//
// # Features
//
//   - Automatic ping/pong and close frame handling
//   - Built-in connection manager
//   - Safe concurrent reads and writes
//   - Middlewares & connect/disconnect hooks
//   - Context support for cancellation
//
// SnapWS is for developers who want WebSockets to
// “just work” with minimal effort and minimal code.
package snapws
