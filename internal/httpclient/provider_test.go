package httpclient

import (
	"net/http"
	"testing"
	"time"

	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/ratelimit"
)

// ---- TestNewClientForProvider_GitHub ----

func TestNewClientForProvider_GitHub(t *testing.T) {
	c := NewClientForProvider(ratelimit.NewRateLimiter(), remoteid.TypeGitHub)
	if c == nil {
		t.Fatal("NewClientForProvider returned nil")
	}
	if c.http == nil {
		t.Fatal("HTTP client is nil")
	}
	if c.http.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want %v", c.http.Timeout, 30*time.Second)
	}
	if c.rateLimiter == nil {
		t.Error("rateLimiter is nil")
	}
	if c.config == nil {
		t.Error("config is nil")
	}
}

// ---- TestNewClientForProvider_PyPI ----

func TestNewClientForProvider_PyPI(t *testing.T) {
	c := NewClientForProvider(ratelimit.NewRateLimiter(), remoteid.TypePyPI)
	if c == nil {
		t.Fatal("NewClientForProvider returned nil")
	}
	if c.rateLimiter == nil {
		t.Error("rateLimiter is nil")
	}
}

// ---- TestNewClientForProvider_GitLab ----

func TestNewClientForProvider_GitLab(t *testing.T) {
	c := NewClientForProvider(ratelimit.NewRateLimiter(), remoteid.TypeGitLab)
	if c == nil {
		t.Fatal("NewClientForProvider returned nil")
	}
	if c.rateLimiter == nil {
		t.Error("rateLimiter is nil")
	}
}

// ---- TestNewClientForProvider_AllTypes ----

func TestNewClientForProvider_AllTypes(t *testing.T) {
	types := []remoteid.RemoteIdType{
		remoteid.TypeGitHub,
		remoteid.TypePyPI,
		remoteid.TypeNPM,
		remoteid.TypeGitLab,
		remoteid.TypeCodeberg,
		remoteid.TypeSourceHut,
	}

	for _, rtype := range types {
		t.Run(string(rtype), func(t *testing.T) {
			c := NewClientForProvider(ratelimit.NewRateLimiter(), rtype)
			if c == nil {
				t.Fatalf("NewClientForProvider(%v) returned nil", rtype)
			}
			if c.http == nil {
				t.Errorf("NewClientForProvider(%v) HTTP client is nil", rtype)
			}
			if c.rateLimiter == nil {
				t.Errorf("NewClientForProvider(%v) rateLimiter is nil", rtype)
			}
			if c.config == nil {
				t.Errorf("NewClientForProvider(%v) config is nil", rtype)
			}
		})
	}
}

// ---- TestNewClientForProvider_UsesSharedLimiter ----

func TestNewClientForProvider_UsesSharedLimiter(t *testing.T) {
	shared := ratelimit.NewRateLimiter()
	c := NewClientForProvider(shared, remoteid.TypePyPI)

	if c.rateLimiter != shared {
		t.Error("NewClientForProvider should use the shared rate limiter, not create a new one")
	}
}

// ---- TestNewClientForProvider_TimeoutDefault ----

func TestNewClientForProvider_TimeoutDefault(t *testing.T) {
	c := NewClientForProvider(ratelimit.NewRateLimiter(), remoteid.TypeGitLab)
	if c.http.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want %v", c.http.Timeout, 30*time.Second)
	}
}

// ---- TestNewClientForProvider_TransportDefaults ----

func TestNewClientForProvider_TransportDefaults(t *testing.T) {
	c := NewClientForProvider(ratelimit.NewRateLimiter(), remoteid.TypePyPI)

	transport, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatal("Transport is not *http.Transport")
	}

	if transport.MaxIdleConns != 100 {
		t.Errorf("MaxIdleConns = %d, want %d", transport.MaxIdleConns, 100)
	}
	if transport.MaxIdleConnsPerHost != 10 {
		t.Errorf("MaxIdleConnsPerHost = %d, want %d", transport.MaxIdleConnsPerHost, 10)
	}
	if transport.IdleConnTimeout != 90*time.Second {
		t.Errorf("IdleConnTimeout = %v, want %v", transport.IdleConnTimeout, 90*time.Second)
	}
}
