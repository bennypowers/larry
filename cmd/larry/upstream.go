package main

import (
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
	"github.com/spf13/cobra"
)

var (
	upstreamFresh       bool
	upstreamPrereleases bool
	upstreamTUI         bool
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
}

var upstreamCmd = cobra.Command{
	Use:   "upstream [cat/pkg...] [overlay-path]",
	Short: "Check for upstream version updates",
	Long: `Check for upstream version updates for packages in a Gentoo overlay.

Walks the specified overlay directory and checks each ebuild against its
upstream source. Skips acct-group/* and acct-user/* packages. Live versions
(9999) are reported separately.

Overlay directory resolution (priority order):
  1. --overlay flag
  2. $LARRY_OVERLAY environment variable
  3. git rev-parse --show-toplevel of the current working directory

Providers are auto-detected from SRC_URI:
  mirror://pypi/   -> PyPI
  mirror://npm/, registry.npmjs.org/ -> npm
  mirror://github/ -> GitHub
  mirror://gitlab/ -> GitLab
  mirror://codeberg/ -> Codeberg
  mirror://sourcehut/ -> SourceHut

Positional arguments:
  larry upstream [cat/pkg ...] [overlay-path]

  An argument is the overlay root iff os.Stat says it is a directory
  and it contains a profiles/ subdirectory. At most one such argument
  is allowed. Every other argument is a category/package atom.

  The overlay root is detected first, then atoms are resolved against it,
  so "larry upstream dev-util/cem /path/to/overlay" works regardless of order.

Examples:
  larry upstream                      # Check overlay from git CWD
  larry upstream dev-util/cem         # Check one package
  larry upstream --fresh              # Bypass cache
  larry upstream --prereleases        # Include prereleases
`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		// Non-TTY mode: run check with spinner.
		if !upstreamTUI {
			spinnerMsg := fmt.Sprintf("Checking packages in overlay: %s", overlayDir)
			out, runErr := cli.AnimateSpinner(spinnerMsg, cfg.Quiet, func() (string, error) {
				results, err := runUpstreamWithTargets(overlayDir, registry, targets, upstreamPrereleases, upstreamFresh)
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
				if err := upstream.RenderTable(&buf, results, upstream.HyperlinksEnabled()); err != nil {
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

		// TUI mode: run check then show interactive TUI.
		results, err := runUpstreamWithTargets(overlayDir, registry, targets, upstreamPrereleases, upstreamFresh)
		if err != nil {
			return fmt.Errorf("upstream check failed: %w", err)
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

		if len(results) == 0 {
			log.Msg("All packages are up-to-date.")
			return nil
		}

		var ptrResults []*upstream.UpstreamResult
		for i := range results {
			ptrResults = append(ptrResults, &results[i])
		}
		m := cli.NewUpstreamModel(ptrResults)
		_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
		return err
	},
}
