package snapws

import (
	"bytes"
	"context"
	"net"
	"time"
)

type PooledBuf struct {
	buf []byte
}

// Returns the underlying net conn.
func (conn *Conn) NetConn() net.Conn {
	return conn.raw
}

// Peek n this discards n from the reader.
// Used to simplify code.
func (conn *Conn) nRead(n int) ([]byte, error) {
	b, err := conn.readBuf.Peek(n)
	if err != nil {
		return nil, err
	}

	// never fails
	_, _ = conn.readBuf.Discard(n)

	return b, nil
}

type mu struct {
	conn *Conn
	ch   chan struct{}
}

func newMu(c *Conn) *mu {
	return &mu{conn: c, ch: make(chan struct{}, 1)}
}

func (m *mu) lock() {
	m.ch <- struct{}{}
}

func (m *mu) unLock() {
	<-m.ch
}

func (m *mu) tryUnlock() bool {
	select {
	case <-m.ch:
		return true
	default:
		return false
	}
}

func (m *mu) lockCtx(ctx context.Context) error {
	if ctx == nil || ctx.Done() == nil {
		m.lock()
		return nil
	}

	select {
	case <-m.conn.done:
		return fatal(ErrConnClosed)
	case m.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *mu) lockTimer(t *time.Timer) error {
	select {
	case <-m.conn.done:
		return fatal(ErrConnClosed)
	case m.ch <- struct{}{}:
		return nil
	case <-t.C:
		return ErrTimeout
	}
}

func comparePayload(p1 []byte, p2 []byte) bool {
	return bytes.Equal(p1, p2)
}
