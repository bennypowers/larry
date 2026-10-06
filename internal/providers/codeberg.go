package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"bennypowers.dev/larry/internal/httpclient"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// CodebergProvider implements upstream version discovery for Codeberg.
// Codeberg uses the Gitea API.
type CodebergProvider struct {
	RateLimiter *ratelimit.RateLimiter
	Client      *httpclient.Client
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

// codebergRelease represents a Codeberg/Gitea release
type codebergRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Prerelease bool `json:"prerelease"`
}

// codebergReleasesResponse is the response from Codeberg's releases endpoint
type codebergReleasesResponse []codebergRelease

func (p *CodebergProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if rid.Owner == "" || rid.Repo == "" {
		return nil, fmt.Errorf("codeberg: missing owner/repo in RemoteId")
	}

	if p.Client == nil {
		p.Client = httpclient.NewClient()
	}

	// Codeberg uses Gitea API at codeberg.org/api/v1/repos/:owner/:repo/releases
	url := fmt.Sprintf("https://codeberg.org/api/v1/repos/%s/%s/releases",
		rid.Owner, rid.Repo)

	resp, err := p.Client.GetWithRetry(url)
	if err != nil {
		return nil, fmt.Errorf("codeberg: failed to fetch releases: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("codeberg: repository %s/%s not found", rid.Owner, rid.Repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("codeberg: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("codeberg: failed to read response: %w", err)
	}

	var releases codebergReleasesResponse
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("codeberg: failed to parse response: %w", err)
	}

	if len(releases) == 0 {
		return nil, fmt.Errorf("codeberg: no releases found")
	}

	// Gitea API returns releases sorted by created_at descending
	// Filter prereleases if not allowed
	var latest *codebergRelease
	for i := range releases {
		if releases[i].Prerelease && !allowPrereleases {
			continue
		}
		latest = &releases[i]
		break
	}

	if latest == nil {
		return nil, fmt.Errorf("codeberg: no releases found (all are prereleases)")
	}

	tagName := latest.TagName
	if tagName == "" {
		tagName = latest.Name
	}

	normVer := version.Normalize(tagName)
	v, err := version.Parse(normVer)
	if err != nil {
		return nil, fmt.Errorf("codeberg: failed to parse version %q: %w", tagName, err)
	}

	return v, nil
}

// IsPrerelease reports whether v is a pre-release version.
func (p *CodebergProvider) IsPrerelease(v *version.Version) bool {
	return v.IsPrerelease()
}

// NewCodebergProvider creates a new CodebergProvider with the given rate limiter.
func NewCodebergProvider(rl *ratelimit.RateLimiter) provider.UpstreamProvider {
	return &CodebergProvider{
		RateLimiter: rl,
		Client:      httpclient.NewClient(),
	}
}

// NewCodebergProviderWithClient creates a new CodebergProvider with a specific HTTP client.
// Useful for testing with httptest.Server.
func NewCodebergProviderWithClient(rl *ratelimit.RateLimiter, client *httpclient.Client) provider.UpstreamProvider {
	return &CodebergProvider{
		RateLimiter: rl,
		Client:      client,
	}
}
