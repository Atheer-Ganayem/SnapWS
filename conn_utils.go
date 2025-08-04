package snapws

import "context"

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
