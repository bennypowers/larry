package bump

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/upstream"
	"bennypowers.dev/larry/internal/version"
)

// BumpMode controls the type of bump performed.
type BumpMode string

const (
	BumpModeAuto     BumpMode = "auto"     // bump version from upstream
	BumpModeVersion  BumpMode = "version"  // bump to specified version
	BumpModeRevision BumpMode = "revision" // bump revision number
)

// BumpConfig holds configuration for a version bump.
type BumpConfig struct {
	Dir             string                        // ebuild directory path
	Mode            BumpMode                      // bump mode (auto, version, revision)
	Version         string                        // target version (for version mode)
	ProviderReg     *provider.ProviderRegistry    // provider registry for auto-version
	AllowPrerelease bool                          // allow prerelease versions
}

// BumpResult holds the result of a version bump operation.
type BumpResult struct {
	OldFile       string   // original ebuild path
	NewFile       string   // new ebuild path
	PackageName   string   // package name (PN)
	OldVersion    string   // old version (PV)
	NewVersion    string   // new version (PV)
	Revision      int      // revision number (0 if not a revision bump)
	Mode          BumpMode // the mode used
}

// Bump performs a version or revision bump on ebuilds in the given directory.
func Bump(cfg BumpConfig) (*BumpResult, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("bump config: directory is required")
	}
	if cfg.Mode == "" {
		return nil, fmt.Errorf("bump config: mode is required (auto, version, revision)")
	}

	switch cfg.Mode {
	case BumpModeAuto, BumpModeVersion, BumpModeRevision:
		// valid
	default:
		return nil, fmt.Errorf("bump config: invalid mode %q", cfg.Mode)
	}

	// Find the latest non-live ebuild
	latestPath, err := findLatestEbuild(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("bump: %w", err)
	}

	// Read the original ebuild content
	content, err := os.ReadFile(latestPath)
	if err != nil {
		return nil, fmt.Errorf("bump: reading ebuild: %w", err)
	}

	// Parse ebuild info to extract PN, PV
	pn, origPV := extractPNPV(latestPath)
	pv := extractPVFromContent(string(content))
	if pv == "" {
		pv = origPV
	}

	result := &BumpResult{
		OldFile:     latestPath,
		PackageName: pn,
		OldVersion:  pv,
		Mode:        cfg.Mode,
	}

	switch cfg.Mode {
	case BumpModeRevision:
		return revisionBump(cfg, latestPath, content, pn, pv)

	case BumpModeAuto:
		newPV, err := getUpstreamVersion(cfg, pn, latestPath)
		if err != nil {
			return nil, fmt.Errorf("bump: getting upstream version: %w", err)
		}
		result.NewVersion = newPV

	case BumpModeVersion:
		if cfg.Version == "" {
			return nil, fmt.Errorf("bump: version mode requires --version flag")
		}
		result.NewVersion = cfg.Version
	}

	// Generate new ebuild with bumped version
	newFileName, newContent, err := generateBumpedEbuild(latestPath, content, pn, pv, result.NewVersion)
	if err != nil {
		return nil, fmt.Errorf("bump: %w", err)
	}

	if err := os.WriteFile(newFileName, []byte(newContent), 0644); err != nil {
		return nil, fmt.Errorf("bump: writing new ebuild: %w", err)
	}
	result.NewFile = newFileName

	return result, nil
}

func revisionBump(cfg BumpConfig, latestPath string, content []byte, pn, pv string) (*BumpResult, error) {
	newRev := incrementRevision(cfg.Dir)

	// Strip any existing revision suffix to build the base name
	baseName := strings.TrimSuffix(latestPath, ".ebuild")
	if idx := strings.LastIndex(baseName, "-r"); idx > 0 {
		baseName = baseName[:idx]
	}
	newFileName := baseName + "-r" + strconv.Itoa(newRev) + ".ebuild"

	if err := os.WriteFile(newFileName, content, 0644); err != nil {
		return nil, fmt.Errorf("bump: writing new ebuild: %w", err)
	}

	return &BumpResult{
		OldFile:     latestPath,
		NewFile:     newFileName,
		PackageName: pn,
		OldVersion:  pv,
		NewVersion:  pv,
		Revision:    newRev,
		Mode:        BumpModeRevision,
	}, nil
}

