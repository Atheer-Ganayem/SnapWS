package snapws

import (
	"net/http"
	"sync"
)

type Manager struct {
	Conns        map[string]*Conn
	Mu           sync.RWMutex
	OnDisconnect func(id string, conn *Conn)
}

func NewManager() *Manager {
	return &Manager{
		Conns: make(map[string]*Conn),
	}
}

func (m *Manager) Connect(id string, w http.ResponseWriter, r *http.Request) (*Conn, error) {
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

	conn := NewConn(c)
	m.Register(id, conn)

	return conn, nil
}

func (m *Manager) Register(id string, conn *Conn) {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.Conns[id] = conn
}

func (m *Manager) Unregister(id string) error {
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

func (m *Manager) GetConn(id string) (*Conn, bool) {
	m.Mu.RLock()
	defer m.Mu.Unlock()

	conn, ok := m.Conns[id]

	return conn, ok
}
