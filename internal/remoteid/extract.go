package remoteid

import (
	"regexp"
	"strings"
)

// compiled regexes for each forge, used by ExtractFromURLs.
var (
	reGitHub   = regexp.MustCompile(`https?://github\.com/([^/]+)/([^/]+?)(?:\.git)?(?:/|$)`)
	reCodeberg = regexp.MustCompile(`https?://codeberg\.org/([^/]+)/([^/]+?)(?:\.git)?(?:/|$)`)
	// GitLab: capture host and everything up to the first /-/ (project path).
	reGitLab = regexp.MustCompile(`https?://(gitlab\.[A-Za-z0-9.-]+)(/.+?)/-/`)
	// SourceHut: capture ~user/project after git.sr.ht or sr.ht.
	reSourceHut = regexp.MustCompile(`https?://(git\.)?sr\.ht/(~[^/]+/[^/]+)`)
	// Mirror URIs: mirror://github/owner/repo, mirror://gitlab.com/path/-/, etc.
	reMirrorGitHub   = regexp.MustCompile(`mirror://github/([^/]+)/([^/]+?)(?:/|$)`)
	reMirrorCodeberg = regexp.MustCompile(`mirror://codeberg/([^/]+)/([^/]+?)(?:/|$)`)
	reMirrorGitLab   = regexp.MustCompile(`mirror://gitlab(?:\.com)?/([^/]+?)/-/(?:.+?)(?:/|$)`)
	reMirrorSourceHut = regexp.MustCompile(`mirror://sourcehut/([^/]+)/([^/]+)`)
)

// ExtractFromURLs scans srcURI then homepage for a URL belonging to the given
// forge and returns the matching RemoteId, or nil when nothing matches.
//
// Candidate handling: the text is split on whitespace, tokens containing "${"
// are skipped, and the first matching token wins. Mirror URIs (mirror://...)
// are checked first, followed by direct URLs.
func ExtractFromURLs(forge RemoteIdType, srcURI, homepage string) *RemoteId {
	// Check mirror URIs first.
	if r := tryMirror(forge, srcURI); r != nil {
		return r
	}
	// Then check direct URLs (tokens from srcURI, then homepage).
	tokens := tokenize(srcURI)
	tokens = append(tokens, tokenize(homepage)...)
	for _, token := range tokens {
		r := tryForge(forge, token)
		if r != nil {
			return r
		}
	}
	return nil
}

// tryMirror checks a single mirror URI for the given forge.
func tryMirror(forge RemoteIdType, text string) *RemoteId {
	// Only check the full text once — no need to tokenize.
	switch forge {
	case TypeGitHub:
		return tryMirrorGitHub(text)
	case TypeCodeberg:
		return tryMirrorCodeberg(text)
	case TypeGitLab:
		return tryMirrorGitLab(text)
	case TypeSourceHut:
		return tryMirrorSourceHut(text)
	}
	return nil
}

func tryMirrorGitHub(text string) *RemoteId {
	m := reMirrorGitHub.FindStringSubmatch(text)
	if len(m) < 3 {
		return nil
	}
	return &RemoteId{Type: TypeGitHub, Owner: m[1], Repo: m[2]}
}

func tryMirrorCodeberg(text string) *RemoteId {
	m := reMirrorCodeberg.FindStringSubmatch(text)
	if len(m) < 3 {
		return nil
	}
	return &RemoteId{Type: TypeCodeberg, Owner: m[1], Repo: m[2]}
}

func tryMirrorGitLab(text string) *RemoteId {
	m := reMirrorGitLab.FindStringSubmatch(text)
	if len(m) < 2 {
		return nil
	}
	return &RemoteId{Type: TypeGitLab, Host: "gitlab.com", Path: m[1]}
}

func tryMirrorSourceHut(text string) *RemoteId {
	m := reMirrorSourceHut.FindStringSubmatch(text)
	if len(m) < 3 {
		return nil
	}
	return &RemoteId{Type: TypeSourceHut, Host: "git.sr.ht", Path: "~" + m[1] + "/" + m[2]}
}

