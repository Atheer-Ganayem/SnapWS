package snapws

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"
)

type messageBatch struct {
	messages  [][]byte
	totalSize int
	mu        sync.Mutex
	conn      *Conn
	flusher   *batchFlusher
}

func (f *batchFlusher) newBatch(conn *Conn) *messageBatch {
	f.add(conn)
	return &messageBatch{
		messages: make([][]byte, 0),
		conn:     conn,
		flusher:  f,
	}
}

// Batch adds raw byte data to the current message batch.
//
// The data is stored as-is without any encoding or validation. For JSON batching,
// the data should already be valid JSON. For prefix batching, any binary data is acceptable.
//
// This method will fail if:
//   - Batching is not enabled
//   - The flusher has been closed
//   - The flusher's context has been cancelled
//
// Returns:
//   - error: nil on success, or an error describing why the message couldn't be added to the batch.
func (conn *Conn) Batch(data []byte) error {
	if conn.batch == nil {
		return ErrBatchingUninitialized
	}

	select {
	case <-conn.done:
		return ErrConnClosed
	case <-conn.batch.flusher.closed:
		return ErrFlusherClosed
	case <-conn.batch.flusher.ctx.Done():
		return conn.batch.flusher.ctx.Err()
	default:
	}

	conn.batch.mu.Lock()
	if conn.batch.totalSize+len(data) > conn.upgrader.MaxBatchSize {
		conn.batch.mu.Unlock()
		conn.batch.flush()
		conn.batch.mu.Lock()
	}

	conn.batch.messages = append(conn.batch.messages, data)
	conn.batch.totalSize += len(data)
	conn.batch.mu.Unlock()

	return nil
}

// BatchJSON marshals the provided value to JSON and adds it to the current message batch.
//
// This is a convenience method that combines json.Marshal with Batch. The JSON encoding
// is performed immediately, so any marshaling errors are returned synchronously.
//
// Returns:
//   - error: JSON marshaling error or batching error.
func (conn *Conn) BatchJSON(v interface{}) error {
	if conn.batch == nil {
		return ErrBatchingUninitialized
	}

	jData, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return conn.Batch(jData)
}

// if the messages slice isn't empty, it copies it, allocates a new one, and sends it.
func (b *messageBatch) flush() {
	b.mu.Lock()
	if len(b.messages) == 0 {
		b.mu.Unlock()
		return
	}
	c := b.messages
	b.messages = make([][]byte, 0)
	b.totalSize = 0
	b.mu.Unlock()

	go b.send(c)
}

// receives the connections slice and the messages and calls the appropriate send function.
func (b *messageBatch) send(messages [][]byte) error {
	var err error
	switch b.flusher.strategy {
	case StrategyJSON:
		err = b.sendWrapper(messages, jsonArrayEncoder)
	case StrategyLengthPrefix:
		err = b.sendWrapper(messages, lengthPrefixEncoder)
	case StrategyCustom:
		err = b.flusher.customSend(b.flusher.ctx, b.conn, messages)
	default:
		panic(fmt.Errorf("unexpected strategy: %v", b.flusher.strategy))
	}

	return err
}

// ################################
// Internal encoding/send functions
// ################################

type batchEncoder func(w *ConnWriter, messages [][]byte, prefixData []byte, suffixData []byte) error

func (b *messageBatch) sendWrapper(messages [][]byte, encoder batchEncoder) error {
	// extracting the prefix and suffex data before acquiring the writer.
	var s, p []byte
	if b.flusher.PrefixFunc != nil {
		p = b.flusher.PrefixFunc(b.conn, messages)
	}
	if b.flusher.SuffixFunc != nil {
		s = b.flusher.SuffixFunc(b.conn, messages)
	}

	w, err := b.conn.NextWriter(b.flusher.ctx, OpcodeBinary)
	if err != nil {
		return err
	}

	err = encoder(w, messages, p, s)
	if err != nil {
		fmt.Println(err)
		w.Close()
		return err
	}

	return w.Close()
}

// Pre-allocated byte slices for JSON array formatting to avoid allocations during batching.
var (
	openBracket    = []byte{'['}
	closingBracket = []byte{']'}
	comma          = []byte{','}
)

// jsonArrayEncoder encodes messages as a JSON array and sends them via websocket binary message.
//
// The messages are written as: [message1,message2,message3,...]
// Each message is assumed to be valid JSON and is written directly without re-encoding.
func jsonArrayEncoder(w *ConnWriter, messages [][]byte, p []byte, s []byte) error {
	// open the array
	if _, err := w.Write(openBracket); err != nil {
		return err
	}

	// write prefix
	if p != nil {
		if _, err := w.Write(p); err != nil {
			return err
		}
		if _, err := w.Write(comma); err != nil {
			return err
		}
	}

	// write the batch
	for i, msg := range messages {
		if i != 0 {
			if _, err := w.Write(comma); err != nil {
				return err
			}
		}
		if _, err := w.Write(msg); err != nil {
			return err
		}
	}

	// write suffix
	if s != nil {
		if _, err := w.Write(comma); err != nil {
			return err
		}
		if _, err := w.Write(s); err != nil {
			return err
		}
	}

	// close the array
	_, err := w.Write(closingBracket)
	return err
}

// lengthPrefixEncoder encodes messages with length prefixes and sends them via websocket binary message.
//
// Each message is prefixed with its length as a 4-byte big-endian uint32, allowing clients
// to parse individual messages from the batch: [len1][msg1][len2][msg2][len3][msg3]...
func lengthPrefixEncoder(w *ConnWriter, messages [][]byte, p []byte, s []byte) error {
	// pre allocating a slice for the lengthPrefix to avoid allocating for every message
	lengthPrefix := make([]byte, 0, 4)

	// write prefix data
	if p != nil {
		if _, err := w.Write(binary.BigEndian.AppendUint32(lengthPrefix, uint32(len(p)))); err != nil {
			return err
		}
		if _, err := w.Write(p); err != nil {
			return err
		}
	}

	// write messages
	for _, msg := range messages {
		lengthPrefix = lengthPrefix[:0]
		if _, err := w.Write(binary.BigEndian.AppendUint32(lengthPrefix, uint32(len(msg)))); err != nil {
			return err
		}
		if _, err := w.Write(msg); err != nil {
			return err
		}
	}

	// write suffix
	if s != nil {
		lengthPrefix = lengthPrefix[:0]
		if _, err := w.Write(binary.BigEndian.AppendUint32(lengthPrefix, uint32(len(s)))); err != nil {
			return err
		}
		_, err := w.Write(s)
		return err
	}

	return nil
}
