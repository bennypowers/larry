package main

import (
	"fmt"
	"os"
	"path/filepath"

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
		"prepare all ebuilds in the directory")
	prepareCmd.Flags().BoolVarP(&prepareDryRun, "dry-run", "n", false,
		"show what would be done without making changes")
	prepareCmd.Flags().BoolVar(&prepareCleanupOrphans, "cleanup", false,
		"remove orphaned GitHub release assets on prepare failure")
}

var prepareCmd = cobra.Command{
	Use:   "prepare [category/package] [ebuild...]",
	Short: "Prepare an ebuild for commit (manifest, distfiles, etc.)",
	Long: `Prepare an ebuild for commit by running the appropriate type-specific pipeline.

Custom prepare scripts:
  If files/prepare.sh exists in the ebuild's directory, it runs instead
  of any built-in pipeline. The script runs in a userspace sandbox:
  PATH is restricted to system dirs, dangerous env vars are stripped,
  and writes are policy-limited to the ebuild dir + DISTDIR.
  Dry-run skips script execution.

Type detection (auto):
  Detects package type from ebuild content:
  - npm:       mirror://npm/ in SRC_URI
  - cargo:     CRATES= variable present
  - go:        inherits go-module
  - vendored:  releases/download/${PN}/ in SRC_URI
  - generic:   everything else

Override detection with --type flag.

Commands:
  larry prepare sys-apps/foo/foo-1.0.0.ebuild    # Prepare single ebuild
  larry prepare sys-apps/foo                      # Prepare all ebuilds in pkg dir
  larry prepare --all /var/db/repos/gentoo/sys-apps/foo  # All ebuilds in pkg dir
  larry prepare --type generic sys-apps/foo/foo.ebuild # Force type

Flags:
  -t, --type string       Force package type (overrides auto-detection)
  -a, --all               Prepare all ebuilds in directory
  -n, --dry-run           Show what would be done without making changes
      --distdir string    Distdir path for source downloads
      --overlay string    Overlay root path
      --cleanup           Remove orphaned GitHub release assets on failure
`,
	Args: cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve distdir through config.
		distdir := cfg.ResolveDistdir(rootFlags.Distdir)

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
			// --all without args: prepare all ebuilds in overlay root.
			return prepare.PrepareAll(overlay, pcfg)
		case len(args) == 0:
			// No args: show help.
			return cmd.Help()
		case len(args) == 1 && !prepareAll:
			// Single path (file or directory).
			path := args[0]
			_, fi, err := resolvePrepareTarget(path, overlay)
			if err != nil {
				return err
			}

			if fi.IsDir() {
				// Directory: prepare all ebuilds.
				return prepare.PrepareAll(path, pcfg)
			}

			// Single file: prepare that ebuild.
			return prepare.PrepareFromFile(path, pcfg)
		default:
			// Multiple paths: prepare each.
			for _, path := range args {
				if err := prepare.PrepareFromFile(path, pcfg); err != nil {
					return fmt.Errorf("prepare %s: %w", path, err)
				}
			}
			return nil
		}
	},
}

// resolvePrepareTarget determines if a path is a file or directory.
// If it's a bare package name (e.g., "sys-apps/foo"), resolve to the overlay path.
func resolvePrepareTarget(path, overlay string) (*ebuild.EbuildInfo, os.FileInfo, error) {
	// Check if it's a file.
	fi, err := os.Stat(path)
	if err != nil {
		// Not a file or directory — try resolving as category/package.
		if parts := filepath.SplitList(path); len(parts) == 2 {
			path = filepath.Join(overlay, parts[0], parts[1])
			fi, err = os.Stat(path)
			if err != nil {
				return nil, nil, fmt.Errorf("path %q not found (tried %q)", path, filepath.Join(overlay, parts[0], parts[1]))
			}
		} else {
			// Try overlay/category/package.
			fullPath := filepath.Join(overlay, path)
			fi, err = os.Stat(fullPath)
			if err != nil {
				return nil, nil, fmt.Errorf("path %q not found (tried %q)", path, fullPath)
			}
			path = fullPath
		}
	}

	if fi.IsDir() {
		return nil, fi, nil
	}

	// Single ebuild file — parse info for validation.
	info, err := prepare.NewEbuildInfoFromPath(path)
	if err != nil {
		return nil, fi, fmt.Errorf("parse ebuild info: %w", err)
	}
	info.Path = path

	return info, fi, nil
}


