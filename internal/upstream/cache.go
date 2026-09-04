package upstream

import (
	"sync"
	"time"
)

// VersionCache caches upstream version lookups.
type VersionCache struct {
	mu    sync.RWMutex
	cache map[string]cachedEntry
}

type cachedEntry struct {
	version   string
	timestamp time.Time
}

// NewVersionCache creates a new cache.
func NewVersionCache() *VersionCache {
	return &VersionCache{
		cache: make(map[string]cachedEntry),
	}
}

// Get returns a cached version if it exists and is not stale.
func (c *VersionCache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.cache[key]
	if !ok || time.Since(entry.timestamp) > 24*time.Hour {
		return "", false
	}
	return entry.version, true
}

// Set stores a version in the cache.
func (c *VersionCache) Set(key, version string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = cachedEntry{
		version:   version,
		timestamp: time.Now(),
	}
}
