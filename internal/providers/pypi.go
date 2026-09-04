package providers

import (
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
)

// PyPIProvider implements upstream version discovery for PyPI.
type PyPIProvider struct{}

func (p *PyPIProvider) Type() remoteid.RemoteIdType {
	return remoteid.TypePyPI
}

func (p *PyPIProvider) Metadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		Name:        "PyPI",
		Description: "Python Package Index",
		Homepage:    "https://pypi.org",
	}
}

func (p *PyPIProvider) FindLatest(rid remoteid.RemoteId) (string, error) {
	return "", nil
}

func (p *PyPIProvider) CheckVersion(rid remoteid.RemoteId, v string) (bool, error) {
	return false, nil
}
