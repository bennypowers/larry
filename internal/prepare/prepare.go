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
	"bennypowers.dev/larry/internal/github"
	"bennypowers.dev/larry/internal/logger"
	"bennypowers.dev/larry/internal/upstream"
)

// PrepareConfig holds configuration for the prepare command.
type PrepareConfig struct {
	DryRun                  bool
	Distdir                 string
	Overlay                 string // overlay root path for manifest
	GitHubRepo              string // GitHub repo (owner/repo) for release operations
	UseTUI                  bool
	ForceType               ebuild.PackageType
	HTTPClient              *http.Client
	CleanupOrphanedAssets   bool // removes orphaned GitHub assets on prepare failure
	RevisionBump            bool // skip source download, only run manifest

	// GitHubBaseURL is the scheme+host used to build archive URLs.
	// Empty means "https://github.com". Tests set it to an httptest server URL.
	GitHubBaseURL string
	// Runner executes external commands (go mod vendor). Nil means the real runner.
	Runner github.CmdRunner
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

	// Load per-package .larry.yaml config for type override and cleanup settings.
	pkgConfig, err := LoadConfig(ei.Path)
	if err != nil {
		// Non-fatal: only log in verbose mode, continue with defaults.
		logger.Debugf("read config: %v", err)
	}

	// Detect type: CLI --type flag > .larry.yaml type_override > auto-detection.
	var pkgType ebuild.PackageType
	if cfg.ForceType != "" {
		pkgType = cfg.ForceType
	} else if pkgConfig != nil && pkgConfig.TypeOverride != "" {
		pkgType = ebuild.PackageType(pkgConfig.TypeOverride)
	} else {
		pkgType, err = ebuild.DetectTypeFromInfo(ei)
		if err != nil {
			return err
		}
	}

	logger.Debug("detected type: " + string(pkgType))
	logger.Debug("ebuild: " + ei.Path)

	// Propagate cleanup setting from .larry.yaml if not already set via CLI.
	if !cfg.CleanupOrphanedAssets && pkgConfig != nil {
		cfg.CleanupOrphanedAssets = pkgConfig.CleanupOrphanedAssets
	}

	// Dispatch to type-specific pipeline
	switch pkgType {
	case ebuild.TypeNPM:
		return prepareNpm(ei, cfg)
	case ebuild.TypeCargo:
		return prepareCargo(ei, cfg)
	case ebuild.TypeGo:
		return PrepareGo(ei, cfg)
	case ebuild.TypeVendored:
		return PrepareVendored(ei, cfg)
	default:
		return prepareGeneric(ei, cfg)
	}
}

// PrepareFile prepares one ebuild. Tests replace it to simulate per-target failures.
var PrepareFile = PrepareFromFile

// PrepareFromFile prepares a single ebuild from file path.
// Uses the shared upstream parser for accurate multi-line value extraction.
func PrepareFromFile(path string, cfg *PrepareConfig) error {
	einfo, err := upstream.ParseEbuildInfo(path)
	if err != nil {
		return fmt.Errorf("parse ebuild info: %w", err)
	}
	if einfo == nil {
		return fmt.Errorf("parse ebuild info: no info found for %s", path)
	}
	einfo.Path = path
	return Prepare(einfo, cfg)
}

// PrepareManifestAll runs ONE bulk `pkgdev manifest -d <distdir>` from the overlay root.
// Used only by `larry prepare --all` with no positional arguments.
// It does NOT run any type-specific pipeline.
func PrepareManifestAll(overlay, distdir string) error {
	if distdir == "" {
		return fmt.Errorf("manifest: distdir is empty")
	}
	// mkdir -p $DISTDIR
	if err := os.MkdirAll(distdir, 0o755); err != nil {
		return fmt.Errorf("mkdir distdir: %w", err)
	}

	// Collect all category/package directories under the overlay root
	// that contain at least one .ebuild, then run pkgdev manifest per target.
	dirs, err := collectCatPkgs(overlay)
	if err != nil {
		return fmt.Errorf("collect cat/pkg: %w", err)
	}
	for _, cp := range dirs {
		bin2, style2, err2 := findManifestRunner()
		if err2 != nil {
			return err2
		}
		args := manifestArgs(style2, distdir, cp, "")
		if err := ManifestRunner(bin2, args, overlay, append(os.Environ(), "DISTDIR="+distdir)); err != nil {
			return fmt.Errorf("manifest %s: %w", cp, err)
		}
	}
	return nil
}

// collectCatPkgs returns category/package directory names under overlay root.
func collectCatPkgs(overlay string) ([]string, error) {
	entries, err := os.ReadDir(overlay)
	if err != nil {
		return nil, err
	}
	var catPkgs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		subDir := filepath.Join(overlay, e.Name())
		subEntries, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if !se.IsDir() {
				continue
			}
			ebuildEntries, err2 := os.ReadDir(filepath.Join(subDir, se.Name()))
			if err2 != nil {
				continue
			}
			hasEbuild := false
			for _, ee := range ebuildEntries {
				if strings.HasSuffix(ee.Name(), ".ebuild") && !strings.HasSuffix(ee.Name(), ".ebuild.bak") {
					hasEbuild = true
					break
				}
			}
			if hasEbuild {
				catPkgs = append(catPkgs, e.Name()+"/"+se.Name())
			}
		}
	}
	return catPkgs, nil
}

