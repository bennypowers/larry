package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/test"

	"github.com/spf13/cobra"
)

var (
	testCompile       bool
	testMerge         bool
	testPretend       bool
	testPhases        []string
	testPortageTmpdir string
	testEmerge        bool
	testImageName     string
	testUse           string
)

func init() {
	rootCmd.AddCommand(&testCmd)

	testCmd.Flags().BoolVarP(&testCompile, "compile", "c", false,
		"run clean + fetch + unpack + compile phases")
	testCmd.Flags().BoolVarP(&testMerge, "merge", "m", false,
		"run clean + fetch + unpack + compile + install phases")
	testCmd.Flags().BoolVar(&testPretend, "pretend", false,
		"show what would be done without executing")
	testCmd.Flags().BoolVarP(&testEmerge, "emerge", "e", false,
		"run in podman container with GPU passthrough (requires Containerfile.larry-test)")
	testCmd.Flags().StringArrayVarP(&testPhases, "phase", "p", nil,
		"specific phase to run (can be specified multiple times; overrides --compile/--merge)")
	testCmd.Flags().StringVar(&testPortageTmpdir, "portage-tmpdir", "",
		"override PORTAGE_TMPDIR for test runs")
	testCmd.Flags().StringVar(&testImageName, "image", "",
		"container image name (default: larry-test)")
	testCmd.Flags().StringVar(&testUse, "use", "",
		`USE flags for the build, space separated (e.g. --use="cuda rocm"); sets USE in the emerge container`)

	// Allow unknown flags (e.g. bare --<phase>) so splitPhaseFlags can detect them.
	testCmd.FParseErrWhitelist = cobra.FParseErrWhitelist{
		UnknownFlags: true,
	}
}

var testCmd = cobra.Command{
	Use:   "test [flags] <category/package|ebuild-path>",
	Short: "Run ebuild test phases",
	Long: `Run ebuild test phases for a package. By default runs 'clean fetch unpack'.

With --compile/-c, runs clean + fetch + unpack + compile.
With --merge/-m, runs clean + fetch + unpack + compile + install.
With --phase/-p, specifies exact phase(s) to run (can be repeated).

Bare --<phase> flags (e.g., --prepare, --install) are appended to the phase list.

The --pretend/-n flag shows what would be executed without actually running.

Examples:
  larry test dev-lisp/sbcl-unicode-dictionary     # Clean fetch unpack
  larry test -c dev-lisp/sbcl-unicode-dictionary  # Compile
  larry test -m dev-lisp/sbcl-unicode-dictionary  # Merge (install)
  larry test --prepare dev-lisp/foo               # Add prepare phase
  larry test --phase fetch --phase unpack dev-lisp/foo
  larry test -p fetch -p unpack dev-lisp/foo
  larry test --pretend dev-lisp/foo               # Preview only
  larry test --emerge dev-lisp/foo             # Run in podman container
  larry test --emerge --use="cuda rocm" dev-lisp/foo  # Container build with USE flags
  larry test /var/db/repos/gentoo/foo/foo-1.0.0.ebuild  # Direct ebuild path
`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return &test.ExitError{Code: 1, Err: fmt.Errorf("missing package argument")}
		}
		input := args[0]

		// Extract bare --<phase> flags and detect unknown flags.
		extraPhases, unknownFlag := splitPhaseFlags()
		if unknownFlag != "" {
			log.Error("Unknown option or phase: %s", unknownFlag)
			return &test.ExitError{Code: 1, Err: fmt.Errorf("unknown option or phase: %s", unknownFlag)}
		}

		// Resolve overlay directory.
		overlayDir, err := cfg.ResolveOverlay(rootFlags.Overlay)
		if err != nil {
			return fmt.Errorf("resolving overlay: %w", err)
		}

		// Resolve ebuild path.
		var ebuildPath string
		if filepath.IsAbs(input) || strings.HasSuffix(input, ".ebuild") {
			ebuildPath, err = filepath.Abs(input)
			if err != nil {
				return fmt.Errorf("resolving ebuild path: %w", err)
			}
		} else {
			// Treat as cat/pkg — resolve atom and find ebuild.
			a, err := atom.ParseAtom(overlayDir, input)
			if err != nil {
				return fmt.Errorf("resolving atom %q: %w", input, err)
			}
			pe, err := atom.FindForAtom(a)
			if err != nil {
				return fmt.Errorf("finding ebuild for %s: %w", input, err)
			}
			ebuildPath = pe.Path
		}

		// Determine mode: container or local.
		if testEmerge {
			log.Info("Mode: container (image %s)", testImageName)
			cacheDir := cfg.ResolveCacheDir(rootFlags.CacheDir)
			cfg := &test.ContainerConfig{
				Overlay:   overlayDir,
				CacheDir:  cacheDir,
				ImageName: testImageName,
				UseFlags:  testUse,
			}
			err := test.TestContainer(ebuildPath, cfg, nil, testPretend)
			if err != nil {
				return fmt.Errorf("container test failed: %w", err)
			}
			log.Msg("Container test completed successfully.")
		} else {
			// Local mode.
			log.Info("Mode: local (phases %v)", testPhases)
			runner := test.NewCmdRunner()
			testCfg := &test.TestLocalConfig{
				Compile:       testCompile,
				Merge:         testMerge,
				Pretend:       testPretend,
				Overlay:       overlayDir,
				Distdir:       rootFlags.Distdir,
				PortageTmpdir: testPortageTmpdir,
				Phases:        testPhases,
				ExtraPhases:   extraPhases,
				Cfg:           cfg,
				Log:           log,
			}
			err := test.TestLocalWithConfig(ebuildPath, nil, runner, testPretend, testCfg)
			if err != nil {
				// Return as-is: TestLocalWithConfig already wraps the
				// subprocess exit code in an ExitError — re-wrapping with a
				// hardcoded code would destroy it (AGENTS.md: subprocess
				// exit codes propagate to os.Exit).
				return err
			}
		}
		return nil
	},
}

// splitPhaseFlags extracts bare --<phase> flags from os.Args[2:].
// It skips known cobra flags and reports unknown ones.
// Returns (extraPhases, unknownFlagOrEmpty).
func splitPhaseFlags() (extraPhases []string, unknownFlag string) {
	// All known cobra flags (test-local + inherited from root).
	knownFlags := map[string]bool{
		"compile": true, "c": true,
		"merge": true, "m": true,
		"pretend": true,
		"emerge":  true, "e": true,
		"phase": true, "p": true,
		"portage-tmpdir": true,
		"image":          true,
		"use":            true,
		// Root persistent flags.
		"verbose": true, "quiet": true, "color": true, "dry-run": true,
		"cache-dir": true, "distdir": true, "jobs": true,
		"github-token": true, "remote": true, "config": true,
		"overlay": true,
	}

	for _, arg := range os.Args[2:] {
		if !strings.HasPrefix(arg, "--") || strings.Contains(arg, "=") {
			continue
		}
		suffix := strings.TrimPrefix(arg, "--")
		if knownFlags[suffix] {
			continue
		}
		if test.ValidPhases(suffix) {
			extraPhases = append(extraPhases, suffix)
		} else {
			return nil, arg
		}
	}
	return extraPhases, ""
}
