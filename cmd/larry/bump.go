package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"bennypowers.dev/larry/internal/bump"
	"bennypowers.dev/larry/internal/config"
	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/git"
	"bennypowers.dev/larry/internal/logger"
	"bennypowers.dev/larry/internal/prepare"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/providers"

	"github.com/spf13/cobra"
)

var (
	bumpRevision    bool
	bumpVersion     string
	bumpPrereleases bool
	bumpDryRun      bool
	bumpOverlay     string
	bumpDistdir     string
	bumpForceType   string
	bumpVerbose     int
)

var bumpCmd = cobra.Command{
	Use:   "bump [category/package]",
	Short: "Bump an ebuild to a newer version or revision",
	Long: `Bump an ebuild to a newer version or revision number.

Version bump (default / --auto):
  Finds the latest non-live ebuild in the category/package directory,
  queries the upstream provider for the latest version, and creates a
  new ebuild file with the updated version.

  Examples:
    larry bump dev-lang/python        # Auto-version bump using upstream
    larry bump --version 3.12.0       # Bump to specific version
    larry bump --prereleases          # Include prereleases in auto-version

Revision bump (--revision / -r):
  Appends a new revision number to the ebuild filename.
  Increments the existing revision or starts at -r1.

  Examples:
    larry bump dev-libs/openssl -r    # Bump to -r1
    larry bump dev-libs/openssl -rr   # Increment to next revision

Examples:
  larry bump dev-python/requests              # Auto-version bump
  larry bump dev-python/requests -r           # Revision bump
  larry bump dev-python/requests --version 2.31.0
  larry bump dev-python/requests --prereleases
  larry bump /path/to/category/package        # Specify full path
`,
	Args: cobra.MaximumNArgs(1),
}

func init() {
	rootCmd.AddCommand(&bumpCmd)

	bumpCmd.Flags().BoolVarP(&bumpRevision, "revision", "r", false,
		"bump revision number instead of version")
	bumpCmd.Flags().StringVar(&bumpVersion, "version", "",
		"version to bump to (requires --auto or --version mode)")
	bumpCmd.Flags().BoolVarP(&bumpPrereleases, "prereleases", "p", false,
		"include pre-release versions in auto-version lookup")
	bumpCmd.Flags().BoolVar(&bumpDryRun, "dry-run", false,
		"show what would be done without making changes")
	bumpCmd.Flags().StringVar(&bumpOverlay, "overlay", "",
		"overlay root path (default: $LARRY_OVERLAY or /var/db/repos/gentoo)")
	bumpCmd.Flags().StringVar(&bumpDistdir, "distdir", "",
		"distdir path for source downloads (default: $LARRY_DISTDIR or PORTAGE_DISTDIR)")
	bumpCmd.Flags().StringVarP(&bumpForceType, "type", "t", "",
		"force package type (npm, cargo, go, vendored, generic)")
	bumpCmd.Flags().CountVarP(&bumpVerbose, "verbose", "v",
		"verbose output (-v for debug, -vv for trace)")

	bumpCmd.RunE = runBump
}

