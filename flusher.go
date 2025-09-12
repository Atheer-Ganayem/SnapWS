package snapws

import (
	"context"
	"maps"
	"sync"
	"time"
)

// BatchStrategy defines the method used for combining multiple messages into a single batch.
type BatchStrategy int

const (
	// StrategyJSON batches messages as elements in a JSON array and sends as binary websocket message.
	// Format: [message1, message2, message3, ...]
	StrategyJSON BatchStrategy = iota
	// StrategyLengthPrefix batches messages with 4-byte big-endian length prefixes before each message.
	// Format: [uint32][message1][uint32][message2][uint32][message3]...
	StrategyLengthPrefix
	// Custom strategy set by the user.
	StrategyCustom
)

// SendFunc is invoked for each connection when sending the batched messages.
//
// The function is responsible for encoding and writing the messages according to
// the user’s own format or transport rules.
type SendFunc func(ctx context.Context, conn *Conn, messages [][]byte) error

func emptySendFunc(ctx context.Context, conn *Conn, messages [][]byte) error {
	return nil
}

// PrefixSuffixFunc defines the signature of "PrefixFunc" and "SuffixFunc" fields for the Flusher.
type PrefixSuffixFunc func(conn *Conn, messages [][]byte) []byte

type batchFlusher struct {
	strategy   BatchStrategy
	flushEvery time.Duration
	ctx        context.Context
	// closed is a channel that signals when the batchFlusher has been shut down.
	// When closed, no new messages should be accepted and pending messages should be flushed.
	closed    chan struct{}
	closeOnce sync.Once

	// used when strategy is set to StrategyCustom
	customSend SendFunc

	conns map[*Conn]bool
	mu    sync.RWMutex

	// This is an optional function.
	//
	// The returned data will appended at the begining of the batch.
	PrefixFunc PrefixSuffixFunc
	// This is an optional function.
	//
	// The returned data will appended at the end of the batch.
	SuffixFunc PrefixSuffixFunc
}

// newFlusher creates and initializes a new batchFlusher for the room with the specified configuration.
// If a batchFlusher already exists, it will be closed before creating the new one.
func newFlusher(ctx context.Context, strategy BatchStrategy, flushEvery time.Duration) *batchFlusher {
	if ctx == nil {
		ctx = context.Background()
	}

	if flushEvery == 0 {
		flushEvery = time.Millisecond * 50
	}

	flusher := &batchFlusher{
		strategy:   strategy,
		ctx:        ctx,
		flushEvery: flushEvery,
		closed:     make(chan struct{}),
		conns:      make(map[*Conn]bool),
	}

	go flusher.listener()
	return flusher
}

// EnableJSONBatching configures connections to batch messages using JSON array format.
// (only if you use methods like: Batch(), BatchJSON(), BroadcastBatch(), etc...)
//
// Messages are collected and sent as a single websocket binary message containing a JSON array.
// This is ideal for JavaScript clients that can easily parse JSON arrays.
//
// If batching is already enabled, it will be properly closed before the new one starts.
//
// Parameters:
//   - ctx: context for the flusher lifecycle control
//   - flushEvery: time interval between automatic batch flushes (0 uses default 50ms)
func (u *Upgrader) EnableJSONBatching(ctx context.Context, flushEvery time.Duration) {
	if u.Flusher != nil {
		u.Flusher.Close()
	}

	u.Flusher = newFlusher(ctx, StrategyJSON, flushEvery)
}

// EnablePrefixBatching configures connections to batch messages using length-prefixed binary format.
// (only if you use methods like: Batch(), BatchJSON(), BroadcastBatch(), etc...)
//
// Each message is prefixed with its length as a 4-byte big-endian integer, allowing clients
// to parse individual messages from the batch. This format is more efficient than JSON for
// binary data and provides precise message boundaries.
//
// Binary format: [uint32][message1][uint32][message2][uint32][message3]...
//
// If batching is already enabled, it will be properly closed before the new one starts.
//
// Parameters:
//   - ctx: context for the flusher lifecycle control
//   - flushEvery: time interval between automatic batch flushes (0 uses default 50ms)
func (u *Upgrader) EnablePrefixBatching(ctx context.Context, flushEvery time.Duration) {
	if u.Flusher != nil {
		u.Flusher.Close()
	}

	u.Flusher = newFlusher(ctx, StrategyLengthPrefix, flushEvery)
}

// EnableBatching configures connections to batch messages using a custon defined format.
// (only if you use methods like: Batch(), BatchJSON(), BroadcastBatch(), etc...)
//
// It's recommended to use the pre-configured formats (EnableJSONBatching or EnablePrefixBatching)
// unless you want something speceific.
//
// If batching is already enabled, it will be properly closed before the new one starts.
//
// Parameters:
//   - ctx: context for the flusher lifecycle control
//   - flushEvery: time interval between automatic batch flushes (0 uses default 50ms)
//   - sendFunc: SendFunc is invoked for each connection when sending the batched messages.
//     The function is responsible for encoding and writing the messages according to
//     the user’s own format or transport rules.
func (u *Upgrader) EnableBatching(ctx context.Context, flushEvery time.Duration, sendFunc SendFunc) {
	if u.Flusher != nil {
		u.Flusher.Close()
	}

	if sendFunc == nil {
		sendFunc = emptySendFunc
	}

	u.Flusher = newFlusher(ctx, StrategyCustom, flushEvery)
	u.Flusher.customSend = sendFunc
}

// add a conn
func (f *batchFlusher) add(conn *Conn) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.conns[conn] = true
}

// removes a conn
func (f *batchFlusher) remove(conn *Conn) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.conns, conn)
}

// Close gracefully shuts down the flusher.
//
// This method flushes all connections batches before closing and ensures the background
// goroutine terminates cleanly. It's safe to call multiple times - subsequent calls
// are ignored. The flusher cannot be reused after closing.
func (f *batchFlusher) Close() {
	f.closeOnce.Do(func() {
		close(f.closed)
	})
}

// IsClosed returns true if the flusher has been closed and batching methods cannot be used.
func (f *batchFlusher) IsClosed() bool {
	select {
	case <-f.closed:
		return true
	default:
		return false
	}
}

// listener runs the periodic flush timer in a dedicated background goroutine.
// it also listens for the closed channel and the context.
func (f *batchFlusher) listener() {
	t := time.NewTicker(f.flushEvery)
	defer t.Stop()

	for {
		select {
		case <-f.closed:
			f.flushAll()
			return
		case <-f.ctx.Done():
			return
		case <-t.C:
			go f.flushAll()
		}
	}
}

// loops over all connections and flushes their batch.
func (f *batchFlusher) flushAll() {
	f.mu.RLock()
	conns := maps.Clone(f.conns)
	f.mu.RUnlock()

	for conn := range conns {
		select {
		case <-f.ctx.Done():
			return
		default:
			conn.batch.flush()
		}
	}
}
