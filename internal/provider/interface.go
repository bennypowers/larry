package provider

import (
	"context"

	"bennypowers.dev/larry/internal/larryconfig"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/remoteid"
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
	// IsPrerelease reports whether v is a pre-release version
	IsPrerelease(v *version.Version) bool
}

// Factory creates a new provider instance with the given rate limiter.
type Factory func(rl *ratelimit.RateLimiter) UpstreamProvider

// Resolution is a provider's answer when it may have found a dated tag rather
// than a release version. It replaces the script's "exit code 2" signal.
type Resolution struct {
	Version string // normalized version string; "" when only a dated tag was found
	Tag     string // raw tag name from the tag-by-date path
	TagDate string // "YYYYMMDD"
	ByDate  bool   // true => the caller must use the date comparison path
}

// ResolverWithTags is implemented by providers that can additionally resolve a
// dated tag when no release version is available. Callers must type-assert and
// fall back to UpstreamProvider.FetchLatestVersion when the assertion fails.
type ResolverWithTags interface {
	UpstreamProvider
	FetchResolution(rid remoteid.RemoteId, allowPrereleases bool) (*Resolution, error)
}

// BatchRequest is one repository in a batch prefetch. Options carries the
// per-package upstream rules for this repository.
type BatchRequest struct {
	ID      remoteid.RemoteId
	Options *larryconfig.ResolveOptions
}

// BatchResolver is implemented by providers whose API can answer many
// repositories in one request (for example GitHub GraphQL aliases). The
// returned map is keyed by BatchKey. Missing keys mean "fall back to the
// per-repository path"; callers must never require a full result.
type BatchResolver interface {
	UpstreamProvider
	BatchResolve(ctx context.Context, reqs []BatchRequest, allowPrereleases bool) (map[string]*Resolution, error)
}

// BatchKey is the stable lookup key for a batched resolution.
func BatchKey(rid remoteid.RemoteId) string {
	return string(rid.Type) + "|" + rid.String()
}
