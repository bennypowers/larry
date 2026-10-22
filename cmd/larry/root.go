package main

import (
	"fmt"
	"os"

	"bennypowers.dev/larry/internal/config"
	"bennypowers.dev/larry/internal/logger"
	"bennypowers.dev/larry/internal/output"

	"github.com/spf13/cobra"
)

var (
	// rootFlags holds the raw persistent flag values. These are populated
	// by cobra during flag parsing; persistentPreRunE then copies the
	// changed values into cfg so flag precedence beats env and config file.
	rootFlags = struct {
		Verbose     int
		Quiet       bool
		Color       string
		DryRun      bool
		CacheDir    string
		Distdir     string
		Jobs        int
		GitHubToken string
		Remote      string
		ConfigPath  string
		Overlay     string
	}{
		Color: "auto",
	}

	// rootCmd is the base command, populated in init() by subcommand
	// registration and persistent flag declaration. newRootCmd() returns
	// this same instance — cobra resets flag values on each ParseFlags.
	rootCmd = &cobra.Command{
		Use:   "larry",
		Short: "Larry — Gentoo upstream tooling",
		Long: `Larry is a Gentoo overlay upstream management tool.

Commands:
  upstream   Check ebuilds for newer upstream versions
  bump       Create a new ebuild version or revision
  prepare    Generate Manifest and distfiles for an ebuild
  import     Copy an ebuild from another overlay
  test       Run ebuild phases locally or in a container

Global flags apply to every command. Verbosity is cumulative:
  -v   informational messages and every upstream status
  -vv  debug diagnostics
  -vvv trace (provider resolution, cache hits, HTTP, subprocess argv)

Machine output:
  larry upstream --output json

Run 'larry <command> --help' for command-specific details.
`,
		Example: `  larry upstream
  larry upstream --fresh --prereleases
  larry upstream --output json | jq '.results[] | select(.status != "up_to_date")'
  larry bump dev-python/requests
  larry prepare dev-python/requests
  larry test --compile dev-python/requests
  larry import --from gentoo dev-python/requests`,
		Version:           "0.0.0-dev",
		SilenceUsage:      true,
		SilenceErrors:     true,
		PersistentPreRunE: persistentPreRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			return &ExitError{Code: 1, Err: fmt.Errorf("no subcommand")}
		},
	}

	cfg   *config.Config
	color *output.ColorHelper
	log   *output.Logger
)

// init registers subcommands and declares persistent flags.
func init() {
	// Persistent global flags.
	rootCmd.PersistentFlags().CountVarP(&rootFlags.Verbose, "verbose", "v",
		"increase verbosity: -v info, -vv debug, -vvv trace")
	rootCmd.PersistentFlags().BoolVarP(&rootFlags.Quiet, "quiet", "q", false,
		"suppress informational output (errors are still printed)")
	rootCmd.PersistentFlags().StringVar(&rootFlags.Color, "color", "auto",
		"color mode: auto, always, never")
	rootCmd.PersistentFlags().BoolVarP(&rootFlags.DryRun, "dry-run", "n", false,
		"show what would be done without making changes")
	rootCmd.PersistentFlags().StringVar(&rootFlags.CacheDir, "cache-dir", "",
		"cache directory for version queries")
	rootCmd.PersistentFlags().StringVar(&rootFlags.Distdir, "distdir", "",
		"distdir path for source downloads")
	rootCmd.PersistentFlags().IntVar(&rootFlags.Jobs, "jobs", 0,
		"parallel worker count (0 = auto-detect; currently single-threaded)")
	rootCmd.PersistentFlags().StringVar(&rootFlags.GitHubToken, "github-token", "",
		"GitHub personal access token (also exported as GITHUB_TOKEN for gh)")
	rootCmd.PersistentFlags().StringVar(&rootFlags.Remote, "remote", "origin",
		"git remote name for upstream resolution (default: origin)")
	rootCmd.PersistentFlags().StringVar(&rootFlags.ConfigPath, "config", "",
		"config file path (default: $XDG_CONFIG_HOME/larry/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&rootFlags.Overlay, "overlay", "",
		"overlay root path (default: $LARRY_OVERLAY, config file, or git CWD)")
}

// persistentPreRunE loads config, applies flag overrides, and
// initializes color helper + logger from the final config.
func persistentPreRunE(cmd *cobra.Command, args []string) error {
	// Wire --config before loading: config.Load reads this package variable.
	config.FilePath = rootFlags.ConfigPath

	var err error
	cfg, err = config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Apply flag overrides (only if the user explicitly set them).
	if cmd.Flags().Changed("verbose") {
		cfg.Verbose = rootFlags.Verbose
	}
	if cmd.Flags().Changed("quiet") {
		cfg.Quiet = rootFlags.Quiet
	}
	if cmd.Flags().Changed("color") {
		cfg.Color = rootFlags.Color
	}
	if cmd.Flags().Changed("dry-run") {
		cfg.DryRun = rootFlags.DryRun
	}
	if cmd.Flags().Changed("cache-dir") {
		cfg.CacheDir = rootFlags.CacheDir
	}
	if cmd.Flags().Changed("distdir") {
		cfg.Distdir = rootFlags.Distdir
	}
	if cmd.Flags().Changed("jobs") {
		cfg.Jobs = rootFlags.Jobs
	}
	if cmd.Flags().Changed("github-token") {
		cfg.GitHubToken = rootFlags.GitHubToken
	}
	if cmd.Flags().Changed("remote") {
		cfg.Remote = rootFlags.Remote
	}
	if cmd.Flags().Changed("overlay") {
		cfg.Overlay = rootFlags.Overlay
	}

	// Re-initialize color helper and logger from the final config.
	color, err = output.NewColorHelper(cfg.Color)
	if err != nil {
		return fmt.Errorf("initializing colors: %w", err)
	}
	if cfg.Color == "auto" {
		if err := color.Detect(); err != nil {
			return err
		}
		cfg.Color = color.Mode()
	}

	log, err = output.NewLogger(cfg.Verbose, cfg.Quiet, color)
	if err != nil {
		return fmt.Errorf("initializing logger: %w", err)
	}

	// Wire injectable writers so test harnesses can capture output.
	log.SetOut(cmd.OutOrStdout())
	log.SetErr(cmd.ErrOrStderr())

	// Give the prepare/ebuild diagnostics the same verbosity and stream.
	logger.SetLevel(cfg.Verbose)
	logger.SetOutput(cmd.ErrOrStderr())

	// gh and internal/providers read GITHUB_TOKEN directly.
	if cfg.GitHubToken != "" {
		_ = os.Setenv("GITHUB_TOKEN", cfg.GitHubToken)
	}

	log.Debug("effective config: overlay=%q distdir=%q cache=%q color=%s verbose=%d quiet=%v",
		cfg.Overlay, cfg.Distdir, cfg.CacheDir, cfg.Color, cfg.Verbose, cfg.Quiet)

	return nil
}

// newRootCmd returns the root command for execution.
// Each call returns the same instance; cobra resets flag values
// during ParseFlags so repeated calls are safe.
func newRootCmd() *cobra.Command {
	return rootCmd
}

// Execute runs the root command.
func Execute() error {
	return newRootCmd().Execute()
}