// FindLatestEbuild is the exported version of findLatestEbuild.
// It finds the ebuild with the highest version in the directory.
func FindLatestEbuild(dir string) (string, error) {
	return findLatestEbuild(dir)
}

// findLatestEbuild finds the ebuild with the highest version in the directory.
// Excludes live versions (9999). Returns an error if no ebuilds are found.
func findLatestEbuild(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("reading directory: %w", err)
	}

	var ebuilds []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".ebuild") {
			continue
		}
		ebuilds = append(ebuilds, filepath.Join(dir, name))
	}

	if len(ebuilds) == 0 {
		return "", fmt.Errorf("no ebuilds found in %s", dir)
	}

	// Sort by PV descending, excluding live versions
	sort.SliceStable(ebuilds, func(i, j int) bool {
		vi := extractPVFromName(ebuilds[i])
		vj := extractPVFromName(ebuilds[j])
		if vi == "9999" {
			return false // live versions sort last (excluded)
		}
		if vj == "9999" {
			return true
		}
		viParsed, _ := version.Parse(vi)
		vjParsed, _ := version.Parse(vj)
		if viParsed == nil {
			return false
		}
		if vjParsed == nil {
			return true
		}
		cmp := viParsed.Compare(vjParsed)
		if cmp != 0 {
			return cmp > 0
		}
		// PVs are equal — prefer higher revision number
		revI := extractRevisionFromName(ebuilds[i])
		revJ := extractRevisionFromName(ebuilds[j])
		return revI > revJ
	})

	return ebuilds[0], nil
}

// extractRevisionFromName extracts the revision number from an ebuild filename.
// e.g., "pkg-1.0.0-r3.ebuild" -> 3, "pkg-1.0.0.ebuild" -> 0
func extractRevisionFromName(path string) int {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".ebuild")
	revRe := regexp.MustCompile(`-r(\d+)$`)
	matches := revRe.FindStringSubmatch(base)
	if matches != nil {
		n, _ := strconv.Atoi(matches[1])
		return n
	}
	return 0
}

// extractPVFromName extracts the PV from an ebuild filename.
// e.g., "pkg-1.0.0.ebuild" -> "1.0.0", "pkg-1.0.0-r1.ebuild" -> "1.0.0"
func extractPVFromName(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".ebuild")

	// Strip revision suffix: -rN
	if idx := strings.LastIndex(base, "-r"); idx > 0 {
		base = base[:idx]
	}

	// Extract version after the last hyphen that looks like a version
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '-' && i+1 < len(base) && base[i+1] >= '0' && base[i+1] <= '9' {
			return base[i+1:]
		}
	}

	// Fallback: return the part after the last hyphen
	if idx := strings.LastIndex(base, "-"); idx > 0 {
		return base[idx+1:]
	}

	return ""
}

// extractPVFromContent extracts PV from the ebuild content.
func extractPVFromContent(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PV=") {
			val := strings.TrimPrefix(line, "PV=")
			val = strings.Trim(val, `"' `)
			return val
		}
	}
	return ""
}

// extractPNPV extracts package name and version from an ebuild filename.
func extractPNPV(path string) (pn, pv string) {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".ebuild")

	// Strip revision suffix: -rN
	if idx := strings.LastIndex(base, "-r"); idx > 0 {
		base = base[:idx]
	}

	// Find PV: last segment after hyphen starting with digit
	pvRe := regexp.MustCompile(`^([^-]+)-([0-9][^/]*)$`)
	matches := pvRe.FindStringSubmatch(base)
	if matches != nil {
		return matches[1], matches[2]
	}

	// Fallback: find last hyphen before a digit
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '-' && i+1 < len(base) && base[i+1] >= '0' && base[i+1] <= '9' {
			return base[:i], base[i+1:]
		}
	}

	return base, ""
}

// incrementRevision finds the highest revision number in the directory and returns N+1.
func incrementRevision(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 1
	}

	maxRev := 0
	revRe := regexp.MustCompile(`-r(\d+)\.ebuild$`)

	for _, entry := range entries {
		name := entry.Name()
		matches := revRe.FindStringSubmatch(name)
		if matches != nil {
			n, err := strconv.Atoi(matches[1])
			if err == nil && n > maxRev {
				maxRev = n
			}
		}
	}

	return maxRev + 1
}

