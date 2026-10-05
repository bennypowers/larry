package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bennypowers.dev/larry/internal/config"
	"bennypowers.dev/larry/internal/test"

	"github.com/spf13/cobra"
)

var (
	testCompile  bool
	testMerge    bool
	testPretend  bool
	testOverlay  string
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
	testCmd.Flags().StringVar(&testOverlay, "overlay", "",
		"overlay root path (default: $LARRY_OVERLAY or git CWD)")
	testCmd.Flags().StringVar(&testDistdir, "distdir", "",
		"distfiles directory (default: $DISTDIR)")
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

		// Resolve overlay directory (shared with all commands)
		overlayDir := config.ResolveOverlay(testOverlay)

		// Resolve ebuild path
		var ebuildPath string
		if filepath.IsAbs(input) {
			ebuildPath = input
		} else {
			// Treat as cat/pkg — find latest ebuild in overlay
			catDir := filepath.Join(overlayDir, input)
			entries, err := os.ReadDir(catDir)
			if err != nil {
				return fmt.Errorf("no ebuilds found for %s in %s", input, catDir)
			}

			parts := strings.Split(input, "/")
			if len(parts) != 2 {
				return fmt.Errorf("invalid category/package: %s", input)
			}
			pkgName := parts[1] + "-"

			var ebuilds []string
			for _, entry := range entries {
				name := entry.Name()
				if entry.IsDir() || !strings.HasPrefix(name, pkgName) || !strings.HasSuffix(name, ".ebuild") {
					continue
				}
				ebuilds = append(ebuilds, name)
			}

			if len(ebuilds) == 0 {
				return fmt.Errorf("no ebuilds found for %s in %s", input, catDir)
			}

			// Sort by version and pick the highest
			ebuildPath = filepath.Join(catDir, ebuilds[len(ebuilds)-1])
		}

		// Determine mode: container or local
		if testEmerge {
			cfg := &test.ContainerConfig{
				Overlay:   overlayDir,
				Distdir:   testDistdir,
				ImageName: "larry-test",
				UseTUI:    false,
			}
			err := test.TestContainer(ebuildPath, cfg, nil, testPretend, true)
			if err != nil {
				return fmt.Errorf("container test failed: %w", err)
			}
			fmt.Println("Container test completed successfully.")
		} else {
			// Local mode
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
