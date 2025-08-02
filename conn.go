package snapws

import (
	"bufio"
	"context"
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Conn represents a single WebSocket connection.
// It owns the underlying network connection and manages
// reading/writing frames, assembling messages, handling control
// frames (ping/pong/close), and lifecycle state.
//
// Reader and Writer aren't safe to use from multiple Go routines.
type Conn struct {
	raw net.Conn
	// curently this is a server library, this is saved for future use
	isServer bool
	upgrader *Upgrader
	// Empty string means its a raw websocket
	SubProtocol string
	MetaData    sync.Map
	// channel used to signal that the conn is closed.
	// when the conn closes, the channel closes, so any go routine trying to read from it,
	// would receive ok=false, indicating that the channel is closed => conn is closed.
	done      chan struct{}
	isClosed  atomic.Bool
	closeOnce sync.Once

	// ticker for ping loop
	ticker *time.Ticker

	reader ConnReader
	writer *ConnWriter

	readBuf *bufio.Reader

	ControlWriter *ControlWriter
}

// ManagedConn is a Conn that is tracked by a Manager.
// It links a connection to a unique key so that the Manager
// can manage it safely (fetch/add/remove).
type ManagedConn[KeyType comparable] struct {
	*Conn
	Key     KeyType
	Manager *Manager[KeyType]
}

type ControlWriter struct {
	conn *Conn
	buf  []byte
	sig  chan struct{}
	err  chan error
	lock chan struct{} // full = locked
}

func (conn *Conn) NewControlWriter() *ControlWriter {
	return &ControlWriter{
		conn: conn,
		buf:  make([]byte, 2+4+MaxControlFramePayload), // 2: fin+opce, 4: masking-key, 125: max control frames payload.
		sig:  make(chan struct{}),
		err:  make(chan error),
		lock: make(chan struct{}, 1),
	}
}

func (u *Upgrader) newConn(c net.Conn, subProtocol string, br *bufio.Reader) *Conn {
	conn := &Conn{
		raw:         c,
		isServer:    true,
		upgrader:    u,
		SubProtocol: subProtocol,
		done:        make(chan struct{}),
		ticker:      time.NewTicker(u.PingEvery),
	}

	size := u.ReadBufferSize
	if size == 0 && br != nil {
		conn.readBuf = br
	} else {
		if size < MaxHeaderSize {
			size += MaxHeaderSize
		}
		conn.readBuf = bufio.NewReaderSize(conn.raw, size)
	}

	conn.reader = ConnReader{conn: conn}
	conn.writer = conn.newWriter(OpcodeText)
	conn.ControlWriter = conn.NewControlWriter()

	go conn.listen()
	go conn.pingLoop()

	return conn
}

func (m *Manager[KeyType]) newManagedConn(conn *Conn, key KeyType) *ManagedConn[KeyType] {
	return &ManagedConn[KeyType]{Conn: conn, Key: key, Manager: m}
}

func (conn *Conn) nRead(n int) ([]byte, error) {
	b, err := conn.readBuf.Peek(n)
	if err != nil {
		return nil, err
	}

	// never fails
	_, _ = conn.readBuf.Discard(n)

	return b, nil
}

// Locks "wLock" indicating that a writer has been intiated.
// It tires to aquire the lock, if the provided context is done before
// succeeding to aquiring the lock, it return an error.
func (conn *Conn) lockW(ctx context.Context) error {
	select {
	case <-conn.done:
		return fatal(ErrConnClosed)
	case conn.writer.lock <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Unlocks "wLock", indicating that the writer has finished.
// Returns a snapws.FatalError if the connection is closed.
// Returns nil if unlocking succeeds or if it was already unlocked.
func (conn *Conn) unlockW() error {
	select {
	case _, ok := <-conn.writer.lock:
		if !ok {
			return fatal(ErrChannelClosed)
		}
		return nil
	default:
		// already unlocked
		return nil
	}
}

// A loop that runs as long as the connection is alive.
// Listens for outgoing frames.
// It gives priotiry to control frames.
func (conn *Conn) listen() {
	for {
		select {
		case _, ok := <-conn.ControlWriter.sig:
			if !ok {
				conn.ControlWriter.err <- fatal(ErrChannelClosed)
				return
			}

			err := conn.sendFrame(conn.ControlWriter.buf)
			conn.ControlWriter.err <- err

		case _, ok := <-conn.writer.sig:
			if !ok {
				if conn.writer != nil {
					trySendErr(conn.writer.errCh, fatal(ErrChannelClosed))
				}
				return
			}

			select {
			case <-conn.writer.ctx.Done():
				trySendErr(conn.writer.errCh, conn.writer.ctx.Err())
			default:
				err := conn.sendFrame(conn.writer.buf[conn.writer.start:conn.writer.used])
				if conn.writer.ctx.Err() == nil {
					trySendErr(conn.writer.errCh, err)
				}
			}
		}
	}
}

// A loop that runs as long as the connection is alive.
// Ping the client every "PingEvery" provided from the manager.
// If pinging fails the connection closes.
func (conn *Conn) pingLoop() {
	for range conn.ticker.C {
		err := conn.Ping()
		if err != nil {
			conn.CloseWithCode(ClosePolicyViolation, err.Error())
			return
		}
	}

}

// CloseWithCode closes the connection with the given code and reason.
func (conn *Conn) CloseWithCode(code uint16, reason string) {
	conn.closeOnce.Do(func() {
		close(conn.done)

		if !conn.isClosed.Load() {
			_ = conn.writeClose(code, reason)
		}

		conn.writer.Close()
		conn.raw.Close()
		conn.isClosed.Store(true)
		if conn.ticker != nil {
			conn.ticker.Stop()
		}
		close(conn.writer.sig)
		close(conn.writer.lock)

		if conn.upgrader.OnDisconnect != nil {
			conn.upgrader.OnDisconnect(conn)
		}

		if conn.upgrader.PoolWriteBuffers {
			conn.upgrader.WritePool.Put(&conn.writer.buf)
		}
	})
}

// Used to trigger closeWithCode with the code and reason parsed from payload.
// The payload must be of at least length 2, first 2 bytes are uint16 represnting the close code,
// The rest of the payload is optional, represnting a UTF-8 reason.
// Any violations would cause a close with CloseProtocolError with the apropiate reason.
func (conn *Conn) CloseWithPayload(n int) {
	if n < 2 {
		conn.CloseWithCode(CloseProtocolError, "invalid close frame payload")
		return
	}

	payload, err := conn.nRead(n)
	if err != nil {
		conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
		return
	}

	code := binary.BigEndian.Uint16(payload[:2])
	if !isValidCloseCode(code) {
		conn.CloseWithCode(CloseProtocolError, "invalid close code")
		return
	}

	if len(payload) > 2 {
		if !utf8.Valid(payload[2:]) {
			conn.CloseWithCode(CloseProtocolError, ErrInvalidUTF8.Error())
			return
		}
		conn.CloseWithCode(code, string(payload[2:]))
	} else {
		conn.CloseWithCode(code, "")
	}
}

// Closes the conn normaly.
func (conn *Conn) Close() {
	conn.CloseWithCode(CloseNormalClosure, "Normal close")
}

// closers for ManagedConn.
func (conn *ManagedConn[KeyType]) CloseWithCode(code uint16, reason string) {
	conn.Conn.CloseWithCode(code, reason)
	conn.Manager.unregister(conn.Key)
}
func (conn *ManagedConn[KeyType]) CloseWithPayload(n int) {
	conn.Conn.CloseWithPayload(n)
	conn.Manager.unregister(conn.Key)
}

func (conn *ManagedConn[KeyType]) Close() {
	conn.Conn.Close()
	conn.Manager.unregister(conn.Key)
}
