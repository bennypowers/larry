package providers

import (
	"fmt"

	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// PyPIProvider implements upstream version discovery for PyPI.
type PyPIProvider struct {
	RateLimiter *ratelimit.RateLimiter
}

func (p *PyPIProvider) Type() remoteid.RemoteIdType {
	return remoteid.TypePyPI
}

func (p *PyPIProvider) Metadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		Name:        "PyPI",
		Description: "Python Package Index",
		Homepage:    "https://pypi.org",
	}
}

func (p *PyPIProvider) Name() string {
	return "PyPI"
}

func (p *PyPIProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if rid.Name == "" {
		return nil, fmt.Errorf("pypi: missing package name in RemoteId")
	}
	// TODO: integrate with httpclient GET /pypi/<name>/json
	// For now, return a placeholder
	return nil, fmt.Errorf("pypi: not yet implemented - needs HTTP integration")
}

// IsPrerelease reports whether v is a pre-release version.
// Delegates to version.Version.IsPrerelease().
func (p *PyPIProvider) IsPrerelease(v *version.Version) bool {
	return v.IsPrerelease()
}

// NewPyPIProvider creates a new PyPIProvider with the given rate limiter.
func NewPyPIProvider(rl *ratelimit.RateLimiter) provider.UpstreamProvider {
	return &PyPIProvider{RateLimiter: rl}
}
