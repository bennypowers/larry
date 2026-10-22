package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"

	"bennypowers.dev/larry/internal/httpclient"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/remoteid"
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
type pypiResponse struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	// Releases maps version strings to arrays of file objects.
	// Each file object has "packagetype" (sdist or bdist_wheel).
	Releases map[string][]pypiFile `json:"releases"`
}

// pypiFile represents a single distribution file on PyPI.
type pypiFile struct {
	PackageType string `json:"packagetype"`
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

	// Find the latest version that has an sdist (source distribution).
	// Gentoo builds from source, so versions without sdists are unusable.
	pv, err := p.latestVersionWithSdist(&pypiResp, allowPrereleases)
	if err != nil {
		return nil, err
	}

	v, err := version.ParsePEP440(pv)
	if err != nil {
		return nil, fmt.Errorf("pypi: failed to parse version %q: %w", pv, err)
	}

	return v, nil
}

// latestVersionWithSdist returns the latest version that has at least one sdist.
// Falls back to info.version if the releases data is unavailable.
func (p *PyPIProvider) latestVersionWithSdist(resp *pypiResponse, allowPrereleases bool) (string, error) {
	// If releases data is available, filter for versions with sdists
	if len(resp.Releases) > 0 {
		// Iterate versions in reverse-sorted order (last = highest)
		versions := make([]string, 0, len(resp.Releases))
		for v := range resp.Releases {
			versions = append(versions, v)
		}
		// Sort descending (highest version first), using PEP 440 semantics.
		slices.SortFunc(versions, func(a, b string) int {
			va, errA := version.ParsePEP440(a)
			vb, errB := version.ParsePEP440(b)
			if errA == nil && errB == nil {
				return vb.Compare(va)
			}
			return version.Compare(b, a)
		})

		for _, v := range versions {
			if !allowPrereleases {
				nv, err := version.ParsePEP440(v)
				if err == nil && nv.IsPrerelease() {
					continue
				}
			}
			for _, f := range resp.Releases[v] {
				if f.PackageType == "sdist" {
					return v, nil
				}
			}
		}
		// No sdist found at all — return info.version so the user sees the issue
		// when they try to bump (and gets a clear 404 from prepare).
		fv := resp.Info.Version
		if !allowPrereleases {
			v, err := version.ParsePEP440(fv)
			if err == nil && v.IsPrerelease() {
				return "", fmt.Errorf("pypi: latest version %q is a prerelease", fv)
			}
		}
		return fv, nil
	}

	// No releases data — fall back to info.version (legacy behavior)
	fv := resp.Info.Version
	if !allowPrereleases {
		v, err := version.ParsePEP440(fv)
		if err == nil && v.IsPrerelease() {
			return "", fmt.Errorf("pypi: latest version %q is a prerelease", fv)
		}
	}
	return fv, nil
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
