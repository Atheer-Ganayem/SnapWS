package snapws

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"sync"
	"sync/atomic"
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
)

// broadcaster defines the interface for objects that can broadcast messages to multiple connections.
// It provides methods to retrieve connections and determine optimal worker count for broadcasting.
type broadcaster interface {
	getWorkersCount(n int) int
	getConns(exclude ...*Conn) []*Conn
}

// OnFlushFunc is a callback function invoked after each batch flush operation completes.
// Parameters:
//   - n: number of connections that successfully received the batch
//   - err: any error that occurred during the flush operation (nil if successful)
type OnFlushFunc func(n int, err error)

// messageBatcher handles automatic batching and periodic flushing of messages to optimize
// network performance by reducing the number of individual websocket writes.
//
// The batcher collects messages in memory and flushes them at regular intervals or when
// explicitly closed. It supports 2 (check StrategyJSON and StrategyLengthPrefix) batching strategies
// and runs background goroutines for timing and broadcasting operations.
type messageBatcher struct {
	strategy BatchStrategy
	// closed is a channel that signals when the batcher has been shut down.
	// When closed, no new messages should be accepted and pending messages should be flushed.
	closed    chan struct{}
	closeOnce sync.Once

	messages   [][]byte
	mu         sync.Mutex
	flushEvery time.Duration

	// ctx provides cancellation and timeout control for the batcher lifecycle.
	ctx context.Context

	// broadcaster is the target for sending batched messages, typically a Room instance.
	broadcaster broadcaster

	// onFlush callback function executed after each flush operation.
	onFlush func(n int, err error)
}

// newBatcher creates and initializes a new message batcher for the room with the specified configuration.
// If a batcher already exists, it will be closed before creating the new one.
func newBatcher(ctx context.Context, b broadcaster, strategy BatchStrategy, flushEvery time.Duration, onFlush OnFlushFunc) *messageBatcher {
	if ctx == nil {
		ctx = context.Background()
	}

	if flushEvery == 0 {
		flushEvery = time.Millisecond * 50
	}

	batcher := &messageBatcher{
		broadcaster: b,
		strategy:    strategy,
		ctx:         ctx,
		flushEvery:  flushEvery,
		messages:    make([][]byte, 0, 8),
		closed:      make(chan struct{}),
		onFlush:     onFlush,
	}

	go batcher.listener()
	return batcher
}

// EnableJSONBatching configures the room to batch messages using JSON array format.
//
// Messages are collected and sent as a single websocket binary message containing a JSON array.
// This is ideal for JavaScript clients that can easily parse JSON arrays.
//
// If a batcher is already active, it will be properly closed before the new one starts.
//
// Parameters:
//   - ctx: context for batcher lifecycle control
//   - flushEvery: time interval between automatic batch flushes (0 uses default 50ms)
//   - onFlush: optional callback for monitoring flush operations
func (r *Room[keyType]) EnableJSONBatching(ctx context.Context, flushEvery time.Duration, onFlush OnFlushFunc) {
	if r.batcher != nil {
		r.batcher.Close()
	}

	r.batcher = newBatcher(ctx, r, StrategyJSON, flushEvery, onFlush)
}

// EnablePrefixBatching configures the room to batch messages using length-prefixed binary format.
//
// Each message is prefixed with its length as a 4-byte big-endian integer, allowing clients
// to parse individual messages from the batch. This format is more efficient than JSON for
// binary data and provides precise message boundaries.
//
// Binary format: [uint32][message1][uint32][message2][uint32][message3]...
//
// If a batcher is already active, it will be properly closed before the new one starts.
//
// Parameters:
//   - ctx: context for batcher lifecycle control
//   - flushEvery: time interval between automatic batch flushes (0 uses default 50ms)
//   - onFlush: optional callback for monitoring flush operations
func (r *Room[keyType]) EnablePrefixBatching(ctx context.Context, flushEvery time.Duration, onFlush OnFlushFunc) {
	if r.batcher != nil {
		r.batcher.Close()
	}

	r.batcher = newBatcher(ctx, r, StrategyLengthPrefix, flushEvery, onFlush)
}

// Close gracefully shuts down the message batcher.
//
// This method flushes any pending messages before closing and ensures the background
// goroutine terminates cleanly. It's safe to call multiple times - subsequent calls
// are ignored. The batcher cannot be reused after closing.
func (mb *messageBatcher) Close() {
	mb.closeOnce.Do(func() {
		close(mb.closed)
	})
}

// IsClosed returns true if the message batcher has been closed and cannot accept new messages.
func (mb *messageBatcher) IsClosed() bool {
	select {
	case <-mb.closed:
		return true
	default:
		return false
	}
}

// listener runs the periodic flush timer in a dedicated background goroutine.
//
// This internal method handles:
//   - Automatic flushing at configured intervals
//   - Graceful shutdown when the batcher is closed
//   - Context cancellation for early termination
//   - Final flush of pending messages on shutdown
func (mb *messageBatcher) listener() {
	t := time.NewTicker(mb.flushEvery)
	defer t.Stop()

	for {
		select {
		case <-mb.closed:
			mb.flush()
			return
		case <-mb.ctx.Done():
			return
		case <-t.C:
			mb.flush()
		}
	}
}

// flush sends all currently batched messages to connected clients and resets the buffer.
//
// This method is thread-safe and handles empty buffers gracefully. The actual broadcasting
// is performed asynchronously in a separate goroutine to avoid blocking the caller.
// The onFlush callback (if configured) is invoked with the results.
func (mb *messageBatcher) flush() {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.messages) == 0 {
		return
	}

	c := mb.messages
	mb.messages = make([][]byte, 0, 8)

	go func() {
		n, err := mb.batchBroadcast(c)
		if mb.onFlush != nil {
			mb.onFlush(n, err)
		}
	}()
}

