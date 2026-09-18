package main

import (
	"fmt"

	"bennypowers.dev/larry/internal/config"
	"bennypowers.dev/larry/internal/output"

	"github.com/spf13/cobra"
)

var (
	cfg   *config.Config
	color *output.ColorHelper
	log   *output.Logger
)

// rootCmd represents the base command.
var rootCmd = &cobra.Command{
	Use:   "larry",
	Short: "Larry — Gentoo upstream tooling",
	Long: `Larry is a Gentoo overlay upstream management tool.

It helps Gentoo maintainers check for upstream version updates, prepare ebuild bumps,
and manage the ebuild lifecycle with provider-specific pipelines.

Run 'larry help' for detailed usage information.
`,
	Version: "0.0.0-dev",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("larry: hello")
		return cmd.Help()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	rootCmd.SetVersionTemplate("larry version {{.Version}}\n")

	// Initialize default config so flag defaults are available
	cfg = config.DefaultConfig()

	// Initialize color helper for flag default detection
	var err error
	color, err = output.NewColorHelper(cfg.Color)
	if err != nil {
		return fmt.Errorf("initializing colors: %w", err)
	}

	// Color auto-detection (before flag binding)
	if cfg.Color == "auto" {
		err := color.Detect()
		if err != nil {
			return err
		}
		cfg.Color = color.Mode()
	}

	// Global flags - use config values as defaults
	rootCmd.Flags().IntVarP(&cfg.Verbose, "verbose", "v", cfg.Verbose,
		"verbose output (can be repeated for debug level: -v info, -vv debug)")
	rootCmd.Flags().BoolVarP(&cfg.Quiet, "quiet", "q", cfg.Quiet,
		"suppress all output")
	rootCmd.Flags().StringVar(&cfg.Color, "color", cfg.Color,
		"color mode: auto, always, never")
	rootCmd.Flags().BoolVar(&cfg.DryRun, "dry-run", cfg.DryRun,
		"show what would be done without making changes")
	rootCmd.Flags().StringVar(&cfg.CacheDir, "cache-dir", cfg.CacheDir,
		"cache directory for version queries")
	rootCmd.Flags().StringVar(&cfg.Distdir, "distdir", cfg.Distdir,
		"distdir path for source downloads")
	rootCmd.Flags().IntVar(&cfg.Jobs, "jobs", cfg.Jobs,
		"number of parallel jobs (0 = auto-detect)")
	rootCmd.Flags().StringVar(&cfg.GitHubToken, "github-token", cfg.GitHubToken,
		"GitHub personal access token")
	rootCmd.Flags().StringVar(&cfg.Remote, "remote", cfg.Remote,
		"git remote name for upstream resolution (default: origin)")
	rootCmd.Flags().StringVar(&config.FilePath, "config", config.FilePath,
		"config file path (default: $XDG_CONFIG_HOME/larry/config.yaml)")

	// Initialize logger after flags are parsed
	log, err = output.NewLogger(cfg.Verbose, cfg.Quiet, color)
	if err != nil {
		return fmt.Errorf("initializing logger: %w", err)
	}

	return rootCmd.Execute()
}
