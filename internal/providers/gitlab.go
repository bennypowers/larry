package providers

import (
	"fmt"

	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// GitLabProvider implements upstream version discovery for GitLab.
type GitLabProvider struct {
	RateLimiter *ratelimit.RateLimiter
}

func (p *GitLabProvider) Type() remoteid.RemoteIdType {
	return remoteid.TypeGitLab
}

func (p *GitLabProvider) Metadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		Name:        "GitLab",
		Description: "GitLab releases and tags",
		Homepage:    "https://gitlab.com",
	}
}

func (p *GitLabProvider) Name() string {
	return "GitLab"
}

func (p *GitLabProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if rid.Host == "" {
		return nil, fmt.Errorf("gitlab: missing host in RemoteId")
	}
	// TODO: integrate with httpclient GET https://gitlab.com/api/v4/projects/:id/releases
	// For now, return a placeholder
	return nil, fmt.Errorf("gitlab: not yet implemented - needs HTTP integration")
}

// IsPrerelease reports whether v is a pre-release version.
// Delegates to version.Version.IsPrerelease().
func (p *GitLabProvider) IsPrerelease(v *version.Version) bool {
	return v.IsPrerelease()
}

// NewGitLabProvider creates a new GitLabProvider with the given rate limiter.
func NewGitLabProvider(rl *ratelimit.RateLimiter) provider.UpstreamProvider {
	return &GitLabProvider{RateLimiter: rl}
}