// Batch adds raw byte data to the current message batch.
//
// The data is stored as-is without any encoding or validation. For JSON batching,
// the data should already be valid JSON. For prefix batching, any binary data is acceptable.
//
// This method will fail if:
//   - No batcher has been configured for the room
//   - The batcher has been closed
//   - The batcher's context has been cancelled
//
// Parameters:
//   - data: raw message bytes to add to the batch
//
// Returns:
//   - error: nil on success, or an error describing why the message couldn't be batched
func (r *Room[keyType]) Batch(data []byte) error {
	if r.batcher == nil {
		return ErrBatcherUninitialized
	} else if r.batcher.IsClosed() {
		return ErrBatcherClosed
	} else if r.batcher.ctx.Err() != nil {
		return r.batcher.ctx.Err()
	}

	r.batcher.mu.Lock()
	defer r.batcher.mu.Unlock()
	r.batcher.messages = append(r.batcher.messages, data)

	return nil
}

// BatchJSON marshals the provided value to JSON and adds it to the current message batch.
//
// This is a convenience method that combines json.Marshal with Batch. The JSON encoding
// is performed immediately, so any marshaling errors are returned synchronously.
//
// Parameters:
//   - v: any value that can be marshaled to JSON
//
// Returns:
//   - error: JSON marshaling error or batching error.
func (r *Room[keyType]) BatchJSON(v interface{}) error {
	if r.batcher == nil {
		return ErrBatcherUninitialized
	}

	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return r.Batch(data)
}

// ============================================================
// ============================================================
// =========== handling broadcasting and encoding ==============
// ============================================================
// ============================================================

// Pre-allocated byte slices for JSON array formatting to avoid allocations during batching.
var (
	openBracket    = []byte{'['}
	closingBracket = []byte{']'}
	comma          = []byte{','}
)

// batchBroadcast sends a batch of messages to all connected clients using the configured strategy.
//
// This method distributes the work across multiple goroutines for optimal performance with
// large numbers of connections. It returns the count of successful sends and any error
// that prevented completion.
//
// The broadcasting respects context cancellation and will terminate early if the context
// is cancelled during the operation.
//
// Parameters:
//   - messages: slice of raw message bytes to broadcast
//
// Returns:
//   - int: number of connections that successfully received the batch
//   - error: context cancellation error, or nil if completed normally
func (mb *messageBatcher) batchBroadcast(messages [][]byte) (int, error) {
	conns := mb.broadcaster.getConns()
	workers := mb.broadcaster.getWorkersCount(len(conns))

	var wg sync.WaitGroup
	ch := make(chan *Conn, workers)
	done := make(chan struct{})
	var n int64

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for conn := range ch {
				if mb.ctx != nil && mb.ctx.Err() != nil {
					return
				}

				var err error
				if mb.strategy == StrategyJSON {
					err = sendJSONStrategy(mb.ctx, conn, messages)
				} else {
					err = sendPrefixStrategy(mb.ctx, conn, messages)
				}

				if err == nil {
					atomic.AddInt64(&n, 1)
				}
			}
		}()
	}

	go func() {
		for _, conn := range conns {
			if mb.ctx != nil && mb.ctx.Err() != nil {
				break
			}
			ch <- conn
		}
		close(ch)
		wg.Wait()
		close(done)
	}()

	if mb.ctx == nil {
		<-done
		return int(n), nil
	}

	select {
	case <-done:
		return int(n), nil
	case <-mb.ctx.Done():
		return int(n), mb.ctx.Err()
	}
}

// sendJSONStrategy encodes messages as a JSON array and sends them via websocket binary message.
//
// The messages are written as: [message1,message2,message3,...]
// Each message is assumed to be valid JSON and is written directly without re-encoding.
//
// Parameters:
//   - ctx: context for cancellation and timeout control
//   - conn: target websocket connection
//   - messages: pre-encoded JSON message bytes
//
// Returns:
//   - error: websocket write error or nil on success// encodes the messages in a json array and sends them to the conn.
func sendJSONStrategy(ctx context.Context, conn *Conn, messages [][]byte) error {
	w, err := conn.NextWriter(ctx, OpcodeBinary)
	if err != nil {
		return err
	}

	if _, err := w.Write(openBracket); err != nil {
		w.Close()
		return err
	}

	for i, msg := range messages {
		if i != 0 {
			if _, err := w.Write(comma); err != nil {
				w.Close()
				return err
			}
		}
		if _, err := w.Write(msg); err != nil {
			w.Close()
			return err
		}
	}

	if _, err := w.Write(closingBracket); err != nil {
		w.Close()
		return err
	}

	return w.Close()
}

// sendPrefixStrategy encodes messages with length prefixes and sends them via websocket binary message.
//
// Each message is prefixed with its length as a 4-byte big-endian uint32, allowing clients
// to parse individual messages from the batch: [len1][msg1][len2][msg2][len3][msg3]...
//
// Parameters:
//   - ctx: context for cancellation and timeout control
//   - conn: target websocket connection
//   - messages: raw message bytes to send
//
// Returns:
//   - error: websocket write error or nil on success// prefixes the messages with their length and sends them to the conn in a single message.
func sendPrefixStrategy(ctx context.Context, conn *Conn, messages [][]byte) error {
	w, err := conn.NextWriter(ctx, OpcodeBinary)
	if err != nil {
		return err
	}

	prefix := make([]byte, 4)
	for _, msg := range messages {
		prefix = prefix[:0]
		if _, err := w.Write(binary.BigEndian.AppendUint32(prefix, uint32(len(msg)))); err != nil {
			w.Close()
			return err
		}
		if _, err := w.Write(msg); err != nil {
			w.Close()
			return err
		}
	}

	return w.Close()
}
