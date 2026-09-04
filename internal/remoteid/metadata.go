package remoteid

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

// Metadata represents the structure of metadata.xml.
type Metadata struct {
	XMLName   xml.Name     `xml:"pkg-metadata"`
	Longdesc  string       `xml:"longdescription"`
	Maintainers []Maintainer `xml:"maintainer"`
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

// FindUpstreamURL attempts to find an upstream URL from metadata.xml.
func (m *Metadata) FindUpstreamURL() string {
	if m.Longdesc == "" {
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
