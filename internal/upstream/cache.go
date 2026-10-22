package upstream

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bennypowers.dev/larry/internal/logger"
)

const (
	// cacheDirName is the name of the cache directory under XDG_CACHE_HOME.
	cacheDirName = "larry"
	// cacheSubdir is the subdirectory for version check cache.
	cacheSubdir = "version-check-cache"
	// defaultTTL is the default time-to-live for cache entries.
	defaultTTL = 6 * time.Hour
)

// VersionCache caches upstream version lookups with disk persistence.
// Cache entries are stored as files under $XDG_CACHE_HOME/larry/version-check-cache/.
type VersionCache struct {
	mu       sync.RWMutex
	dir      string
	ttl      time.Duration
	inMemory map[string]cachedEntry
}

type cachedEntry struct {
	version   string
	timestamp time.Time
}

// DefaultCachePath returns the default cache directory path.
// It uses XDG_CACHE_HOME/larry/version-check-cache if available,
// otherwise falls back to ~/.cache/larry/version-check-cache.
func DefaultCachePath() string {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, cacheDirName, cacheSubdir)
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".cache", cacheDirName, cacheSubdir)
	}
	return filepath.Join(u, ".cache", cacheDirName, cacheSubdir)
}

// NewVersionCacheWithPath creates a new cache with disk persistence.
// Entries are stored as files under dir/.
func NewVersionCacheWithPath(dir string, ttl time.Duration) *VersionCache {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	if dir == "" {
		dir = DefaultCachePath()
	}
	return &VersionCache{
		dir:      dir,
		ttl:      ttl,
		inMemory: make(map[string]cachedEntry),
	}
}

// keyToPath converts a cache key to a file path.
// Keys like "cat/pkg/pypi:1.0.0" become "cat_pkg_pypi_1.0.0".
func keyToPath(dir, key string) string {
	path := strings.ReplaceAll(key, "/", "_")
	path = strings.ReplaceAll(path, ":", "_")
	path = strings.ReplaceAll(path, " ", "_")
	return filepath.Join(dir, path)
}

// Get returns a cached version if it exists and is not stale.
// For disk-backed caches, checks file mtime against TTL.
func (c *VersionCache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Try in-memory first
	if entry, ok := c.inMemory[key]; ok {
		if time.Since(entry.timestamp) <= c.ttl {
			logger.Tracef("cache hit (memory): %s", key)
			return entry.version, true
		}
		// Expire stale in-memory entry
		delete(c.inMemory, key)
	}

	// Try disk
	if c.dir == "" {
		logger.Tracef("cache miss (no disk cache): %s", key)
		return "", false
	}

	path := keyToPath(c.dir, key)
	info, err := os.Stat(path)
	if err != nil {
		logger.Tracef("cache miss (not cached): %s", key)
		return "", false
	}

	// Check TTL based on file mtime
	if time.Since(info.ModTime()) > c.ttl {
		logger.Tracef("cache miss (stale): %s", key)
		return "", false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		logger.Tracef("cache miss (read error): %s: %v", key, err)
		return "", false
	}

	version := strings.TrimSpace(string(data))
	if version == "" {
		logger.Tracef("cache miss (empty entry): %s", key)
		return "", false
	}

	logger.Tracef("cache hit (disk): %s = %s", key, version)
	return version, true
}

// Set stores a version in the cache.
// For disk-backed caches, writes atomically via temp file + rename.
func (c *VersionCache) Set(key, version string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update in-memory cache
	c.inMemory[key] = cachedEntry{
		version:   version,
		timestamp: time.Now(),
	}
	logger.Tracef("cache set: %s = %s", key, version)

	// Write to disk if directory configured
	if c.dir == "" {
		return
	}

	// Ensure directory exists
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return
	}

	// Write atomically: write to temp file, then rename
	path := keyToPath(c.dir, key)
	tmpPath := path + ".tmp"

	// Write version to temp file
	if err := os.WriteFile(tmpPath, []byte(version+"\n"), 0644); err != nil {
		return
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		// Best-effort cleanup of temp file on atomic rename failure.
		_ = os.Remove(tmpPath)
		return
	}
}

// Clear removes all cached entries.
func (c *VersionCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.inMemory = make(map[string]cachedEntry)

	if c.dir == "" {
		return nil
	}

	return os.RemoveAll(c.dir)
}

// --- Cache key helpers ---

// cacheKeySchema invalidates entries whose meaning changed. Bump it whenever
// version resolution semantics change (v2: PEP 440 dev releases are filtered,
// GitHub tags strategy added).
const cacheKeySchema = "v2"

// MakeKey creates a cache key from category, package, provider type, and version.
func MakeKey(category, pkg, provider string, version string) string {
	return fmt.Sprintf("%s/%s/%s/%s:%s", cacheKeySchema, category, pkg, provider, version)
}
