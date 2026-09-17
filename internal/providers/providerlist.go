package providers

import (
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/ratelimit"
)

// AllProviders returns a list of all supported provider instances.
func AllProviders() []provider.UpstreamProvider {
	rl := ratelimit.NewRateLimiter()
	return []provider.UpstreamProvider{
		NewPyPIProvider(rl),
		NewNpmProvider(rl),
		NewGitHubProvider(rl),
		NewGitLabProvider(rl),
		NewCodebergProvider(rl),
		NewSourceHutProvider(rl),
	}
}
