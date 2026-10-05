package main

import (
	"fmt"
	"path/filepath"

	"bennypowers.dev/larry/internal/importcmd"

	"github.com/spf13/cobra"
)

var (
	importFrom string // --from flag: source overlay path
	importDry  bool   // --dry-run flag
	importForce bool  // --force flag
)

func init() {
	rootCmd.AddCommand(&importCmd)

	importCmd.Flags().StringVar(&importFrom, "from", "",
		"source overlay path (default: /var/db/repos/gentoo)")
	importCmd.Flags().BoolVar(&importDry, "dry-run", false,
		"show what would be done without making changes")
	importCmd.Flags().BoolVar(&importForce, "force", false,
		"overwrite even if target version is newer or equal")
}

var importCmd = cobra.Command{
	Use:   "import <category/package>",
	Short: "Import an ebuild from another overlay",
	Long: `Import an ebuild from a source overlay to the local overlay.

Searches the source overlay (default: /var/db/repos/gentoo, or --from to specify)
for the latest stable ebuild, copies it along with the files/ directory and
metadata.xml (merging maintainers and preserving upstream remote-ids) into
the local overlay at the same category/package path.

The target overlay (where the ebuild is imported to) is auto-detected from the
local git repository (same resolution as all other larry commands).

After importing, run 'larry prepare' on the imported ebuild to run the
appropriate type-specific pipeline (npm, cargo, go, vendored, or generic).

Examples:
  larry import dev-lisp/sbcl-unicode-dictionary  # Import to local overlay
  larry import --from /var/db/repos/guru dev-lisp/foo  # Explicit source
  larry import --dry-run dev-lisp/foo  # Preview only
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		catPkg := args[0]

		// Resolve overlays through config.
		sourceOverlay, err := cfg.ResolveOverlay(importFrom)
		if err != nil {
			return fmt.Errorf("resolving source overlay: %w", err)
		}
		targetOverlay, err := cfg.ResolveOverlay("")
		if err != nil {
			return fmt.Errorf("resolving target overlay: %w", err)
		}

		importCfg := &importcmd.ImportConfig{
			SourceOverlay: sourceOverlay,
			TargetOverlay: targetOverlay,
			DryRun:        importDry,
			Force:         importForce,
		}

		result, err := importcmd.Import(catPkg, importCfg)
		if err != nil {
			return fmt.Errorf("import failed: %w", err)
		}

		if importDry {
			fmt.Printf("Would import:\n")
			fmt.Printf("  Source: %s\n", result.SourcePath)
			fmt.Printf("  Target: %s\n", result.EbuildPath)
			if result.MetaPath != "" {
				fmt.Printf("  Metadata: %s\n", result.MetaPath)
			}
			return nil
		}

		fmt.Printf("Imported %s:\n", catPkg)
		fmt.Printf("  Ebuild: %s\n", result.EbuildPath)
		fmt.Printf("  Metadata: %s\n", result.MetaPath)

		// Print next steps
		ebuildDir := filepath.Dir(result.EbuildPath)
		ebuildRel := ebuildDir
		fmt.Printf("\nRun 'larry prepare %s' to prepare the ebuild for commit.\n", ebuildRel)

		return nil
	},
}