func runBump(cmd *cobra.Command, args []string) error {
	// Set verbose level
	logger.Verbose = bumpVerbose >= 1

	// Resolve overlay root and package path.
	var overlayRoot, pkgPath string
	if len(args) > 0 {
		// User provided a category/package path — resolve overlay from git env
		overlayRoot = config.ResolveOverlay(bumpOverlay)
		pkgPath = filepath.Join(overlayRoot, args[0])
	} else {
		overlayRoot = config.ResolveOverlay(bumpOverlay)
		pkgPath = overlayRoot
	}

	// Determine bump mode
	var mode bump.BumpMode
	switch {
	case bumpRevision:
		mode = bump.BumpModeRevision
	case bumpVersion != "":
		mode = bump.BumpModeVersion
	default:
		mode = bump.BumpModeAuto
	}

	// Validate arguments
	if bumpRevision && bumpVersion != "" {
		return fmt.Errorf("--revision and --version are mutually exclusive")
	}

	// For dry-run mode, just report what would happen
	if bumpDryRun {
		return runBumpDryRun(cmd.OutOrStdout(), pkgPath, mode, bumpVersion)
	}

	// Create provider registry for auto-version
	var reg *provider.ProviderRegistry
	if mode == bump.BumpModeAuto {
		reg = provider.NewProviderRegistry()
		for _, p := range providers.AllProviders() {
			reg.Register(p)
		}
	}

	// Run bump
	result, err := bump.Bump(bump.BumpConfig{
		Dir:             pkgPath,
		Mode:            mode,
		Version:         bumpVersion,
		ProviderReg:     reg,
		AllowPrerelease: bumpPrereleases,
	})
	if err != nil {
		return fmt.Errorf("bump failed: %w", err)
	}

	// Print result
	if err := printBumpResult(cmd.OutOrStdout(), result); err != nil {
		return err
	}

	// Run prepare on the new ebuild (downloads distfiles, generates Manifest)
	// Revision bumps also benefit from prepare (manifest regeneration, etc.)

	// Determine distdir (same default as fish script)
	distdir := bumpDistdir
	if distdir == "" {
		distdir = os.Getenv("LARRY_DISTDIR")
	}
	if distdir == "" {
		distdir = os.Getenv("PORTAGE_DISTDIR")
	}
	if distdir == "" {
		home, _ := os.UserHomeDir()
		distdir = filepath.Join(home, ".cache", "larry", "distfiles")
	}

	// Determine force type
	var forceType ebuild.PackageType
	if bumpForceType != "" {
		forceType = ebuild.PackageType(bumpForceType)
	}

	pcfg := &prepare.PrepareConfig{
		Distdir:                 distdir,
		Overlay:                 overlayRoot,
		ForceType:               forceType,
		CleanupOrphanedAssets:   true,
		RevisionBump:            mode == bump.BumpModeRevision,
	}

	logger.Debug("prepare distdir=" + distdir + " forceType=" + string(forceType))
	fmt.Fprintf(cmd.OutOrStdout(), "\nPreparing %s...\n", result.NewFile)
	if err := prepare.PrepareFromFile(result.NewFile, pcfg); err != nil {
		// Delete the stale new ebuild — prepare rollback restores from backup
		// but the backup was of the new file itself, so it persists on disk.
		os.Remove(result.NewFile)
		return fmt.Errorf("prepare failed: %w", err)
	}

	// Commit the bump (only files in the package directory)
	if args[0] != "" {
		var msg string
		if result.Revision > 0 {
			msg = fmt.Sprintf("bump(%s): revision -r%d", args[0], result.Revision)
		} else {
			msg = fmt.Sprintf("bump(%s): %s", args[0], result.NewVersion)
		}
		repo, err := git.Open(overlayRoot)
		if err != nil {
			return fmt.Errorf("commit: %w", err)
		}
		// Stage only files in the package directory to avoid picking up
		// stale files from previous failed bumps in other packages.
		pkgDir := args[0]
		commitHash, err := repo.CommitWithDir(msg, pkgDir)
		if err != nil {
			return fmt.Errorf("commit: %w", err)
		}
		logger.Debug("committed " + commitHash[:7] + ": " + msg)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Done.")
	return nil
}

func runBumpDryRun(out io.Writer, dir string, mode bump.BumpMode, version string) error {
	// Find the latest ebuild
	latest, err := bump.FindLatestEbuild(dir)
	if err != nil {
		return fmt.Errorf("dry-run: %w", err)
	}

	var newVer, action string
	switch mode {
	case bump.BumpModeRevision:
		newVer = "revision bump"
		action = "Would create revision bump"
	case bump.BumpModeVersion:
		newVer = version
		action = "Would bump to version"
	case bump.BumpModeAuto:
		action = "Would auto-version bump"
	}

	fmt.Fprintf(out, "dry-run: %s %s -> %s\n", action, latest, newVer)
	fmt.Fprintln(out, "dry-run: would also run prepare (distfiles + manifest)")
	return nil
}

func printBumpResult(out io.Writer, result *bump.BumpResult) error {
	fmt.Fprintf(out, "Bumped: %s\n", result.NewFile)
	fmt.Fprintf(out, "  Package:     %s\n", result.PackageName)
	fmt.Fprintf(out, "  Old version: %s\n", result.OldVersion)
	fmt.Fprintf(out, "  New version: %s\n", result.NewVersion)
	if result.Revision > 0 {
		fmt.Fprintf(out, "  Revision:    -r%d\n", result.Revision)
	}
	return nil
}
