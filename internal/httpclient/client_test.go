package httpclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bennypowers.dev/larry/internal/ratelimit"
)

// helper: build an http.Response with properly canonicalized headers.
func makeResponse(withRateLimitHeaders bool) *http.Response {
	hdr := http.Header{}
	if withRateLimitHeaders {
		hdr.Set("X-RateLimit-Remaining", "42")
		hdr.Set("X-RateLimit-Reset", "1609459200")
		hdr.Set("X-RateLimit-Limit", "100")
		hdr.Set("Retry-After", "30")
	}
	return &http.Response{Header: hdr}
}

// ---- ParseRateHeaders Tests ----

func TestParseRateHeaders(t *testing.T) {
	resp := makeResponse(true)

	headers := ParseRateHeaders(resp)
	if len(headers) != 4 {
		t.Fatalf("expected 4 headers, got %d: %v", len(headers), headers)
	}
	if headers["X-RateLimit-Remaining"] != "42" {
		t.Errorf("X-RateLimit-Remaining = %q, want 42", headers["X-RateLimit-Remaining"])
	}
	if headers["X-RateLimit-Reset"] != "1609459200" {
		t.Errorf("X-RateLimit-Reset = %q, want 1609459200", headers["X-RateLimit-Reset"])
	}
	if headers["X-RateLimit-Limit"] != "100" {
		t.Errorf("X-RateLimit-Limit = %q, want 100", headers["X-RateLimit-Limit"])
	}
	if headers["Retry-After"] != "30" {
		t.Errorf("Retry-After = %q, want 30", headers["Retry-After"])
	}
}

func TestParseRateHeaders_NilResponse(t *testing.T) {
	result := ParseRateHeaders(nil)
	if result != nil {
		t.Errorf("expected nil for nil response, got %v", result)
	}
}

func TestParseRateHeaders_EmptyHeaders(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	headers := ParseRateHeaders(resp)
	if len(headers) != 0 {
		t.Errorf("expected 0 headers, got %d", len(headers))
	}
}

func TestParseRateHeaders_PartialHeaders(t *testing.T) {
	hdr := http.Header{}
	hdr.Set("X-RateLimit-Remaining", "5")
	resp := &http.Response{Header: hdr}

	headers := ParseRateHeaders(resp)
	if len(headers) != 1 {
		t.Fatalf("expected 1 header, got %d: %v", len(headers), headers)
	}
	if headers["X-RateLimit-Remaining"] != "5" {
		t.Errorf("X-RateLimit-Remaining = %q, want 5", headers["X-RateLimit-Remaining"])
	}
}

func TestParseRateHeaders_FromServer(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("X-RateLimit-Remaining", "99")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusOK)
		// httptest writer; a write error cannot occur here.
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !called {
		t.Fatal("handler was not called")
	}

	headers := ParseRateHeaders(resp)
	if len(headers) != 3 {
		t.Fatalf("expected 3 headers, got %d: %v", len(headers), headers)
	}
	if headers["X-RateLimit-Remaining"] != "99" {
		t.Errorf("X-RateLimit-Remaining = %q, want 99", headers["X-RateLimit-Remaining"])
	}
	if headers["X-RateLimit-Limit"] != "5000" {
		t.Errorf("X-RateLimit-Limit = %q, want 5000", headers["X-RateLimit-Limit"])
	}
	if headers["Retry-After"] != "60" {
		t.Errorf("Retry-After = %q, want 60", headers["Retry-After"])
	}
}

func TestParseRateHeaders_IgnoreNonRateLimit(t *testing.T) {
	hdr := http.Header{}
	hdr.Set("Content-Type", "application/json")
	hdr.Set("Date", "Mon, 01 Jan 2024 00:00:00 GMT")
	hdr.Set("Server", "nginx")
	hdr.Set("X-RateLimit-Remaining", "1")

	resp := &http.Response{Header: hdr}

	headers := ParseRateHeaders(resp)
	if len(headers) != 1 {
		t.Fatalf("expected 1 header, got %d: %v", len(headers), headers)
	}
	if _, ok := headers["Content-Type"]; ok {
		t.Error("Content-Type should not be included")
	}
	if _, ok := headers["Date"]; ok {
		t.Error("Date should not be included")
	}
	if headers["X-RateLimit-Remaining"] != "1" {
		t.Errorf("X-RateLimit-Remaining = %q, want 1", headers["X-RateLimit-Remaining"])
	}
}

func TestParseRateHeaders_CaseInsensitive(t *testing.T) {
	// http.Header.Set() canonicalizes keys; verify we find them.
	hdr := http.Header{}
	hdr.Set("x-ratelimit-remaining", "10") // Set() canonicalizes to X-Ratelimit-Remaining
	hdr.Set("x-rateLimit-limit", "20")    // Set() canonicalizes

	resp := &http.Response{Header: hdr}

	headers := ParseRateHeaders(resp)
	if len(headers) != 2 {
		t.Fatalf("expected 2 headers, got %d: %v", len(headers), headers)
	}
	if headers["X-RateLimit-Remaining"] != "10" {
		t.Errorf("X-RateLimit-Remaining = %q, want 10", headers["X-RateLimit-Remaining"])
	}
	if headers["X-RateLimit-Limit"] != "20" {
		t.Errorf("X-RateLimit-Limit = %q, want 20", headers["X-RateLimit-Limit"])
	}
}

