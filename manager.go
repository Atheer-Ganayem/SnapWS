package snapws

import (
	"net/http"
	"sync"
)

type Manager[KeyType comparable] struct {
	// Conns map keeps track of all active connections.
	// Each connection must be keyed by a unique identifier, preferably the user id.
	Conns map[KeyType]*Conn
	Mu    sync.RWMutex
	Args[KeyType]
}

// Creates a new manager. KeyType is the type of the key of the conns map.
// KeyType must be comparable.
func NewManager[KeyType comparable](args *Args[KeyType]) *Manager[KeyType] {
	args.WithDefault()

	m := &Manager[KeyType]{
		Conns: make(map[KeyType]*Conn),
		Args:  *args,
	}

	return m
}

func (m *Manager[KeyType]) Connect(key KeyType, w http.ResponseWriter, r *http.Request) (*Conn, error) {
	err := handShake(w, r)
	if err != nil {
		return nil, err
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, ErrHijackerNotSupported
	}

	c, _, err := hj.Hijack()
	if err != nil {
		return nil, err
	}

	conn := m.NewConn(c)
	m.Register(key, conn)
	if m.OnConnect != nil {
		m.OnConnect(key, conn)
	}

	go conn.listen()
	// go ping pong ops

	return conn, nil
}

func (m *Manager[KeyType]) Register(id KeyType, conn *Conn) {
	m.Mu.Lock()
	defer m.Mu.Unlock()

	if conn, ok := m.Conns[id]; ok {
		conn.raw.Close()
	}
	m.Conns[id] = conn
}

func (m *Manager[KeyType]) Unregister(id KeyType) error {
	m.Mu.Lock()

	conn, ok := m.Conns[id]
	if !ok {
		return ErrConnNotFound
	}

	conn.raw.Close()
	delete(m.Conns, id)

	m.Mu.Unlock()

	if m.OnDisconnect != nil {
		m.OnDisconnect(id, conn)
	}

	return nil
}

func (m *Manager[KeyType]) GetConn(id KeyType) (*Conn, bool) {
	m.Mu.RLock()
	defer m.Mu.Unlock()

	conn, ok := m.Conns[id]

	return conn, ok
}
