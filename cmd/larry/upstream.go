package main

import (
	"fmt"
	"strings"

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
	Use:   "upstream [categories...]",
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
  mirror://npm/    -> npm
  mirror://github/ -> GitHub
  mirror://gitlab/ -> GitLab
  mirror://codeberg/ -> Codeberg
  mirror://sourcehut/ -> SourceHut

Examples:
  larry upstream                      # Check overlay from git CWD
  larry upstream dev-python dev-lisp  # Check specific categories
  larry upstream --fresh              # Bypass cache
  larry upstream --prereleases        # Include prereleases
`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve overlay directory.
		overlayDir, err := cfg.ResolveOverlay(rootFlags.Overlay)
		if err != nil {
			return fmt.Errorf("resolving overlay: %w", err)
		}

		// Determine categories to check.
		var categories []string
		if len(args) > 0 {
			categories = []string{args[0]}
		}

		// Create provider registry with default providers.
		registry := provider.NewProviderRegistry()
		for _, p := range providers.AllProviders() {
			registry.Register(p)
		}

		// Non-TTY mode: run check with spinner.
		if !upstreamTUI {
			spinnerMsg := fmt.Sprintf("Checking packages in overlay: %s", overlayDir)
			cli.AnimateSpinner(spinnerMsg, false, func() string {
				results, err := upstream.RunWithCategories(overlayDir, registry, categories, upstreamPrereleases, upstreamFresh)
				if err != nil {
					return "Error: " + err.Error()
				}

				// Filter results based on verbosity (exclude LIVE unless --verbose).
				if rootFlags.Verbose == 0 {
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
					return ""
				}

				var buf strings.Builder
				upstream.RenderTable(&buf, results)
				buf.WriteString("\nCheck complete. Cache stored in: " + upstream.DefaultCachePath() + " (expires after 6h)\n")
				buf.WriteString("Use --fresh to force a refresh.\n")
				return buf.String()
			})
			return nil
		}

		// TUI mode: run check then show interactive TUI.
		results, err := upstream.RunWithCategories(overlayDir, registry, categories, upstreamPrereleases, upstreamFresh)
		if err != nil {
			return fmt.Errorf("upstream check failed: %w", err)
		}

		// Filter results based on verbosity (exclude LIVE unless --verbose).
		if rootFlags.Verbose == 0 {
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


