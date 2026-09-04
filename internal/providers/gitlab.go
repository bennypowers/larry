package providers

import (
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
)

// GitLabProvider implements upstream version discovery for GitLab.
type GitLabProvider struct{}

func (p *GitLabProvider) Type() remoteid.RemoteIdType {
	return remoteid.TypeGitLab
}

func (p *GitLabProvider) Metadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		Name:        "GitLab",
		Description: "GitLab releases and tags",
		Homepage:    "https://gitlab.com",
	}
}

func (p *GitLabProvider) FindLatest(rid remoteid.RemoteId) (string, error) {
	return "", nil
}

func (p *GitLabProvider) CheckVersion(rid remoteid.RemoteId, v string) (bool, error) {
	return false, nil
}
