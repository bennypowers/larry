package providers

import (
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
)

// NpmProvider implements upstream version discovery for npm.
type NpmProvider struct{}

func (p *NpmProvider) Type() remoteid.RemoteIdType {
	return remoteid.TypeNpm
}

func (p *NpmProvider) Metadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		Name:        "npm",
		Description: "Node Package Manager",
		Homepage:    "https://npmjs.com",
	}
}

func (p *NpmProvider) FindLatest(rid remoteid.RemoteId) (string, error) {
	return "", nil
}

func (p *NpmProvider) CheckVersion(rid remoteid.RemoteId, v string) (bool, error) {
	return false, nil
}
