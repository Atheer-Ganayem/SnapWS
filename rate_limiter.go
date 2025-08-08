package snapws

import (
	"sync"

	"golang.org/x/time/rate"
)

type RateLimiter struct {
	clients map[*Conn]*rate.Limiter
	mu      sync.RWMutex
	// Number of message allowed per second
	mps int
	// Number of bursts allowed
	burst int
	// Called when the user exceeds the limit.
	// Important: if you close the connection you must return a non-nil error.
	OnRateLimitHit func(conn *Conn) error
}

func NewRateLimiter(rps, burst int) *RateLimiter {
	return &RateLimiter{
		clients: make(map[*Conn]*rate.Limiter),
		mps:     rps,
		burst:   burst,
	}
}

func (rl *RateLimiter) getLimiter(c *Conn) *rate.Limiter {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return rl.clients[c]
}

func (rl *RateLimiter) addClient(c *Conn) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.clients[c] = rate.NewLimiter(rate.Limit(rl.mps), rl.burst)
}

func (rl *RateLimiter) removeClient(c *Conn) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.clients, c)
}

func (rl *RateLimiter) allow(c *Conn) bool {
	l := rl.getLimiter(c)
	if l == nil {
		return true
	}

	return l.Allow()
}