func TestParseRateHeaders_EmptyValueSkipped(t *testing.T) {
	hdr := http.Header{}
	hdr.Set("X-RateLimit-Limit", "100")
	// Deliberately do NOT set X-RateLimit-Remaining (empty)
	hdr.Set("Content-Type", "application/json")

	resp := &http.Response{Header: hdr}

	headers := ParseRateHeaders(resp)
	if len(headers) != 1 {
		t.Fatalf("expected 1 header, got %d: %v", len(headers), headers)
	}
	if _, ok := headers["X-RateLimit-Remaining"]; ok {
		t.Error("empty X-RateLimit-Remaining should be skipped")
	}
	if headers["X-RateLimit-Limit"] != "100" {
		t.Errorf("X-RateLimit-Limit = %q, want 100", headers["X-RateLimit-Limit"])
	}
}

// ---- NewClientWithTransport Tests ----

func TestNewClientWithTransport(t *testing.T) {
	transport := &http.Transport{}
	client := NewClientWithTransport(transport)

	if client == nil {
		t.Fatal("NewClientWithTransport returned nil")
	}
	if client.http == nil {
		t.Fatal("HTTP client is nil")
	}
	if client.rateLimiter == nil {
		t.Fatal("rateLimiter is nil")
	}
	if client.config == nil {
		t.Fatal("config is nil")
	}
	if client.http.Transport != transport {
		t.Error("custom transport not set")
	}
}

func TestNewClientWithTransport_DefaultLimiter(t *testing.T) {
	client := NewClientWithTransport(nil)
	if client.rateLimiter == nil {
		t.Fatal("expected default rateLimiter")
	}
}

func TestNewClientWithTransport_DefaultTransport(t *testing.T) {
	client := NewClientWithTransport(nil)
	if client.http.Transport == nil {
		t.Fatal("default transport not set")
	}
	if client.http.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", client.http.Timeout)
	}
}

func TestNewClientWithLimiterAndTransport(t *testing.T) {
	rl := ratelimit.NewRateLimiter()
	transport := &http.Transport{}
	client := NewClientWithLimiterAndTransport(rl, transport)

	if client == nil {
		t.Fatal("NewClientWithLimiterAndTransport returned nil")
	}
	if client.rateLimiter != rl {
		t.Error("should use the provided rate limiter")
	}
	if client.http.Transport != transport {
		t.Error("should use the provided transport")
	}
}

// ---- GetWithRetry via httptest.Server ----

func TestGetWithRetry_Success(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
		// httptest writer; a write error cannot occur here.
		_, _ = w.Write([]byte(`{"version":"1.0.0"}`))
	}))
	defer srv.Close()

	client := NewClientWithTransport(nil)
	resp, err := client.GetWithRetry(srv.URL)
	if err != nil {
		t.Fatalf("GetWithRetry failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if called != 1 {
		t.Errorf("expected 1 request, got %d", called)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestGetWithRetry_Retries5xx(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if called < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		// httptest writer; a write error cannot occur here.
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	client := NewClientWithTransport(nil)
	resp, err := client.GetWithRetry(srv.URL)
	if err != nil {
		t.Fatalf("GetWithRetry failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if called != 3 {
		t.Errorf("expected 3 requests (2 retries + 1 success), got %d", called)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestGetWithRetry_NoRetryOn4xx(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusBadRequest)
		// httptest writer; a write error cannot occur here.
		_, _ = w.Write([]byte(`bad request`))
	}))
	defer srv.Close()

	client := NewClientWithTransport(nil)
	resp, err := client.GetWithRetry(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if called != 1 {
		t.Errorf("expected 1 request (no retry on 4xx), got %d", called)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestGetWithRetry_ExhaustsRetries(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClientWithTransport(nil)
	_, err := client.GetWithRetry(srv.URL)
	if err == nil {
		t.Fatal("expected error after retries")
	}
	expectedCalls := ratelimit.DefaultRetryConfig().MaxRetries + 1
	if called != expectedCalls {
		t.Errorf("expected %d requests, got %d", expectedCalls, called)
	}
}

func TestGetWithRetry_Retries429(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if called < 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		// httptest writer; a write error cannot occur here.
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	client := NewClientWithTransport(nil)
	resp, err := client.GetWithRetry(srv.URL)
	if err != nil {
		t.Fatalf("GetWithRetry failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if called != 2 {
		t.Errorf("expected 2 requests, got %d", called)
	}
}

func TestGetWithRetry_ServerErrorAfter429(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClientWithTransport(nil)
	_, err := client.GetWithRetry(srv.URL)
	if err == nil {
		t.Fatal("expected error after all retries")
	}

	expectedCalls := ratelimit.DefaultRetryConfig().MaxRetries + 1
	if called != expectedCalls {
		t.Errorf("expected %d requests, got %d", expectedCalls, called)
	}
}

// ---- extractHost Tests ----

func TestExtractHost(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://api.example.com/v1/packages", "api.example.com"},
		{"https://api.example.com", "api.example.com"},
		{"http://localhost:8080/test", "localhost:8080"},
		{"https://pypi.org/pypi/requests/json", "pypi.org"},
		{"https://registry.npmjs.org/lodash", "registry.npmjs.org"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractHost(tt.url)
			if got != tt.want {
				t.Errorf("extractHost(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
