package providers

import (
	"fmt"

	"bennypowers.dev/larry/internal/github"
	"bennypowers.dev/larry/internal/git"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// GitHubProvider implements upstream version discovery for GitHub.
// Uses the gh CLI for all GitHub API interaction.
type GitHubProvider struct {
	RateLimiter *ratelimit.RateLimiter
	Client      *github.Client
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

// FetchLatestVersion fetches the latest release version from GitHub.
// It uses the gh CLI to query the latest release.
// If allowPrereleases is true, prereleases are included in the results.
func (p *GitHubProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if rid.Owner == "" || rid.Repo == "" {
		return nil, fmt.Errorf("github: missing owner/repo in RemoteId")
	}

	if p.Client == nil {
		p.Client = github.NewClient()
	}

	tag, _, err := p.Client.LatestRelease(rid.Owner, rid.Repo)
	if err != nil {
		// Fall back to tags if no releases exist
		tag, err = p.Client.LatestTag(rid.Owner, rid.Repo)
		if err != nil {
			return nil, fmt.Errorf("github: failed to get latest release or tag: %w", err)
		}
	}

	if tag == "" {
		return nil, fmt.Errorf("github: no releases or tags found for %s/%s", rid.Owner, rid.Repo)
	}

	// Normalize the tag (strip v prefix)
	normTag := version.Normalize(tag)

	// Parse the version
	v, err := version.Parse(normTag)
	if err != nil {
		return nil, fmt.Errorf("github: failed to parse version %q: %w", normTag, err)
	}

	// Filter prereleases if not allowed
	if !allowPrereleases && v.IsPrerelease() {
		return nil, fmt.Errorf("github: latest release %q is a prerelease", tag)
	}

	return v, nil
}

// FetchLatestVersionWithRepo fetches the latest version from a local repo's origin.
// This is used by the upstream command when a remote-id is available but we need
// to resolve tags from the local git repo.
func (p *GitHubProvider) FetchLatestVersionWithRepo(repo *git.Repo) (*version.Version, error) {
	if repo == nil {
		return nil, fmt.Errorf("github: nil repo")
	}

	tag, err := github.GetLatestTag(repo)
	if err != nil {
		return nil, fmt.Errorf("github: failed to get latest tag: %w", err)
	}

	normTag := version.Normalize(tag)
	v, err := version.Parse(normTag)
	if err != nil {
		return nil, fmt.Errorf("github: failed to parse version %q: %w", normTag, err)
	}

	return v, nil
}

// IsPrerelease reports whether v is a pre-release version.
// Delegates to version.Version.IsPrerelease().
func (p *GitHubProvider) IsPrerelease(v *version.Version) bool {
	return v.IsPrerelease()
}

// NewGitHubProvider creates a new GitHubProvider with the given rate limiter.
func NewGitHubProvider(rl *ratelimit.RateLimiter) provider.UpstreamProvider {
	return &GitHubProvider{
		RateLimiter: rl,
		Client:      github.NewClient(),
	}
}

// NewGitHubProviderWithClient creates a new GitHubProvider with a specific gh CLI client.
// Useful for testing with a stub client.
func NewGitHubProviderWithClient(rl *ratelimit.RateLimiter, client *github.Client) provider.UpstreamProvider {
	return &GitHubProvider{
		RateLimiter: rl,
		Client:      client,
	}
}
