package providers

import (
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
)

// CodebergProvider implements upstream version discovery for Codeberg.
type CodebergProvider struct{}

func (p *CodebergProvider) Type() remoteid.RemoteIdType {
	return remoteid.TypeCodeberg
}

func (p *CodebergProvider) Metadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		Name:        "Codeberg",
		Description: "Codeberg git hosting",
		Homepage:    "https://codeberg.org",
	}
}

func (p *CodebergProvider) FindLatest(rid remoteid.RemoteId) (string, error) {
	return "", nil
}

func (p *CodebergProvider) CheckVersion(rid remoteid.RemoteId, v string) (bool, error) {
	return false, nil
}
