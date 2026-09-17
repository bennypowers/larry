package prepare

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"bennypowers.dev/larry/internal/ebuild"
)

// PrepareConfig holds configuration for the prepare command.
type PrepareConfig struct {
	DryRun     bool
	Distdir    string
	UseTUI     bool
	ForceType  ebuild.PackageType
	HTTPClient *http.Client
}

// Prepare handles the ebuild preparation pipeline.
func Prepare(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error {
	if cfg == nil {
		cfg = &PrepareConfig{}
	}

	// Check for custom prepare script first.
	// If files/prepare.sh exists in the ebuild's directory, run it
	// instead of any built-in pipeline.
	customScript := filepath.Join(filepath.Dir(ei.Path), "files", "prepare.sh")
	if _, err := os.Stat(customScript); err == nil {
		return runCustomPrepare(ei, customScript, cfg)
	}

	// Detect type (with optional force)
	var pkgType ebuild.PackageType
	var err error
	if cfg.ForceType != "" {
		pkgType = cfg.ForceType
	} else {
		pkgType, err = ebuild.DetectTypeFromInfo(ei)
		if err != nil {
			return err
		}
	}

	// Dispatch to type-specific pipeline
	switch pkgType {
	case ebuild.TypeNPM:
		return prepareNpm(ei, cfg)
	case ebuild.TypeCargo:
		return prepareCargo(ei, cfg)
	case ebuild.TypeGo:
		return prepareGo(ei, cfg)
	case ebuild.TypeVendored:
		return PrepareVendored(ei, cfg)
	default:
		return prepareGeneric(ei, cfg)
	}
}

// PrepareAll prepares all ebuilds in the given directory.
func PrepareAll(dir string, cfg *PrepareConfig) error {
	files, err := collectEbuilds(dir)
	if err != nil {
		return fmt.Errorf("collect ebuilds: %w", err)
	}

	for _, f := range files {
		if err := PrepareFromFile(f, cfg); err != nil {
			return fmt.Errorf("prepare %s: %w", f, err)
		}
	}
	return nil
}

// PrepareFromFile prepares a single ebuild from file path.
func PrepareFromFile(path string, cfg *PrepareConfig) error {
	ebContent, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read ebuild: %w", err)
	}

	einfo, err := parseEbuildInfo(path, ebContent)
	if err != nil {
		return fmt.Errorf("parse ebuild info: %w", err)
	}
	einfo.Path = path
	return Prepare(einfo, cfg)
}

