package upstream

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCache_Miss(t *testing.T) {
	cache := NewVersionCache()
	_, ok := cache.Get("nonexistent")
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestCache_Hit(t *testing.T) {
	cache := NewVersionCache()
	cache.Set("cat/pkg/pypi:1.0.0", "2.0.0")
	version, ok := cache.Get("cat/pkg/pypi:1.0.0")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if version != "2.0.0" {
		t.Fatalf("expected 2.0.0, got %s", version)
	}
}

func TestCache_TTL6h(t *testing.T) {
	dir := t.TempDir()
	cache := NewVersionCacheWithPath(dir, 6*time.Hour)

	// Write a file with an old timestamp directly (no Set to avoid in-memory entry)
	path := filepath.Join(dir, "cat_pkg_pypi_1.0.0")
	data := []byte("2.0.0\n")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	// Modify the mtime to be >6h ago
	oldTime := time.Now().Add(-7 * time.Hour)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// Should be stale now
	_, ok := cache.Get("cat/pkg/pypi:1.0.0")
	if ok {
		t.Fatal("expected cache miss after TTL")
	}

	// Also test fresh file works
	freshPath := filepath.Join(dir, "cat_pkg_npm_1.0.0")
	if err := os.WriteFile(freshPath, []byte("1.5.0\n"), 0644); err != nil {
		t.Fatalf("WriteFile(fresh): %v", err)
	}
	version, ok := cache.Get("cat/pkg/npm:1.0.0")
	if !ok {
		t.Fatal("expected cache hit for fresh file")
	}
	if version != "1.5.0" {
		t.Fatalf("expected 1.5.0, got %s", version)
	}
}

func TestCache_DiskPersistence(t *testing.T) {
	dir := t.TempDir()
	cache := NewVersionCacheWithPath(dir, 6*time.Hour)

	// Set value
	cache.Set("cat/pkg/npm:0.1.0", "1.5.0")

	// Create a new cache pointing to the same dir
	cache2 := NewVersionCacheWithPath(dir, 6*time.Hour)

	// Should be persisted
	version, ok := cache2.Get("cat/pkg/npm:0.1.0")
	if !ok {
		t.Fatal("expected persisted cache entry")
	}
	if version != "1.5.0" {
		t.Fatalf("expected 1.5.0, got %s", version)
	}
}

func TestCache_KeyFormat(t *testing.T) {
	dir := t.TempDir()
	cache := NewVersionCacheWithPath(dir, 6*time.Hour)

	// Various key formats should all produce valid file paths
	keys := []string{
		"cat/pkg/pypi:1.0.0",
		"dev-python/requests/github:owner/repo",
		"app-admin/foo/codeberg:owner/repo",
		"@scope/pkg/npm:1.0.0",
	}

	for _, key := range keys {
		cache.Set(key, "1.0.0")
		// Should not panic
		_, _ = cache.Get(key)
	}
}

func TestCache_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	cache := NewVersionCacheWithPath(dir, 6*time.Hour)

	// Set value twice with different content
	cache.Set("cat/pkg/pypi:1.0.0", "1.0.0")
	cache.Set("cat/pkg/pypi:1.0.0", "2.0.0")

	// Should have the latest value
	version, ok := cache.Get("cat/pkg/pypi:1.0.0")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if version != "2.0.0" {
		t.Fatalf("expected 2.0.0, got %s", version)
	}
}

func TestCache_XDGPath(t *testing.T) {
	// Default path should use XDG_CACHE_HOME or ~/.cache/larry/version-check-cache
	path := DefaultCachePath()
	if path == "" {
		t.Fatal("default cache path should not be empty")
	}
	// Should contain the expected directory name
	if !stringsContains(path, "larry") {
		t.Fatalf("default cache path should reference larry, got: %s", path)
	}
}

func stringsContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && stringsContainsAt(s, substr))
}

func stringsContainsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
