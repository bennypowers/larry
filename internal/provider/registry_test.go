package provider

import (
	"bennypowers.dev/larry/internal/remoteid"
	"testing"
)

// stubProvider implements UpstreamProvider for testing.
type stubProvider struct {
	t  remoteid.RemoteIdType
	md ProviderMetadata
}

func (s *stubProvider) Type() remoteid.RemoteIdType  { return s.t }
func (s *stubProvider) Metadata() ProviderMetadata   { return s.md }
func (s *stubProvider) FindLatest(remoteid.RemoteId) (string, error) { return "", nil }
func (s *stubProvider) CheckVersion(remoteid.RemoteId, string) (bool, error) { return false, nil }

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
