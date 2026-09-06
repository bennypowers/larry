package providers

import (
	"fmt"

	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// SourceHutProvider implements upstream version discovery for SourceHut.
//
// API Strategy (research outcome):
// SourceHut does not have a conventional "latest release" REST endpoint.
// The recommended approach is to use the GraphQL API at https://api.sr.ht
// to query refs with type: tag, sorted by date, or use go-git to clone/fetch
// tags locally.
//
// For now, the provider returns a placeholder — T8b integration with
// httpclient + GraphQL will be implemented in the next phase.
type SourceHutProvider struct {
	RateLimiter *ratelimit.RateLimiter
}

func (p *SourceHutProvider) Type() remoteid.RemoteIdType {
	return remoteid.TypeSourceHut
}

func (p *SourceHutProvider) Metadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		Name:        "SourceHut",
		Description: "SourceHut git hosting",
		Homepage:    "https://sr.ht",
	}
}

func (p *SourceHutProvider) Name() string {
	return "SourceHut"
}

func (p *SourceHutProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if rid.Host == "" || rid.Path == "" {
		return nil, fmt.Errorf("sourcehut: missing host/path in RemoteId")
	}
	// TODO: integrate with GraphQL API at https://api.sr.ht
	// Query: { user(login: "~user") { repositories(first: 10) { edges { node { name refs(refPrefix: "refs/tags/", first: 1, orderBy: { field: CREATED_AT, direction: DESC }) { edges { node { name target { ... on Commit { oid } } } } } } } } } }
	// For now, return a placeholder
	return nil, fmt.Errorf("sourcehut: not yet implemented - needs GraphQL API integration")
}

// IsPrerelease reports whether v is a pre-release version.
// Delegates to version.Version.IsPrerelease().
func (p *SourceHutProvider) IsPrerelease(v *version.Version) bool {
	return v.IsPrerelease()
}

// NewSourceHutProvider creates a new SourceHutProvider with the given rate limiter.
func NewSourceHutProvider(rl *ratelimit.RateLimiter) provider.UpstreamProvider {
	return &SourceHutProvider{RateLimiter: rl}
}
