package providers

import (
	"fmt"

	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// GitHubProvider implements upstream version discovery for GitHub.
// Uses the gh CLI for all GitHub API interaction.
type GitHubProvider struct {
	RateLimiter *ratelimit.RateLimiter
}

func (p *GitHubProvider) Type() remoteid.RemoteIdType {
	return remoteid.TypeGitHub
}

func (p *GitHubProvider) Metadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		Name:        "GitHub",
		Description: "GitHub releases and tags",
		Homepage:    "https://github.com",
	}
}

func (p *GitHubProvider) Name() string {
	return "GitHub"
}

func (p *GitHubProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if rid.Owner == "" || rid.Repo == "" {
		return nil, fmt.Errorf("github: missing owner/repo in RemoteId")
	}
	// TODO: integrate with gh CLI wrapper (internal/github)
	// For now, return a placeholder
	return nil, fmt.Errorf("github: not yet implemented - needs gh CLI integration")
}

// IsPrerelease reports whether v is a pre-release version.
// Delegates to version.Version.IsPrerelease().
func (p *GitHubProvider) IsPrerelease(v *version.Version) bool {
	return v.IsPrerelease()
}

// NewGitHubProvider creates a new GitHubProvider with the given rate limiter.
func NewGitHubProvider(rl *ratelimit.RateLimiter) provider.UpstreamProvider {
	return &GitHubProvider{RateLimiter: rl}
}
