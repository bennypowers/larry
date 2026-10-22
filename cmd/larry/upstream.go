package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/cli"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/providers"
	"bennypowers.dev/larry/internal/upstream"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	upstreamFresh       bool
	upstreamPrereleases bool
	upstreamTUI         bool
	upstreamOutput      string
)

// runUpstreamWithTargets is upstream.RunWithTargets; tests replace it.
var runUpstreamWithTargets = upstream.RunWithTargets

func init() {
	rootCmd.AddCommand(&upstreamCmd)

	upstreamCmd.Flags().BoolVarP(&upstreamFresh, "fresh", "f", false,
		"bypass cache and fetch fresh versions")
	upstreamCmd.Flags().BoolVarP(&upstreamPrereleases, "prereleases", "p", false,
		"include pre-release versions in checks")
	upstreamCmd.Flags().BoolVar(&upstreamTUI, "tui", false,
		"render results as an interactive TUI (requires TTY)")
	upstreamCmd.Flags().StringVar(&upstreamOutput, "output", "text",
		"output format: text or json")
}

var upstreamCmd = cobra.Command{
	Use:   "upstream [cat/pkg...] [overlay-path]",
	Short: "Check for upstream version updates",
	Long: `Check for upstream version updates for packages in a Gentoo overlay.

Walks the overlay and compares each ebuild with its upstream source.
acct-group/* and acct-user/* are skipped. Dependencies declared in
DEPEND/RDEPEND are scanned up to 3 levels deep and shown indented.
Live (9999) ebuilds are reported separately.

Overlay directory resolution (priority order):
  1. --overlay flag
  2. $LARRY_OVERLAY environment variable
  3. the current git repository root

Providers are auto-detected from SRC_URI:
  mirror://pypi/                    -> PyPI
  mirror://npm/, registry.npmjs.org/ -> npm
  mirror://github/                  -> GitHub
  mirror://gitlab/                  -> GitLab
  mirror://codeberg/                -> Codeberg
  mirror://sourcehut/               -> SourceHut

Positional arguments:
  larry upstream [cat/pkg ...] [overlay-path]

  An argument is the overlay root when it is a directory containing
  profiles/. At most one is allowed; every other argument is an atom.

Output:
  default        significant statuses only (outdated, ahead, newer-tag, ...)
  -v             every status, including up-to-date, live, and manual
  --output json  complete machine-readable document (ignores the filter)
  --tui          interactive filtered table with details (requires a TTY)

Use --fresh to bypass the 6-hour version cache.`,
	Example: `  larry upstream
  larry upstream dev-util/cem
  larry upstream --fresh --prereleases
  larry upstream -v
  larry upstream --output json | jq '.results[] | select(.status == "outdated")'
  larry upstream --tui
  larry upstream dev-util/cem /path/to/overlay`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		switch upstreamOutput {
		case "text", "json":
		default:
			return fmt.Errorf("invalid --output %q (must be text or json)", upstreamOutput)
		}
		if upstreamTUI && upstreamOutput == "json" {
			return fmt.Errorf("--tui cannot be combined with --output json")
		}
		// Resolve overlay directory.
		overlayDir, err := cfg.ResolveOverlay(rootFlags.Overlay)
		if err != nil {
			return fmt.Errorf("resolving overlay: %w", err)
		}

		// Classify arguments: overlay root vs atoms.
		// Classification rule: an arg is the overlay root iff it is a directory
		// and contains a profiles/ subdirectory.
		var overlayPath string
		var atomArgs []string

		for _, arg := range args {
			// Check if this looks like an overlay root
			if info, err := os.Stat(arg); err == nil && info.IsDir() {
				profilesPath := filepath.Join(arg, "profiles")
				if _, err := os.Stat(profilesPath); err == nil {
					if overlayPath != "" {
						return fmt.Errorf("multiple overlay paths given: %q and %q", overlayPath, arg)
					}
					overlayPath = arg
					continue
				}
			}
			// Otherwise it's an atom
			atomArgs = append(atomArgs, arg)
		}

		// If overlay was detected, use it; otherwise fall back to resolved overlay.
		if overlayPath != "" {
			overlayDir = overlayPath
		}

		// Resolve atom arguments against the overlay.
		var targets []*atom.Atom
		for _, atomStr := range atomArgs {
			a, err := atom.ParseAtom(overlayDir, atomStr)
			if err != nil {
				return fmt.Errorf("invalid atom %q: %w", atomStr, err)
			}
			targets = append(targets, a)
		}

		// Create provider registry with default providers.
		registry := provider.NewProviderRegistry()
		for _, p := range providers.AllProviders() {
			registry.Register(p)
		}

		runOpts := upstream.RunOptions{
			OverlayDir:       overlayDir,
			Registry:         registry,
			Targets:          targets,
			AllowPrereleases: upstreamPrereleases,
			Fresh:            upstreamFresh,
			CacheDir:         cfg.ResolveCacheDir(rootFlags.CacheDir),
		}

		// Machine output: every result, no spinner, no text on stdout.
		if upstreamOutput == "json" {
			results, err := runUpstreamWithTargets(runOpts)
			if err != nil {
				return fmt.Errorf("upstream check failed: %w", err)
			}
			return upstream.RenderJSON(cmd.OutOrStdout(), results)
		}

		// --tui needs a terminal; fall back rather than corrupt captured output.
		useTUI := upstreamTUI
		if useTUI && !cli.IsTTY(cmd.OutOrStdout()) {
			log.Warn("--tui requires a terminal; falling back to plain output")
			useTUI = false
		}

		// Non-TTY mode: run check with spinner.
		if !useTUI {
			spinnerMsg := fmt.Sprintf("Checking packages in overlay: %s", overlayDir)
			out, runErr := cli.AnimateSpinner(cmd.OutOrStdout(), spinnerMsg,
				!cfg.Quiet && cli.IsTTY(cmd.OutOrStdout()),
				func() (string, error) {
					results, err := runUpstreamWithTargets(runOpts)
					if err != nil {
						// Errors belong to the caller: return "" so the spinner
						// never prints error text — RunE reports it to stderr
						// with a non-zero exit.
						return "", err
					}

					// Filter results based on verbosity (exclude LIVE unless --verbose).
					if cfg.Verbose == 0 {
						var filtered []upstream.UpstreamResult
						for _, r := range results {
							if r.Status.IsSignificant() {
								filtered = append(filtered, r)
							}
						}
						results = filtered
					}

					// Render output.
					if len(results) == 0 {
						log.Msg("All packages are up-to-date.")
						log.Msg("Check complete. Cache stored in: %s (expires after 6h)", upstream.DefaultCachePath())
						log.Msg("Use --fresh to force a refresh.")
						return "", nil
					}

					var buf strings.Builder
					if err := upstream.RenderTable(&buf, results, upstream.RenderOptions{
						Color:      color.Enabled(),
						Hyperlinks: color.Enabled(),
					}); err != nil {
						return "", fmt.Errorf("render table: %w", err)
					}
					buf.WriteString("\nCheck complete. Cache stored in: " + upstream.DefaultCachePath() + " (expires after 6h)\n")
					buf.WriteString("Use --fresh to force a refresh.\n")
					return buf.String(), nil
				})
			if runErr != nil {
				return fmt.Errorf("upstream check failed: %w", runErr)
			}
			// Results are the caller's to write: --quiet suppresses them,
			// while errors above still reach stderr.
			if out != "" && !cfg.Quiet {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), out)
			}
			return nil
		}

		// TUI mode: run the walk inside the model so progress and the spinner
		// are live, and filter there according to -v.
		ctx, cancel := context.WithCancel(context.Background())
		m := cli.NewUpstreamModel(cli.UpstreamModelOptions{
			Context: ctx,
			Fetch: func(ctx context.Context, progress func(upstream.Progress)) ([]upstream.UpstreamResult, error) {
				opts := runOpts
				opts.Context = ctx
				opts.Progress = progress
				return runUpstreamWithTargets(opts)
			},
			ShowAll: cfg.Verbose >= 1,
			Theme: cli.NewTheme(cli.ThemeOptions{
				Color:      color.Enabled(),
				Hyperlinks: color.Enabled(),
				Renderer:   lipgloss.NewRenderer(cmd.OutOrStdout()),
			}),
			Overlay: overlayDir,
			Cancel:  cancel,
		})
		_, runErr := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(cmd.OutOrStdout())).Run()
		cancel()
		if runErr != nil {
			return runErr
		}
		return m.Err()
	},
}
