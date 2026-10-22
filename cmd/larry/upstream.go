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

	"github.com/spf13/cobra"
)

var (
	upstreamFresh       bool
	upstreamPrereleases bool
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
  default        plain table of significant statuses
  --output json  complete machine-readable document (ignores the filter)
  -v             every status, including up-to-date, live, and manual

For the interactive session (check, bump, prepare, test in one place),
use 'larry manage'.

Use --fresh to bypass the 6-hour version cache.`,
	Example: `  larry upstream
  larry upstream dev-util/cem
  larry upstream --fresh --prereleases
  larry upstream -v
  larry upstream --output json | jq '.results[] | select(.status == "outdated")'
  larry upstream dev-util/cem /path/to/overlay`,
	Args: cobra.ArbitraryArgs,
	RunE: runUpstream,
}

func runUpstream(cmd *cobra.Command, args []string) error {
	switch upstreamOutput {
	case "text", "json":
	default:
		return fmt.Errorf("invalid --output %q (must be text or json)", upstreamOutput)
	}

	overlayDir, targets, registry, err := resolveUpstreamArgs(args)
	if err != nil {
		return err
	}

	runOpts := upstream.RunOptions{
		OverlayDir:       overlayDir,
		Registry:         registry,
		Targets:          targets,
		AllowPrereleases: upstreamPrereleases,
		Fresh:            upstreamFresh,
		CacheDir:         cfg.ResolveCacheDir(rootFlags.CacheDir),
		Jobs:             cfg.Jobs,
	}

	// Machine output: every result, no spinner, no text on stdout.
	if upstreamOutput == "json" {
		results, err := runUpstreamWithTargets(runOpts)
		if err != nil {
			return fmt.Errorf("upstream check failed: %w", err)
		}
		return upstream.RenderJSON(cmd.OutOrStdout(), results)
	}

	spinnerMsg := fmt.Sprintf("Checking packages in overlay: %s", overlayDir)
	out, runErr := cli.AnimateSpinner(cmd.OutOrStdout(), spinnerMsg,
		!cfg.Quiet && cli.IsTTY(cmd.OutOrStdout()),
		func() (string, error) {
			results, err := runUpstreamWithTargets(runOpts)
			if err != nil {
				return "", err
			}

			if cfg.Verbose == 0 {
				results = significantOnly(results)
			}

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
	if out != "" && !cfg.Quiet {
		_, _ = fmt.Fprint(cmd.OutOrStdout(), out)
	}
	return nil
}

// significantOnly drops rows that the default view hides.
func significantOnly(results []upstream.UpstreamResult) []upstream.UpstreamResult {
	filtered := make([]upstream.UpstreamResult, 0, len(results))
	for _, r := range results {
		if r.Status.IsSignificant() {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// resolveUpstreamArgs classifies positional args, resolves the overlay, and
// builds the provider registry. Shared by upstream and manage.
func resolveUpstreamArgs(args []string) (string, []*atom.Atom, *provider.ProviderRegistry, error) {
	overlayDir, err := cfg.ResolveOverlay(rootFlags.Overlay)
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolving overlay: %w", err)
	}

	var overlayPath string
	var atomArgs []string
	for _, arg := range args {
		if info, err := os.Stat(arg); err == nil && info.IsDir() {
			if _, err := os.Stat(filepath.Join(arg, "profiles")); err == nil {
				if overlayPath != "" {
					return "", nil, nil, fmt.Errorf("multiple overlay paths given: %q and %q", overlayPath, arg)
				}
				overlayPath = arg
				continue
			}
		}
		atomArgs = append(atomArgs, arg)
	}
	if overlayPath != "" {
		overlayDir = overlayPath
	}

	var targets []*atom.Atom
	for _, atomStr := range atomArgs {
		a, err := atom.ParseAtom(overlayDir, atomStr)
		if err != nil {
			return "", nil, nil, fmt.Errorf("invalid atom %q: %w", atomStr, err)
		}
		targets = append(targets, a)
	}

	registry := provider.NewProviderRegistry()
	for _, p := range providers.AllProviders() {
		registry.Register(p)
	}
	return overlayDir, targets, registry, nil
}
