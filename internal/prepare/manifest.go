package prepare

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"bennypowers.dev/larry/internal/logger"
)

// ManifestRunner executes the resolved manifest subprocess. Tests replace it.
var ManifestRunner = func(bin string, args []string, dir string, env []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprint(os.Stderr, stdout.String(), stderr.String())
		return err
	}
	logger.Debug("stdout:\n" + stdout.String())
	logger.Debug("stderr:\n" + stderr.String())
	return nil
}

// ManifestRunnerRestore restores the original ManifestRunner (called from tests).
func ManifestRunnerRestore(orig func(string, []string, string, []string) error) {
	ManifestRunner = orig
}

// manifestStyle selects the manifest generation tool and argument format.
type manifestStyle int

const (
	stylePkgdev manifestStyle = iota // pkgdev manifest -d <distdir> <cat/pkg>, cwd = overlay
	styleEbuildDigest                // <ebuild path> digest, cwd = overlay
)

// manifestRequest is the input to runManifest.
type manifestRequest struct {
	Overlay    string            // overlay root; becomes cmd.Dir
	CatPkg     string            // "dev-util/fish-lsp" — NEVER a file path
	Distdir    string            // must be non-empty
	Distfiles  []string          // distfile basenames the caller expects; may be nil
	EbuildPath string            // ebuild file path; used to target a single ebuild instead of the whole cat/pkg
	Prefetch   func(pkgDir, distdir string) error // runs before Manifest removal; may be nil
}

// findManifestRunner finds the manifest runner binary and returns its style.
func findManifestRunner() (bin string, style manifestStyle, err error) {
	if path, err := exec.LookPath("pkgdev"); err == nil {
		logger.Debug("using pkgdev: " + path)
		return path, stylePkgdev, nil
	}
	if path, err := exec.LookPath("ebuild"); err == nil {
		logger.Debug("using ebuild: " + path)
		return path, styleEbuildDigest, nil
	}
	return "", 0, fmt.Errorf("manifest runner not found: try installing portage (pkgdev or ebuild)")
}

// manifestArgs builds the command arguments for manifest.
// When ebuildPath is non-empty, it is passed to pkgdev manifest to target
// only that specific ebuild (avoids fetching distfiles for sibling ebuilds).
func manifestArgs(style manifestStyle, distdir, catPkg, ebuildPath string) []string {
	target := catPkg
	if ebuildPath != "" {
		target = ebuildPath
	}
	switch style {
	case stylePkgdev:
		return []string{"manifest", "-d", distdir, target}
	case styleEbuildDigest:
		return []string{target, "digest"}
	default:
		return nil
	}
}

// runManifest executes pkgdev manifest (or ebuild digest) for a category/package target.
func runManifest(req manifestRequest) error {
	if req.Distdir == "" {
		return fmt.Errorf("manifest: distdir is empty")
	}

	// mkdir -p $DISTDIR
	if err := os.MkdirAll(req.Distdir, 0o755); err != nil {
		return fmt.Errorf("mkdir distdir: %w", err)
	}

	pkgDir := filepath.Join(req.Overlay, req.CatPkg)

	// Prefetch runs BEFORE Manifest removal
	// Save Manifest before prefetch so we can restore it on any failure.
	manifestPath := filepath.Join(pkgDir, "Manifest")
	var manifestData []byte
	if data, err := os.ReadFile(manifestPath); err == nil {
		manifestData = data
	}

	if req.Prefetch != nil {
		if err := req.Prefetch(pkgDir, req.Distdir); err != nil {
			// Restore Manifest on prefetch failure so it's not left deleted.
			if manifestData != nil {
				_ = os.WriteFile(manifestPath, manifestData, 0644)
			}
			return err
		}
	}

	// Remove stale Manifest before regen, but only when processing a full cat/pkg.
	// When targeting a single ebuild path, preserve the existing Manifest so
	// pkgdev manifest can merge in the new ebuild's entries without clobbering
	// entries from sibling ebuilds.
	if req.EbuildPath == "" {
		_ = os.Remove(manifestPath)
	}

	// Resolve runner
	bin, style, err := findManifestRunner()
	if err != nil {
		return err
	}
	args := manifestArgs(style, req.Distdir, req.CatPkg, req.EbuildPath)

	env := os.Environ()
	env = append(env, "DISTDIR="+req.Distdir)
	// No GENTOO_MIRRORS — do not set it at all.

	if err := ManifestRunner(bin, args, req.Overlay, env); err != nil {
		// Manifest generation failed — restore the old Manifest if we had one.
		if manifestData != nil {
			_ = os.WriteFile(manifestPath, manifestData, 0644)
		}
		return err
	}

	// Verify manifest validity
	return verifyManifestValid(pkgDir, req.Distfiles)
}

// verifyManifestValid checks <pkgDir>/Manifest.
// expectedDistfiles may be empty, which means "no DIST lines required".
func verifyManifestValid(pkgDir string, expectedDistfiles []string) error {
	manifestPath := filepath.Join(pkgDir, "Manifest")

	// Check Manifest file exists — only an error if distfiles were expected.
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		if len(expectedDistfiles) > 0 {
			return fmt.Errorf("manifest file not found: %s", manifestPath)
		}
		return nil
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	// Valid DIST line: DIST <name> <size> <algo> <hash1> <algo2> <hash2>
	// Size must be [1-9]\d*, hashes must be non-empty \S+
	distRe := regexp.MustCompile(`^DIST\s+\S+\s+[1-9]\d*\s+\S+\s+\S+`)

	entries := 0
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		if distRe.MatchString(scanner.Text()) {
			entries++
		}
	}

	// Zero matching DIST lines is an error only if distfiles were expected.
	if entries == 0 && len(expectedDistfiles) > 0 {
		return fmt.Errorf("manifest has no valid DIST entries (expected %d distfile(s))", len(expectedDistfiles))
	}

	return nil
}
