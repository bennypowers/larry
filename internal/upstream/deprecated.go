package upstream

import (
	"os"
	"path/filepath"
	"strings"
)

// DeprecatedInfo holds information about a deprecated/renamed package.
type DeprecatedInfo struct {
	// Package is the deprecated package atom (e.g., "mail-client/vireo")
	Package string
	// Target is the new package atom, if renamed (e.g., "mail-client/hylki")
	Target string
	// Source is the reason: "deprecated" or "renamed"
	Source string
}

// LoadDeprecatedPackages reads the profiles/package.deprecated file
// from the overlay and returns a set of deprecated package atoms.
func LoadDeprecatedPackages(overlayDir string) map[string]DeprecatedInfo {
	result := make(map[string]DeprecatedInfo)

	// Read package.deprecated
	deprecatedPath := filepath.Join(overlayDir, "profiles", "package.deprecated")
	if data, err := os.ReadFile(deprecatedPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			// Skip comments and empty lines
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			result[line] = DeprecatedInfo{
				Package: line,
				Source:  "deprecated",
			}
		}
	}

	// Read profiles/updates/* for renames/moves
	updatesDir := filepath.Join(overlayDir, "profiles", "updates")
	if entries, err := os.ReadDir(updatesDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			updatePath := filepath.Join(updatesDir, entry.Name())
			if data, err := os.ReadFile(updatePath); err == nil {
				for _, line := range strings.Split(string(data), "\n") {
					line = strings.TrimSpace(line)
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}
					// Format: move old-package new-package
					// Format: rename old-package new-package
					parts := strings.Fields(line)
					if len(parts) >= 3 && (parts[0] == "move" || parts[0] == "rename") {
						oldAtom := parts[1]
						newAtom := parts[2]
						result[oldAtom] = DeprecatedInfo{
							Package: oldAtom,
							Target:  newAtom,
							Source:  "renamed",
						}
					}
				}
			}
		}
	}

	return result
}

// IsDeprecated checks if a package atom is deprecated or renamed.
func (di *DeprecatedInfo) IsDeprecated() bool {
	return di != nil && (di.Source == "deprecated" || di.Source == "renamed")
}
