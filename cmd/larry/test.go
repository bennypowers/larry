package main

import (
	"fmt"
	"path/filepath"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/test"

	"github.com/spf13/cobra"
)

var (
	testCompile  bool
	testMerge    bool
	testPretend  bool
	testDistdir  string
	testPhases   []string
	testEmerge bool
)

func init() {
	rootCmd.AddCommand(&testCmd)

	testCmd.Flags().BoolVar(&testCompile, "compile", false,
		"run clean + compile phases")
	testCmd.Flags().BoolVar(&testMerge, "merge", false,
		"run clean + fetch + unpack + compile + install + qmerge phases")
	testCmd.Flags().BoolVar(&testPretend, "pretend", false,
		"show what would be done without executing")
	testCmd.Flags().BoolVarP(&testEmerge, "emerge", "e", false,
		"run in podman container with GPU passthrough (requires Containerfile.larry-test)")
	testCmd.Flags().StringArrayVar(&testPhases, "phase", nil,
		"specific phase to run (can be specified multiple times; overrides --compile/--merge)")
}

var testCmd = cobra.Command{
	Use:   "test <category/package|ebuild-path>",
	Short: "Run ebuild test phases",
	Long: `Run ebuild test phases for a package. By default runs 'clean' phase.

With --compile, runs 'clean' + 'compile'.
With --merge, runs 'clean' + 'fetch' + 'unpack' + 'compile' + 'install' + 'qmerge'.
With --phase, specifies exact phase(s) to run (can be repeated).

The --pretend flag shows what would be executed without actually running.

The --emerge flag runs the test inside a podman container with GPU passthrough
(requires Containerfile.larry-test).

Examples:
  larry test dev-lisp/sbcl-unicode-dictionary     # Clean only (default)
  larry test --compile dev-lisp/sbcl-unicode-dictionary
  larry test --merge dev-lisp/sbcl-unicode-dictionary
  larry test --phase fetch --phase unpack dev-lisp/foo
  larry test --pretend dev-lisp/foo               # Preview only
  larry test --emerge dev-lisp/foo             # Run in podman container
  larry test /var/db/repos/gentoo/foo/foo-1.0.0.ebuild  # Direct ebuild path
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		input := args[0]

		// Resolve overlay directory.
		overlayDir, err := cfg.ResolveOverlay(rootFlags.Overlay)
		if err != nil {
			return fmt.Errorf("resolving overlay: %w", err)
		}

		// Resolve ebuild path.
		var ebuildPath string
		if filepath.IsAbs(input) {
			ebuildPath = input
		} else {
			// Treat as cat/pkg — resolve atom and find latest ebuild.
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
			distdir := cfg.ResolveDistdir(rootFlags.Distdir)
			cfg := &test.ContainerConfig{
				Overlay:   overlayDir,
				Distdir:   distdir,
				ImageName: "larry-test",
				UseTUI:    false,
			}
			err := test.TestContainer(ebuildPath, cfg, nil, testPretend, true)
			if err != nil {
				return fmt.Errorf("container test failed: %w", err)
			}
			fmt.Println("Container test completed successfully.")
		} else {
			// Local mode.
			runner := test.NewCmdRunner()
			err := test.TestLocal(ebuildPath, nil, runner, testPretend, true)
			if err != nil {
				return fmt.Errorf("test failed: %w", err)
			}
			fmt.Println("Test completed successfully.")
		}
		return nil
	},
}
