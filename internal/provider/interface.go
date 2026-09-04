package provider

import (
	"bennypowers.dev/larry/internal/remoteid"
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
	// FindLatest finds the latest version from the upstream
	FindLatest(remoteid.RemoteId) (string, error)
	// CheckVersion checks if a specific version exists on the upstream
	CheckVersion(remoteid.RemoteId, string) (bool, error)
}
