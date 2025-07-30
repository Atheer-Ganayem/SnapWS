package snapws

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

type Manager[KeyType comparable] struct {
	// Conns map keeps track of all active connections.
	// Each connection must be keyed by a unique identifier, preferably the user id.
	Conns    map[KeyType]*ManagedConn[KeyType]
	Mu       sync.RWMutex
	Upgrader *Upgrader

	OnRigester   func(id KeyType, conn *ManagedConn[KeyType])
	OnUnrigester func(id KeyType, conn *ManagedConn[KeyType])
}

// Creates a new manager. KeyType is the type of the key of the conns map.
// KeyType must be comparable.
func NewManager[KeyType comparable](u *Upgrader) *Manager[KeyType] {
	if u == nil {
		u = NewUpgrader(nil)
	}

	m := &Manager[KeyType]{
		Conns:    make(map[KeyType]*ManagedConn[KeyType]),
		Upgrader: u,
	}

	return m
}

// Connect does 3 things:
//   - websocket handshake & runs the middlewares defined in upgrader options.
//   - connect the user to the manager & runs the onConnect & onRegister hooks.
//   - runs the connection loops (listeners, pingers, etc...)
//
// Any error retuened by this method is a handhshake error, and the response is handled by the handshake.
// You shouln't write to the writer after this fucntion is called even if it return a non-nil error.
func (m *Manager[KeyType]) Connect(key KeyType, w http.ResponseWriter, r *http.Request) (*ManagedConn[KeyType], error) {
	c, err := m.Upgrader.Upgrade(w, r)
	if err != nil {
		return nil, err
	}

	conn := &ManagedConn[KeyType]{Conn: c, Key: key, Manager: m}

	m.Register(key, conn)

	return conn, nil
}

// Adds a conn[KeyType] to the manager (Manager[KeyType]).
// Receives a key and a pointer to a conn.
// If the key already exists, it will close the connection associated with the key,
// and replace it with the new connection received by the fucntion.
func (m *Manager[KeyType]) Register(key KeyType, conn *ManagedConn[KeyType]) {
	m.Mu.Lock()
	if conn, ok := m.Conns[key]; ok {
		conn.raw.Close()
	}
	m.Conns[key] = conn
	m.Mu.Unlock()

	if m.OnRigester != nil {
		m.OnRigester(key, conn)
	}
}

// only to be called bt conn.Close(), dont use it manually.
func (m *Manager[KeyType]) unregister(id KeyType) error {
	m.Mu.Lock()

	conn, ok := m.Conns[id]
	if !ok {
		m.Mu.Unlock()
		return ErrConnNotFound
	}

	delete(m.Conns, id)
	m.Mu.Unlock()

	if m.OnUnrigester != nil {
		m.OnUnrigester(id, conn)
	}

	return nil
}

// Receives a key, returns a pointer to the connection associated with the key and a bool.
// If the connection exists, it will return a pointer to it and a true value.
// If the connection deosn't exists, it will return nil and a false value.
func (m *Manager[KeyType]) GetConn(key KeyType) (*ManagedConn[KeyType], bool) {
	m.Mu.RLock()
	defer m.Mu.RUnlock()

	conn, ok := m.Conns[key]

	return conn, ok
}

// Get all connections associated with the manager as a slice of pointers.
func (m *Manager[KeyType]) GetAllConns() []*ManagedConn[KeyType] {
	m.Mu.RLock()
	defer m.Mu.RUnlock()

	conns := make([]*ManagedConn[KeyType], 0, len(m.Conns))
	for _, v := range m.Conns {
		conns = append(conns, v)
	}
	return conns
}

// Get all connections associated with the manager as a slice of pointers except the conn of key "exclude".
func (m *Manager[KeyType]) GetAllConnsWithExclude(exclude KeyType) []*ManagedConn[KeyType] {
	m.Mu.RLock()
	defer m.Mu.RUnlock()

	conns := make([]*ManagedConn[KeyType], 0, len(m.Conns))
	var n int
	for k, v := range m.Conns {
		if k != exclude {
			conns = append(conns, v)
			n++
		}
	}
	return conns
}

// broadcast sends a message to all active connections.
// this function is to be used by the library, you can use BroadcastString or BroadcastBytes.
// It takes a context.Context, a connection "key" to exlude (if you want to include every conn
// you can set it as a zero value of you KeyType), opcode (text or binary), data as a slice of bytes.
// It returns "n" the number of successfull writes, and an error.
func (m *Manager[KeyType]) broadcast(ctx context.Context, exclude KeyType, opcode int, data []byte) (int, error) {
	if data == nil {
		return 0, ErrEmptyPayload
	}
	if ctx == nil {
		ctx = context.TODO()
	}

	if opcode != OpcodeText && opcode != OpcodeBinary {
		return 0, ErrInvalidOPCODE
	}

	conns := m.GetAllConnsWithExclude(exclude)
	connsLength := len(conns)
	if connsLength == 0 {
		return 0, nil
	}
	if len(data) == 0 {
		return 0, ErrEmptyPayload
	}

	var workers int
	if m.Upgrader.BroadcastWorkers != nil {
		workers = m.Upgrader.BroadcastWorkers(connsLength)
	}
	if workers <= 0 {
		workers = (connsLength / 10) + 2
	}

	var wg sync.WaitGroup
	ch := make(chan *ManagedConn[KeyType], workers)
	done := make(chan struct{})
	var n int64

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for conn := range ch {
				if ctx.Err() != nil {
					return
				}
				var err error
				if opcode == OpcodeText {
					err = conn.SendString(ctx, data)
				} else {
					err = conn.SendBytes(ctx, data)
				}
				if err == nil {
					atomic.AddInt64(&n, 1)
				}
			}
		}()
	}

	go func() {
		for _, conn := range conns {
			if ctx.Err() != nil {
				break
			}
			ch <- conn
		}
		close(ch)
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return int(n), nil
	case <-ctx.Done():
		return int(n), ctx.Err()
	}
}

// broadcast sends a message to all active connections except the connection of key "exclude".
// It takes a context.Context, a connection "key" to exlude (if you want to include every conn
// you can set it as a zero value of your KeyType).
// data must be a non-empty valid UTF-8 string, otherwise an error will be returned.
// It returns "n" the number of successfull writes, and an error.
func (m *Manager[KeyType]) BroadcastString(ctx context.Context, exclude KeyType, data []byte) (int, error) {
	if len(data) <= 0 {
		return 0, ErrEmptyPayload
	}
	if !utf8.Valid(data) {
		return 0, ErrInvalidUTF8
	}
	return m.broadcast(ctx, exclude, OpcodeText, data)
}

// broadcast sends a message to all active connections except the connection of key "exclude".
// It takes a context.Context, a connection "key" to exlude (if you want to include every conn
// you can set it as a zero value of your KeyType).
// data must be a non-empty byte slice, otherwise an error will be returned.
// It returns "n" the number of successfull writes, and an error.
func (m *Manager[KeyType]) BroadcastBytes(ctx context.Context, exclude KeyType, data []byte) (int, error) {
	if len(data) <= 0 {
		return 0, ErrEmptyPayload
	}

	return m.broadcast(ctx, exclude, OpcodeBinary, data)
}

func (m *Manager[KeyType]) Shutdown() {
	workers := (len(m.Conns) / 10) + 2
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

	m.Mu.Lock()
	m.Conns = make(map[KeyType]*ManagedConn[KeyType])
	m.Mu.Unlock()

}
