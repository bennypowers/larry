package provider

import (
	"testing"

	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// stubProvider implements UpstreamProvider for testing.
type stubProvider struct {
	t  remoteid.RemoteIdType
	md ProviderMetadata
}

func (s *stubProvider) Type() remoteid.RemoteIdType          { return s.t }
func (s *stubProvider) Metadata() ProviderMetadata           { return s.md }
func (s *stubProvider) FetchLatestVersion(remoteid.RemoteId, bool) (*version.Version, error) {
	return nil, nil
}
func (s *stubProvider) Name() string                         { return s.md.Name }
func (s *stubProvider) IsPrerelease(v *version.Version) bool { return v.IsPrerelease() }

func TestProviderRegistry_RegisterResolve(t *testing.T) {
	reg := NewProviderRegistry()

	p := &stubProvider{
		t: remoteid.TypeGitHub,
		md: ProviderMetadata{Name: "GitHub"},
	}

	reg.Register(p)

	resolved, err := reg.Resolve(remoteid.RemoteId{Type: remoteid.TypeGitHub})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved.Type() != remoteid.TypeGitHub {
		t.Errorf("resolved.Type() = %v, want %v", resolved.Type(), remoteid.TypeGitHub)
	}
}

func TestProviderRegistry_NotFound(t *testing.T) {
	reg := NewProviderRegistry()

	_, err := reg.Resolve(remoteid.RemoteId{Type: remoteid.TypeGitHub})
	if err == nil {
		t.Error("Resolve() should error for unregistered provider")
	}
}

func TestResolveProvider(t *testing.T) {
	reg := NewProviderRegistry()

	p := &stubProvider{
		t: remoteid.TypePyPI,
		md: ProviderMetadata{Name: "PyPI"},
	}
	reg.Register(p)

	resolved, err := ResolveProvider(remoteid.RemoteId{Type: remoteid.TypePyPI}, reg)
	if err != nil {
		t.Fatalf("ResolveProvider() error = %v", err)
	}

	if resolved.Metadata().Name != "PyPI" {
		t.Errorf("Metadata().Name = %q, want %q", resolved.Metadata().Name, "PyPI")
	}
}

func TestProviderRegistry_Resolve_GitHub(t *testing.T) {
	reg := NewProviderRegistryWithRateLimiter(ratelimit.NewRateLimiter())

	// Register factory for GitHub
	reg.RegisterFactory(remoteid.TypeGitHub, func(rl *ratelimit.RateLimiter) UpstreamProvider {
		return &stubProvider{
			t: remoteid.TypeGitHub,
			md: ProviderMetadata{Name: "GitHub"},
		}
	})

	resolved, err := reg.Resolve(remoteid.RemoteId{Type: remoteid.TypeGitHub})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved.Type() != remoteid.TypeGitHub {
		t.Errorf("resolved.Type() = %v, want %v", resolved.Type(), remoteid.TypeGitHub)
	}
	if resolved.Name() != "GitHub" {
		t.Errorf("Name() = %q, want %q", resolved.Name(), "GitHub")
	}
}

func TestProviderRegistry_Resolve_PyPI(t *testing.T) {
	reg := NewProviderRegistryWithRateLimiter(ratelimit.NewRateLimiter())

	reg.RegisterFactory(remoteid.TypePyPI, func(rl *ratelimit.RateLimiter) UpstreamProvider {
		return &stubProvider{
			t: remoteid.TypePyPI,
			md: ProviderMetadata{Name: "PyPI"},
		}
	})

	resolved, err := reg.Resolve(remoteid.RemoteId{Type: remoteid.TypePyPI})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved.Type() != remoteid.TypePyPI {
		t.Errorf("resolved.Type() = %v, want %v", resolved.Type(), remoteid.TypePyPI)
	}
	if resolved.Name() != "PyPI" {
		t.Errorf("Name() = %q, want %q", resolved.Name(), "PyPI")
	}
}

func TestProviderRegistry_Resolve_NPM(t *testing.T) {
	reg := NewProviderRegistryWithRateLimiter(ratelimit.NewRateLimiter())

	reg.RegisterFactory(remoteid.TypeNPM, func(rl *ratelimit.RateLimiter) UpstreamProvider {
		return &stubProvider{
			t: remoteid.TypeNPM,
			md: ProviderMetadata{Name: "NPM"},
		}
	})

	resolved, err := reg.Resolve(remoteid.RemoteId{Type: remoteid.TypeNPM})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved.Type() != remoteid.TypeNPM {
		t.Errorf("resolved.Type() = %v, want %v", resolved.Type(), remoteid.TypeNPM)
	}
}

func TestProviderRegistry_Resolve_GitLab(t *testing.T) {
	reg := NewProviderRegistryWithRateLimiter(ratelimit.NewRateLimiter())

	reg.RegisterFactory(remoteid.TypeGitLab, func(rl *ratelimit.RateLimiter) UpstreamProvider {
		return &stubProvider{
			t: remoteid.TypeGitLab,
			md: ProviderMetadata{Name: "GitLab"},
		}
	})

	resolved, err := reg.Resolve(remoteid.RemoteId{Type: remoteid.TypeGitLab})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved.Type() != remoteid.TypeGitLab {
		t.Errorf("resolved.Type() = %v, want %v", resolved.Type(), remoteid.TypeGitLab)
	}
}

func TestProviderRegistry_Resolve_Codeberg(t *testing.T) {
	reg := NewProviderRegistryWithRateLimiter(ratelimit.NewRateLimiter())

	reg.RegisterFactory(remoteid.TypeCodeberg, func(rl *ratelimit.RateLimiter) UpstreamProvider {
		return &stubProvider{
			t: remoteid.TypeCodeberg,
			md: ProviderMetadata{Name: "Codeberg"},
		}
	})

	resolved, err := reg.Resolve(remoteid.RemoteId{Type: remoteid.TypeCodeberg})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved.Type() != remoteid.TypeCodeberg {
		t.Errorf("resolved.Type() = %v, want %v", resolved.Type(), remoteid.TypeCodeberg)
	}
}

func TestProviderRegistry_Resolve_SourceHut(t *testing.T) {
	reg := NewProviderRegistryWithRateLimiter(ratelimit.NewRateLimiter())

	reg.RegisterFactory(remoteid.TypeSourceHut, func(rl *ratelimit.RateLimiter) UpstreamProvider {
		return &stubProvider{
			t: remoteid.TypeSourceHut,
			md: ProviderMetadata{Name: "SourceHut"},
		}
	})

	resolved, err := reg.Resolve(remoteid.RemoteId{Type: remoteid.TypeSourceHut})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved.Type() != remoteid.TypeSourceHut {
		t.Errorf("resolved.Type() = %v, want %v", resolved.Type(), remoteid.TypeSourceHut)
	}
}

func TestProviderRegistry_NoMatch(t *testing.T) {
	reg := NewProviderRegistry()

	_, err := reg.Resolve(remoteid.RemoteId{Type: "unknown-type"})
	if err == nil {
		t.Error("Resolve() should error for unknown provider type")
	}
}

func TestProviderRegistry_WithRateLimiter(t *testing.T) {
	rl := ratelimit.NewRateLimiter()
	reg := NewProviderRegistryWithRateLimiter(rl)

	reg.RegisterFactory(remoteid.TypeGitHub, func(rl *ratelimit.RateLimiter) UpstreamProvider {
		// Verify the rate limiter is passed to the factory
		if rl == nil {
			t.Fatal("RateLimiter should not be nil")
		}
		return &stubProvider{
			t: remoteid.TypeGitHub,
			md: ProviderMetadata{Name: "GitHub"},
		}
	})

	resolved, err := reg.Resolve(remoteid.RemoteId{Type: remoteid.TypeGitHub})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if resolved.Name() != "GitHub" {
		t.Errorf("Name() = %q, want %q", resolved.Name(), "GitHub")
	}
}

func TestProviderRegistry_ResolveFactory(t *testing.T) {
	reg := NewProviderRegistry()

	reg.RegisterFactory(remoteid.TypePyPI, func(rl *ratelimit.RateLimiter) UpstreamProvider {
		return &stubProvider{
			t: remoteid.TypePyPI,
			md: ProviderMetadata{Name: "PyPI"},
		}
	})

	factory, err := reg.ResolveFactory(remoteid.TypePyPI)
	if err != nil {
		t.Fatalf("ResolveFactory() error = %v", err)
	}

	if factory == nil {
		t.Fatal("factory should not be nil")
	}

	// Verify factory creates provider correctly
	p := factory(ratelimit.NewRateLimiter())
	if p.Type() != remoteid.TypePyPI {
		t.Errorf("factory provider type = %v, want %v", p.Type(), remoteid.TypePyPI)
	}
}

func TestProviderRegistry_ResolveFactory_NotFound(t *testing.T) {
	reg := NewProviderRegistry()

	_, err := reg.ResolveFactory(remoteid.TypeGitHub)
	if err == nil {
		t.Error("ResolveFactory() should error for unregistered factory")
	}
}
