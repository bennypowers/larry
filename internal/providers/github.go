package providers

import (
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
)

// GitHubProvider implements upstream version discovery for GitHub.
type GitHubProvider struct{}

func (p *GitHubProvider) Type() remoteid.RemoteIdType {
	return remoteid.TypeGitHub
}

func (p *GitHubProvider) Metadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		Name:        "GitHub",
		Description: "GitHub releases and tags",
		Homepage:    "https://github.com",
	}
}

func (p *GitHubProvider) FindLatest(rid remoteid.RemoteId) (string, error) {
	return "", nil
}

func (p *GitHubProvider) CheckVersion(rid remoteid.RemoteId, v string) (bool, error) {
	return false, nil
}