// tokenize splits text on whitespace and removes tokens that are purely
// variable references (e.g. "${CARGO_CRATE_URIS}", "${P}.tar.gz").
// Tokens that start with a URL scheme (http/https/ftp) are kept even if
// they contain embedded variable expansions like "v${PV}.tar.gz".
func tokenize(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	for _, tok := range strings.Fields(text) {
		// Keep tokens that look like URLs (contain http://, https://, ftp://).
		if strings.Contains(tok, "http://") || strings.Contains(tok, "https://") || strings.HasPrefix(tok, "ftp://") {
			out = append(out, tok)
			continue
		}
		// Skip pure variable references starting with "${".
		if strings.HasPrefix(tok, "${") {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// tryForge applies the forge's regex to a single candidate token.
func tryForge(forge RemoteIdType, token string) *RemoteId {
	switch forge {
	case TypeGitHub:
		return tryGitHub(token)
	case TypeCodeberg:
		return tryCodeberg(token)
	case TypeGitLab:
		return tryGitLab(token)
	case TypeSourceHut:
		return trySourceHut(token)
	}
	return nil
}

// tryGitHub matches https://github.com/owner/repo and returns a RemoteId.
// Rejects captures containing "${" (unexpanded variable like ${PN}).
func tryGitHub(url string) *RemoteId {
	m := reGitHub.FindStringSubmatch(url)
	if len(m) < 3 || containsVarRef(m[1]) || containsVarRef(m[2]) {
		return nil
	}
	return &RemoteId{
		Type:  TypeGitHub,
		Owner: m[1],
		Repo:  m[2],
	}
}

// tryCodeberg matches https://codeberg.org/owner/repo and returns a RemoteId.
// Rejects captures containing "${" (unexpanded variable like ${PN}).
func tryCodeberg(url string) *RemoteId {
	m := reCodeberg.FindStringSubmatch(url)
	if len(m) < 3 || containsVarRef(m[1]) || containsVarRef(m[2]) {
		return nil
	}
	return &RemoteId{
		Type:  TypeCodeberg,
		Owner: m[1],
		Repo:  m[2],
	}
}

// tryGitLab matches https://gitlab.<host>/<path>/-/ and returns a RemoteId.
// The path is truncated at the first /-/ (everything after the host up to /-/).
// Rejects captures containing "${" (unexpanded variable like ${PN}).
func tryGitLab(url string) *RemoteId {
	m := reGitLab.FindStringSubmatch(url)
	if len(m) < 3 || containsVarRef(m[1]) || containsVarRef(m[2]) {
		return nil
	}
	host := m[1]
	// m[2] starts with "/", strip it.
	path := strings.TrimPrefix(m[2], "/")
	return &RemoteId{
		Type: TypeGitLab,
		Host: host,
		Path: path,
	}
}

// trySourceHut matches https://git.sr.ht/~user/project or https://sr.ht/~user/project.
// Rejects captures containing "${" (unexpanded variable like ${PN}).
func trySourceHut(url string) *RemoteId {
	m := reSourceHut.FindStringSubmatch(url)
	if len(m) < 3 || containsVarRef(m[1]) || containsVarRef(m[2]) {
		return nil
	}
	// m[1] is optional "git." prefix; normalise to "git.sr.ht".
	host := "git.sr.ht"
	if m[1] != "" {
		host = "git.sr.ht"
	}
	return &RemoteId{
		Type: TypeSourceHut,
		Host: host,
		Path: m[2],
	}
}

// containsVarRef reports whether s contains a shell variable reference
// (e.g. "${PN}", "${PV}"). Used to reject regex captures that reference
// unexpanded variables — the actual value isn't known at parse time.
func containsVarRef(s string) bool { return strings.Contains(s, "${") }
