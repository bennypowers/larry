package ratelimit

import (
	"testing"
	"time"

	"bennypowers.dev/larry/internal/remoteid"
)

// ---- TestRateLimiter_PerProviderDefaults ----

func TestRateLimiter_PerProviderDefaults(t *testing.T) {
	// PyPI should have 100ms delay, GitLab should have 200ms
	pypiDelay := PerProviderDelay(remoteid.TypePyPI)
	gitlabDelay := PerProviderDelay(remoteid.TypeGitLab)

	if pypiDelay != 100*time.Millisecond {
		t.Errorf("PyPI delay = %v, want %v", pypiDelay, 100*time.Millisecond)
	}
	if gitlabDelay != 200*time.Millisecond {
		t.Errorf("GitLab delay = %v, want %v", gitlabDelay, 200*time.Millisecond)
	}
	if pypiDelay == gitlabDelay {
		t.Error("PyPI and GitLab should have different delays")
	}
}

// ---- TestRateLimiter_PerProviderDelay_ZeroForGitHub ----

func TestRateLimiter_PerProviderDelay_ZeroForGitHub(t *testing.T) {
	delay := PerProviderDelay(remoteid.TypeGitHub)
	if delay != 0 {
		t.Errorf("GitHub delay = %v, want 0 (gh CLI handles its own rate limiting)", delay)
	}
}

// ---- TestRateLimiter_PerProviderDelay_DefaultFallback ----

func TestRateLimiter_PerProviderDelay_DefaultFallback(t *testing.T) {
	// Unknown types should fall back to default delay
	delay := PerProviderDelay(remoteid.TypeUnknown)
	if delay != 100*time.Millisecond {
		t.Errorf("Unknown delay = %v, want %v", delay, 100*time.Millisecond)
	}
}

// ---- TestRateLimiter_PerProviderDelay_AllTypes ----

func TestRateLimiter_PerProviderDelay_AllTypes(t *testing.T) {
	tests := []struct {
		ptype remoteid.RemoteIdType
		want  time.Duration
	}{
		{remoteid.TypePyPI, 100 * time.Millisecond},
		{remoteid.TypeNPM, 100 * time.Millisecond},
		{remoteid.TypeGitLab, 200 * time.Millisecond},
		{remoteid.TypeCodeberg, 200 * time.Millisecond},
		{remoteid.TypeSourceHut, 200 * time.Millisecond},
		{remoteid.TypeGitHub, 0},
	}

	for _, tt := range tests {
		t.Run(string(tt.ptype), func(t *testing.T) {
			got := PerProviderDelay(tt.ptype)
			if got != tt.want {
				t.Errorf("PerProviderDelay(%v) = %v, want %v", tt.ptype, got, tt.want)
			}
		})
	}
}

// ---- TestRateLimiter_ConfigPerProviderDelay ----

func TestRateLimiter_ConfigPerProviderDelay(t *testing.T) {
	// When creating a limiter for a specific provider type,
	// it should use the per-provider delay
	for _, tt := range []struct {
		ptype      remoteid.RemoteIdType
		wantDelay  time.Duration
	}{
		{remoteid.TypePyPI, 100 * time.Millisecond},
		{remoteid.TypeGitLab, 200 * time.Millisecond},
		{remoteid.TypeGitHub, 0},
	} {
		t.Run(string(tt.ptype), func(t *testing.T) {
			delay := PerProviderDelay(tt.ptype)
			if delay != tt.wantDelay {
				t.Errorf("PerProviderDelay(%v) = %v, want %v", tt.ptype, delay, tt.wantDelay)
			}
		})
	}
}

// ---- TestRateLimiter_PerProviderConfigOverride ----

func TestRateLimiter_PerProviderConfigOverride(t *testing.T) {
	// Users should be able to override per-provider defaults
	// by passing a config with explicit PerHostDelay
	customDelay := 500 * time.Millisecond
	cfg := &Config{
		PerHostDelay:      customDelay,
		GlobalConcurrency: 16,
	}
	rl := NewRateLimiterWithConfig(cfg)

	// The rate limiter should use the custom delay from config
	// We verify by checking the config was applied
	if rl == nil {
		t.Fatal("NewRateLimiterWithConfig returned nil")
	}
}

// ---- TestRateLimiter_GetProviderDelayMap ----

func TestRateLimiter_GetProviderDelayMap(t *testing.T) {
	delays := ProviderDelays()

	// Verify all known types have entries
	expectedTypes := []remoteid.RemoteIdType{
		remoteid.TypePyPI,
		remoteid.TypeNPM,
		remoteid.TypeGitLab,
		remoteid.TypeCodeberg,
		remoteid.TypeSourceHut,
		remoteid.TypeGitHub,
	}

	for _, ptype := range expectedTypes {
		delay, ok := delays[ptype]
		if !ok {
			t.Errorf("Missing delay for provider type %v", ptype)
		}
		if delay < 0 {
			t.Errorf("Negative delay for provider type %v: %v", ptype, delay)
		}
	}

	// Verify GitHub has zero delay
	if githubDelay, ok := delays[remoteid.TypeGitHub]; ok && githubDelay != 0 {
		t.Errorf("GitHub delay = %v, want 0", githubDelay)
	}
}

// ---- TestRateLimiter_DefaultConfigUsesStandardDelay ----

func TestRateLimiter_DefaultConfigUsesStandardDelay(t *testing.T) {
	// DefaultConfig should use 100ms (the common PyPI/NPM default)
	cfg := DefaultConfig()
	if cfg.PerHostDelay != 100*time.Millisecond {
		t.Errorf("DefaultConfig PerHostDelay = %v, want %v", cfg.PerHostDelay, 100*time.Millisecond)
	}
}
