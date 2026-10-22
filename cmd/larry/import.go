package main

import (
	"fmt"

	"bennypowers.dev/larry/internal/importcmd"

	"github.com/spf13/cobra"
)

var (
	importFrom  string // --from flag: raw value (path, repo name, or empty)
	importForce bool   // --force flag
)

func init() {
	rootCmd.AddCommand(&importCmd)

	importCmd.Flags().StringVar(&importFrom, "from", "",
		"source overlay path or repo name (default: auto-search /var/db/repos/*)")
	importCmd.Flags().BoolVar(&importForce, "force", false,
		"overwrite even if target version is newer or equal")
}

var importCmd = cobra.Command{
	Use:   "import <category/package>",
	Short: "Import an ebuild from another overlay",
	Long: `Import an ebuild from a source overlay to the local overlay.

Searches the source overlay (--from to specify a path or repo name, or
auto-searches /var/db/repos/* skipping the target overlay) for the latest
stable ebuild, copies it along with the files/ directory and
metadata.xml (merging maintainers and preserving upstream remote-ids) into
the local overlay at the same category/package path.

The target overlay (where the ebuild is imported to) is auto-detected from the
local git repository (same resolution as all other larry commands).

The imported ebuild is prepared automatically; use 'larry prepare' to re-run it.

Examples:
  larry import dev-lisp/sbcl-unicode-dictionary  # Import to local overlay
  larry import --from /var/db/repos/guru dev-lisp/foo  # Explicit source path
  larry import --from guru dev-lisp/foo  # Repo name
  larry import --dry-run dev-lisp/foo  # Preview only
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		catPkg := args[0]

		// Target overlay through config; source is passed raw to resolveSourceOverlay.
		targetOverlay, err := cfg.ResolveOverlay("")
		if err != nil {
			return fmt.Errorf("resolving target overlay: %w", err)
		}

		importCfg := &importcmd.ImportConfig{
			SourceOverlay:   importFrom,
			TargetOverlay:   targetOverlay,
			DryRun:          cfg.DryRun,
			Force:           importForce,
			MaintainerName:  cfg.MaintainerName,
			MaintainerEmail: cfg.MaintainerEmail,
			Log:             newCLILogger(cmd),
		}

		result, err := importcmd.Import(catPkg, importCfg)
		if err != nil {
			return fmt.Errorf("import failed: %w", err)
		}

		log.Info("Source overlay: %s", result.SourcePath)
		if cfg.DryRun {
			log.Msg("Would import:")
			log.Msg("  Source: %s", result.SourcePath)
			log.Msg("  Target: %s", result.EbuildPath)
			if result.MetaPath != "" {
				log.Msg("  Metadata: %s", result.MetaPath)
			}
			return nil
		}

		log.Msg("Imported %s:", catPkg)
		log.Msg("  Ebuild: %s", result.EbuildPath)
		log.Msg("  Metadata: %s", result.MetaPath)

		return nil
	},
}

// newCLILogger wraps the cobra command's output writers into an importcmd.ImportLogger.
func newCLILogger(cmd *cobra.Command) *importcmd.ImportLogger {
	l := &importcmd.ImportLogger{}
	l.SetOut(cmd.OutOrStdout())
	l.SetErr(cmd.ErrOrStderr())
	return l
}