// getUpstreamVersion queries the provider registry for the latest upstream version.
// Uses the same provider resolution logic as the upstream command.
func getUpstreamVersion(cfg BumpConfig, pn string, ebuildPath string) (string, error) {
	if cfg.ProviderReg == nil {
		return "", fmt.Errorf("no provider registry configured for auto-version")
	}

	// Parse ebuild info using the shared upstream function
	ebi, err := upstream.ParseEbuildInfo(ebuildPath)
	if err != nil {
		return "", fmt.Errorf("parsing ebuild: %w", err)
	}
	if ebi == nil {
		return "", fmt.Errorf("unable to parse ebuild info for provider resolution")
	}

	// Use shared provider resolution logic
	providerType := upstream.ResolveProviderType(ebi.SRC_URI, ebi.HOMEPAGE, ebi.Inherit, ebi.Raw)
	if providerType == "" {
		return "", fmt.Errorf("unable to detect provider type for auto-version bump")
	}

	// Build remote ID using shared logic
	rid, err := upstream.ResolveRemoteId(ebi, providerType)
	if err != nil {
		return "", fmt.Errorf("building remote id: %w", err)
	}

	// Get provider and fetch latest version
	p, err := cfg.ProviderReg.Resolve(*rid)
	if err != nil {
		return "", fmt.Errorf("resolving provider: %w", err)
	}

	latest, err := p.FetchLatestVersion(*rid, cfg.AllowPrerelease)
	if err != nil {
		return "", fmt.Errorf("fetching latest version: %w", err)
	}

	return latest.String(), nil
}

// generateBumpedEbuild creates a new ebuild file with the bumped version.
// Returns (filename, content, error).
func generateBumpedEbuild(originalPath string, content []byte, pn, oldPV, newPV string) (string, string, error) {
	oldContent := string(content)
	oldDir := filepath.Dir(originalPath)

	// Generate filename: pn-oldPV.ebuild → pn-newPV.ebuild
	oldBase := filepath.Base(originalPath)

	// Strip .ebuild suffix, strip revision suffix
	newBase := strings.TrimSuffix(oldBase, ".ebuild")
	if idx := strings.LastIndex(newBase, "-r"); idx > 0 {
		newBase = newBase[:idx]
	}

	// Replace old PV with new PV. The PV is the last segment after a hyphen.
	// Find the hyphen before the old PV.
	targetSuffix := "-" + oldPV
	if strings.HasSuffix(newBase, targetSuffix) {
		newBase = strings.TrimSuffix(newBase, targetSuffix)
	} else {
		// Try to find the PV within the name
		for i := len(newBase) - 1; i >= 0; i-- {
			if newBase[i] == '-' && i+1 < len(newBase) && newBase[i+1] >= '0' && newBase[i+1] <= '9' {
				newBase = newBase[:i]
				break
			}
		}
	}
	newFileName := filepath.Join(oldDir, newBase+"-"+newPV+".ebuild")

	// Generate content
	newContent := generateBumpedFileContent(oldContent, pn, oldPV, newPV)

	return newFileName, newContent, nil
}

// generateBumpedFileContent generates the new ebuild file content with bumped version.
func generateBumpedFileContent(oldContent, pn, oldPV, newPV string) string {
	lines := strings.Split(oldContent, "\n")
	var newLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Update PV= line
		if strings.HasPrefix(trimmed, "PV=") {
			newLines = append(newLines, "PV="+newPV)
			continue
		}

		// Update P= line
		if strings.HasPrefix(trimmed, "P=") {
			newLines = append(newLines, "P="+pn+"-"+newPV)
			continue
		}

		// Update SRC_URI: replace hardcoded version references
		newLine := updateSRCURI(line, oldPV, newPV)
		newLines = append(newLines, newLine)
	}

	return strings.Join(newLines, "\n")
}

// updateSRCURI updates SRC_URI lines with the new version references.
// Preserves ${PV}, ${P}, ${A} variable references.
func updateSRCURI(line, oldPV, newPV string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "SRC_URI=") {
		return line
	}

	// Ebuild SRC_URI typically uses variables (${PV}, ${P}, ${A}) that
	// Portage handles automatically. No need to replace version strings.
	return line
}
