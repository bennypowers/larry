package providers

import (
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
)

// SourceHutProvider implements upstream version discovery for SourceHut.
type SourceHutProvider struct{}

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

func (p *SourceHutProvider) FindLatest(rid remoteid.RemoteId) (string, error) {
	return "", nil
}

func (p *SourceHutProvider) CheckVersion(rid remoteid.RemoteId, v string) (bool, error) {
	return false, nil
}
