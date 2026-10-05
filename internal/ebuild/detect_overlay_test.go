//go:build manual

package ebuild

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDetectType_ReferenceOverlaySweep reads every *.ebuild in the reference
// overlay and asserts that npm ebuilds are correctly detected.  This is a
// read-only Tier-2 test.
func TestDetectType_ReferenceOverlaySweep(t *testing.T) {
	overlay := os.Getenv("LARRY_TEST_OVERLAY")
	if overlay == "" {
		overlay = filepath.Join(os.Getenv("HOME"), "Developer", "gentoo-overlay")
	}

	_, err := os.Stat(overlay)
	if err != nil {
		t.Skipf("overlay %q not found; skip", overlay)
	}

	// Packages that must NOT be detected as vendored.
	nonVendored := map[string]bool{
		"app-admin/cockpit/cockpit-367.ebuild": true,
	}

	// Packages that MUST be detected as npm.
	npmPkgs := map[string]bool{
		"dev-util/fish-lsp":                          true,
		"dev-util/vtsls":                             true,
		"dev-util/eslint_d":                          true,
		"dev-util/pi-coding-agent":                   true,
		"dev-util/yaml-language-server":              true,
		"dev-util/fixjson":                           true,
		"dev-util/emmet-language-server":             true,
		"dev-util/stylelint-lsp":                     true,
		"dev-util/vscode-langservers-extracted":      true,
	}

	seenNpm := make(map[string]bool)

	err = filepath.WalkDir(overlay, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".ebuild") {
			return nil
		}

		relPath, relErr := filepath.Rel(overlay, path)
		if relErr != nil {
			relPath = path
		}

		// Check known non-vendored packages are NOT vendored.
		if nonVendored[relPath] {
			pt, detErr := DetectType(path)
			if detErr != nil {
				t.Errorf("DetectType(%s): %v", relPath, detErr)
			} else if pt == TypeVendored {
				t.Errorf("%s: DetectType = %q, should NOT be vendored", relPath, pt)
			}
		}

		// Check that known npm packages are detected as npm.
		for npmPkg := range npmPkgs {
			if strings.Contains(relPath, npmPkg) {
				seenNpm[npmPkg] = true
				pt, detErr := DetectType(path)
				if detErr != nil {
					t.Errorf("DetectType(%s): %v", relPath, detErr)
				} else if pt != TypeNPM {
					t.Errorf("%s: DetectType = %q, want %q", relPath, pt, TypeNPM)
				}
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk overlay: %v", err)
	}

	// Verify we actually found npm packages.
	for npmPkg := range npmPkgs {
		if !seenNpm[npmPkg] {
			t.Logf("note: did not see npm package %q in overlay", npmPkg)
		}
	}
}
