package provider

import (
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// ProviderMetadata holds metadata about a provider.
type ProviderMetadata struct {
	Name        string
	Description string
	Homepage    string
}

// UpstreamProvider defines the interface for upstream version discovery.
type UpstreamProvider interface {
	// Type returns the provider type
	Type() remoteid.RemoteIdType
	// Metadata returns provider metadata
	Metadata() ProviderMetadata
	// FetchLatestVersion fetches the latest version for the given remote id
	FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error)
	// Name returns a human-readable name
	Name() string
}

// Factory creates a new provider instance with the given rate limiter.
type Factory func(rl *ratelimit.RateLimiter) UpstreamProvider
