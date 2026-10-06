package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"bennypowers.dev/larry/internal/httpclient"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// GitLabProvider implements upstream version discovery for GitLab.
type GitLabProvider struct {
	RateLimiter *ratelimit.RateLimiter
	Client      *httpclient.Client
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

// gitLabRelease represents a GitLab release
type gitLabRelease struct {
	Name   string `json:"name"`
	TagName string `json:"tag_name"`
}

// gitLabReleasesResponse is the response from GitLab's releases endpoint
type gitLabReleasesResponse []gitLabRelease

// gitLabTagsResponse is the response from GitLab's tags endpoint (fallback)
type gitLabTagsResponse []struct {
	Name string `json:"name"`
}

func (p *GitLabProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if rid.Host == "" {
		return nil, fmt.Errorf("gitlab: missing host in RemoteId")
	}

	if p.Client == nil {
		p.Client = httpclient.NewClient()
	}

	// Determine the path from RemoteId
	path := rid.Path
	if path == "" {
		return nil, fmt.Errorf("gitlab: missing path in RemoteId")
	}

	// Ensure path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Build the API URL
	host := rid.Host
	if !strings.HasPrefix(host, "http") {
		host = "https://" + host
	}

	// Try releases endpoint first: /api/v4/projects/:encoded_path/releases
	encodedPath := url.PathEscape(strings.TrimPrefix(path, "/"))
	releasesURL := fmt.Sprintf("%s/api/v4/projects/%s/releases", host, encodedPath)

	resp, err := p.Client.GetWithRetry(releasesURL)
	if err != nil {
		return nil, fmt.Errorf("gitlab: failed to fetch releases: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		// Fallback to tags endpoint
		return p.fetchLatestTag(host, encodedPath, allowPrereleases)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gitlab: failed to read response: %w", err)
	}

	var releases gitLabReleasesResponse
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("gitlab: failed to parse response: %w", err)
	}

	if len(releases) == 0 {
		// Fallback to tags
		return p.fetchLatestTag(host, encodedPath, allowPrereleases)
	}

	// GitLab returns releases sorted by created_at descending (newest first)
	latest := releases[0]
	tagName := latest.TagName
	if tagName == "" {
		tagName = latest.Name
	}

	normVer := version.Normalize(tagName)
	v, err := version.Parse(normVer)
	if err != nil {
		return nil, fmt.Errorf("gitlab: failed to parse version %q: %w", tagName, err)
	}

	if !allowPrereleases && v.IsPrerelease() {
		return nil, fmt.Errorf("gitlab: latest release %q is a prerelease", tagName)
	}

	return v, nil
}

// fetchLatestTag is a fallback when releases endpoint is not available.
func (p *GitLabProvider) fetchLatestTag(host, encodedPath string, allowPrereleases bool) (*version.Version, error) {
	tagsURL := fmt.Sprintf("%s/api/v4/projects/%s/repository/tags", host, encodedPath)

	resp, err := p.Client.GetWithRetry(tagsURL)
	if err != nil {
		return nil, fmt.Errorf("gitlab: failed to fetch tags: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("gitlab: project not found")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gitlab: failed to read response: %w", err)
	}

	var tags gitLabTagsResponse
	if err := json.Unmarshal(body, &tags); err != nil {
		return nil, fmt.Errorf("gitlab: failed to parse response: %w", err)
	}

	if len(tags) == 0 {
		return nil, fmt.Errorf("gitlab: no tags found")
	}

	// Tags are sorted by creation date descending by default
	tagName := tags[0].Name
	normVer := version.Normalize(tagName)
	v, err := version.Parse(normVer)
	if err != nil {
		return nil, fmt.Errorf("gitlab: failed to parse version %q: %w", tagName, err)
	}

	return v, nil
}

// IsPrerelease reports whether v is a pre-release version.
func (p *GitLabProvider) IsPrerelease(v *version.Version) bool {
	return v.IsPrerelease()
}

// NewGitLabProvider creates a new GitLabProvider with the given rate limiter.
func NewGitLabProvider(rl *ratelimit.RateLimiter) provider.UpstreamProvider {
	return &GitLabProvider{
		RateLimiter: rl,
		Client:      httpclient.NewClient(),
	}
}

// NewGitLabProviderWithClient creates a new GitLabProvider with a specific HTTP client.
// Useful for testing with httptest.Server.
func NewGitLabProviderWithClient(rl *ratelimit.RateLimiter, client *httpclient.Client) provider.UpstreamProvider {
	return &GitLabProvider{
		RateLimiter: rl,
		Client:      client,
	}
}
