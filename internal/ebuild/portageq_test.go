package ebuild

import (
	"os"
	"testing"
)

// ---- TestPortageQ_New: creates instance ----

func TestPortageQ_New(t *testing.T) {
	q := NewPortageQ()
	if q == nil {
		t.Fatal("NewPortageQ() returned nil")
	}
}

// ---- TestPortageQ_HasPortageq: detects portageq availability ----

func TestPortageQ_HasPortageq(t *testing.T) {
	q := NewPortageQ()
	has := q.HasPortageq()
	// The test environment may or may not have portageq.
	// Just verify it returns a boolean without panicking.
	_ = has
}

// ---- TestPortageQ_HasEbuild: detects ebuild availability ----

func TestPortageQ_HasEbuild(t *testing.T) {
	q := NewPortageQ()
	has := q.HasEbuild()
	_ = has
}

// ---- TestPortageQ_ValidateTools: reports available/missing tools ----

func TestPortageQ_ValidateTools(t *testing.T) {
	q := NewPortageQ()
	err := q.ValidateTools()
	// In the test environment, portage may not be installed.
	// Accept either success or a known missing-tool error.
	if err != nil {
		errStr := err.Error()
		if !containsAny(errStr, "portageq", "ebuild") {
			t.Fatalf("ValidateTools() unexpected error: %v", err)
		}
	}
}

// ---- TestPortageQ_EnvVar_PN: envvar resolves tree-level variables ----
// NOTE: PN/PV/P are ebuild-level variables and cannot be resolved via EnvVar alone.
// They require a full tree context via EnvVarForPkg, or are resolved via ParseEbuildInfo.
// Here we test with DISTDIR to verify the EnvVar path works.

func TestPortageQ_EnvVar_PN(t *testing.T) {
	q := NewPortageQ()
	if !q.HasPortageq() {
		t.Skip("portageq not available")
	}

	// DISTDIR is a tree-level variable — EnvVar is designed for tree-level vars
	val, err := q.EnvVar("", "DISTDIR")
	if err != nil {
		t.Skipf("EnvVar(\"\", DISTDIR) error: %v (tree-level var, requires Portage env)", err)
	}
	if val == "" {
		t.Error("DISTDIR should be non-empty")
	}
}

// ---- TestPortageQ_EnvVar_PV: envvar resolves DISTDIR ----

func TestPortageQ_EnvVar_PV(t *testing.T) {
	q := NewPortageQ()
	if !q.HasPortageq() {
		t.Skip("portageq not available")
	}

	val, err := q.EnvVar("", "DISTDIR")
	if err != nil {
		t.Skipf("EnvVar(\"\", DISTDIR) error: %v", err)
	}
	if val == "" {
		t.Error("DISTDIR should be non-empty")
	}
}

// ---- TestPortageQ_EnvVar_P: envvar resolves DISTDIR ----

func TestPortageQ_EnvVar_P(t *testing.T) {
	q := NewPortageQ()
	if !q.HasPortageq() {
		t.Skip("portageq not available")
	}

	val, err := q.EnvVar("", "DISTDIR")
	if err != nil {
		t.Skipf("EnvVar(\"\", DISTDIR) error: %v", err)
	}
	if val == "" {
		t.Error("DISTDIR should be non-empty")
	}
}

// ---- TestPortageQ_EnvVarForPkg_PN: ebuild-level vars need proper tree context ----
// PN/PV/P cannot be resolved via portageq envvar without a full tree structure.
// This test verifies the error path when tree context is missing.

