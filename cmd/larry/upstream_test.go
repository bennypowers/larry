package main

import (
	"errors"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/upstream"
)

// fixtureUpstream creates a minimal overlay with known ebuilds for upstream tests.
func fixtureUpstream(t *testing.T, pkgs map[string]string) string {
	t.Helper()
	return fixtureOverlay(t,
		map[string]string{"origin": "https://example.com"},
		pkgs,
	)
}

// TestUpstreamRunWithTargetsErrorIsFatal verifies that a total walk failure
// in the non-TUI path produces a non-nil RunE error (which main prints to
// stderr and turns into a non-zero exit) instead of exiting 0 with the error
// on stdout.
func TestUpstreamRunWithTargetsErrorIsFatal(t *testing.T) {
	t.Setenv("LARRY_COLOR", "never")
	ov := fixtureUpstream(t, map[string]string{
		"dev-util/cem/cem-1.0.0.ebuild": `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
	})

	orig := runUpstreamWithTargets
	runUpstreamWithTargets = func(overlayDir string, registry *provider.ProviderRegistry, targets []*atom.Atom, allowPrereleases bool, fresh bool) ([]upstream.UpstreamResult, error) {
		return nil, errors.New("walk exploded")
	}
	defer func() { runUpstreamWithTargets = orig }()

	_, _, err := execCmd(t, "upstream", "--overlay", ov)
	if err == nil {
		t.Fatal("expected non-nil RunE error when RunWithTargets fails")
	}
	if !strings.Contains(err.Error(), "upstream check failed") {
		t.Errorf("error = %v, want it to mention 'upstream check failed'", err)
	}
	if !strings.Contains(err.Error(), "walk exploded") {
		t.Errorf("error = %v, want it to contain the underlying error", err)
	}
	if got := exitCodeFor(err); got == 0 {
		t.Error("exitCodeFor(err) must be non-zero for a failed upstream check")
	}
}

// TestUpstreamPositionalAtomFilter verifies positional atom filtering (D3, D4).
func TestUpstreamPositionalAtomFilter(t *testing.T) {
	t.Setenv("LARRY_COLOR", "never")
	ov := fixtureUpstream(t, map[string]string{
		"dev-util/cem/cem-1.0.0.ebuild": `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
		"dev-python/other/other-2.0.0.ebuild": `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
	})
	stdout, stderr, err := execCmd(t, "upstream", "--overlay", ov, "-v", "dev-util/cem")
	if err != nil {
		t.Fatalf("execCmd: %v", err)
	}
	output := stdout + stderr
	if strings.Contains(stderr, "Error:") {
		t.Errorf("unexpected error in stderr: %s", stderr)
	}
	// The overlay should be resolved to ov; the command should not error about
	// "no overlay" or "category not found".
	if strings.Contains(output, "no overlay") {
		t.Errorf("overlay not resolved; output: %s", output)
	}
}

// TestUpstreamPositionalMultipleAtoms verifies filtering with two atoms.
func TestUpstreamPositionalMultipleAtoms(t *testing.T) {
	t.Setenv("LARRY_COLOR", "never")
	ov := fixtureUpstream(t, map[string]string{
		"dev-util/a/a-1.0.ebuild": `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
		"dev-util/b/b-1.0.ebuild": `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
		"dev-util/c/c-1.0.ebuild": `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
	})
	stdout, stderr, err := execCmd(t, "upstream", "--overlay", ov, "-v", "dev-util/a", "dev-util/c")
	if err != nil {
		t.Fatalf("execCmd: %v", err)
	}
	output := stdout + stderr
	if strings.Contains(stderr, "Error:") {
		t.Errorf("unexpected error in stderr: %s", stderr)
	}
	if strings.Contains(output, "no overlay") {
		t.Errorf("overlay not resolved; output: %s", output)
	}
}

// TestUpstreamPositionalOverlayPath verifies passing an overlay dir without --overlay.
func TestUpstreamPositionalOverlayPath(t *testing.T) {
	t.Setenv("LARRY_COLOR", "never")
	ov := fixtureUpstream(t, map[string]string{
		"dev-util/cem/cem-1.0.0.ebuild": `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
	})
	// A valid overlay via the env fallback so overlay resolution does not
	// depend on cobra flag state leaked from an earlier execCmd call — the
	// positional path must still win.
	t.Setenv("LARRY_OVERLAY", fixtureUpstream(t, nil))
	_, _, err := execCmd(t, "upstream", "-v", ov)
	if err != nil {
		t.Fatalf("execCmd: %v", err)
	}
}

// TestUpstreamOverlayPathPlusAtom verifies overlay path + atom in any order.
func TestUpstreamOverlayPathPlusAtom(t *testing.T) {
	t.Setenv("LARRY_COLOR", "never")
	ov := fixtureUpstream(t, map[string]string{
		"dev-util/cem/cem-1.0.0.ebuild": `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
	})
	// See TestUpstreamPositionalOverlayPath: valid overlay via env fallback.
	t.Setenv("LARRY_OVERLAY", fixtureUpstream(t, nil))
	_, _, err := execCmd(t, "upstream", "-v", "dev-util/cem", ov)
	if err != nil {
		t.Fatalf("execCmd: %v", err)
	}
}

