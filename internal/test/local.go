package test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/config"
	"bennypowers.dev/larry/internal/exit"
	"bennypowers.dev/larry/internal/logger"
	"bennypowers.dev/larry/internal/output"
)

// ExitError wraps an error with a specific exit code. It is an alias of the
// single shared type in internal/exit so subprocess exit codes propagate
// unchanged through cmd/larry to os.Exit.
type ExitError = exit.ExitError

// CmdRunner abstracts os/exec.Command for testability.
type CmdRunner interface {
	Run(name string, args ...string) error
	RunWithEnv(env []string, name string, args ...string) error
}

// runner is a concrete CmdRunner implementation using os/exec.Command.
type runner struct{}

// NewCmdRunner creates a new CmdRunner.
func NewCmdRunner() CmdRunner {
	return &runner{}
}

// For testing: when non-nil, TestLocalWithConfig uses this runner instead of NewCmdRunner.
var TestRunnerForTest CmdRunner

func (r *runner) Run(name string, args ...string) error {
	logger.Tracef("exec: %s %s", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *runner) RunWithEnv(env []string, name string, args ...string) error {
	logger.Tracef("exec: %s %s", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ---------------------------------------------------------------------------
// Phase validation
// ---------------------------------------------------------------------------

// validPhases lists all valid ebuild phases — exactly the script's set (L724).
var validPhases = map[string]bool{
	"clean":     true,
	"fetch":     true,
	"unpack":    true,
	"prepare":   true,
	"configure": true,
	"compile":   true,
	"install":   true,
	"test":      true,
	"package":   true,
	"preinst":   true,
	"postinst":  true,
	"prerm":     true,
	"postrm":    true,
	"nofetch":   true,
}

// isValidPhase checks if a phase name is valid for ebuild.
func isValidPhase(phase string) bool {
	return validPhases[strings.ToLower(phase)]
}

// ValidPhases returns true if the given phase name is valid for ebuild.
// Exported for use by the CLI in splitting bare --<phase> flags.
func ValidPhases(phase string) bool {
	return isValidPhase(phase)
}

// ---------------------------------------------------------------------------
// Phase list building
// ---------------------------------------------------------------------------

// buildPhaseList assembles the phase list from flags.
//
// Precedence (script L724–771):
//  1. explicit phases win outright — overrides compile/merge entirely
//  2. merge list if merge was given
//  3. compile list if compile was given
//  4. default list (clean fetch unpack)
//  5. extra phases appended (bare --<phase> flags)
//
// compile → [clean, fetch, unpack, compile]
// merge   → [clean, fetch, unpack, compile, install]
// default → [clean, fetch, unpack]
func buildPhaseList(compile, merge bool, explicit, extra []string) []string {
	// Rule 1: explicit phases win outright.
	if len(explicit) > 0 {
		return append([]string(nil), explicit...)
	}

	// Rule 2/3/4: base list.
	var phases []string
	if merge {
		phases = []string{"clean", "fetch", "unpack", "compile", "install"}
	} else if compile {
		phases = []string{"clean", "fetch", "unpack", "compile"}
	} else {
		phases = []string{"clean", "fetch", "unpack"}
	}

	// Rule 5: append extra phases.
	return append(phases, extra...)
}

// ---------------------------------------------------------------------------
// Ebuild resolution
// ---------------------------------------------------------------------------

// resolveEbuild resolves an ebuild path.
// If path is an absolute path, convert with filepath.Abs.
// Otherwise, treat it as a cat/pkg string and find the ebuild.
func resolveEbuild(path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Abs(path)
	}
	// Relative path — convert to absolute.
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving path %q: %w", path, err)
	}
	return abs, nil
}

// resolveEbuildFromCatPkg finds the latest ebuild for a cat/pkg in an overlay.
func resolveEbuildFromCatPkg(catPkg string, overlay string) (string, error) {
	a, err := atom.ParseAtom(overlay, catPkg)
	if err != nil {
		return "", fmt.Errorf("resolving atom %q: %w", catPkg, err)
	}
	pe, err := atom.FindForAtom(a)
	if err != nil {
		return "", fmt.Errorf("finding ebuild for %s: %w", catPkg, err)
	}
	return pe.Path, nil
}

// ---------------------------------------------------------------------------
// Local test configuration
// ---------------------------------------------------------------------------

// TestLocalConfig holds configuration for the local test runner.
type TestLocalConfig struct {
	Compile       bool           // --compile flag
	Merge         bool           // --merge flag
	Pretend       bool           // --pretend flag
	Overlay       string         // --overlay flag
	Distdir       string         // --distdir flag
	PortageTmpdir string         // --portage-tmpdir flag
	Phases        []string       // explicit --phase values (overrides compile/merge)
	ExtraPhases   []string       // bare --<phase> flags
	Cfg           *config.Config // full config (for ResolveDistdir, etc.)
	Log           *output.Logger // logger (may be nil — TestLocal creates a default)
	Progress      ProgressFunc   // optional progress sink
}

// ---------------------------------------------------------------------------
// Local test execution
// ---------------------------------------------------------------------------

// TestLocal runs ebuild phases for a given ebuild path.
func TestLocal(ebuildPath string, phases []string, runner CmdRunner, pretend bool) error {
	return TestLocalWithConfig(ebuildPath, phases, runner, pretend, nil)
}

// TestLocalWithConfig runs ebuild phases with full configuration.
func TestLocalWithConfig(ebuildPath string, phases []string, runner CmdRunner, pretend bool, cfg *TestLocalConfig) error {
	if TestRunnerForTest != nil {
		runner = TestRunnerForTest
	} else if runner == nil {
		runner = NewCmdRunner()
	}

	// Resolve ebuild path (handles relative paths).
	resolved, err := resolveEbuild(ebuildPath)
	if err != nil {
		return err
	}

	if _, err := os.Stat(resolved); err != nil {
		return fmt.Errorf("ebuild not found: %s", resolved)
	}

	// Build phase list from config.
	if cfg == nil {
		cfg = &TestLocalConfig{Phases: phases}
	}
	phases = buildPhaseList(cfg.Compile, cfg.Merge, cfg.Phases, cfg.ExtraPhases)

	// Validate phases.
	for _, phase := range phases {
		if !isValidPhase(phase) {
			return fmt.Errorf("invalid phase: %s", phase)
		}
	}

	// Build package identifier for banners.
	pkgName := ""
	if pe, err := atom.ParseEbuildPath(resolved); err == nil {
		if cfg.Cfg != nil {
			pkgName = pe.Category + "/" + pe.PN + "-" + pe.PVR
		} else {
			pkgName = pe.PN + "-" + pe.PVR
		}
	}

	// Resolve distdir and portage tmpdir.
	var distdir, portageTmpdir string
	if cfg.Cfg != nil {
		distdir = cfg.Cfg.ResolveDistdir(cfg.Distdir)
		portageTmpdir = cfg.Cfg.ResolvePortageTmpdir(cfg.PortageTmpdir)
	} else {
		// Fall back to direct config values or defaults.
		distdir = cfg.Distdir
		portageTmpdir = cfg.PortageTmpdir
		if portageTmpdir == "" {
			portageTmpdir = "/tmp/portage-test"
		}
	}
	if distdir != "" {
		if err := os.MkdirAll(distdir, 0755); err != nil {
			return fmt.Errorf("creating distdir %s: %w", distdir, err)
		}
	}
	if err := os.MkdirAll(portageTmpdir, 0755); err != nil {
		return fmt.Errorf("creating portage tmpdir %s: %w", portageTmpdir, err)
	}

	// Logger setup (create default if nil).
	log := cfg.Log
	if log == nil {
		var err error
		log, err = output.NewLogger(0, false, nil)
		if err != nil {
			return fmt.Errorf("creating logger: %w", err)
		}
	}

	// Banner (script L785–787).
	log.Msg("Package: %s", pkgName)
	log.Msg("Ebuild:  %s", resolved)
	log.Msg("Phases:  %s", strings.Join(phases, " "))
	fmt.Println() // blank line after banner

	// ebuild runs every phase in one process, so the checklist is the planned
	// phase list; it is marked done or failed as a whole.
	reportProgress(cfg.Progress, ProgressEvent{Kind: ProgressStart, Steps: phases})

	// Pretend mode (script L790–792).
	if pretend {
		log.Warn("Pretend mode: would run:")
		log.Msg("  ebuild %s %s", resolved, strings.Join(phases, " "))
		reportProgress(cfg.Progress, ProgressEvent{Kind: ProgressDone, Message: "pretend"})
		return nil
	}

	// Build environment (script L796–803).
	env := os.Environ()
	// Filter out user-set PORTAGE_TMPDIR and DISTDIR from environment
	// so our overrides take effect.
	var filtered []string
	for _, e := range env {
		if !strings.HasPrefix(e, "PORTAGE_TMPDIR=") &&
			!strings.HasPrefix(e, "DISTDIR=") &&
			!strings.HasPrefix(e, "GENTOO_MIRRORS=") {
			filtered = append(filtered, e)
		}
	}
	filtered = append(filtered,
		"FEATURES=-sandbox -usersandbox -network-sandbox -ipc-sandbox -pid-sandbox",
		"PORTAGE_TMPDIR="+portageTmpdir,
		"DISTDIR="+distdir,
		"GENTOO_MIRRORS=",
	)
	env = filtered

	// Single invocation (script L804).
	err = runner.RunWithEnv(env, "ebuild", append([]string{resolved}, phases...)...)
	if err != nil {
		// Extract exit code from the error.
		exitCode := 1
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.Code
		} else {
			// Try os/exec.ExitError for subprocess exit codes.
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}
		// Wrap in ExitError for exit code propagation.
		err = &ExitError{Code: exitCode, Err: err}
		// Failure summary (script L810–813).
		fmt.Println() // blank line before summary
		log.Error("Failed: %s (%s)", pkgName, strings.Join(phases, " "))
		log.Error("Exit code: %d", exitCode)
		reportProgress(cfg.Progress, ProgressEvent{Kind: ProgressError, Err: err})
		return err
	}

	// Success summary (script L810).
	fmt.Println()
	log.Msg("Success: %s (%s)", pkgName, strings.Join(phases, " "))
	reportProgress(cfg.Progress, ProgressEvent{Kind: ProgressDone})

	return nil
}
