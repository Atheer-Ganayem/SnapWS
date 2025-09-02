package snapws

import (
	"bytes"
	"context"
	"net"
	"time"
)

type pooledBuf struct {
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

func (m *mu) lock() error {
	select {
	case <-m.conn.done:
		return fatal(ErrConnClosed)
	case m.ch <- struct{}{}:
		return nil
	}
}

func (m *mu) forceLock() {
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
		return m.lock()
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

// bool: if is limited return true if not returns false.
// error: the error from the onLimitExceeded hook.
func (conn *Conn) allow() (bool, error) {
	if conn.upgrader.Limiter != nil && !conn.upgrader.Limiter.allow(conn) {
		if conn.upgrader.Limiter.OnRateLimitHit != nil {
			err := conn.upgrader.Limiter.OnRateLimitHit(conn)
			return false, err
		}
		return false, nil
	}
	return true, nil
}

func (conn *Conn) skipRestOfMessage() error {
	if _, err := conn.reader.conn.readBuf.Discard(conn.reader.remaining); err != nil {
		conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
		return err
	}

	for !conn.reader.fin {
		opcode, err := conn.acceptFrame()
		if err != nil {
			return err
		}

		if opcode != OpcodeContinuation {
			conn.CloseWithCode(CloseProtocolError, ErrExpectedContinuation.Error())
			return ErrExpectedContinuation
		}

		if _, err = conn.readBuf.Discard(conn.reader.remaining); err != nil {
			conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
			return err
		}
	}

	return nil
}
