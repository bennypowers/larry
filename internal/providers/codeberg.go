package providers

import (
	"fmt"

	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// CodebergProvider implements upstream version discovery for Codeberg.
type CodebergProvider struct {
	RateLimiter *ratelimit.RateLimiter
}

func (p *CodebergProvider) Type() remoteid.RemoteIdType {
	return remoteid.TypeCodeberg
}

func (p *CodebergProvider) Metadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		Name:        "Codeberg",
		Description: "Codeberg git hosting",
		Homepage:    "https://codeberg.org",
	}
}

func (p *CodebergProvider) Name() string {
	return "Codeberg"
}

func (p *CodebergProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if rid.Owner == "" || rid.Repo == "" {
		return nil, fmt.Errorf("codeberg: missing owner/repo in RemoteId")
	}
	// TODO: integrate with httpclient GET https://codeberg.org/api/v1/repos/:owner/:repo/releases
	// For now, return a placeholder
	return nil, fmt.Errorf("codeberg: not yet implemented - needs HTTP integration")
}

// IsPrerelease reports whether v is a pre-release version.
// Delegates to version.Version.IsPrerelease().
func (p *CodebergProvider) IsPrerelease(v *version.Version) bool {
	return v.IsPrerelease()
}

// NewCodebergProvider creates a new CodebergProvider with the given rate limiter.
func NewCodebergProvider(rl *ratelimit.RateLimiter) provider.UpstreamProvider {
	return &CodebergProvider{RateLimiter: rl}
}
