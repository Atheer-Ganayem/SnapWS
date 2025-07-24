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
	Conns map[KeyType]*Conn[KeyType]
	Mu    sync.RWMutex
	Options[KeyType]
}

// Creates a new manager. KeyType is the type of the key of the conns map.
// KeyType must be comparable.
func NewManager[KeyType comparable](args *Options[KeyType]) *Manager[KeyType] {
	if args == nil {
		args = &Options[KeyType]{}
	}
	args.WithDefault()

	m := &Manager[KeyType]{
		Conns:   make(map[KeyType]*Conn[KeyType]),
		Options: *args,
	}

	return m
}

func (m *Manager[KeyType]) Connect(key KeyType, w http.ResponseWriter, r *http.Request) (*Conn[KeyType], error) {
	subProtocol, err := m.handShake(w, r)
	if err != nil {
		return nil, err
	}

	c, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return nil, err
	}

	conn := m.newConn(c, key, subProtocol)
	m.Register(key, conn)
	if m.OnConnect != nil {
		m.OnConnect(key, conn)
	}

	go conn.readLoop()
	go conn.acceptMessage()
	go conn.listen()
	go conn.pingLoop()

	return conn, nil
}

func (m *Manager[KeyType]) Register(key KeyType, conn *Conn[KeyType]) {
	m.Mu.Lock()
	defer m.Mu.Unlock()

	if conn, ok := m.Conns[key]; ok {
		conn.raw.Close()
	}
	m.Conns[key] = conn
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

	if m.OnDisconnect != nil {
		m.OnDisconnect(id, conn)
	}

	return nil
}

func (m *Manager[KeyType]) Use(mw Middlware) {
	m.Middlwares = append(m.Middlwares, mw)
}

func (m *Manager[KeyType]) GetConn(key KeyType) (*Conn[KeyType], bool) {
	m.Mu.RLock()
	defer m.Mu.RUnlock()

	conn, ok := m.Conns[key]

	return conn, ok
}

func (m *Manager[KeyType]) GetAllConns() []*Conn[KeyType] {
	m.Mu.RLock()
	defer m.Mu.RUnlock()

	conns := make([]*Conn[KeyType], 0, len(m.Conns))
	for _, v := range m.Conns {
		conns = append(conns, v)
	}
	return conns
}

func (m *Manager[KeyType]) GetAllConnsWithExclude(exclude KeyType) []*Conn[KeyType] {
	m.Mu.RLock()
	defer m.Mu.RUnlock()

	conns := make([]*Conn[KeyType], 0, len(m.Conns))
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
	if m.BroadcastWorkers != nil {
		workers = m.BroadcastWorkers(connsLength)
	}
	if workers <= 0 {
		workers = (connsLength / 10) + 2
	}

	var wg sync.WaitGroup
	ch := make(chan *Conn[KeyType], workers)
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
	ch := make(chan *Conn[KeyType], workers)

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
	m.Conns = make(map[KeyType]*Conn[KeyType])
	m.Mu.Unlock()

}
