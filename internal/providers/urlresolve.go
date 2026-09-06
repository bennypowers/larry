package providers

import (
	"regexp"
	"strings"

	"bennypowers.dev/larry/internal/remoteid"
)

// urlMatchers holds URL pattern matchers for each provider type.
type urlMatcher struct {
	typeID   remoteid.RemoteIdType
	patterns []*regexp.Regexp
}

var matchers = []urlMatcher{
	{
		typeID: remoteid.TypeGitHub,
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)github\.com/[^/]+/[^/]+`),
			regexp.MustCompile(`(?i)(?:github\.com[:/]|@github\.com[:/])`),
		},
	},
	{
		typeID: remoteid.TypePyPI,
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)pypi\.org/project/[^/]+`),
			regexp.MustCompile(`(?i)pypi\.org/simple/[^/]+`),
			regexp.MustCompile(`(?i)files\.pythonhosted\.org`),
		},
	},
	{
		typeID: remoteid.TypeNPM,
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)registry\.npmjs\.org/[^/]+`),
			regexp.MustCompile(`(?i)npmjs\.com/package/@[^/]+/[^/]+`), // scoped: @scope/package
			regexp.MustCompile(`(?i)npmjs\.com/package/[^/]+`),         // non-scoped
		},
	},
	{
		typeID: remoteid.TypeGitLab,
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)gitlab\.com/[^/]+/[^/]+`),
		},
	},
	{
		typeID: remoteid.TypeCodeberg,
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)codeberg\.org/[^/]+/[^/]+`),
		},
	},
	{
		typeID: remoteid.TypeSourceHut,
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)git\.sr\.ht/~[^/]+/[^/]+`),
		},
	},
}

// ResolveProviderFromURL matches a URL to a RemoteIdType.
// It tries known URL patterns and returns the first match.
// Returns remoteid.TypeUnknown if no match is found.
func ResolveProviderFromURL(url string) remoteid.RemoteIdType {
	url = strings.TrimSpace(url)
	if url == "" {
		return remoteid.TypeUnknown
	}

	for _, m := range matchers {
		for _, pat := range m.patterns {
			if pat.MatchString(url) {
				return m.typeID
			}
		}
	}
	return remoteid.TypeUnknown
}

// ResolveRemoteIdFromURL creates a RemoteId from a URL and its resolved type.
// It calls ResolveProviderFromURL internally and delegates to ParseRemoteIdFromStrings.
func ResolveRemoteIdFromURL(url string) (*remoteid.RemoteId, error) {
	rtype := ResolveProviderFromURL(url)
	if rtype == remoteid.TypeUnknown {
		return nil, nil // not a known URL pattern
	}

	// For GitHub/Codeberg: extract owner/repo from URL
	if rtype == remoteid.TypeGitHub || rtype == remoteid.TypeCodeberg {
		re := regexp.MustCompile(`(?i)[^/]+\.com/([^/]+)/([^/]+)`)
		m := re.FindStringSubmatch(url)
		if len(m) == 3 {
			return remoteid.ParseRemoteIdFromStrings(string(rtype), m[1]+"/"+m[2])
		}
		return nil, nil
	}

	// For PyPI/NPM: extract package name
	if rtype == remoteid.TypePyPI {
		// Try pypi.org/project/name
		re := regexp.MustCompile(`(?i)pypi\.org/project/([^/]+)`)
		m := re.FindStringSubmatch(url)
		if len(m) == 2 {
			return remoteid.ParseRemoteIdFromStrings(string(rtype), m[1])
		}
		// Try pypi.org/simple/name/
		re = regexp.MustCompile(`(?i)pypi\.org/simple/([^/]+)/?`)
		m = re.FindStringSubmatch(url)
		if len(m) == 2 {
			return remoteid.ParseRemoteIdFromStrings(string(rtype), m[1])
		}
		return nil, nil
	}
	if rtype == remoteid.TypeNPM {
		// Try scoped package first (@scope/name)
		re := regexp.MustCompile(`(?i)(?:[\w.]*npmjs\.com/package|registry\.npmjs\.org)/(@[^/]+/[^/]+)`)
		m := re.FindStringSubmatch(url)
		if len(m) == 2 {
			return remoteid.ParseRemoteIdFromStrings(string(rtype), m[1])
		}
		// Non-scoped fallback
		re = regexp.MustCompile(`(?i)(?:[\w.]*npmjs\.com/package|registry\.npmjs\.org)/([^@/]+)`)
		m = re.FindStringSubmatch(url)
		if len(m) == 2 {
			return remoteid.ParseRemoteIdFromStrings(string(rtype), m[1])
		}
		return nil, nil
	}

	// For GitLab: extract host and full path (supports multi-level groups)
	if rtype == remoteid.TypeGitLab {
		re := regexp.MustCompile(`(?i)([^/]+\.com)(/[^/]+(?:/[^/]+)*)`)
		m := re.FindStringSubmatch(url)
		if len(m) == 3 {
			path := m[2]
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			return remoteid.ParseRemoteIdFromStrings(string(rtype), m[1]+path)
		}
		return nil, nil
	}

	// For SourceHut: extract host/path
	if rtype == remoteid.TypeSourceHut {
		re := regexp.MustCompile(`(?i)([^/]+/~[^/]+/[^/]+)`)
		m := re.FindStringSubmatch(url)
		if len(m) == 2 {
			return remoteid.ParseRemoteIdFromStrings(string(rtype), m[1])
		}
		return nil, nil
	}

	return nil, nil
}

// ProviderTypes returns all supported RemoteIdType values.
func ProviderTypes() []remoteid.RemoteIdType {
	return []remoteid.RemoteIdType{
		remoteid.TypeGitHub,
		remoteid.TypePyPI,
		remoteid.TypeNPM,
		remoteid.TypeGitLab,
		remoteid.TypeCodeberg,
		remoteid.TypeSourceHut,
	}
}
