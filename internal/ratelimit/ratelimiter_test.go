package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRateLimiter_PerHost(t *testing.T) {
	cfg := &Config{
		PerHostDelay:      50 * time.Millisecond,
		GlobalConcurrency: 100,
	}
	rl := NewRateLimiterWithConfig(cfg)
	defer rl.StopAll()

	var count int64
	host := "example.com"

	// Sequential requests to same host should be spaced by per-host delay
	start := time.Now()
	for i := 0; i < 5; i++ {
		if err := rl.Allow(context.Background(), host); err != nil {
			t.Fatal(err)
		}
		atomic.AddInt64(&count, 1)
	}
	elapsed := time.Since(start)

	// Should have taken at least 4 * 50ms = 200ms (first request doesn't wait)
	if elapsed < 180*time.Millisecond {
		t.Errorf("5 sequential requests took %v, expected >= 200ms", elapsed)
	}

	if got := atomic.LoadInt64(&count); got != 5 {
		t.Errorf("count = %d, want 5", got)
	}
}

func TestRateLimiter_GlobalSemaphore(t *testing.T) {
	cfg := &Config{
		PerHostDelay:      1 * time.Millisecond, // negligible
		GlobalConcurrency: 3,
	}
	rl := NewRateLimiterWithConfig(cfg)
	defer rl.StopAll()

	var concurrent int64
	var maxConcurrent int64
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			host := fmt.Sprintf("host%d.com", id%5)
			_ = rl.Do(context.Background(), host, func() error {
				// Simulate work while holding semaphore
				cur := atomic.AddInt64(&concurrent, 1)
				if cur > atomic.LoadInt64(&maxConcurrent) {
					atomic.StoreInt64(&maxConcurrent, cur)
				}
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt64(&concurrent, -1)
				return nil
			})
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt64(&maxConcurrent); got > 3 {
		t.Errorf("max concurrent = %d, expected <= 3", got)
	}
}

func TestRateLimiter_ResetOnWindow(t *testing.T) {
	cfg := &Config{
		PerHostDelay:      50 * time.Millisecond,
		GlobalConcurrency: 100,
	}
	rl := NewRateLimiterWithConfig(cfg)
	defer rl.StopAll()

	host := "example.com"

	// First request
	if err := rl.Allow(context.Background(), host); err != nil {
		t.Fatal(err)
	}

	// Second request should be delayed
	start := time.Now()
	if err := rl.Allow(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 30*time.Millisecond {
		t.Errorf("expected delay after per-host window, got %v", elapsed)
	}
}

func TestRateLimiter_DifferentHosts(t *testing.T) {
	cfg := &Config{
		PerHostDelay:      10 * time.Millisecond,
		GlobalConcurrency: 100,
	}
	rl := NewRateLimiterWithConfig(cfg)
	defer rl.StopAll()

	var count int64
	hosts := []string{"host1.com", "host2.com", "host3.com", "host4.com", "host5.com"}

	// Sequential calls to different hosts - each has its own ticker
	start := time.Now()
	for _, host := range hosts {
		if err := rl.Allow(context.Background(), host); err != nil {
			t.Fatal(err)
		}
		atomic.AddInt64(&count, 1)
	}
	elapsed := time.Since(start)

	// Different hosts each get their own ticker, so first request per host waits
	// 5 hosts * 10ms = 50ms minimum
	if elapsed < 40*time.Millisecond {
		t.Errorf("5 different hosts took %v, expected >= 40ms", elapsed)
	}
	if got := atomic.LoadInt64(&count); got != 5 {
		t.Errorf("count = %d, want 5", got)
	}
}

func TestRateLimiter_ContextCancellation(t *testing.T) {
	cfg := &Config{
		PerHostDelay:      1 * time.Second, // long delay to force context cancellation
		GlobalConcurrency: 100,
	}
	rl := NewRateLimiterWithConfig(cfg)
	defer rl.StopAll()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	host := "example.com"

	// This should fail due to context timeout
	err := rl.Allow(ctx, host)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestRetry_ExponentialBackoff(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.BaseDelay = 10 * time.Millisecond
	cfg.MaxDelay = 500 * time.Millisecond
	cfg.MaxRetries = 5

	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 3 {
			return fmt.Errorf("temporary error")
		}
		return nil
	}

	err := Retry(cfg, fn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestRetry_5xx(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.BaseDelay = 10 * time.Millisecond
	cfg.MaxRetries = 3

	attempts := 0
	fn := func() error {
		attempts++
		return fmt.Errorf("500 error")
	}

	err := Retry(cfg, fn)
	if err == nil {
		t.Error("expected error after retries")
	}
	if attempts != 4 { // 1 initial + 3 retries
		t.Errorf("attempts = %d, want 4", attempts)
	}
}

func TestRetry_Success(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.MaxRetries = 5

	attempts := 0
	fn := func() error {
		attempts++
		return nil
	}

	err := Retry(cfg, fn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (no retries)", attempts)
	}
}

func TestRetryHTTP_HaltsOn4xx(t *testing.T) {
	// Test that HTTP-aware Retry does NOT retry 4xx errors
	cfg := DefaultRetryConfig()
	cfg.BaseDelay = 5 * time.Millisecond
	cfg.MaxRetries = 5

	attempts := 0
	fn := func() (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 400, Body: http.NoBody}, nil
	}

	err := RetryHTTP(cfg, fn)
	// 4xx returns nil (no retry needed), caller handles the status
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// 4xx should NOT be retried — only 1 attempt
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (4xx should not retry)", attempts)
	}
}

func TestRetryHTTP_Retries5xx(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.BaseDelay = 5 * time.Millisecond
	cfg.MaxRetries = 3

	attempts := 0
	fn := func() (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 500, Body: http.NoBody}, nil
	}

	err := RetryHTTP(cfg, fn)
	if err == nil {
		t.Error("expected error after retries")
	}
	if attempts != 4 { // 1 initial + 3 retries
		t.Errorf("attempts = %d, want 4", attempts)
	}
}

