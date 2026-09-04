package ratelimit

import (
	"sync"
	"time"
)

// RateLimiter enforces per-host and global rate limits.
type RateLimiter struct {
	mu       sync.Mutex
	limits   map[string]*time.Ticker
	globalCh chan struct{}
}

// NewRateLimiter creates a new RateLimiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		limits:   make(map[string]*time.Ticker),
		globalCh: make(chan struct{}, 100),
	}
}

// Allow checks if a request to the given host is allowed.
func (r *RateLimiter) Allow(host string) bool {
	r.mu.Lock()
	ticker, ok := r.limits[host]
	if !ok {
		ticker = time.NewTicker(100 * time.Millisecond)
		r.limits[host] = ticker
	}
	r.mu.Unlock()

	select {
	case <-ticker.C:
		return true
	default:
		return false
	}
}
