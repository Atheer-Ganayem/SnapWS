package snapws

import (
	"bufio"
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
type Conn struct {
	raw net.Conn
	// curently this is a server library its always true. this is saved for future use.
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
	onClose   func()

	// for ping loop
	ticker      *time.Ticker
	pingSent    atomic.Bool
	pingPayload []byte

	reader        *ConnReader
	writer        *ConnWriter
	controlWriter *controlWriter
	writeLock     *mu
	readBuf       *bufio.Reader
}

func (u *Upgrader) newConn(c net.Conn, subProtocol string, br *bufio.Reader, writeBuf []byte) *Conn {
	conn := &Conn{
		raw:         c,
		isServer:    true,
		upgrader:    u,
		SubProtocol: subProtocol,
		done:        make(chan struct{}),
		pingPayload: make([]byte, 0, MaxControlFramePayload),
	}
	conn.writeLock = newMu(conn)

	size := u.ReadBufferSize
	if (size == 0 && br != nil) || (br != nil && size == br.Size()) {
		conn.readBuf = br
	} else {
		if size < MaxHeaderSize {
			size += MaxHeaderSize
		}
		conn.readBuf = bufio.NewReaderSize(conn.raw, size)
	}

	conn.reader = &ConnReader{conn: conn}
	conn.writer = conn.newWriter(OpcodeText, writeBuf)
	conn.controlWriter = conn.newControlWriter()

	go conn.pingLoop()

	return conn
}

// ManagedConn is a Conn that is tracked by a Manager.
// It links a connection to a unique key so that the Manager
// can manage it safely (fetch/add/remove).
type ManagedConn[KeyType comparable] struct {
	*Conn
	Key     KeyType
	Manager *Manager[KeyType]
}

func (m *Manager[KeyType]) newManagedConn(conn *Conn, key KeyType) *ManagedConn[KeyType] {
	conn.onClose = func() {
		m.unregister(key)
	}
	return &ManagedConn[KeyType]{Conn: conn, Key: key, Manager: m}
}

// Used to write control frames.
// This is needed because control frames can be written mid data frames.
type controlWriter struct {
	conn    *Conn
	buf     []byte
	maskKey [4]byte
	err     chan error
	lock    *mu
	t       *time.Timer
}

func (conn *Conn) newControlWriter() *controlWriter {
	cw := &controlWriter{
		conn: conn,
		buf:  make([]byte, 2+4+MaxControlFramePayload), // 2: fin+opce, 4: masking-key, 125: max control frames payload.
		err:  make(chan error),
		lock: newMu(conn),
	}

	return cw
}

// A loop that runs as long as the connection is alive.
// Ping the client every "PingEvery" provided from the manager.
// If pinging fails the connection closes.
func (conn *Conn) pingLoop() {
	if conn.ticker == nil {
		conn.ticker = time.NewTicker(conn.upgrader.PingEvery)
	}
	for range conn.ticker.C {
		if err := conn.Ping(); err != nil {
			conn.CloseWithCode(ClosePolicyViolation, err.Error())
			return
		}
		conn.pingSent.Store(true)
	}
}

// used to handle pong when sent from the client.
// called by acceptFrame() (in conn_read.go).
// returns an error, if the error is fatal it closes the connection.
func (conn *Conn) handlePong(n int, isMasked bool) error {
	if conn.pingSent.Load() {
		if isMasked {
			b, err := conn.nRead(4)
			if err != nil {
				conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
				return err
			}
			copy(conn.controlWriter.maskKey[:], b)
		}

		p, err := conn.nRead(n)
		if err != nil {
			conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
			return fatal(err)
		}

		conn.controlWriter.unMask(p)
		if ok := comparePayload(conn.pingPayload[:], p); !ok {
			conn.CloseWithCode(CloseProtocolError, "ping/pong payload mismatch")
			return fatal(ErrConnClosed)
		}
		if err = conn.raw.SetReadDeadline(time.Now().Add(conn.upgrader.ReadWait)); err != nil {
			conn.CloseWithCode(CloseInternalServerErr, "timeout")
			return fatal(ErrConnClosed)
		}
		conn.pingSent.Store(false)
	} else {
		if _, err := conn.readBuf.Discard(4 + n); err != nil {
			conn.CloseWithCode(CloseInternalServerErr, "something went wrong")
			return fatal(ErrConnClosed)
		}
	}

	return nil
}

// CloseWithCode closes the connection with the given code and reason.
func (conn *Conn) CloseWithCode(code uint16, reason string) {
	conn.closeOnce.Do(func() {
		if conn.ticker != nil {
			conn.ticker.Stop()
		}

		// close the current writer (if exists) and put the buffer back to the pool (if exists).
		conn.writer.Close()
		if !conn.upgrader.DisableWriteBuffersPooling {
			conn.upgrader.WritePool.Put(conn.writer.buf)
		}

		// close the done channel and set isClosed=true to prevent any reads and writes.
		close(conn.done)
		conn.isClosed.Store(true)

		// writer a close frame.
		conn.controlWriter.writeClose(code, reason)

		// run hooks.
		if conn.upgrader.OnDisconnect != nil {
			conn.upgrader.OnDisconnect(conn)
		}
		if conn.onClose != nil {
			conn.onClose()
		}
	})
}

// This function is called upon receiving a close frame from the client.
// It receives "n" representing the payload length, and "isMasked".
// It parses the frame and calls CloseWithCode after extracting and validating the close code and reason.
func (conn *Conn) handleClose(n int, isMasked bool) {
	if n == 0 {
		conn.CloseWithCode(CloseNormalClosure, "")
		return
	} else if n == 1 {
		conn.CloseWithCode(CloseProtocolError, "invalid close frame payload")
		return
	}

	if isMasked {
		b, err := conn.nRead(4)
		if err != nil {
			conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
			return
		}
		copy(conn.controlWriter.maskKey[:], b)
	}

	payload, err := conn.nRead(n)
	if err != nil {
		conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
		return
	}

	conn.controlWriter.unMask(payload)

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
