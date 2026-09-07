package ebuild

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrPortageqNotFound is returned when the portageq binary is not available.
type ErrPortageqNotFound struct{}

func (e *ErrPortageqNotFound) Error() string {
	return "portageq not found in PATH; is Portage installed?"
}

// ErrOverlayUnavailable is returned when a Portage tree cannot be accessed.
type ErrOverlayUnavailable struct {
	Message string
}

func (e *ErrOverlayUnavailable) Error() string {
	return "overlay tree unavailable: " + e.Message
}

// PortageQ wraps the portageq binary for ebuild variable resolution.
type PortageQ struct{}

// NewPortageQ creates a new PortageQ wrapper.
func NewPortageQ() *PortageQ {
	return &PortageQ{}
}

// PortageTree reports whether the specified Portage tree is accessible.
// It uses `portageq get_repo_path` to check tree availability.
func (p *PortageQ) PortageTree(treeName string) error {
	if treeName == "" {
		treeName = "gentoo"
	}
	cmd := exec.Command("portageq", "get_repo_path", "/", treeName)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		stderr := buf.String()
		// Distinguish "tree not found" from "not installed"
		if strings.Contains(stderr, "no such repository") || strings.Contains(stderr, "does not exist") {
			return &ErrOverlayUnavailable{Message: fmt.Sprintf("tree %q not found: %s", treeName, stderr)}
		}
		return fmt.Errorf("portageq get_repo_path failed: %w", err)
	}
	return nil
}

// PortageTreeMain reports whether the official Gentoo tree is accessible.
func (p *PortageQ) PortageTreeMain() error {
	return p.PortageTree("gentoo")
}

// PortageTreeOverlay reports whether the given overlay path is a valid Portage tree.
func (p *PortageQ) PortageTreeOverlay(overlayPath string) error {
	// If overlayPath is empty, try to detect from environment
	cmd := exec.Command("portageq", "portage_trees")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		stderr := buf.String()
		if strings.Contains(stderr, "not found") || strings.Contains(stderr, "not installed") {
			return &ErrPortageqNotFound{}
		}
		return fmt.Errorf("portageq portage_trees failed: %w", err)
	}
	return nil
}

// EnvVar resolves a Portage tree-level variable (e.g. DISTDIR, PORTAGE_TMPDIR).
// It calls `portageq envvar / <varname>` to get the value.
// NOTE: portageq envvar always exits with code 1 in some Portage versions,
// so we ignore the exit code and only fail on stderr containing errors.
func (p *PortageQ) EnvVar(tree string, varName string) (string, error) {
	// portageq envvar takes <eroot> <varname> — tree name is not used
	cmd := exec.Command("portageq", "envvar", "/", varName)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	cmd.Run()

	// Check for actual error conditions in stderr
	stderrStr := strings.TrimSpace(stderr.String())
	if stderrStr != "" {
		// Filter out deprecation warnings — treat them as non-fatal
		cleanStderr := strings.TrimSpace(strings.TrimPrefix(stderrStr, "WARNING: "))
		if cleanStderr != "" {
			return "", fmt.Errorf("portageq envvar %s: %s", varName, cleanStderr)
		}
	}

	result := strings.TrimSpace(out.String())
	return result, nil
}

// GetRepoPath returns the filesystem path for a Portage tree.
func (p *PortageQ) GetRepoPath(treeName string) (string, error) {
	if treeName == "" {
		treeName = "gentoo"
	}

	if _, err := exec.LookPath("portageq"); err != nil {
		return "", &ErrPortageqNotFound{}
	}

	cmd := exec.Command("portageq", "get_repo_path", "/", treeName)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := stderr.String()
		if strings.Contains(stderrStr, "no such repository") || strings.Contains(stderrStr, "does not exist") {
			return "", &ErrOverlayUnavailable{Message: fmt.Sprintf("tree %q not found", treeName)}
		}
		if _, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("portageq get_repo_path %s: %s", treeName, stderrStr)
		}
		return "", fmt.Errorf("portageq get_repo_path %s: %w", treeName, err)
	}

	result := strings.TrimSpace(out.String())
	if result == "" {
		return "", fmt.Errorf("portageq get_repo_path %s: empty result", treeName)
	}
	return result, nil
}

// EnvVarForPkg resolves a Portage variable for a specific ebuild.
// It calls `portageq envvar / <varname>` with EBUILD set to the ebuild path.
// NOTE: portageq envvar always exits with code 1 in some Portage versions,
// so we ignore the exit code and only fail on stderr containing errors.
//
// For ebuild-level variables (PN, PV, P), the ebuild must reside in a proper
// Portage tree with a Manifest for portageq to resolve them. Without a tree,
// only tree-level variables (DISTDIR, PORTAGE_TMPDIR, etc.) will resolve.
func (p *PortageQ) EnvVarForPkg(tree string, ebuildPath string, varName string) (string, error) {
	// Check file exists before invoking portageq
	if _, err := os.Stat(ebuildPath); err != nil {
		return "", fmt.Errorf("ebuild file does not exist: %w", err)
	}

	// portageq envvar takes <eroot> <varname> — tree name is not used
	cmd := exec.Command("portageq", "envvar", "/", varName)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	// Set EBUILD env var so portageq resolves from this specific ebuild
	cmd.Env = append(os.Environ(), "EBUILD="+ebuildPath)

	cmd.Run()

	// Check for actual error conditions in stderr
	stderrStr := strings.TrimSpace(stderr.String())
	if stderrStr != "" {
		cleanStderr := strings.TrimSpace(strings.TrimPrefix(stderrStr, "WARNING: "))
		if cleanStderr != "" {
			return "", fmt.Errorf("portageq envvar %s (ebuild %s): %s", varName, ebuildPath, cleanStderr)
		}
	}

	result := strings.TrimSpace(out.String())
	return result, nil
}

// HasPortageq checks whether portageq is available on the system.
func (p *PortageQ) HasPortageq() bool {
	_, err := exec.LookPath("portageq")
	return err == nil
}

// HasEbuild checks whether ebuild is available on the system.
func (p *PortageQ) HasEbuild() bool {
	_, err := exec.LookPath("ebuild")
	return err == nil
}

// ValidateTools checks that required Portage tools are available and returns errors.
func (p *PortageQ) ValidateTools() error {
	var errs []error

	if _, err := exec.LookPath("portageq"); err != nil {
		errs = append(errs, &ErrPortageqNotFound{})
	}

	if _, err := exec.LookPath("ebuild"); err != nil {
		errs = append(errs, fmt.Errorf("ebuild not found in PATH; is Portage installed?"))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