// PrepareDir runs the full per-type pipeline for every *.ebuild in dir,
// counting failures instead of aborting on the first one (D9).
func PrepareDir(dir string, cfg *PrepareConfig) error {
	files, err := collectEbuilds(dir)
	if err != nil {
		return fmt.Errorf("collect ebuilds: %w", err)
	}

	var failures int
	for _, f := range files {
		if err := PrepareFile(f, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "prepare %s: %v\n", f, err)
			failures++
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d package(s) failed", failures)
	}
	return nil
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
	defer os.Remove(ei.Path + ".bak")

	// Run manifest
	req := manifestRequest{
		Overlay:    cfg.Overlay,
		CatPkg:     ei.Category + "/" + ei.PN(),
		Distdir:    cfg.Distdir,
		EbuildPath: ei.Path,
		Prefetch:   nil,
	}
	if err := runManifest(req); err != nil {
		// Rollback on failure
		Restore(ei.Path)
		return fmt.Errorf("generic: manifest: %w", err)
	}

	return nil
}

// prepareNpm runs the npm-specific prepare pipeline.
func prepareNpm(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error {
	return npmPrepare(ei, cfg)
}

// prepareCargo runs the cargo-specific prepare pipeline.
func prepareCargo(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error {
	return PrepareCargo(ei, cfg)
}

// NewEbuildInfoFromPath creates an EbuildInfo from a file path.
// Uses the shared upstream parser for accurate multi-line value extraction.
func NewEbuildInfoFromPath(path string) (*ebuild.EbuildInfo, error) {
	return upstream.ParseEbuildInfo(path)
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
//
// The script runs inside a userspace sandbox modeled after Portage's sandbox:
//
//	SANDBOXED (userspace policy):
//	  - Environment is sanitized (dangerous variables unset)
//	  - PATH restricted to system directories only
//	  - Write access limited to: ebuild directory, DISTDIR
//
//	NOT SANDBOXED (limitations):
//	  - No syscall-level file access control (unlike Portage's ld.so preload)
//	  - Network access is NOT blocked
//	  - No PID or mount namespace isolation
//	  - No user/group switching
//
// To achieve true sandboxing, run larry inside a container or use --emerge.
func runCustomPrepare(ei *ebuild.EbuildInfo, scriptPath string, cfg *PrepareConfig) error {
	if cfg.DryRun {
		return nil
	}

	// Warn about sandbox limitations (once)
	fmt.Fprint(os.Stderr, "\nWARNING: custom prepare.sh runs with partial sandboxing. "+
		"Network access is NOT blocked and file writes are only policy-restricted. "+
		"For full sandboxing, use larry test --emerge or run inside a container.\n")

	// Build sandboxed environment
	env := sandboxEnv(ei, cfg)

	cmd := exec.Command(scriptPath)
	cmd.Env = env
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

// sandboxEnv builds a sandboxed environment for custom prepare scripts,
// modeled after Portage's ebuild.sh environment handling.
func sandboxEnv(ei *ebuild.EbuildInfo, cfg *PrepareConfig) []string {
	// Start with a minimal set of inherited env vars
	systemDirs := []string{
		"/usr/local/sbin", "/usr/local/bin",
		"/usr/sbin", "/usr/bin",
		"/sbin", "/bin",
	}
	systemPath := strings.Join(systemDirs, ":")

	// Dangerous vars to strip (matching Portage's unset list)
	dangerousVars := map[string]bool{
		"GZIP": true, "BZIP": true, "BZIP2": true, "CDPATH": true,
		"GREP_OPTIONS": true, "GREP_COLOR": true, "GLOBIGNORE": true,
		"BASH_COMPAT": true, "BASH_ENV": true,
		// Shell options that can affect behavior
		"PROMPT_COMMAND": true, "PROMPT_DIRTRIM": true,
	}

	env := make([]string, 0, 64)
	for _, e := range os.Environ() {
		key := strings.SplitN(e, "=", 2)[0]
		// Whitelist safe vars, strip dangerous ones
		if dangerousVars[key] {
			continue
		}
		if key == "PATH" {
			// Restrict PATH to system directories only
			env = append(env, "PATH="+systemPath)
			continue
		}
		env = append(env, e)
	}

	// Add larry-specific env vars
	env = append(env,
		"EBUILD="+ei.Path,
		"PN="+ei.PN(),
		"PV="+ei.PV(),
		"P="+ei.P(),
		"CATEGORY="+ei.Category,
		"DISTDIR="+cfg.Distdir,
		"LARRY_PREPARE=1",
		// Sandbox policy (informational — userspace only, no syscall enforcement)
		"LARRY_SANDBOX_WRITE="+filepath.Dir(ei.Path)+":"+cfg.Distdir,
		"LARRY_SANDBOX_READ=/",
	)

	// Temp dir — restrict to a larry-owned temp dir
	tmpDir, _ := os.MkdirTemp("", "larry-prepare-")
	defer os.RemoveAll(tmpDir)
	env = append(env,
		"TMPDIR="+tmpDir,
		"TMP="+tmpDir,
		"TEMP="+tmpDir,
	)

	return env
}
