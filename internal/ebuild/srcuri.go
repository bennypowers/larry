package ebuild

import "strings"

// ResolveSRCURI resolves SRC_URI, handling mirror:// and other special schemes.
func ResolveSRCURI(uri string) ([]string, error) {
	// Handle mirror:// scheme (simplified)
	if strings.HasPrefix(uri, "mirror://") {
		return []string{strings.TrimPrefix(uri, "mirror://")}, nil
	}
	return []string{uri}, nil
}
