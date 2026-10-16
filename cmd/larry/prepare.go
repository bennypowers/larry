package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/prepare"

	"github.com/spf13/cobra"
)

var (
	prepareType             string
	prepareAll              bool
	prepareDryRun           bool
	prepareCleanupOrphans   bool
)

func init() {
	rootCmd.AddCommand(&prepareCmd)

	prepareCmd.Flags().StringVarP(&prepareType, "type", "t", "",
		"force package type: npm, cargo, go, vendored, generic (overrides auto-detection)")
	prepareCmd.Flags().BoolVarP(&prepareAll, "all", "a", false,
		"regenerate Manifest for all packages via pkgdev (manifest-only, no type-specific pipeline)")
	prepareCmd.Flags().BoolVarP(&prepareDryRun, "dry-run", "n", false,
		"show what would be done without making changes")
	prepareCmd.Flags().BoolVar(&prepareCleanupOrphans, "cleanup", false,
		"remove orphaned GitHub release assets on prepare failure")
}

var prepareCmd = cobra.Command{
	Use:   "prepare [category/package] [ebuild...]",
	Short: "Prepare an ebuild for commit (manifest, distfiles, etc.)",
	Long: `Prepare an ebuild for commit by running the appropriate type-specific pipeline.

Custom prepare scripts (escape hatch):
  If <pkg>/.larry/prepare.sh exists, it runs instead of any built-in
  pipeline. The script lives under .larry/ so it doesn't ship to end
  users — it's purely an overlay-maintainer concern.
  The script runs in a userspace sandbox:
  PATH is restricted to system dirs, dangerous env vars are stripped,
  and writes are policy-limited to the ebuild dir + DISTDIR.
  Dry-run skips script execution.
  Example: docs/examples/rpi-kernel-prepare.sh

Type detection (auto):
  Detects package type from ebuild content:
  - npm:       mirror://npm/ in SRC_URI
  - cargo:     CRATES= variable present
  - go:        inherits go-module
  - vendored:  releases/download/${PN}/ in SRC_URI
  - generic:   everything else (including rpi-kernel)

  rpi-kernel packages are detected as generic. Use a custom
  .larry/prepare.sh for those — see docs/examples/rpi-kernel-prepare.sh.

Override detection with --type flag.

Commands:
  larry prepare sys-apps/foo/foo-1.0.0.ebuild    # Prepare single ebuild
  larry prepare sys-apps/foo                      # Prepare all ebuilds in pkg dir
  larry prepare --all                             # Manifest-only: all packages in overlay
  larry prepare --type generic sys-apps/foo/foo.ebuild # Force type

Flags:
  -t, --type string       Force package type (overrides auto-detection)
  -n, --dry-run           Show what would be done without making changes
  -a, --all               Manifest-only: pkgdev for every cat/pkg in overlay (no pipeline)
  -n, --dry-run           Show what would be done without making changes
      --distdir string    Distdir path for source downloads
      --overlay string    Overlay root path
      --cleanup           Remove orphaned GitHub release assets on failure
`,
	Args: cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve distdir through config.
		distdir := cfg.ResolveDistdir(rootFlags.Distdir)

		// Reject empty distdir (D5).
		if distdir == "" {
			return fmt.Errorf("prepare: distdir is empty — set --distdir, $LARRY_DISTDIR, or $PORTAGE_DISTDIR")
		}

		// Resolve overlay directory.
		overlay, err := cfg.ResolveOverlay(rootFlags.Overlay)
		if err != nil {
			return fmt.Errorf("resolving overlay: %w", err)
		}

		// Create config.
		pcfg := &prepare.PrepareConfig{
			DryRun:                  prepareDryRun,
			Distdir:                 distdir,
			Overlay:                 overlay,
			UseTUI:                  false,
			CleanupOrphanedAssets:   prepareCleanupOrphans,
		}

		// Resolve GitHub repo from overlay git remote (for release uploads).
		if overlay != "" {
			if repo, err := cfg.ResolveGitHubRepo(overlay, cfg.Remote); err == nil && repo != "" {
				pcfg.GitHubRepo = repo
			}
		}

		// Force type if specified.
		if prepareType != "" {
			pt, err := ebuild.ParsePackageType(prepareType)
			if err != nil {
				return err
			}
			pcfg.ForceType = pt
		}

		// Determine what to prepare.
		switch {
		case prepareAll && len(args) == 0:
			// --all without args: manifest-only bulk pkgdev call (D8).
			return prepare.PrepareManifestAll(overlay, distdir)
		case len(args) == 0:
			// No args: show help.
			return cmd.Help()
		case len(args) == 1 && !prepareAll:
			// Single path (file or directory).
			resolved, fi, err := resolvePrepareTarget(args[0], overlay)
			if err != nil {
				return err
			}

			if fi.IsDir() {
				// Directory: prepare all ebuilds with failure counting.
				return prepare.PrepareDir(resolved, pcfg)
			}

			// Single file: prepare that ebuild.
			return prepare.PrepareFromFile(resolved, pcfg)
		default:
			// Multiple paths: attempt each, aggregate failures (D9).
			var failures int
			for _, raw := range args {
				resolved, _, err := resolvePrepareTarget(raw, overlay)
				if err != nil {
					failures++
					continue
				}
				if err := prepare.PrepareFile(resolved, pcfg); err != nil {
					failures++
				}
			}
			if failures > 0 {
				return fmt.Errorf("%d package(s) failed", failures)
			}
			return nil
		}
	},
}

// resolvePrepareTarget resolves a path to an absolute filesystem path and returns its info.
// If it's a bare package name (e.g., "sys-apps/foo"), resolves to overlay/category/package.
// Returns the resolved path (D11), file info, and error.
func resolvePrepareTarget(path, overlay string) (string, os.FileInfo, error) {
	// Check if it's a file or directory already.
	fi, err := os.Stat(path)
	if err == nil {
		return path, fi, nil
	}

	// Not a file or directory — try resolving as category/package.
	// Use strings.Split (not filepath.SplitList which splits on ":").
	if parts := strings.Split(path, "/"); len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		candidate := filepath.Join(overlay, path)
		fi, err = os.Stat(candidate)
		if err == nil {
			return candidate, fi, nil
		}
		return "", nil, fmt.Errorf("path %q not found (tried %q)", path, candidate)
	}

	// Try overlay/path as a whole.
	fullPath := filepath.Join(overlay, path)
	fi, err = os.Stat(fullPath)
	if err == nil {
		return fullPath, fi, nil
	}
	return "", nil, fmt.Errorf("path %q not found (tried %q)", path, fullPath)
}