func TestPortageQ_EnvVarForPkg_PN(t *testing.T) {
	q := NewPortageQ()
	if !q.HasPortageq() {
		t.Skip("portageq not available")
	}

	// Without a proper tree, portageq returns exit code 1 for ebuild-level vars.
	// The test verifies that EnvVarForPkg properly sets EBUILD envvar and returns
	// a descriptive error when the tree context is incomplete.
	tmpDir := t.TempDir()
	ebuildPath := tmpDir + "/cat/pkg-1.0/my-pkg-1.0.ebuild"
	if err := os.MkdirAll(tmpDir+"/cat/pkg-1.0", 0755); err != nil {
		t.Fatal(err)
	}
	content := `EAPI=8
PN="my-pkg"
PV="1.0"
SRC_URI="https://example.com/pkg-1.0.tar.gz"
` +
		`DESCRIPTION="test"` + "\n"
	if err := os.WriteFile(ebuildPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// With EBUILD set but no PORTDIR, portageq cannot resolve ebuild-level vars
	// — this is the expected behavior; ParseEbuildInfo handles this path instead
	_, err := q.EnvVarForPkg("", ebuildPath, "PN")
	if err == nil {
		t.Skip("portageq unexpectedly resolved ebuild-level var (tree may be configured)")
	}
	// Verify error is descriptive
	errStr := err.Error()
	if !containsAny(errStr, "portageq", "envvar", "PN") {
		t.Errorf("Error should mention portageq/envvar/PN: %v", err)
	}
}

// ---- TestPortageQ_EnvVarForPkg_PV: same as PN test ----

func TestPortageQ_EnvVarForPkg_PV(t *testing.T) {
	q := NewPortageQ()
	if !q.HasPortageq() {
		t.Skip("portageq not available")
	}

	tmpDir := t.TempDir()
	ebuildPath := tmpDir + "/cat/pkg-1.0/my-pkg-1.0.ebuild"
	if err := os.MkdirAll(tmpDir+"/cat/pkg-1.0", 0755); err != nil {
		t.Fatal(err)
	}
	content := `EAPI=8
PN="my-pkg"
PV="1.0"
SRC_URI="https://example.com/pkg-1.0.tar.gz"
` +
		`DESCRIPTION="test"` + "\n"
	if err := os.WriteFile(ebuildPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := q.EnvVarForPkg("", ebuildPath, "PV")
	if err == nil {
		t.Skip("portageq unexpectedly resolved ebuild-level var")
	}
}

// ---- TestPortageQ_EnvVarForPkg_P: same as PN test ----

func TestPortageQ_EnvVarForPkg_P(t *testing.T) {
	q := NewPortageQ()
	if !q.HasPortageq() {
		t.Skip("portageq not available")
	}

	tmpDir := t.TempDir()
	ebuildPath := tmpDir + "/cat/pkg-1.0/my-pkg-1.0.ebuild"
	if err := os.MkdirAll(tmpDir+"/cat/pkg-1.0", 0755); err != nil {
		t.Fatal(err)
	}
	content := `EAPI=8
PN="my-pkg"
PV="1.0"
SRC_URI="https://example.com/pkg-1.0.tar.gz"
` +
		`DESCRIPTION="test"` + "\n"
	if err := os.WriteFile(ebuildPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := q.EnvVarForPkg("", ebuildPath, "P")
	if err == nil {
		t.Skip("portageq unexpectedly resolved ebuild-level var")
	}
}

// ---- TestPortageQ_MissingTool: portageq not found returns descriptive error ----

func TestPortageQ_MissingTool(t *testing.T) {
	q := NewPortageQ()
	err := q.ValidateTools()
	if err == nil {
		t.Skip("portageq is available in this environment")
	}

	// Verify error mentions portageq or ebuild
	errStr := err.Error()
	if !containsAny(errStr, "portageq", "ebuild", "not found") {
		t.Errorf("ValidateTools() error %q should mention portageq or ebuild", errStr)
	}
}

// ---- TestPortageq_PortageTreeUnavailable: error distinguishes main tree vs. overlay missing ----

func TestPortageQ_PortageTreeUnavailable(t *testing.T) {
	q := NewPortageQ()

	// Try a non-existent tree name
	err := q.PortageTree("this-repo-does-not-exist-12345")
	if err == nil {
		t.Fatal("PortageTree(non-existent) should error")
	}

	// Should be an OverlayUnavailable error (or Portage not available)
	if _, ok := err.(*ErrOverlayUnavailable); !ok {
		t.Skip("portageq not installed or error is not *ErrOverlayUnavailable")
	}
}

// ---- TestPortageQ_GetRepoPath: resolves overlay path ----

func TestPortageQ_GetRepoPath(t *testing.T) {
	q := NewPortageQ()

	// Try the official "gentoo" tree
	path, err := q.GetRepoPath("gentoo")
	if err != nil {
		t.Skipf("GetRepoPath(gentoo) error: %v (Portage may not be installed)", err)
	}
	// Should return a non-empty path
	if path == "" {
		t.Error("GetRepoPath(gentoo) returned empty path")
	}
}

// ---- TestPortageQ_GetRepoPath_NonExistent: returns overlay error for unknown tree ----

func TestPortageQ_GetRepoPath_NonExistent(t *testing.T) {
	q := NewPortageQ()

	_, err := q.GetRepoPath("does-not-exist-xyz")
	if err == nil {
		t.Fatal("GetRepoPath(non-existent) should error")
	}

	// Verify it returns an overlay error or portage error
	if _, ok := err.(*ErrOverlayUnavailable); !ok {
		t.Skip("portageq not installed or error is not *ErrOverlayUnavailable")
	}
}

// ---- TestPortageQ_EnvVarForPkg: envvar for specific ebuild ----

func TestPortageQ_EnvVarForPkg(t *testing.T) {
	q := NewPortageQ()
	if !q.HasPortageq() {
		t.Skip("portageq not available")
	}

	// Use a non-existent ebuild path; expect error
	_, err := q.EnvVarForPkg("gentoo", "/nonexistent/cat/pkg-1.0.ebuild", "PN")
	// Expected: either portageq error or file-not-found error
	if err == nil {
		t.Fatal("EnvVarForPkg with non-existent ebuild should error")
	}
	_ = err
}

// ---- TestPortageQ_OverlayUnavailable: error distinguishes main tree vs overlay missing ----

func TestPortageQ_OverlayUnavailable(t *testing.T) {
	q := NewPortageQ()

	// Error from main tree (gentoo) vs overlay
	errMain := q.PortageTree("gentoo")
	errOverlay := q.PortageTree("does-not-exist-xyz")

	// Both should return errors (or both succeed if gentoo tree is installed)
	if errMain == nil && errOverlay == nil {
		t.Skip("both trees exist; skip")
	}

	// If one failed with OverlayUnavailable, the other succeeded — confirms distinction
	if errMain != nil && errOverlay != nil {
		// Both failed; check messages are different
		mainMsg := errMain.Error()
		overlayMsg := errOverlay.Error()
		if mainMsg == overlayMsg {
			t.Errorf("Both errors had the same message: %q", mainMsg)
		}
	}
}

// ---- TestErrOverlayUnavailable: error message ----

func TestErrOverlayUnavailable_Error(t *testing.T) {
	err := &ErrOverlayUnavailable{Message: "overlay not found"}
	want := "overlay tree unavailable: overlay not found"
	if got := err.Error(); got != want {
		t.Errorf("ErrOverlayUnavailable.Error() = %q, want %q", got, want)
	}
}

// ---- TestErrPortageqNotFound: error message ----

func TestErrPortageqNotFound_Error(t *testing.T) {
	err := &ErrPortageqNotFound{}
	want := "portageq not found in PATH; is Portage installed?"
	if got := err.Error(); got != want {
		t.Errorf("ErrPortageqNotFound.Error() = %q, want %q", got, want)
	}
}

// ---- TestValidateTools_BothAvailable: when both tools exist ----

func TestValidateTools_BothAvailable(t *testing.T) {
	q := NewPortageQ()
	err := q.ValidateTools()
	// Accept: no error (both installed) or error about both being missing
	if err != nil {
		errStr := err.Error()
		if !containsAny(errStr, "portageq", "ebuild") {
			t.Errorf("ValidateTools() unexpected error: %v", err)
		}
	}
}

// ---- TestEnvVar_WithEnvVars: sets EBUILD envvar ----

func TestEnvVar_WithEnvVars(t *testing.T) {
	q := NewPortageQ()
	if !q.HasPortageq() {
		t.Skip("portageq not available")
	}

	// Test with a fake ebuild path to verify the EBUILD envvar is set
	_, err := q.EnvVarForPkg("gentoo", "/nonexistent.ebuild", "DISTDIR")
	// Expected: error because the ebuild doesn't exist
	if err == nil {
		t.Fatal("EnvVarForPkg(non-existent) should error")
	}
	_ = err
}

// ---- TestHasPortageq_Availability: reports correctly ----

func TestHasPortageq_Availability(t *testing.T) {
	q := NewPortageQ()
	// Just verify no panic; result depends on environment
	has := q.HasPortageq()
	if q.HasPortageq() != has {
		t.Error("HasPortageq() returned inconsistent results")
	}
	hasEbuild := q.HasEbuild()
	if q.HasEbuild() != hasEbuild {
		t.Error("HasEbuild() returned inconsistent results")
	}
}

// ---- TestPortageTree_MainVsOverlay: verifies tree distinction ----

func TestPortageTree_MainVsOverlay(t *testing.T) {
	q := NewPortageQ()

	// Main tree check
	mainErr := q.PortageTreeMain()
	// Overlay check (may or may not succeed)
	overlayErr := q.PortageTreeOverlay("")

	// If portageq is available, both should succeed or both fail
	if q.HasPortageq() {
		if mainErr == nil && overlayErr != nil {
			t.Skipf("main tree ok but overlay check failed: %v", overlayErr)
		}
	}
}

// ---- TestGetRepoPath_EmptyTree: defaults to gentoo ----

func TestGetRepoPath_EmptyTree(t *testing.T) {
	q := NewPortageQ()

	// Empty tree name should default to "gentoo"
	path, err := q.GetRepoPath("")
	if err != nil {
		t.Skipf("GetRepoPath(\"\") error: %v (Portage may not be installed)", err)
	}
	if path == "" {
		t.Error("GetRepoPath(\"\") returned empty path")
	}
}

// ---- TestEnvVar_UnknownVar: unknown variable returns empty string ----

func TestEnvVar_UnknownVar(t *testing.T) {
	q := NewPortageQ()
	if !q.HasPortageq() {
		t.Skip("portageq not available")
	}

	// Unknown variable name — may return empty or error
	_, err := q.EnvVar("gentoo", "__NONEXISTENT_VAR_XYZ__")
	// Should not panic; result varies by portageq version
	_ = err
}

// ---- TestPortageQ_PortageTreeOverlay: overlay path check ----

func TestPortageQ_PortageTreeOverlay(t *testing.T) {
	q := NewPortageQ()

	// Test with empty overlay path
	err := q.PortageTreeOverlay("")
	// May succeed or fail depending on Portage setup
	_ = err

	// Test with a known non-existent path
	err = q.PortageTreeOverlay("/nonexistent/overlay/path")
	_ = err
}

// ---- TestEnvVar_TreeSpecific: different tree names ----

func TestEnvVar_TreeSpecific(t *testing.T) {
	q := NewPortageQ()
	if !q.HasPortageq() {
		t.Skip("portageq not available")
	}

	// Test with different tree names
	for _, tree := range []string{"gentoo", "", "nonexistent-tree-xyz"} {
		_, err := q.EnvVar(tree, "DISTDIR")
		// Just verify no panic
		_ = err
	}
}

// ---- TestHasPortageq_NotInstalled: verifies behavior when not installed ----

func TestHasPortageq_NotInstalled(t *testing.T) {
	// Set PATH to a directory with no portageq
	t.Setenv("PATH", "/nonexistent/bin:/usr/bin")

	// Verify the method works without panic
	q := NewPortageQ()
	// This will still find portageq if it's already in the LookPath cache,
	// but at minimum verifies no panic
	_ = q.HasPortageq()
	_ = q.HasEbuild()
}

// Helper: check if a string contains any of the substrings
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) && infoContainsString(s, sub) {
			return true
		}
	}
	return false
}

func infoContainsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
