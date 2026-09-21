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

// manifestRunner returns the manifest command path (pkgdev or ebuild).
var manifestRunner = func() (string, error) {
	return findManifestRunner()
}

// manifestRunnerArgs builds the command arguments for manifest.
var manifestRunnerArgs = func(path, distdir string) []string {
	return manifestArgs(path, distdir)
}

// runManifest executes pkgdev manifest on the given ebuild path.
func runManifest(overlay, path, distdir string) error {
	binPath, err := manifestRunner()
	if err != nil {
		return err
	}
	logger.Debug("using runner: " + binPath)

	args := manifestRunnerArgs(path, distdir)
	logger.Debug("args: " + strings.Join(args, " "))
	logger.Debug("ebuild: " + path)
	logger.Debug("distdir: " + distdir)
	logger.Debug("overlay: " + overlay)

	cmd := exec.Command(binPath, args...)
	logger.Debug("running: " + strings.Join(cmd.Args, " "))

	// Remove stale Manifest so ebuild generates fresh entries
	manifestPath := filepath.Join(filepath.Dir(path), "Manifest")
	_ = os.Remove(manifestPath)

	// Capture both stderr and stdout for debugging on failure
	var stdoutBuf, stderrBuf strings.Builder

	// Run from overlay root so ebuild finds the ebuild
	cmd.Dir = overlay
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// Set DISTDIR and disable Gentoo mirrors (npm mirror doesn't need them)
	cmd.Env = append(os.Environ(), "DISTDIR="+distdir, "GENTOO_MIRRORS=")

	if err := cmd.Run(); err != nil {
		fmt.Fprint(os.Stderr, stdoutBuf.String())
		fmt.Fprint(os.Stderr, stderrBuf.String())
		return err
	}
	if stdoutBuf.Len() > 0 {
		logger.Debug("stdout:\n" + stdoutBuf.String())
	}
	if stderrBuf.Len() > 0 {
		logger.Debug("runner warnings:\n" + stderrBuf.String())
	}

	// Verify manifest validity: check the Manifest file exists and has
	// valid entries matching the ebuild's distfiles.
	return verifyManifestValid(path)
}

// runManifestCmd is an internal helper that runs manifest without TUI piping.
func runManifestCmd(args ...string) error {
	binPath, err := manifestRunner()
	if err != nil {
		return err
	}
	cmd := exec.Command(binPath, args...)
	logger.Debug("runManifestCmd: " + strings.Join(cmd.Args, " "))
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// FindManifestRunner is the exported version of findManifestRunner.
func FindManifestRunner() (string, error) {
	return findManifestRunner()
}

// GetManifestArgs is the exported version of manifestArgs.
func GetManifestArgs(path, distdir string) []string {
	return manifestArgs(path, distdir)
}

// RunManifestCommand is the exported version of runManifest.
func RunManifestCommand(overlay, path, distdir string) error {
	return runManifest(overlay, path, distdir)
}

// verifyManifestValid checks that the Manifest file for the given ebuild
// exists and contains valid entries for all declared distfiles.
func verifyManifestValid(ebuildPath string) error {
	manifestPath := filepath.Join(filepath.Dir(ebuildPath), "Manifest")

	// Check Manifest file exists
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return fmt.Errorf("manifest file not found: %s", manifestPath)
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	// Count valid DIST entries: DIST filename size ALGO hash or DIST filename size hash
	entryRe := regexp.MustCompile(`^DIST\s+\S+\s+\d+\s+`)
	entries := 0
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		if entryRe.MatchString(scanner.Text()) {
			entries++
		}
	}

	// A valid Manifest must have at least one DIST entry
	if entries == 0 {
		return fmt.Errorf("manifest has no valid DIST entries")
	}

	return nil
}
