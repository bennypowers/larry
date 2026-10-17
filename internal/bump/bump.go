package bump

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/larryconfig"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/providers"
	"bennypowers.dev/larry/internal/upstream"
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
	Warnings      []string // advisory warnings, e.g. version-pinned FILESDIR refs
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

	// Find the latest non-live ebuild via atom
	latest, err := atom.FindLatest(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("bump: %w", err)
	}

	latestPath := latest.Path

	// Read the original ebuild content
	content, err := os.ReadFile(latestPath)
	if err != nil {
		return nil, fmt.Errorf("bump: reading ebuild: %w", err)
	}

	// Parse ebuild info to extract PN, PV
	pn := latest.PN
	pv := extractPVFromContent(string(content))
	if pv == "" {
		pv = latest.PV
	}

	result := &BumpResult{
		OldFile:     latestPath,
		PackageName: pn,
		OldVersion:  pv,
		Mode:        cfg.Mode,
	}

	switch cfg.Mode {
	case BumpModeRevision:
		return revisionBump(latest, content)

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
	// Skip if old and new versions are the same (no bump needed).
	if result.NewVersion == result.OldVersion {
		return nil, fmt.Errorf("already at latest version %s", result.OldVersion)
	}

	newFileName, newContent, err := generateBumpedEbuild(latest, content, pn, pv, result.NewVersion)
	if err != nil {
		return nil, fmt.Errorf("bump: %w", err)
	}

	if err := os.WriteFile(newFileName, []byte(newContent), 0644); err != nil {
		return nil, fmt.Errorf("bump: writing new ebuild: %w", err)
	}
	result.NewFile = newFileName

	// Version-pinned ${FILESDIR} references re-evaluate to the new version,
	// but files/ entries are not carried over by bumps — surface those that
	// no longer resolve (plus the fragile pattern itself).
	for _, w := range ebuild.CheckFilesdirPins(newFileName, newContent) {
		result.Warnings = append(result.Warnings, w.Message)
	}

	return result, nil
}

func revisionBump(latest *atom.ParsedEbuild, content []byte) (*BumpResult, error) {
	// D8 fix: derive revision from the latest ebuild, not max over all files.
	_, curRev := atom.SplitRevision(latest.PVR)
	newRev := curRev + 1
	newRevStr := atom.NextRevision(latest.PVR)

	newFileName := filepath.Join(filepath.Dir(latest.Path), latest.PN+"-"+newRevStr+".ebuild")

	if err := os.WriteFile(newFileName, content, 0644); err != nil {
		return nil, fmt.Errorf("bump: writing new ebuild: %w", err)
	}

	return &BumpResult{
		OldFile:     latest.Path,
		NewFile:     newFileName,
		PackageName: latest.PN,
		OldVersion:  latest.PV,
		NewVersion:  latest.PV,
		Revision:    newRev,
		Mode:        BumpModeRevision,
	}, nil
}

// FindLatestEbuild is the exported version of findLatestEbuild.
// It finds the ebuild with the highest version in the directory.
func FindLatestEbuild(dir string) (string, error) {
	pe, err := atom.FindLatest(dir)
	if err != nil {
		return "", err
	}
	return pe.Path, nil
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

// getUpstreamVersion queries the provider registry for the latest upstream version.
// Loads .larry/config.yaml per-package config (tag_match, version_template) and wires it
// onto the GitHubProvider so that date-based tags like stable_20260911 are expanded
// into the full Gentoo version (e.g. 6.18.46_p20260911).
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
	providerType := upstream.ResolveProviderType(ebuildPath, ebi.SRC_URI, ebi.HOMEPAGE, ebi.Inherit, ebi.Raw)
	if providerType == "" {
		return "", fmt.Errorf("unable to detect provider type for auto-version bump")
	}

	// Build remote ID using shared logic
	rid, err := upstream.ResolveRemoteId(ebi, providerType)
	if err != nil {
		return "", fmt.Errorf("building remote id: %w", err)
	}
	if rid == nil {
		return "", fmt.Errorf("could not resolve remote id for %s", providerType)
	}

	// Load per-package .larry/config.yaml upstream config and wire ResolveOptions
	// onto the provider (same logic as upstream command's resolveOne).
	var resOpts *larryconfig.ResolveOptions
	if pkgDir := filepath.Dir(ebuildPath); pkgDir != "" {
		if lcfg, lerr := larryconfig.LoadConfig(pkgDir); lerr == nil && lcfg.Upstream != nil {
			if opts, cerr := lcfg.Upstream.Compile(ebi.PVVal); cerr == nil {
				resOpts = opts
			}
		}
	}

	// Get provider and wire ResolveOptions onto GitHubProvider.
	p, err := cfg.ProviderReg.Resolve(*rid)
	if err != nil {
		return "", fmt.Errorf("resolving provider: %w", err)
	}
	if ghProv, ok := p.(*providers.GitHubProvider); ok {
		ghProv.ResolveOptions = resOpts
	}

	// Try FetchResolution first so we can apply version_template for ByDate.
	if rt, ok := p.(provider.ResolverWithTags); ok {
		res, rerr := rt.FetchResolution(*rid, cfg.AllowPrerelease)
		if rerr == nil && res != nil {
			if res.ByDate && resOpts != nil {
				// Apply tag_match captures + version_template for date-based
				// resolution — same path as the upstream command.
				tagForVersion := res.Tag
				if resOpts.TagMatchRe != nil {
					tagCaps := larryconfig.ExtractCaptures(resOpts.TagMatchRe, res.Tag)
					allVars := make(map[string]string)
					for k, v := range resOpts.PVCaptures {
						allVars[k] = v
					}
					for k, v := range tagCaps {
						allVars[k] = v
					}
					if resOpts.VersionTemplate != "" {
						tagForVersion = larryconfig.ExpandTemplate(resOpts.VersionTemplate, allVars)
					}
				}
				// Return the template-expanded string directly (already in
				// correct Gentoo version format).
				if tagForVersion != res.Tag {
					return tagForVersion, nil
				}
			}
			if res.Version != "" {
				return res.Version, nil
			}
		}
		// When FetchResolution fails or yields nothing, fall through to
		// plain FetchLatestVersion.
	}

	latest, err := p.FetchLatestVersion(*rid, cfg.AllowPrerelease)
	if err != nil {
		return "", fmt.Errorf("fetching latest version: %w", err)
	}

	return latest.String(), nil
}

// generateBumpedEbuild creates a new ebuild file with the bumped version.
// Returns (filename, content, error).
func generateBumpedEbuild(latest *atom.ParsedEbuild, content []byte, pn, oldPV, newPV string) (string, string, error) {
	oldDir := filepath.Dir(latest.Path)

	// New filename: pn-newPV.ebuild
	newFileName := filepath.Join(oldDir, pn+"-"+newPV+".ebuild")

	// Generate content
	newContent := generateBumpedFileContent(string(content), pn, oldPV, newPV)

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
