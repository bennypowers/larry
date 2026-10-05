package main

import (
	"fmt"

	"bennypowers.dev/larry/internal/config"
	"bennypowers.dev/larry/internal/output"

	"github.com/spf13/cobra"
)

var (
	// rootFlags holds the raw persistent flag values. These are populated
	// by cobra during flag parsing; persistentPreRunE then copies the
	// changed values into cfg so flag precedence beats env and config file.
	rootFlags = struct {
		Verbose      int
		Quiet        bool
		Color        string
		DryRun       bool
		CacheDir     string
		Distdir      string
		Jobs         int
		GitHubToken  string
		Remote       string
		ConfigPath   string
		Overlay      string
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

It helps Gentoo maintainers check for upstream version updates, prepare ebuild bumps,
and manage the ebuild lifecycle with provider-specific pipelines.

Run 'larry help' for detailed usage information.
`,
		Version:         "0.0.0-dev",
		SilenceUsage:    true,
		SilenceErrors:   true,
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
		"verbose output (-v info, -vv debug)")
	rootCmd.PersistentFlags().BoolVarP(&rootFlags.Quiet, "quiet", "q", false,
		"suppress all output")
	rootCmd.PersistentFlags().StringVar(&rootFlags.Color, "color", "auto",
		"color mode: auto, always, never")
	rootCmd.PersistentFlags().BoolVarP(&rootFlags.DryRun, "dry-run", "n", false,
		"show what would be done without making changes")
	rootCmd.PersistentFlags().StringVar(&rootFlags.CacheDir, "cache-dir", "",
		"cache directory for version queries")
	rootCmd.PersistentFlags().StringVar(&rootFlags.Distdir, "distdir", "",
		"distdir path for source downloads")
	rootCmd.PersistentFlags().IntVar(&rootFlags.Jobs, "jobs", 0,
		"number of parallel jobs (0 = auto-detect)")
	rootCmd.PersistentFlags().StringVar(&rootFlags.GitHubToken, "github-token", "",
		"GitHub personal access token")
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