// prepareGeneric runs pkgdev manifest on a generic ebuild.
func prepareGeneric(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error {
	// In dry-run mode, skip manifest
	if cfg.DryRun {
		return nil
	}

	// Backup the ebuild
	if err := Backup(ei.Path); err != nil {
		return fmt.Errorf("backup: %w", err)
	}

	// Run manifest
	if err := runManifest(ei.Path, cfg.Distdir); err != nil {
		// Rollback on failure
		Restore(ei.Path)
		return fmt.Errorf("manifest: %w", err)
	}

	return nil
}

// prepareNpm runs the npm-specific prepare pipeline.
func prepareNpm(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error {
	if cfg.DryRun {
		return nil
	}
	return runManifest(ei.Path, cfg.Distdir)
}

// prepareCargo runs the cargo-specific prepare pipeline.
func prepareCargo(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error {
	return PrepareCargo(ei, cfg)
}

// prepareGo runs the go-specific prepare pipeline.
func prepareGo(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error {
	return PrepareGo(ei, cfg)
}

// NewEbuildInfoFromPath creates an EbuildInfo from a file path.
func NewEbuildInfoFromPath(path string) (*ebuild.EbuildInfo, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseEbuildInfo(path, content)
}

// parseEbuildInfo extracts basic info from ebuild content.
func parseEbuildInfo(path string, content []byte) (*ebuild.EbuildInfo, error) {
	var ei ebuild.EbuildInfo
	ebContent := string(content)
	dir := filepath.Dir(path)
	ebName := filepath.Base(path)

	// Parse category from directory
	ei.Category = filepath.Base(dir)

	// Parse PN from filename: pn-pv[.ebuild]
	name := strings.TrimSuffix(ebName, ".ebuild")
	// Strip revision: pn-pv-rN → pn-pv
	if idx := strings.LastIndex(name, "-r"); idx > 0 {
		name = name[:idx]
	}
	// Find the last hyphen to split PN from PV
	if idx := strings.LastIndex(name, "-"); idx > 0 {
		ei.Pkg = name[:idx]
		ei.PVVal = name[idx+1:]
	}

	// Set P
	if ei.Pkg != "" && ei.PVVal != "" {
		ei.PVal = ei.Pkg + "-" + ei.PVVal
	}

	// Parse SRC_URI
	for _, line := range strings.Split(ebContent, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SRC_URI=") {
			ebURI := strings.TrimPrefix(line, "SRC_URI=")
			ebURI = strings.Trim(ebURI, `"'`)
			ei.SRC_URI = resolveVariables(ebURI, map[string]string{
				"${PN}": ei.Pkg,
				"${PV}": ei.PVVal,
				"${P}":  ei.PVal,
			})
		}
		if strings.HasPrefix(line, "HOMEPAGE=") {
			ebHomepage := strings.TrimPrefix(line, "HOMEPAGE=")
			ebHomepage = strings.Trim(ebHomepage, `"'`)
			ei.HOMEPAGE = ebHomepage
		}
		if strings.HasPrefix(line, "INHERITED=") {
			ebInherited := strings.TrimPrefix(line, "INHERITED=")
			ebInherited = strings.Trim(ebInherited, `"'`)
			ei.Inherit = strings.Fields(ebInherited)
		}
	}

	return &ei, nil
}

// resolveVariables replaces ${VAR} references with values from the map.
func resolveVariables(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, k, v)
	}
	return s
}

// parsePVRaw parses a PV string and returns (basePV, revision).
func parsePVRaw(pv string) (string, int) {
	// Check for revision suffix: -rN
	revRegex := regexp.MustCompile(`-r(\d+)$`)
	if matches := revRegex.FindStringSubmatch(pv); matches != nil {
		pv = strings.TrimSuffix(pv, matches[0])
		var rev int
		fmt.Sscanf(matches[1], "%d", &rev)
		return pv, rev
	}
	return pv, 0
}

// isLiveVersion checks if a PV is a live version (9999).
func isLiveVersion(pv string) bool {
	return pv == "9999"
}

// isRevisionBump checks if the current version is a revision bump.
func isRevisionBump(pv string, rev int) bool {
	return rev > 0
}

// collectEbuilds finds all .ebuild files in a directory.
func collectEbuilds(dir string) ([]string, error) {
	var files []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Only include .ebuild files, not .ebuild.bak
		if strings.HasSuffix(name, ".ebuild") && !strings.HasSuffix(name, ".ebuild.bak") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	return files, nil
}

// getEnv reads an environment variable.
func getEnv(key string) string {
	return os.Getenv(key)
}

// findManifestRunner finds the pkgdev manifest runner binary.
func findManifestRunner() (string, error) {
	// Try pkgdev first (Gentoo tool)
	if path, err := exec.LookPath("pkgdev"); err == nil {
		return path, nil
	}
	// Try ebuild manifest runner
	if path, err := exec.LookPath("ebuild"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("manifest runner not found: try installing pkgdev or ebuild")
}

// manifestArgs returns the command-line arguments for running manifest.
func manifestArgs(path, distdir string) []string {
	args := []string{"manifest", path}
	if distdir != "" {
		// Insert --distdir before the path
		newArgs := []string{"manifest", "--distdir=" + distdir, path}
		return newArgs
	}
	return args
}

// verifyDistfiles checks that all distfiles exist in the distdir.
func verifyDistfiles(distdir string, distfiles []string) bool {
	if distdir == "" {
		return true // no distdir means we don't check
	}
	for _, df := range distfiles {
		path := filepath.Join(distdir, df)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// extractDistfilesFromSRCURI parses distfile names from a resolved SRC_URI.
func extractDistfilesFromSRCURI(srcURI string) []string {
	var distfiles []string
	for _, part := range strings.Fields(srcURI) {
		// Remove any hash URLs (e.g., ?mirror=...)
		if !strings.Contains(part, "?") {
			distfiles = append(distfiles, part)
		}
	}
	return distfiles
}

// runCustomPrepare executes a per-package custom prepare script.
// If dry-run mode is enabled, it logs the script path but does not execute it.
func runCustomPrepare(ei *ebuild.EbuildInfo, scriptPath string, cfg *PrepareConfig) error {
	if cfg.DryRun {
		return nil
	}

	// Build environment for the script
	env := []string{
		"EBUILD=" + ei.Path,
		"PN=" + ei.PN(),
		"PV=" + ei.PV(),
		"P=" + ei.P(),
		"CATEGORY=" + ei.Category,
		"DISTDIR=" + cfg.Distdir,
		"LARRY_PREPARE=1",
	}

	cmd := exec.Command(scriptPath)
	cmd.Env = append(os.Environ(), env...)
	cmd.Dir = filepath.Dir(ei.Path)

	// Pipe stdout/stderr unless TUI is active
	if !cfg.UseTUI {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("custom prepare script %q failed: %w", scriptPath, err)
	}

	return nil
}
