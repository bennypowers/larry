package ebuild

import (
	"bennypowers.dev/larry/internal/remoteid"
	"strings"
)

// DetectType determines the provider type from SRC_URI.
func DetectType(srcURI string) remoteid.RemoteIdType {
	switch {
	case strings.Contains(srcURI, "github.com"):
		return remoteid.TypeGitHub
	case strings.Contains(srcURI, "gitlab.com"):
		return remoteid.TypeGitLab
	case strings.Contains(srcURI, "pypi.io") || strings.Contains(srcURI, "pypi.org"):
		return remoteid.TypePyPI
	case strings.Contains(srcURI, "registry.npmjs.org"):
		return remoteid.TypeNPM
	case strings.Contains(srcURI, "codeberg.org"):
		return remoteid.TypeCodeberg
	case strings.Contains(srcURI, "git.sr.ht"):
		return remoteid.TypeSourceHut
	default:
		return remoteid.TypeUnknown
	}
}
