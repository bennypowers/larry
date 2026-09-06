package ratelimit

import (
	"context"
	"sync"
	"time"

	"bennypowers.dev/larry/internal/remoteid"
)

// Config holds rate limiter configuration.
type Config struct {
	// PerHostDelay is the minimum interval between requests to the same host.
	PerHostDelay time.Duration

	// GlobalConcurrency is the maximum number of concurrent requests across all hosts.
	// Default: 8
	GlobalConcurrency int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		PerHostDelay:      100 * time.Millisecond, // common default (PyPI, NPM)
		GlobalConcurrency: 8,
	}
}

// ProviderDelays returns a copy of the per-provider default delay map.
// Each provider type has its own recommended per-host delay based on
// the upstream API's rate limiting characteristics.
func ProviderDelays() map[remoteid.RemoteIdType]time.Duration {
	return map[remoteid.RemoteIdType]time.Duration{
		remoteid.TypePyPI:      100 * time.Millisecond,  // pypi.org rate limits
		remoteid.TypeNPM:       100 * time.Millisecond,  // registry.npmjs.org
		remoteid.TypeGitLab:    200 * time.Millisecond,  // gitlab.com API
		remoteid.TypeCodeberg:  200 * time.Millisecond,  // codeberg.org API
		remoteid.TypeSourceHut: 200 * time.Millisecond,  // sr.ht
		remoteid.TypeGitHub:    0,                       // gh CLI handles its own rate limiting
	}
}

// PerProviderDelay returns the recommended per-host delay for a provider type.
// Returns 0 for GitHub (uses gh CLI), 100ms for PyPI/NPM, 200ms for GitLab/Codeberg/SourceHut.
// Falls back to 100ms for unknown types.
func PerProviderDelay(rtype remoteid.RemoteIdType) time.Duration {
	delays := ProviderDelays()
	if delay, ok := delays[rtype]; ok {
		return delay
	}
	return 100 * time.Millisecond // fallback: same as PyPI/NPM
}

// ConfigForProvider returns a Config with per-provider defaults applied.
// The PerHostDelay is set to the provider-specific default, and GlobalConcurrency
// defaults to 8.
func ConfigForProvider(rtype remoteid.RemoteIdType) *Config {
	return &Config{
		PerHostDelay:      PerProviderDelay(rtype),
		GlobalConcurrency: 8,
	}
}

// RateLimiter enforces per-host and global rate limits.
type RateLimiter struct {
	mu       sync.Mutex
	perHost  map[string]*time.Ticker
	sem      chan struct{} // global concurrency semaphore
	config   *Config
}

// NewRateLimiter creates a new RateLimiter with default config.
func NewRateLimiter() *RateLimiter {
	return NewRateLimiterWithConfig(DefaultConfig())
}

// NewRateLimiterForProvider creates a RateLimiter optimized for a specific provider type.
// Uses per-provider default delay (e.g., 200ms for GitLab, 100ms for PyPI, 0 for GitHub).
func NewRateLimiterForProvider(rtype remoteid.RemoteIdType) *RateLimiter {
	return NewRateLimiterWithConfig(ConfigForProvider(rtype))
}

// NewRateLimiterWithConfig creates a new RateLimiter with the given config.
func NewRateLimiterWithConfig(cfg *Config) *RateLimiter {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if cfg.GlobalConcurrency <= 0 {
		cfg.GlobalConcurrency = 8
	}
	return &RateLimiter{
		perHost: make(map[string]*time.Ticker),
		sem:     make(chan struct{}, cfg.GlobalConcurrency),
		config:  cfg,
	}
}

// AllowPerHost checks if a per-host delay has elapsed.
// This does NOT acquire the global concurrency semaphore.
// Callers should use Do() which handles both.
func (r *RateLimiter) AllowPerHost(ctx context.Context, host string) error {
	ticker := r.getPerHostTicker(host)
	select {
	case <-ticker.C:
		return nil
	case <-ctx.Done():
		ticker.Stop()
		return ctx.Err()
	}
}

// Allow acquires the global concurrency semaphore AND checks per-host delay.
// This is a convenience method — prefer Do() for actual work.
func (r *RateLimiter) Allow(ctx context.Context, host string) error {
	// Acquire global concurrency slot
	select {
	case r.sem <- struct{}{}:
		// Will release via release() in Do()
	case <-ctx.Done():
		return ctx.Err()
	}

	// Check per-host delay (semaphore is held during work)
	ticker := r.getPerHostTicker(host)
	select {
	case <-ticker.C:
		return nil
	case <-ctx.Done():
		ticker.Stop()
		<-r.sem // release semaphore
		return ctx.Err()
	}
}

// Release releases a previously acquired global concurrency slot.
// Called after Do() completes.
func (r *RateLimiter) Release() {
	<-r.sem
}

// Do wraps a function call with full rate limiting (concurrency + per-host).
// The global concurrency slot is acquired before fn() runs and released after.
// If ctx is nil, no cancellation is supported and the call blocks until a slot is available.
func (r *RateLimiter) Do(ctx context.Context, host string, fn func() error) error {
	if ctx == nil {
		// No context: acquire semaphore and proceed.
		// Block until a slot is available.
		r.sem <- struct{}{}
		defer func() { <-r.sem }()

		ticker := r.getPerHostTicker(host)
		<-ticker.C
		return fn()
	}

	// Acquire global concurrency slot
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return ctx.Err()
	}

	// Check per-host delay
	ticker := r.getPerHostTicker(host)
	select {
	case <-ticker.C:
		return fn()
	case <-ctx.Done():
		ticker.Stop()
		return ctx.Err()
	}
}

// DoNonBlocking checks if the request can proceed without blocking.
// Returns true if both per-host and global limits are satisfied.
func (r *RateLimiter) DoNonBlocking(host string, fn func() error) (bool, error) {
	// Try to acquire global concurrency slot without blocking
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	default:
		return false, nil
	}

	// Try per-host delay without blocking
	ticker := r.getPerHostTicker(host)
	select {
	case <-ticker.C:
		return true, fn()
	default:
		return false, nil
	}
}

func (r *RateLimiter) getPerHostTicker(host string) *time.Ticker {
	r.mu.Lock()
	defer r.mu.Unlock()
	ticker, ok := r.perHost[host]
	if !ok {
		ticker = time.NewTicker(r.config.PerHostDelay)
		r.perHost[host] = ticker
	}
	return ticker
}

// StopAll stops all per-host tickers. Call when done.
func (r *RateLimiter) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ticker := range r.perHost {
		ticker.Stop()
	}
}
