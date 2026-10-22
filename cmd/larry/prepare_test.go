package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/prepare"
)

func TestPrepareTypeValidation_Rejects(t *testing.T) {
	// --type with an unknown value should fail before any pipeline is invoked.
	overlay := fixtureOverlay(t, nil, nil)
	_, _, err := execCmd(t, "prepare", "--overlay", overlay, "--type", "npmm", "foo-1.0.ebuild")
	if err == nil {
		t.Fatal("expected error for --type npmm, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "unknown type \"npmm\"") {
		t.Errorf("err = %q, want substring %q", msg, "unknown type \"npmm\"")
	}
	if !strings.Contains(msg, "cargo") {
		t.Errorf("err = %q, want substring %q (valid types list)", msg, "cargo")
	}
}

func TestPrepareTypeValidation_Accepts(t *testing.T) {
	// --type generic --dry-run should not error on type validation.
	overlay := fixtureOverlay(t, nil, nil)
	_, _, err := execCmd(t, "prepare", "--overlay", overlay, "--type", "generic", "--dry-run", "foo-1.0.ebuild")
	// The path won't exist so it may error on the path resolution, but it
	// should NOT error on the type validation. The error (if any) should not
	// be about an unknown type.
	if err != nil {
		// Allow path-not-found errors; reject type-validation errors.
		if strings.Contains(err.Error(), "unknown type") {
			t.Errorf("unexpected type-validation error: %v", err)
		}
	}
}

func TestPrepareAll_InvokesBulkPkgdev(t *testing.T) {
	// D8 — --all with no args calls PrepareManifestAll, which calls ManifestRunner
	// for each cat/pkg found under the overlay root.
	overlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/foo/foo-1.0.ebuild": "EAPI=8\n",
		"sys-apps/bar/bar-2.0.ebuild": "EAPI=8\n",
	})
	dd := t.TempDir()

	orig := prepare.ManifestRunner
	calls := 0
	prepare.ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		calls++
		return nil
	}
	t.Cleanup(func() { prepare.ManifestRunner = orig })

	_, _, err := execCmd(t, "prepare", "--all", "--overlay", overlay, "--distdir", dd)
	if err != nil {
		t.Fatalf("execCmd prepare --all: %v", err)
	}
	// Should have called ManifestRunner for each cat/pkg (2 in fixtureOverlay).
	if calls != 2 {
		t.Errorf("ManifestRunner called %d times, want 2", calls)
	}
}

func TestPrepareDirArgument_RunsPipelineNotBulkManifest(t *testing.T) {
	// Regression guard for D8 fix — directory argument runs per-type pipeline, not bulk manifest.
	overlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/foo/foo-1.0.ebuild": "EAPI=8\n",
		"dev-util/foo/foo-2.0.ebuild": "EAPI=8\n",
	})

	pkgDir := filepath.Join(overlay, "dev-util", "foo")

	var pfCalls int
	pfOrig := prepare.PrepareFile
	prepare.PrepareFile = func(path string, cfg *prepare.PrepareConfig) error {
		pfCalls++
		return nil
	}
	t.Cleanup(func() { prepare.PrepareFile = pfOrig })

	var mrCalls int
	mrOrig := prepare.ManifestRunner
	prepare.ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		mrCalls++
		return nil
	}
	t.Cleanup(func() { prepare.ManifestRunner = mrOrig })

	// Call PrepareDir directly to avoid CLI overhead and test pollution.
	err := prepare.PrepareDir(pkgDir, &prepare.PrepareConfig{DryRun: true})
	if err != nil {
		t.Fatalf("PrepareDir: %v", err)
	}
	if pfCalls != 2 {
		t.Errorf("PrepareFile called %d times, want 2", pfCalls)
	}
	if mrCalls != 0 {
		t.Errorf("ManifestRunner called %d times, want 0", mrCalls)
	}
}

func TestPrepareMultipleTargets_AggregatesFailures(t *testing.T) {
	// D9 — three ebuild paths; second fails; all three attempted; returns "1 package(s) failed".
	overlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/foo/foo-1.0.ebuild": "EAPI=8\n",
		"dev-util/foo/foo-2.0.ebuild": "EAPI=8\n",
		"dev-util/foo/foo-3.0.ebuild": "EAPI=8\n",
	})
	dd := t.TempDir()

	targets := []string{}
	pfOrig := prepare.PrepareFile
	prepare.PrepareFile = func(path string, cfg *prepare.PrepareConfig) error {
		targets = append(targets, path)
		if len(targets) == 2 {
			return errors.New("prepare boom")
		}
		return nil
	}
	t.Cleanup(func() { prepare.PrepareFile = pfOrig })

	_, _, err := execCmd(t, "prepare",
		filepath.Join(overlay, "dev-util", "foo", "foo-1.0.ebuild"),
		filepath.Join(overlay, "dev-util", "foo", "foo-2.0.ebuild"),
		filepath.Join(overlay, "dev-util", "foo", "foo-3.0.ebuild"),
		"--overlay", overlay, "--distdir", dd)
	if err == nil {
		t.Fatal("expected error from aggregate failures")
	}
	if !strings.Contains(err.Error(), "1 package(s) failed") {
		t.Errorf("err = %v, want '1 package(s) failed'", err)
	}
	if len(targets) != 3 {
		t.Errorf("PrepareFile called %d times, want 3", len(targets))
	}
}

