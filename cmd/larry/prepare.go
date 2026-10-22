package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bennypowers.dev/larry/internal/cli"
	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/prepare"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	prepareType           string
	prepareAll            bool
	prepareCleanupOrphans bool
	prepareTUI            bool
	prepareProgress       string
)

func init() {
	rootCmd.AddCommand(&prepareCmd)

	prepareCmd.Flags().StringVarP(&prepareType, "type", "t", "",
		"force package type: npm, cargo, go, vendored, generic (overrides auto-detection)")
	prepareCmd.Flags().BoolVarP(&prepareAll, "all", "a", false,
		"regenerate Manifest for all packages via pkgdev (manifest-only, no type-specific pipeline)")
	prepareCmd.Flags().BoolVar(&prepareCleanupOrphans, "cleanup", false,
		"remove orphaned GitHub release assets on prepare failure")
	prepareCmd.Flags().BoolVar(&prepareTUI, "tui", false,
		"interactive progress UI (requires a TTY; falls back to plain output)")
	prepareCmd.Flags().StringVar(&prepareProgress, "progress", "",
		"internal: emit NDJSON progress on stderr (json)")
	_ = prepareCmd.Flags().MarkHidden("progress")
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
  - npm:       mirror://npm/ or registry.npmjs.org/ in SRC_URI
  - cargo:     CRATES= variable present
  - go:        inherits go-module
  - vendored:  releases/download/${PN}/ in SRC_URI
  - generic:   everything else (including rpi-kernel)

  rpi-kernel packages are detected as generic. Use a custom
  .larry/prepare.sh for those. Override detection with --type.

Modes:
  Single ebuild   larry prepare sys-apps/foo/foo-1.0.0.ebuild
  Package dir     larry prepare sys-apps/foo
  Whole overlay   larry prepare --all   (Manifest only, no pipeline)

Use the global -n/--dry-run to preview. --tui streams live pipeline
progress in an interactive view (requires a terminal; falls back to
plain output when piped).`,
	Example: `  larry prepare dev-util/cem/cem-1.2.0.ebuild
  larry prepare dev-util/cem
  larry prepare --all
  larry prepare --type generic dev-util/cem/cem-1.2.0.ebuild
  larry prepare --dry-run --tui dev-util/cem`,
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
			DryRun:                cfg.DryRun,
			Distdir:               distdir,
			Overlay:               overlay,
			UseTUI:                false,
			CleanupOrphanedAssets: prepareCleanupOrphans,
			Log:                   log,
			Progress:              prepareProgressReporter(prepareProgress),
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
		log.Info("Preparing %d target(s) in %s (dry-run=%v)", len(args), overlay, cfg.DryRun)
		if len(args) == 0 && !prepareAll {
			return cmd.Help()
		}

		if prepareTUI {
			if cli.IsTTY(cmd.OutOrStdout()) {
				return runPrepareTUI(cmd, args, pcfg)
			}
			log.Warn("--tui requires a terminal; falling back to plain output")
		}
		return runPrepareTargets(cmd, args, pcfg)
	},
}

// runPrepareTargets executes prepare for the requested targets. It is shared
// by the plain and TUI paths.
func runPrepareTargets(cmd *cobra.Command, args []string, pcfg *prepare.PrepareConfig) error {
	switch {
	case prepareAll && len(args) == 0:
		// --all without args: manifest-only bulk pkgdev call (D8).
		return prepare.PrepareManifestAllWithConfig(pcfg)
	case len(args) == 0:
		// No args: show help.
		return cmd.Help()
	case len(args) == 1 && !prepareAll:
		// Single path (file or directory).
		resolved, fi, err := resolvePrepareTarget(args[0], pcfg.Overlay)
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
			resolved, _, err := resolvePrepareTarget(raw, pcfg.Overlay)
			if err != nil {
				log.Error("prepare %s: %v\n", raw, err)
				failures++
				continue
			}
			if err := prepare.PrepareFile(resolved, pcfg); err != nil {
				log.Error("prepare %s: %v\n", resolved, err)
				failures++
			}
		}
		if failures > 0 {
			return fmt.Errorf("%d package(s) failed", failures)
		}
		return nil
	}
}

// runPrepareTUI runs the pipeline in a goroutine and streams progress events
// into the Bubble Tea model.
func runPrepareTUI(cmd *cobra.Command, args []string, pcfg *prepare.PrepareConfig) error {
	ctx, cancel := context.WithCancel(context.Background())
	pcfg.Context = ctx
	pcfg.UseTUI = true

	events := make(chan prepare.ProgressEvent, 128)
	send := func(ev prepare.ProgressEvent) {
		select {
		case events <- ev:
		case <-ctx.Done():
		}
	}
	pcfg.Progress = send

	go func() {
		err := runPrepareTargets(cmd, args, pcfg)
		if err != nil {
			send(prepare.ProgressEvent{Kind: prepare.ProgressError, Message: err.Error(), Err: err})
		} else {
			send(prepare.ProgressEvent{Kind: prepare.ProgressDone})
		}
		close(events)
	}()

	title := "prepare"
	if len(args) > 0 {
		title = "prepare " + strings.Join(args, " ")
	}
	m := cli.NewPrepareModel(cli.PrepareModelOptions{
		Context: ctx,
		Events:  events,
		Theme: cli.NewTheme(cli.ThemeOptions{
			Color:      color.Enabled(),
			Hyperlinks: color.Enabled(),
			Renderer:   lipgloss.NewRenderer(cmd.OutOrStdout()),
		}),
		Title:  title,
		Cancel: cancel,
	})
	_, runErr := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(cmd.OutOrStdout())).Run()
	cancel()
	if runErr != nil {
		return runErr
	}
	return m.Err()
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
