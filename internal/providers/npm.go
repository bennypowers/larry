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

// NpmProvider implements upstream version discovery for npm.
type NpmProvider struct {
	RateLimiter *ratelimit.RateLimiter
	Client      *httpclient.Client
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

// npmResponse represents the JSON response from registry.npmjs.org/<name>/latest
type npmResponse struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (p *NpmProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if rid.Name == "" {
		return nil, fmt.Errorf("npm: missing package name in RemoteId")
	}

	if p.Client == nil {
		p.Client = httpclient.NewClient()
	}

	// Handle scoped packages (@scope/name)
	name := rid.Name
	if name[0] == '@' {
		name = "@" + name[1:]
	}

	url := fmt.Sprintf("https://registry.npmjs.org/%s/latest", name)
	resp, err := p.Client.GetWithRetry(url)
	if err != nil {
		return nil, fmt.Errorf("npm: failed to fetch package info: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("npm: package %q not found", rid.Name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("npm: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("npm: failed to read response: %w", err)
	}

	var npmResp npmResponse
	if err := json.Unmarshal(body, &npmResp); err != nil {
		return nil, fmt.Errorf("npm: failed to parse response: %w", err)
	}

	if npmResp.Version == "" {
		return nil, fmt.Errorf("npm: no version found in response")
	}

	normVer := version.Normalize(npmResp.Version)
	v, err := version.Parse(normVer)
	if err != nil {
		return nil, fmt.Errorf("npm: failed to parse version %q: %w", npmResp.Version, err)
	}

	// Filter prereleases if not allowed
	if !allowPrereleases && v.IsPrerelease() {
		return nil, fmt.Errorf("npm: latest version %q is a prerelease", npmResp.Version)
	}

	return v, nil
}

// IsPrerelease reports whether v is a pre-release version.
func (p *NpmProvider) IsPrerelease(v *version.Version) bool {
	return v.IsPrerelease()
}

// NewNpmProvider creates a new NpmProvider with the given rate limiter.
func NewNpmProvider(rl *ratelimit.RateLimiter) provider.UpstreamProvider {
	return &NpmProvider{
		RateLimiter: rl,
		Client:      httpclient.NewClient(),
	}
}

// NewNpmProviderWithClient creates a new NpmProvider with a specific HTTP client.
// Useful for testing with httptest.Server.
func NewNpmProviderWithClient(rl *ratelimit.RateLimiter, client *httpclient.Client) provider.UpstreamProvider {
	return &NpmProvider{
		RateLimiter: rl,
		Client:      client,
	}
}
