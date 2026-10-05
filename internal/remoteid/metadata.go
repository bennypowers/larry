package remoteid

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// rawRemoteIdXML is the raw XML representation of a <remote-id> element.
type rawRemoteIdXML struct {
	XMLName xml.Name `xml:"remote-id"`
	Type    string   `xml:"type,attr"`
	Content string   `xml:",chardata"`
}

// Metadata represents the structure of metadata.xml.
type Metadata struct {
	XMLName  xml.Name       `xml:"pkgmetadata"`
	Longdesc string         `xml:"longdescription"`
	Maintainers []Maintainer `xml:"maintainer"`
	RawRemotes []rawRemoteIdXML `xml:"upstream>remote-id"` // <upstream><remote-id> elements
	Remotes  []*RemoteId    `xml:"-"`                  // parsed (computed from RawRemotes)
}

// Maintainer represents a maintainer entry in metadata.xml.
type Maintainer struct {
	Email string `xml:"email"`
	Name  string `xml:"name"`
}

// ParseMetadata parses metadata.xml content.
func ParseMetadata(data []byte) (*Metadata, error) {
	var m Metadata
	if err := xml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing metadata.xml: %w", err)
	}
	// Parse raw remote-id XML elements into RemoteId objects
	for _, raw := range m.RawRemotes {
		r, err := ParseRemoteIdFromStrings(raw.Type, raw.Content)
		if err != nil {
			// Skip malformed remote-id entries
			continue
		}
		m.Remotes = append(m.Remotes, r)
	}
	_ = m.RawRemotes
	return &m, nil
}

// ParseMetadataFile parses a metadata.xml file.
func ParseMetadataFile(path string) (*Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading metadata.xml: %w", err)
	}
	return ParseMetadata(data)
}

// ResolveRemoteId walks from an ebuild path to find and parse metadata.xml.
// It looks in the ebuild's directory first, then parent directories.
func ResolveRemoteId(ebuildPath string) ([]*RemoteId, error) {
	dir := filepath.Dir(ebuildPath)
	// Search upward for metadata.xml
	for d := dir; ; d = filepath.Dir(d) {
		metaPath := filepath.Join(d, "metadata.xml")
		if _, err := os.Stat(metaPath); err == nil {
			meta, err := ParseMetadataFile(metaPath)
			if err != nil {
				return nil, fmt.Errorf("resolving remote-id: %w", err)
			}
			return meta.Remotes, nil
		}
		// Stop at filesystem root
		if d == filepath.Dir(d) {
			break
		}
	}
	return nil, nil // not found, not an error
}

// GetRemoteIds returns all remote-id entries from a parsed Metadata.
func (m *Metadata) GetRemoteIds() []*RemoteId {
	if m == nil {
		return nil
	}
	return m.Remotes
}

// FindUpstreamURL attempts to find an upstream URL from metadata.xml.
func (m *Metadata) FindUpstreamURL() string {
	if m == nil || m.Longdesc == "" {
		return ""
	}
	for _, line := range strings.Split(m.Longdesc, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "http://") || strings.Contains(line, "https://") {
			start := strings.Index(line, "http")
			if start >= 0 {
				end := strings.IndexAny(line[start:], " \t\n")
				if end == -1 {
					end = len(line)
				}
				return line[start:end]
			}
		}
	}
	return ""
}

// HasRemoteIds reports whether this metadata contains any remote-id entries.
func (m *Metadata) HasRemoteIds() bool {
	return m != nil && len(m.Remotes) > 0
}
