package providers

import (
	"fmt"

	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// NpmProvider implements upstream version discovery for npm.
type NpmProvider struct {
	RateLimiter *ratelimit.RateLimiter
}

func (p *NpmProvider) Type() remoteid.RemoteIdType {
	return remoteid.TypeNPM
}

func (p *NpmProvider) Metadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		Name:        "npm",
		Description: "Node Package Manager",
		Homepage:    "https://npmjs.com",
	}
}

func (p *NpmProvider) Name() string {
	return "npm"
}

func (p *NpmProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if rid.Name == "" {
		return nil, fmt.Errorf("npm: missing package name in RemoteId")
	}
	// TODO: integrate with httpclient GET https://registry.npmjs.org/<name>/latest
	// For now, return a placeholder
	return nil, fmt.Errorf("npm: not yet implemented - needs HTTP integration")
}

// IsPrerelease reports whether v is a pre-release version.
// Delegates to version.Version.IsPrerelease().
func (p *NpmProvider) IsPrerelease(v *version.Version) bool {
	return v.IsPrerelease()
}

// NewNpmProvider creates a new NpmProvider with the given rate limiter.
func NewNpmProvider(rl *ratelimit.RateLimiter) provider.UpstreamProvider {
	return &NpmProvider{RateLimiter: rl}
}
