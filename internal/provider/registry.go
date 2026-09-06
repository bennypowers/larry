package provider

import (
	"fmt"
	"sync"

	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// ProviderRegistry holds registered providers and resolves them by RemoteIdType.
type ProviderRegistry struct {
	mu         sync.RWMutex
	providers  map[remoteid.RemoteIdType]UpstreamProvider
	factories  map[remoteid.RemoteIdType]Factory
	rateLimiter *ratelimit.RateLimiter
}

// NewProviderRegistry creates a new empty registry with a default rate limiter.
func NewProviderRegistry() *ProviderRegistry {
	return NewProviderRegistryWithRateLimiter(ratelimit.NewRateLimiter())
}

// NewProviderRegistryWithRateLimiter creates a new registry with a specific rate limiter.
func NewProviderRegistryWithRateLimiter(rl *ratelimit.RateLimiter) *ProviderRegistry {
	if rl == nil {
		rl = ratelimit.NewRateLimiter()
	}
	return &ProviderRegistry{
		providers:   make(map[remoteid.RemoteIdType]UpstreamProvider),
		factories:   make(map[remoteid.RemoteIdType]Factory),
		rateLimiter: rl,
	}
}

// Register registers a provider directly.
func (r *ProviderRegistry) Register(p UpstreamProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Type()] = p
}

// RegisterFactory registers a factory for a provider type.
// The factory will be called when ResolveFactory is used to create providers.
func (r *ProviderRegistry) RegisterFactory(ridType remoteid.RemoteIdType, factory Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[ridType] = factory
}

// Resolve returns a provider for the given RemoteId.
// Providers are instantiated once and cached.
func (r *ProviderRegistry) Resolve(rid remoteid.RemoteId) (UpstreamProvider, error) {
	r.mu.RLock()
	p, ok := r.providers[rid.Type]
	r.mu.RUnlock()
	if ok {
		return p, nil
	}

	// Try factory
	r.mu.RLock()
	factory, ok := r.factories[rid.Type]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no provider registered for type %q", rid.Type)
	}

	// Create provider from factory and cache it
	r.mu.Lock()
	defer r.mu.Unlock()
	// Double-check after acquiring write lock
	p, ok = r.providers[rid.Type]
	if ok {
		return p, nil
	}
	p = factory(r.rateLimiter)
	r.providers[rid.Type] = p
	return p, nil
}

// ResolveFactory resolves a RemoteId and returns the correct factory for creating
// a fresh provider instance (useful for testing with mock rate limiters).
func (r *ProviderRegistry) ResolveFactory(ridType remoteid.RemoteIdType) (Factory, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, ok := r.factories[ridType]
	if !ok {
		return nil, fmt.Errorf("no factory registered for type %q", ridType)
	}
	return factory, nil
}

// Version resolves the latest version for a RemoteId using the correct provider.
func (r *ProviderRegistry) Version(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	p, err := r.Resolve(rid)
	if err != nil {
		return nil, err
	}
	return p.FetchLatestVersion(rid, allowPrereleases)
}
