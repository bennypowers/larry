package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// CmdRunner abstracts os/exec.Command for testability.
type CmdRunner interface {
	Run(name string, args ...string) error
}

// runner is a concrete CmdRunner implementation using os/exec.Command.
type runner struct{}

// NewCmdRunner creates a new CmdRunner.
func NewCmdRunner() CmdRunner {
	return &runner{}
}

func (r *runner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ---------------------------------------------------------------------------
// Phase validation
// ---------------------------------------------------------------------------

// validPhases lists all valid ebuild phases.
var validPhases = map[string]bool{
	"clean":      true,
	"fetch":      true,
	"postunpack": true,
	"unpack":     true,
	"prepare":    true,
	"configure":  true,
	"compile":    true,
	"install":    true,
	"qmerge":     true,
	"digest":     true,
	"manifest":   true,
}

// isValidPhase checks if a phase name is valid for ebuild.
func isValidPhase(phase string) bool {
	return validPhases[strings.ToLower(phase)]
}

// ---------------------------------------------------------------------------
// Phase list building
// ---------------------------------------------------------------------------

// buildPhaseList assembles the phase list from flags.
// compile → [clean, compile], merge → [clean, fetch, unpack, compile, install, qmerge]
// empty → [clean] (default)
func buildPhaseList(compile, merge, pretend bool) []string {
	if compile {
		return []string{"clean", "compile"}
	}
	if merge {
		return []string{"clean", "fetch", "unpack", "compile", "install", "qmerge"}
	}
	// Default: clean only
	return []string{"clean"}
}

// ---------------------------------------------------------------------------
// Ebuild resolution
// ---------------------------------------------------------------------------

// resolveEbuild resolves an ebuild path.
// If path is an absolute path to an existing file, return it.
// Otherwise, treat it as a cat/pkg string and find the latest ebuild.
func resolveEbuild(path string) (string, error) {
	// Check if it's a direct file path
	if strings.HasPrefix(path, "/") {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("ebuild not found: %s", path)
}

// resolveEbuildFromCatPkg finds the latest ebuild for a cat/pkg in an overlay.
func resolveEbuildFromCatPkg(catPkg string, overlay string) (string, error) {
	parts := strings.SplitN(catPkg, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid cat/pkg: %s (expected category/name)", catPkg)
	}

	catDir := filepath.Join(overlay, parts[0], parts[1])
	entries, err := os.ReadDir(catDir)
	if err != nil {
		return "", fmt.Errorf("no ebuilds found for %s: %w", catPkg, err)
	}

	var ebuilds []string
	pkgName := parts[1] + "-"
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, pkgName) || !strings.HasSuffix(name, ".ebuild") {
			continue
		}
		ebuilds = append(ebuilds, name)
	}

	if len(ebuilds) == 0 {
		return "", fmt.Errorf("no ebuilds found for %s in %s", catPkg, catDir)
	}

	// Sort by version descending (highest first)
	sort.Strings(ebuilds)
	return filepath.Join(catDir, ebuilds[len(ebuilds)-1]), nil
}

// ---------------------------------------------------------------------------
// Environment helpers
// ---------------------------------------------------------------------------

// setNetworkFeatures ensures FEATURES includes -network-sandbox.
func setNetworkFeatures(env []string) []string {
	// Remove existing FEATURES, then add our own
	var filtered []string
	for _, e := range env {
		if !strings.HasPrefix(e, "FEATURES=") {
			filtered = append(filtered, e)
		}
	}
	filtered = append(filtered, "FEATURES=-network-sandbox")
	return filtered
}

// ---------------------------------------------------------------------------
// Phase execution
// ---------------------------------------------------------------------------

// runPhase runs a single ebuild phase.
func runPhase(ebuildPath string, phase string, runner CmdRunner, pretend bool) error {
	cmdStr := fmt.Sprintf("ebuild %s %s", ebuildPath, phase)
	if pretend {
		fmt.Printf("Would run: %s\n", cmdStr)
		return nil
	}

	if runner != nil {
		if err := runner.Run("ebuild", ebuildPath, phase); err != nil {
			return fmt.Errorf("ebuild %s failed: %w", cmdStr, err)
		}
		return nil
	}

	cmd := exec.Command("ebuild", ebuildPath, phase)
	cmd.Env = setNetworkFeatures(os.Environ())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ebuild %s failed: %w", cmdStr, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// TestLocal — main entry point
// ---------------------------------------------------------------------------

// TestLocalConfig holds configuration for the local test runner.
type TestLocalConfig struct {
	Compile bool // --compile flag
	Merge   bool // --merge flag
	Pretend bool // --pretend flag
	Overlay string // --overlay flag (default: /var/db/repos/gentoo)
}

// TestLocal runs ebuild phases for a given ebuild path.
func TestLocal(ebuildPath string, phases []string, runner CmdRunner, pretend bool, useTUI bool) error {
	if runner == nil {
		runner = NewCmdRunner()
	}

	// Resolve ebuild path
	resolved, err := resolveEbuild(ebuildPath)
	if err != nil {
		return err
	}

	if _, err := os.Stat(resolved); err != nil {
		return fmt.Errorf("ebuild not found: %s", resolved)
	}

	// Build phase list
	if len(phases) == 0 {
		phases = buildPhaseList(false, false, pretend)
	}

	// Validate phases
	for _, phase := range phases {
		if !isValidPhase(phase) {
			return fmt.Errorf("invalid phase: %s", phase)
		}
	}

	// Run phases
	for _, phase := range phases {
		if err := runPhase(resolved, phase, runner, pretend); err != nil {
			return fmt.Errorf("phase %s: %w", phase, err)
		}
	}

	return nil
}
