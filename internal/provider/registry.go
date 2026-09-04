package provider

import (
	"fmt"
	"sync"

	"bennypowers.dev/larry/internal/remoteid"
)

// ProviderRegistry holds registered providers.
type ProviderRegistry struct {
	mu     sync.RWMutex
	providers map[remoteid.RemoteIdType]UpstreamProvider
}

// NewProviderRegistry creates a new empty registry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[remoteid.RemoteIdType]UpstreamProvider),
	}
}

// Register registers a provider.
func (r *ProviderRegistry) Register(p UpstreamProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Type()] = p
}

// Resolve returns a provider for the given RemoteId.
func (r *ProviderRegistry) Resolve(rid remoteid.RemoteId) (UpstreamProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[rid.Type]
	if !ok {
		return nil, fmt.Errorf("no provider registered for type %q", rid.Type)
	}
	return p, nil
}
