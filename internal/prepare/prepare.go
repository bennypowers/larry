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
	"bennypowers.dev/larry/internal/larryconfig"
	"bennypowers.dev/larry/internal/logger"
	"bennypowers.dev/larry/internal/output"
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
	CustomPrepareScript     string // relative path (from pkg dir) to custom prepare script; default: .larry/prepare.sh

	// GitHubBaseURL is the scheme+host used to build archive URLs.
	// Empty means "https://github.com". Tests set it to an httptest server URL.
	GitHubBaseURL string
	// Runner executes external commands (go mod vendor). Nil means the real runner.
	Runner github.CmdRunner

	// Log is the logger used for structured output. When nil, prepareGeneric
	// emits warnings to stderr directly (fallback for code paths that don't
	// construct a full config). Tests inject a logger with SetOut/SetErr
	// pointing at bytes.Buffer for capture.
	Log *output.Logger
}

// Prepare handles the ebuild preparation pipeline.
func Prepare(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error {
	if cfg == nil {
		cfg = &PrepareConfig{}
	}

	// Version-pinned ${FILESDIR} references that do not resolve break
	// src_install — warn about them before any work happens. Resolving
	// pins stay silent: the pattern is fragile but not broken, and
	// warning on every prepare would be noise.
	raw := ei.Raw
	if raw == "" {
		if data, err := os.ReadFile(ei.Path); err == nil {
			raw = string(data)
		}
	}
	for _, w := range ebuild.CheckFilesdirPins(ei.Path, raw) {
		if w.Missing {
			warnToLogger(cfg, w.Message)
		}
	}

	// Load per-package config (reads .larry/config.yaml).
	pkgDir := filepath.Dir(ei.Path)
	pkgConfig, err := larryconfig.LoadConfig(pkgDir)
	if err != nil {
		// Non-fatal: only log in verbose mode, continue with defaults.
		logger.Debugf("read config: %v", err)
	}

	// Check for custom prepare script first.
	// If it exists under .larry/ in the package dir, run it instead of
	// any built-in pipeline. This is an overlay-maintainer concern —
	// it lives outside the ebuild's files/ so it doesn't ship to end users.
	customScript := larryconfig.ResolvePrepareScript(pkgDir, pkgConfig)
	if has, _ := larryconfig.HasPrepareScript(pkgDir); has {
		return runCustomPrepare(ei, customScript, cfg)
	}

	// Detect type: CLI --type flag > .larry/config.yaml type_override > auto-detection.
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

	// Propagate cleanup setting from .larry/config.yaml if not already
	// set via CLI.
	if !cfg.CleanupOrphanedAssets && pkgConfig != nil {
		cfg.CleanupOrphanedAssets = pkgConfig.CleanupOrphanedAssets
	}

	// Dispatch to type-specific pipeline
	// (keep this comment to preserve line numbering for existing tests)
	switch pkgType {
	case ebuild.TypeNPM:
		return npmPrepare(ei, cfg)
	case ebuild.TypeCargo:
		return PrepareCargo(ei, cfg)
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

// genericManifestOnlyWarning is emitted when a package type has no pipeline
// and only the Manifest was regenerated. It points the user at the escape hatch.
const genericManifestOnlyWarning = "this package type has no pipeline; only the Manifest was regenerated — add " + larryconfig.Dir + "/" + larryconfig.PrepareScript + " to do more"

// prepareGeneric runs pkgdev manifest on a generic ebuild.
func prepareGeneric(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error {
	// Warn: this type has no pipeline beyond the Manifest.
	warnToLogger(cfg, genericManifestOnlyWarning)

	// In dry-run mode, skip manifest
	if cfg.DryRun {
		return nil
	}

	// Backup the ebuild
	if err := Backup(ei.Path); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer func() { _ = os.Remove(ei.Path + ".bak") }() // best-effort cleanup

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
		_ = Restore(ei.Path) // best-effort recovery after failure
		return fmt.Errorf("generic: manifest: %w", err)
	}

	return nil
}

// warnToLogger emits a warning via cfg.Log.Warn when available, falling back
// to a bare fmt.Fprintf to stderr (the old behaviour) when Log is nil.
func warnToLogger(cfg *PrepareConfig, msg string) {
	if cfg != nil && cfg.Log != nil {
		cfg.Log.Warn("%s", msg)
		return
	}
	fmt.Fprintf(os.Stderr, "*** %s\n", msg)
}

// parsePVRaw parses a PV string and returns (basePV, revision).
func parsePVRaw(pv string) (string, int) {
	// Check for revision suffix: -rN
	revRegex := regexp.MustCompile(`-r(\d+)$`)
	if matches := revRegex.FindStringSubmatch(pv); matches != nil {
		pv = strings.TrimSuffix(pv, matches[0])
		var rev int
		_, _ = fmt.Sscanf(matches[1], "%d", &rev) // matches[1] is \d+ from regex; error ignored // matches[1] is already \d+ from regex, error ignored
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

// customPrepareSandboxWarning is printed once to alert users that the
// custom prepare script runs in a userspace sandbox (not a full container).
const customPrepareSandboxWarning = "custom prepare.sh runs with partial sandboxing. " +
	"Network access is NOT blocked and file writes are only policy-restricted. " +
	"For full sandboxing, use larry test --emerge or run inside a container."

// runCustomPrepare executes a per-package custom prepare script.
// If dry-run mode is enabled, it returns immediately without executing anything.
//
// Pipeline: backup → run script → manifest → restore on any failure.
// The framework owns rollback, not the script.
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

	// Warn about sandbox limitations (quiet-suppressed)
	warnToLogger(cfg, customPrepareSandboxWarning)

	// Backup the ebuild before running the script.
	if err := Backup(ei.Path); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer func() { _ = os.Remove(ei.Path + ".bak") }() // best-effort cleanup

	// Sandbox temp dir. Created here (not inside sandboxEnv) so it stays
	// alive for the script's whole execution AND the manifest step below —
	// sandboxEnv returns before the script runs, so a temp dir created (and
	// deferred-removed) there would already be gone by the time the script
	// uses TMPDIR. The deferred cleanup fires only when runCustomPrepare
	// returns, after both steps complete.
	tmpDir, err := os.MkdirTemp("/tmp", "larry-prepare-")
	if err != nil {
		return fmt.Errorf("create sandbox temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }() // best-effort cleanup

	// Build sandboxed environment
	env := sandboxEnv(ei, cfg, tmpDir)

	// The child chdirs into the package dir before exec, so a relative
	// script path would resolve against the package dir instead of larry's
	// working directory. Absolutize against the current dir first.
	if !filepath.IsAbs(scriptPath) {
		if abs, err := filepath.Abs(scriptPath); err == nil {
			scriptPath = abs
		}
	}

	cmd := exec.Command(scriptPath)
	cmd.Env = env
	cmd.Dir = filepath.Dir(ei.Path)

	// Pipe stdout/stderr unless TUI is active (TUI has its own stream isolation).
	if !cfg.UseTUI {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Run(); err != nil {
		// Rollback ebuild on script failure.
		_ = Restore(ei.Path) // best-effort recovery after failure
		return fmt.Errorf("custom prepare script %q failed: %w", scriptPath, err)
	}

	// Run manifest for this package (catPkg, not a file path).
	//
	// The single-ebuild target makes pkgdev manifest merge into the
	// existing Manifest, keeping existing DIST lines untouched — right
	// when the hook changed nothing, wrong when it regenerated a
	// distfile: the stale entry would survive and break fetches later
	// (merge mode never re-hashes, and -f verifies against the stale
	// entries first). Detect mismatched entries and fall back to a full
	// package regeneration, which drops the Manifest first.
	manifestTarget := ei.Path
	if manifestEntriesStale(filepath.Join(filepath.Dir(ei.Path), "Manifest"), cfg.Distdir) {
		logger.Debug("custom prepare: stale Manifest entries, forcing full package regeneration")
		manifestTarget = ""
	}
	req := manifestRequest{
		Overlay:    cfg.Overlay,
		CatPkg:     ei.Category + "/" + ei.PN(),
		Distdir:    cfg.Distdir,
		EbuildPath: manifestTarget,
		Prefetch:   nil,
	}
	if err := runManifest(req); err != nil {
		// Rollback ebuild on manifest failure.
		_ = Restore(ei.Path) // best-effort recovery after failure
		return fmt.Errorf("manifest: %w", err)
	}

	return nil
}

// sandboxEnv builds a sandboxed environment for custom prepare scripts,
// modeled after Portage's ebuild.sh environment handling. tmpDir is the
// larry-owned temp dir exposed as TMPDIR/TMP/TEMP; the caller owns its
// lifetime (it must outlive the script's execution).
func sandboxEnv(ei *ebuild.EbuildInfo, cfg *PrepareConfig, tmpDir string) []string {
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

	// Temp dir — restrict to a larry-owned temp dir (caller-managed lifetime)
	env = append(env,
		"TMPDIR="+tmpDir,
		"TMP="+tmpDir,
		"TEMP="+tmpDir,
	)

	return env
}
