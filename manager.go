package snapws

import (
	"net/http"
	"sync"
)

type Manager[KeyType comparable] struct {
	// Conns map keeps track of all active connections.
	// Each connection must be keyed by a unique identifier, preferably the user id.
	Conns map[KeyType]*Conn[KeyType]
	Mu    sync.RWMutex
	Args[KeyType]
}

// Creates a new manager. KeyType is the type of the key of the conns map.
// KeyType must be comparable.
func NewManager[KeyType comparable](args *Args[KeyType]) *Manager[KeyType] {
	if args == nil {
		args = &Args[KeyType]{}
	}
	args.WithDefault()

	m := &Manager[KeyType]{
		Conns: make(map[KeyType]*Conn[KeyType]),
		Args:  *args,
	}

	return m
}

func (m *Manager[KeyType]) Connect(key KeyType, w http.ResponseWriter, r *http.Request) (*Conn[KeyType], error) {
	subProtocol, err := handShake(w, r, m.SubProtocols, m.RejectRaw)
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

	conn := m.newConn(c, key, subProtocol)
	m.Register(key, conn)
	if m.OnConnect != nil {
		m.OnConnect(key, conn)
	}

	go conn.acceptMessage()
	go conn.frameListener()
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

func (m *Manager[KeyType]) GetConn(key KeyType) (*Conn[KeyType], bool) {
	m.Mu.RLock()
	defer m.Mu.Unlock()

	conn, ok := m.Conns[key]

	return conn, ok
}

// Broadcast sends a message to all active connections.
// It returns "n" the number of successfull writes, and an error.
// func (m *Manager[KeyType]) Broadcast() (n int, err error)