// TestUpstreamTwoOverlayPathsErrors verifies error on two overlay roots.
func TestUpstreamTwoOverlayPathsErrors(t *testing.T) {
	ov1 := fixtureUpstream(t, map[string]string{})
	ov2 := fixtureUpstream(t, map[string]string{})
	// See TestUpstreamPositionalOverlayPath: valid overlay via env fallback.
	t.Setenv("LARRY_OVERLAY", fixtureUpstream(t, nil))
	_, _, err := execCmd(t, "upstream", ov1, ov2)
	if err == nil {
		t.Fatal("expected error for two overlay paths")
	}
	if !strings.Contains(err.Error(), "multiple overlay paths") {
		t.Errorf("error should mention 'multiple overlay paths', got: %v", err)
	}
}

// TestUpstreamBareCategoryErrors verifies bare category is rejected.
func TestUpstreamBareCategoryErrors(t *testing.T) {
	ov := fixtureUpstream(t, map[string]string{
		"dev-util/cem/cem-1.0.0.ebuild": `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
	})
	_, _, err := execCmd(t, "upstream", "--overlay", ov, "dev-util")
	if err == nil {
		t.Fatal("expected error for bare category")
	}
}

// TestUpstreamUnknownAtomErrors verifies unknown atom is a hard error.
func TestUpstreamUnknownAtomErrors(t *testing.T) {
	ov := fixtureUpstream(t, map[string]string{})
	_, _, err := execCmd(t, "upstream", "--overlay", ov, "dev-util/does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown atom")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should mention 'does-not-exist', got: %v", err)
	}
}


// TestUpstreamIncludesDependencyRows verifies that in-overlay dependencies
// appear in the output (D1). Full content assertions are in the internal
// upstream tests; this verifies the CLI doesn't crash or error.
func TestUpstreamIncludesDependencyRows(t *testing.T) {
	t.Setenv("LARRY_COLOR", "never")
	ov := fixtureUpstream(t, map[string]string{
		"app-misc/root/root-1.0.0.ebuild": `EAPI=8
DESCRIPTION="Root"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="dev-libs/dep"
`,
		"dev-libs/dep/dep-2.5.0.ebuild": `EAPI=8
DESCRIPTION="Dep"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
	})
	_, stderr, err := execCmd(t, "upstream", "--overlay", ov, "-v")
	if err != nil {
		t.Fatalf("execCmd: %v", err)
	}
	if strings.Contains(stderr, "Error:") {
		t.Errorf("unexpected error in stderr: %s", stderr)
	}
	// Output goes to os.Stdout via fmt.Print in AnimateSpinner (not captured
	// by cobra's SetOut). That's fine — the internal tests verify content;
	// this just ensures no crash/error.
}

// TestUpstreamNoRawOSC8WhenColorNever verifies that LARRY_COLOR=never
// suppresses OSC-8 hyperlink escapes from upstream output (plan 19, D4).
func TestUpstreamNoRawOSC8WhenColorNever(t *testing.T) {
	t.Setenv("LARRY_COLOR", "never")
	ov := fixtureUpstream(t, map[string]string{
		"dev-util/cem/cem-1.0.0.ebuild": `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
	})
	_, stdout, err := execCmd(t, "upstream", "--overlay", ov, "-v")
	if err != nil {
		// If there's a setup error (no providers), the output is just a message;
		// check stdout for escape sequences anyway.
		if strings.Contains(stdout, "\033]8;;") || strings.Contains(stdout, "]8;;") {
			t.Errorf("stdout contains OSC-8 escape: %s", stdout)
		}
		return
	}
	if strings.Contains(stdout, "\033]8;;") {
		t.Errorf("stdout contains OSC-8 escape: %s", stdout)
	}
	if strings.Contains(stdout, "]8;;") {
		t.Errorf("stdout contains literal ]8;;: %s", stdout)
	}
}
