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

// PyPIProvider implements upstream version discovery for PyPI.
type PyPIProvider struct {
	RateLimiter *ratelimit.RateLimiter
	Client      *httpclient.Client
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

// pypiResponse represents the JSON response from pypi.org/pypi/<name>/json
// Only the fields we care about are included.
type pypiResponse struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
}

func (p *PyPIProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if rid.Name == "" {
		return nil, fmt.Errorf("pypi: missing package name in RemoteId")
	}

	if p.Client == nil {
		p.Client = httpclient.NewClient()
	}

	url := fmt.Sprintf("https://pypi.org/pypi/%s/json", rid.Name)
	resp, err := p.Client.GetWithRetry(url)
	if err != nil {
		return nil, fmt.Errorf("pypi: failed to fetch package info: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("pypi: package %q not found", rid.Name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pypi: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("pypi: failed to read response: %w", err)
	}

	var pypiResp pypiResponse
	if err := json.Unmarshal(body, &pypiResp); err != nil {
		return nil, fmt.Errorf("pypi: failed to parse response: %w", err)
	}

	if pypiResp.Info.Version == "" {
		return nil, fmt.Errorf("pypi: no version found in response")
	}

	pv := pypiResp.Info.Version
	normVer := version.Normalize(pv)
	v, err := version.Parse(normVer)
	if err != nil {
		return nil, fmt.Errorf("pypi: failed to parse version %q: %w", pv, err)
	}

	// Filter prereleases if not allowed
	if !allowPrereleases && v.IsPrerelease() {
		return nil, fmt.Errorf("pypi: latest version %q is a prerelease", pv)
	}

	return v, nil
}

// IsPrerelease reports whether v is a pre-release version.
func (p *PyPIProvider) IsPrerelease(v *version.Version) bool {
	return v.IsPrerelease()
}

// NewPyPIProvider creates a new PyPIProvider with the given rate limiter.
func NewPyPIProvider(rl *ratelimit.RateLimiter) provider.UpstreamProvider {
	return &PyPIProvider{
		RateLimiter: rl,
		Client:      httpclient.NewClient(),
	}
}

// NewPyPIProviderWithClient creates a new PyPIProvider with a specific HTTP client.
// Useful for testing with httptest.Server.
func NewPyPIProviderWithClient(rl *ratelimit.RateLimiter, client *httpclient.Client) provider.UpstreamProvider {
	return &PyPIProvider{
		RateLimiter: rl,
		Client:      client,
	}
}
