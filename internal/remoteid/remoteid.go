package remoteid

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// RemoteIdType represents the type of an upstream source.
type RemoteIdType string

const (
	TypeGitHub    RemoteIdType = "github"
	TypeGitLab    RemoteIdType = "gitlab"
	TypePyPI      RemoteIdType = "pypi"
	TypeNPM       RemoteIdType = "npm"
	TypeCodeberg  RemoteIdType = "codeberg"
	TypeSourceHut RemoteIdType = "sourcehut"
	TypeUnknown   RemoteIdType = "unknown"
)

// RemoteId identifies an upstream source with type-specific fields.
//
// For GitHub/Codeberg: Owner + Repo
// For PyPI/NPM:       Name (package name)
// For GitLab/SourceHut: Host + Path (e.g., gitlab.com:group/project)
type RemoteId struct {
	Type RemoteIdType

	// Owner/Repo — used for GitHub and Codeberg
	Owner string
	Repo  string

	// Name — used for PyPI and NPM (package name)
	Name string

	// Host/Path — used for GitLab and SourceHut
	Host string
	Path string
}

// String returns a human-readable representation of the RemoteId.
func (r *RemoteId) String() string {
	switch r.Type {
	case TypeGitHub, TypeCodeberg:
		return fmt.Sprintf("%s:%s/%s", r.Type, r.Owner, r.Repo)
	case TypePyPI, TypeNPM:
		return fmt.Sprintf("%s:%s", r.Type, r.Name)
	case TypeGitLab, TypeSourceHut:
		return fmt.Sprintf("%s:%s/%s", r.Type, r.Host, r.Path)
	case TypeUnknown:
		return "unknown"
	default:
		return fmt.Sprintf("%s:?", r.Type)
	}
}

// IsZero reports whether r is a nil or empty RemoteId.
func (r *RemoteId) IsZero() bool {
	return r == nil || r.Type == "" || (r.Type == TypeUnknown)
}

// ParseRemoteId parses an XML <remote-id> element into a RemoteId.
//
// Formats:
//   <remote-id type="github">owner/repo</remote-id>
//   <remote-id type="pypi">package-name</remote-id>
//   <remote-id type="npm">package-name</remote-id>
//   <remote-id type="gitlab">gitlab.com:group/project</remote-id>
//   <remote-id type="codeberg">owner/repo</remote-id>
//   <remote-id type="sourcehut">git.sr.ht/~user/project</remote-id>
func ParseRemoteId(xmlContent []byte) (*RemoteId, error) {
	var raw struct {
		XMLName xml.Name `xml:"remote-id"`
		Type    string   `xml:"type,attr"`
		Content string   `xml:",chardata"`
	}
	if err := xml.Unmarshal(xmlContent, &raw); err != nil {
		return nil, fmt.Errorf("parsing remote-id: %w", err)
	}
	return ParseRemoteIdFromStrings(raw.Type, raw.Content)
}

// ParseRemoteIdFromStrings creates a RemoteId from type and content strings.
func ParseRemoteIdFromStrings(rtype string, content string) (*RemoteId, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty remote-id content")
	}

	switch rtype {
	case "github", "codeberg":
		parts := strings.SplitN(content, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid %s remote-id %q: expected owner/repo", rtype, content)
		}
		return &RemoteId{
			Type:  RemoteIdType(rtype),
			Owner: parts[0],
			Repo:  parts[1],
		}, nil

	case "pypi", "npm":
		return &RemoteId{
			Type: RemoteIdType(rtype),
			Name: content,
		}, nil

	case "gitlab", "sourcehut":
		// SourceHut: ~user/project → git.sr.ht, or git.sr.ht/~user/project
		if rtype == "sourcehut" && strings.HasPrefix(content, "~") {
			return &RemoteId{
				Type: RemoteIdType(rtype),
				Host: "git.sr.ht",
				Path: content,
			}, nil
		}
		// Format: host:path or host/group/project
		// GitLab: gitlab.com:group/project
		// SourceHut: git.sr.ht/~user/project
		sepIdx := strings.Index(content, ":")
		host, path := "", ""
		if sepIdx >= 0 {
			host = content[:sepIdx]
			path = content[sepIdx+1:]
		} else {
			// No colon — might be host/path already
			slashIdx := strings.Index(content, "/")
			if slashIdx > 0 {
				host = content[:slashIdx]
				path = content[slashIdx+1:] // strip leading / but keep remainder
			} else {
				host = content
			}
		}
		host = strings.TrimRight(host, ":")
		path = strings.TrimLeft(path, "/")

		if host == "" || path == "" {
			return nil, fmt.Errorf("invalid %s remote-id %q: expected host:path", rtype, content)
		}
		return &RemoteId{
			Type: RemoteIdType(rtype),
			Host: host,
			Path: path,
		}, nil

	default:
		return nil, fmt.Errorf("unknown remote-id type %q", rtype)
	}
}


