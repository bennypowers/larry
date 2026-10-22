package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/test"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
	testProgress      string
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
	testCmd.Flags().StringVar(&testProgress, "progress", "",
		"internal: emit NDJSON progress on stderr (json)")
	_ = testCmd.Flags().MarkHidden("progress")

	// Allow unknown flags (e.g. bare --<phase>) so splitPhaseFlags can detect them.
	testCmd.FParseErrWhitelist = cobra.FParseErrWhitelist{
		UnknownFlags: true,
	}
}

var testCmd = cobra.Command{
	Use:   "test [flags] <category/package|ebuild-path>",
	Short: "Run ebuild test phases",
	Long: `Run ebuild test phases for a package, locally or in a container.

Phase selection:
  default        clean fetch unpack
  -c, --compile  clean fetch unpack compile
  -m, --merge    clean fetch unpack compile install
  -p, --phase    an exact phase; repeat the flag for several
  --<phase>      bare phase flags are appended (e.g. --prepare)

  --pretend shows the commands without executing anything. The global
  -n/--dry-run is inherited but is not the pretend switch.

Modes:
  local (default)   runs ebuild <path> <phases...>
  -e, --emerge      runs emerge in a podman container with GPU passthrough
                    (--image, --use, --portage-tmpdir tune the run)`,
	Example: `  larry test dev-lisp/sbcl-unicode-dictionary
  larry test -c dev-lisp/sbcl-unicode-dictionary
  larry test -m dev-lisp/sbcl-unicode-dictionary
  larry test -p fetch -p unpack dev-lisp/foo
  larry test --prepare dev-lisp/foo
  larry test --pretend dev-lisp/foo
  larry test --emerge --use="cuda rocm" dev-lisp/foo
  larry test /var/db/repos/gentoo/foo/foo-1.0.0.ebuild`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateProgressMode(testProgress); err != nil {
			return err
		}
		// Check flag-shaped arguments before the positional check: an unknown
		// --flag=value is otherwise silently ignored, and an unknown bare flag
		// can consume the package token.
		extraPhases, unknownFlag := splitPhaseFlags(cmd)
		if unknownFlag != "" {
			log.Error("Unknown option or phase: %s", unknownFlag)
			return &test.ExitError{Code: 1, Err: fmt.Errorf("unknown option or phase: %s", unknownFlag)}
		}
		if len(args) == 0 {
			return &test.ExitError{Code: 1, Err: fmt.Errorf("missing package argument")}
		}
		if cfg.DryRun {
			return fmt.Errorf("--dry-run is not supported by test; use --pretend to preview phases")
		}
		input := args[0]

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
				Progress:  testProgressReporter(testProgress),
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
				Progress:      testProgressReporter(testProgress),
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

func testProgressReporter(mode string) test.ProgressFunc {
	emitter := newProgressEmitter(mode)
	if !emitter.enabled {
		return nil
	}
	return func(ev test.ProgressEvent) {
		wire := actionProgressWire{
			Kind:    int(ev.Kind),
			Steps:   ev.Steps,
			Step:    ev.Step,
			State:   int(ev.State),
			Message: ev.Message,
		}
		if ev.Err != nil {
			wire.Error = ev.Err.Error()
		}
		emitter.emit(wire)
	}
}

// splitPhaseFlags extracts bare --<phase> flags from os.Args[2:].
// It skips known cobra flags and reports unknown ones.
// Returns (extraPhases, unknownFlagOrEmpty).
func splitPhaseFlags(cmd *cobra.Command) (extraPhases []string, unknownFlag string) {
	return splitPhaseFlagsFrom(os.Args[2:], knownFlagNames(cmd))
}

func knownFlagNames(cmd *cobra.Command) map[string]bool {
	known := make(map[string]bool)
	add := func(f *pflag.FlagSet) {
		if f == nil {
			return
		}
		f.VisitAll(func(flag *pflag.Flag) {
			known[flag.Name] = true
			if flag.Shorthand != "" {
				known[flag.Shorthand] = true
			}
		})
	}
	add(cmd.Flags())
	add(cmd.InheritedFlags())
	return known
}

func splitPhaseFlagsFrom(argv []string, knownFlags map[string]bool) (extraPhases []string, unknownFlag string) {
	for _, arg := range argv {
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		suffix := strings.TrimPrefix(arg, "--")
		name, _, hasValue := strings.Cut(suffix, "=")
		if knownFlags[name] {
			continue
		}
		if hasValue {
			return nil, arg
		}
		if test.ValidPhases(name) {
			extraPhases = append(extraPhases, name)
		} else {
			return nil, arg
		}
	}
	return extraPhases, ""
}
