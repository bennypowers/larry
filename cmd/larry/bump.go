package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"bennypowers.dev/larry/internal/bump"
	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/git"
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
	bumpForceType   string
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
	bumpCmd.Flags().StringVarP(&bumpForceType, "type", "t", "",
		"force package type (npm, cargo, go, vendored, generic)")

	bumpCmd.RunE = runBump
}

func runBump(cmd *cobra.Command, args []string) error {
	// Resolve overlay root and package path.
	var overlayRoot, pkgPath string
	var err error
	overlayRoot, err = cfg.ResolveOverlay(rootFlags.Overlay)
	if err != nil {
		return fmt.Errorf("resolving overlay: %w", err)
	}
	if len(args) > 0 {
		pkgPath = filepath.Join(overlayRoot, args[0])
	} else {
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

	// Run bump.
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

	// Print result.
	if err := printBumpResult(cmd.OutOrStdout(), result); err != nil {
		return err
	}

	// Run prepare on the new ebuild (downloads distfiles, generates Manifest).
	// Revision bumps also benefit from prepare (manifest regeneration, etc.)

	// Resolve distdir through config (override → env → config → PORTAGE_DISTDIR → cache).
	distdir := cfg.ResolveDistdir(rootFlags.Distdir)

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

	// Resolve GitHub repo for release uploads (non-fatal for non-upload types).
	if repo, err := cfg.ResolveGitHubRepo(overlayRoot, cfg.Remote); err == nil && repo != "" {
		pcfg.GitHubRepo = repo
	} else if err != nil {
		log.Warn("resolve GitHub repo: %v (uploads may fail)", err)
	}

	log.Debug("prepare distdir=%s forceType=%s", distdir, string(forceType))
	// Log output failures are unrecoverable and not meaningful in this context.
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nPreparing %s...\n", result.NewFile)
	if err := prepare.PrepareFromFile(result.NewFile, pcfg); err != nil {
		// Delete the stale new ebuild — prepare rollback restores from backup
		// but the backup was of the new file itself, so it persists on disk.
		_ = os.Remove(result.NewFile) // best-effort cleanup on prepare failure
		return fmt.Errorf("prepare failed: %w", err)
	}

	// Commit the bump (only files in the package directory).
	var pkgName string
	if len(args) > 0 {
		pkgName = args[0]
	} else {
		pkgName = result.PackageName
	}
	if pkgName != "" {
		var msg string
		if result.Revision > 0 {
			msg = fmt.Sprintf("bump(%s): revision -r%d", pkgName, result.Revision)
		} else {
			msg = fmt.Sprintf("bump(%s): %s", pkgName, result.NewVersion)
		}
		repo, err := git.Open(overlayRoot)
		if err != nil {
			return fmt.Errorf("commit: %w", err)
		}
		// Stage only files in the package directory to avoid picking up
		// stale files from previous failed bumps in other packages.
		commitHash, err := repo.CommitWithDir(msg, pkgName)
		if err != nil {
			return fmt.Errorf("commit: %w", err)
		}
		log.Debug("committed %s: %s", commitHash[:7], msg)
	}

	// Log output failures are unrecoverable and not meaningful in this context.
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Done.")
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

	// Dry-run output; failures are unrecoverable.
	_, _ = fmt.Fprintf(out, "dry-run: %s %s -> %s\n", action, latest, newVer)
	_, _ = fmt.Fprintln(out, "dry-run: would also run prepare (distfiles + manifest)")
	return nil
}

func printBumpResult(out io.Writer, result *bump.BumpResult) error {
	// Output writer; failures are unrecoverable.
	_, _ = fmt.Fprintf(out, "Bumped: %s\n", result.NewFile)
	_, _ = fmt.Fprintf(out, "  Package:     %s\n", result.PackageName)
	_, _ = fmt.Fprintf(out, "  Old version: %s\n", result.OldVersion)
	_, _ = fmt.Fprintf(out, "  New version: %s\n", result.NewVersion)
	if result.Revision > 0 {
		_, _ = fmt.Fprintf(out, "  Revision:    -r%d\n", result.Revision)
	}
	for _, w := range result.Warnings {
		_, _ = fmt.Fprintf(out, "  WARNING: %s\n", w)
	}
	return nil
}
