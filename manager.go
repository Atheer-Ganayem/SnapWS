package snapws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"unicode/utf8"
)

// Manager tracks active WebSocket connections in a thread-safe way.
//
// Key points:
//   - Generic over KeyType (e.g., user ID), which must be comparable.
//   - Each connection is stored as *ManagedConn[KeyType] in the Conns map.
//   - Manager provides safe fetch/add/remove of connections without requiring
//     additional synchronization in user code.
//   - Thread-safety is enforced with sync.RWMutex (expect some performance overhead).
//   - Broadcast messages.
//
// The Upgrader field handles the WebSocket upgrade, and the optional
// OnRegister / OnUnregister callbacks are invoked when connections are added or removed.
type Manager[KeyType comparable] struct {
	// conns stores active connections keyed by a unique identifier.
	conns    map[KeyType]*ManagedConn[KeyType]
	mu       sync.RWMutex
	Upgrader *Upgrader

	OnRegister   func(conn *ManagedConn[KeyType])
	OnUnregister func(conn *ManagedConn[KeyType])
}

// Creates a new manager. KeyType is the type of the key of the conns map.
// KeyType must be comparable.
func NewManager[KeyType comparable](u *Upgrader) *Manager[KeyType] {
	if u == nil {
		u = NewUpgrader(nil)
	}

	m := &Manager[KeyType]{
		conns:    make(map[KeyType]*ManagedConn[KeyType]),
		Upgrader: u,
	}

	return m
}

// Connect does 2 things:
//   - Upgrades the connection.
//   - connect the user to the manager & runs the hooks.
//
// Any error retuened by this method is a handhshake error, and the response is handled by the handshake.
// You shouln't write to the http writer after this fucntion is called even if it return a non-nil error.
func (m *Manager[KeyType]) Connect(key KeyType, w http.ResponseWriter, r *http.Request) (*ManagedConn[KeyType], error) {
	c, err := m.Upgrader.Upgrade(w, r)
	if err != nil {
		return nil, err
	}

	conn := m.newManagedConn(c, key)

	m.Register(key, conn)

	return conn, nil
}

// Adds a conn[KeyType] to the manager (Manager[KeyType]).
// Receives a key and a pointer to a conn.
// If the key already exists, it will close the connection associated with the key,
// and replace it with the new connection received by the fucntion.
func (m *Manager[KeyType]) Register(key KeyType, conn *ManagedConn[KeyType]) {
	m.mu.Lock()
	if conn, ok := m.conns[key]; ok {
		conn.raw.Close()
	}
	m.conns[key] = conn
	m.mu.Unlock()

	conn.enableBroadcasting()
	if m.OnRegister != nil {
		m.OnRegister(conn)
	}
}

// only to be called bt conn.Close(), dont use it manually.
func (m *Manager[KeyType]) unregister(id KeyType) error {
	m.mu.Lock()

	conn, ok := m.conns[id]
	if !ok {
		m.mu.Unlock()
		return ErrConnNotFound
	}

	delete(m.conns, id)
	m.mu.Unlock()

	if m.OnUnregister != nil {
		m.OnUnregister(conn)
	}

	return nil
}

// Receives a key, returns a pointer to the connection associated with the key.
// If the connection exists, it will return a pointer to it.
// If the connection deosn't exists, it will return a nil pointer.
func (m *Manager[KeyType]) Get(key KeyType) *ManagedConn[KeyType] {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.conns[key]
}

// Get all connections associated with the manager as a slice of pointers to a ManagedConn

// except the conns that appear in "exclude".
func (m *Manager[KeyType]) GetAllConns(exclude ...KeyType) []*ManagedConn[KeyType] {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conns := make([]*ManagedConn[KeyType], 0, len(m.conns))
	for k, v := range m.conns {
		if !slices.Contains(exclude, k) {
			conns = append(conns, v)
		}
	}
	return conns
}

