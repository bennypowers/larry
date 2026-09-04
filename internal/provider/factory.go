package provider

import (
	"bennypowers.dev/larry/internal/remoteid"
)

// ResolveProvider resolves a RemoteId to the correct provider from a registry.
func ResolveProvider(rid remoteid.RemoteId, registry *ProviderRegistry) (UpstreamProvider, error) {
	return registry.Resolve(rid)
}
