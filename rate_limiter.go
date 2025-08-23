package snapws

import (
	"sync"

	"golang.org/x/time/rate"
)

// RateLimiter manages per-connection message rate limiting for WebSocket connections.
// It uses a token bucket algorithm to limit the number of messages each connection
// can send per second, with configurable burst capacity.
//
// The rate limiter operates on WebSocket messages (not frames), meaning:
// - Only data frames (text/binary) count toward the limit
// - Control frames (ping, pong, close) are ignored
// - Fragmented messages count as a single message
//
// note: if you want to close the connection you must close it manually in
// SetOnLimitExceeded and return a non-nil error.
//
// Example usage:
//
//	limiter := NewRateLimiter(10, 5) // 10 msg/sec, burst of 5
//	limiter.SetOnLimitExceeded(func(conn *Conn) error {
//	    log.Printf("Rate limit exceeded for connection %v", conn.RemoteAddr())
//	    return nil // Don't close connection
//	})
//	upgrader.Limiter = limiter
type RateLimiter struct {
	// clients maps each connection to its individual rate limiter.
	// Each connection gets its own token bucket with the same rate/burst settings.
	clients map[*Conn]*rate.Limiter
	mu      sync.RWMutex

	// mps is the number of messages allowed per second for each connection
	mps int

	// burst is the maximum number of messages that can be sent in a burst
	// before rate limiting kicks in. This allows for brief spikes in traffic.
	burst int

	// onLimitExceeded is called when a connection exceeds its rate limit.
	// The function receives the offending connection and can perform actions
	// like logging, metrics collection, or closing the connection.
	//
	// Important: If this function closes the connection, it MUST return a
	// non-nil error to signal that the connection should be terminated.
	// Returning nil means the message should be dropped but the connection
	// should remain open.
	OnRateLimitHit func(conn *Conn) error
}

// NewRateLimiter creates and returns a new RateLimiter.
// The limiter uses a token bucket algorithm, where:
//   - mps: number of tokens (messages) added per second.
//   - burst: maximum number of tokens the bucket can hold at once.
//
// Example:
//
//	mps = 3, burst = 5
//	- At most 5 messages can be allowed immediately if the bucket is full.
//	- Each second, 3 new tokens are added, up to the burst limit.
func NewRateLimiter(mps, burst int) *RateLimiter {
	return &RateLimiter{
		clients: make(map[*Conn]*rate.Limiter),
		mps:     mps,
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