// Get all connections associated with the manager as a slice of pointers to a conn
// except the conn that appear in "exclude".
func (m *Manager[KeyType]) GetAllConnsAsConn(exclude ...KeyType) []*Conn {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conns := make([]*Conn, 0, len(m.conns))
	for k, v := range m.conns {
		if !slices.Contains(exclude, k) {
			conns = append(conns, v.Conn)
		}
	}

	return conns
}

// broadcast sends a message to all active connections.
// this function is to be used by the library, you can use BroadcastString or BroadcastBytes.
// It takes a context.Context, opcode (text or binary), data as a slice of bytes,
// and an optional "exclude" which are the keys of connections to exclude from the broadcast
// It returns "n" the number of successfull writes, and an error.
func (m *Manager[KeyType]) broadcast(ctx context.Context, opcode uint8, data []byte, exclude ...KeyType) (int, error) {
	if !isData(opcode) {
		return 0, fmt.Errorf("%w: must be text or binary", ErrInvalidOPCODE)
	}

	conns := m.GetAllConnsAsConn(exclude...)
	connsLength := len(conns)
	if connsLength == 0 {
		return 0, nil
	}

	return m.Upgrader.broadcast(ctx, conns, opcode, data)
}

// broadcast sends a message to all active connections except the connection of key "exclude".
// It takes a context.Context, data as a slice of bytes,
// and an optional "exclude" which are the keys of connections to exclude from the broadcast
// data must be a valid UTF-8 string, otherwise an error will be returned.
// It returns "n" the number of successfull writes, and an error.
func (m *Manager[KeyType]) BroadcastString(ctx context.Context, data []byte, exclude ...KeyType) (int, error) {
	if !m.Upgrader.SkipUTF8Validation && !utf8.Valid(data) {
		return 0, ErrInvalidUTF8
	}
	return m.broadcast(ctx, OpcodeText, data, exclude...)
}

// broadcast sends a message to all active connections except the connection of key "exclude".
// It takes a context.Context, data as a slice of bytes,
// and an optional "exclude" which are the keys of connections to exclude from the broadcast
// It returns "n" the number of successfull writes, and an error.
func (m *Manager[KeyType]) BroadcastBytes(ctx context.Context, data []byte, exclude ...KeyType) (int, error) {
	return m.broadcast(ctx, OpcodeBinary, data, exclude...)
}

// Shut downs the manager:
// - Closes all connections normaly.
// - Clears the conns map.
func (m *Manager[KeyType]) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	workers := (len(m.conns) / 5) + 2
	var wg sync.WaitGroup
	ch := make(chan *ManagedConn[KeyType], workers)

	for range workers {
		wg.Add(1)
		go func() {
			for conn := range ch {
				conn.Close()
			}
			wg.Done()
		}()
	}

	conns := m.GetAllConns()
	for _, conn := range conns {
		ch <- conn
	}
	close(ch)
	wg.Wait()

	m.conns = make(map[KeyType]*ManagedConn[KeyType])
}

// BatchBroadcast loops over the manager's connections and adds the given data into their batch.
//
// Returns:
//   - int: number of connections that successfully received the message in thier
//     queue (doesnt necessarily mean they sent/batched it successfully)
//   - error: context cancellation error, flusher errors, or nil if completed normally
func (m *Manager[KeyType]) BatchBroadcast(ctx context.Context, data []byte, exclude ...KeyType) (int, error) {
	conns := m.GetAllConnsAsConn(exclude...)

	return m.Upgrader.batchBroadcast(ctx, conns, data)
}

// BatchBroadcastJSON is just a helper method that marshals the given v into json and calls BatchBraodcast.
//
// Returns:
//   - int: number of connections that successfully received the message in thier
//     queue (doesnt necessarily mean they sent/batched it successfully)
//   - error: context cancellation error, mrashal error, flusher errors, or nil if completed normally
func (m *Manager[KeyType]) BatchBroadcastJSON(ctx context.Context, v interface{}, exclude ...KeyType) (int, error) {
	jData, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}

	return m.BatchBroadcast(ctx, jData, exclude...)
}