func TestPrepareMultipleTargets_PrintsPrepareFailure(t *testing.T) {
	// A failing target in a multi-target run must be named on stderr
	// (path + error), mirroring PrepareDir — counting failures alone hides
	// which package failed and why.
	overlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/foo/foo-1.0.ebuild": "EAPI=8\n",
		"dev-util/foo/foo-2.0.ebuild": "EAPI=8\n",
	})
	dd := t.TempDir()

	failing := filepath.Join(overlay, "dev-util", "foo", "foo-2.0.ebuild")
	pfOrig := prepare.PrepareFile
	prepare.PrepareFile = func(path string, cfg *prepare.PrepareConfig) error {
		if path == failing {
			return errors.New("prepare boom")
		}
		return nil
	}
	t.Cleanup(func() { prepare.PrepareFile = pfOrig })

	_, stderr, _ := execCmd(t, "prepare",
		filepath.Join(overlay, "dev-util", "foo", "foo-1.0.ebuild"),
		failing,
		"--overlay", overlay, "--distdir", dd)

	if !strings.Contains(stderr, failing) {
		t.Errorf("stderr = %q, want it to name the failing target %q", stderr, failing)
	}
	if !strings.Contains(stderr, "prepare boom") {
		t.Errorf("stderr = %q, want it to contain the failure reason", stderr)
	}
}

func TestPrepareMultipleTargets_PrintsResolveFailure(t *testing.T) {
	// Same contract for targets that fail path resolution before PrepareFile
	// even runs.
	overlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/foo/foo-1.0.ebuild": "EAPI=8\n",
	})
	dd := t.TempDir()

	_, stderr, _ := execCmd(t, "prepare",
		filepath.Join(overlay, "dev-util", "foo", "foo-1.0.ebuild"),
		"dev-util/missing/bar",
		"--overlay", overlay, "--distdir", dd)

	if !strings.Contains(stderr, "dev-util/missing/bar") {
		t.Errorf("stderr = %q, want it to name the unresolvable target", stderr)
	}
}

func TestResolvePrepareTarget_CategoryPackage(t *testing.T) {
	// D10, D11 — "sys-apps/foo" resolves to overlay/sys-apps/foo.
	overlay := fixtureOverlay(t, nil, map[string]string{
		"sys-apps/foo/foo-1.0.ebuild": "EAPI=8\n",
	})
	_, fi, err := resolvePrepareTarget("sys-apps/foo", overlay)
	if err != nil {
		t.Fatalf("resolvePrepareTarget(sys-apps/foo, ov) = %v", err)
	}
	want := filepath.Join(overlay, "sys-apps", "foo")
	if fi == nil {
		t.Fatal("FileInfo is nil")
	}
	if !fi.IsDir() {
		t.Error("expected IsDir() == true for package directory")
	}
	// Check that the path resolves to the package directory.
	_, base := filepath.Split(want)
	if !strings.HasSuffix(fi.Name(), base) {
		t.Errorf("Name() = %q, want to end with %q", fi.Name(), base)
	}
}

func TestResolvePrepareTarget_EbuildPath(t *testing.T) {
	overlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/foo/foo-1.0.ebuild": "EAPI=8\n",
	})
	eb := filepath.Join(overlay, "dev-util", "foo", "foo-1.0.ebuild")
	resolved, fi, err := resolvePrepareTarget(eb, overlay)
	if err != nil {
		t.Fatalf("resolvePrepareTarget(eb, ov) = %v", err)
	}
	if resolved != eb {
		t.Errorf("resolved = %q, want %q", resolved, eb)
	}
	if fi == nil {
		t.Fatal("FileInfo is nil")
	}
	if fi.IsDir() {
		t.Error("expected IsDir() == false for ebuild file")
	}
}

func TestResolvePrepareTarget_PackageDir(t *testing.T) {
	overlay := fixtureOverlay(t, nil, map[string]string{
		"sys-apps/foo/foo-1.0.ebuild": "EAPI=8\n",
	})
	pkgDir := filepath.Join(overlay, "sys-apps", "foo")
	resolved, fi, err := resolvePrepareTarget(pkgDir, overlay)
	if err != nil {
		t.Fatalf("resolvePrepareTarget(pkgDir, ov) = %v", err)
	}
	if resolved != pkgDir {
		t.Errorf("resolved = %q, want %q", resolved, pkgDir)
	}
	if fi == nil {
		t.Fatal("FileInfo is nil")
	}
	if !fi.IsDir() {
		t.Errorf("IsDir() = false, want true for package directory")
	}
}

func TestResolvePrepareTarget_Missing(t *testing.T) {
	overlay := fixtureOverlay(t, nil, nil)
	_, _, err := resolvePrepareTarget("dev-util/nope", overlay)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	msg := err.Error()
	if !strings.Contains(msg, "dev-util/nope") {
		t.Errorf("err = %q, want to contain 'dev-util/nope'", msg)
	}
	if !strings.Contains(msg, overlay) {
		t.Errorf("err = %q, want to contain overlay path %q", msg, overlay)
	}
}

// --- Helper to stub ManifestRunner for execCmd tests ---

func TestPrepare_DryRun_MakesNoDistdir(t *testing.T) {
	// Dry-run should not create distdir.
	overlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/foo/foo-1.0.ebuild": "EAPI=8\n",
	})
	dd := filepath.Join(t.TempDir(), "should-not-exist")
	t.Setenv("LARRY_DISTDIR", dd)

	_, _, err := execCmd(t, "prepare", "--dry-run", filepath.Join(overlay, "dev-util", "foo", "foo-1.0.ebuild"), "--overlay", overlay)
	if err != nil {
		t.Fatalf("execCmd prepare --dry-run: %v", err)
	}
	if _, err := os.Stat(dd); err == nil {
		t.Error("dry-run should not create distdir")
	}
}