func TestRetryHTTP_Retries429(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.BaseDelay = 5 * time.Millisecond
	cfg.MaxRetries = 3

	attempts := 0
	fn := func() (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 429, Body: http.NoBody}, nil
	}

	err := RetryHTTP(cfg, fn)
	if err == nil {
		t.Error("expected error after retries")
	}
	if attempts != 4 { // 1 initial + 3 retries
		t.Errorf("attempts = %d, want 4", attempts)
	}
}

func TestRetryHTTP_Success2xx(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.MaxRetries = 5

	attempts := 0
	fn := func() (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	}

	err := RetryHTTP(cfg, fn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (2xx should not retry)", attempts)
	}
}

func TestRateLimiter_Do(t *testing.T) {
	cfg := &Config{
		PerHostDelay:      1 * time.Millisecond,
		GlobalConcurrency: 100,
	}
	rl := NewRateLimiterWithConfig(cfg)
	defer rl.StopAll()

	called := false
	err := rl.Do(context.Background(), "example.com", func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("function was not called")
	}
}

func TestRateLimiter_DoError(t *testing.T) {
	cfg := &Config{
		PerHostDelay:      1 * time.Millisecond,
		GlobalConcurrency: 100,
	}
	rl := NewRateLimiterWithConfig(cfg)
	defer rl.StopAll()

	wantErr := fmt.Errorf("operation failed")
	err := rl.Do(context.Background(), "example.com", func() error {
		return wantErr
	})
	if err != wantErr {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

func TestRateLimiter_DoConcurrencyLimit(t *testing.T) {
	cfg := &Config{
		PerHostDelay:      1 * time.Millisecond,
		GlobalConcurrency: 2,
	}
	rl := NewRateLimiterWithConfig(cfg)
	defer rl.StopAll()

	var concurrent int64
	var maxConcurrent int64
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			host := "same-host.com"
			_ = rl.Do(context.Background(), host, func() error {
				cur := atomic.AddInt64(&concurrent, 1)
				if cur > atomic.LoadInt64(&maxConcurrent) {
					atomic.StoreInt64(&maxConcurrent, cur)
				}
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt64(&concurrent, -1)
				return nil
			})
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&maxConcurrent); got > 2 {
		t.Errorf("max concurrent = %d, expected <= 2", got)
	}
}

func TestHTTPClient_ParseRateHeaders(t *testing.T) {
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "42")
		w.Header().Set("X-RateLimit-Reset", "1700000000")
		w.Header().Set("X-RateLimit-Limit", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	receivedHeaders = resp.Header

	remaining := receivedHeaders.Get("X-RateLimit-Remaining")
	if remaining != "42" {
		t.Errorf("X-RateLimit-Remaining = %q, want %q", remaining, "42")
	}
	reset := receivedHeaders.Get("X-RateLimit-Reset")
	if reset != "1700000000" {
		t.Errorf("X-RateLimit-Reset = %q, want %q", reset, "1700000000")
	}
	limit := receivedHeaders.Get("X-RateLimit-Limit")
	if limit != "100" {
		t.Errorf("X-RateLimit-Limit = %q, want %q", limit, "100")
	}
}
