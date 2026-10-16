package prepare

import (
	"crypto/sha512"
	"fmt"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/output"
)

// ---------------------------------------------------------------------------
// Plan 25a: runCustomPrepare — backup, manifest, restore-on-failure
// ---------------------------------------------------------------------------

// fixtureCustomPrepare sets up the shared fixture for custom prepare tests:
//   - <tmpdir>/foo/foo-1.0.0.ebuild containing EAPI=8\nCOMMIT="aaaa"\n
//   - <tmpdir>/foo/.larry/prepare.sh (mode 0o755)
//   - stubs ManifestRunner to record (bin, args, dir, env) calls.
// Returns (ebuildPath, pkgDir, stubRecorder, cleanup).
func fixtureCustomPrepare(t *testing.T, scriptBody string) (ebuildPath, pkgDir string, rec *callRecorder, cleanup func()) {
	t.Helper()
	pkgDir = filepath.Join(t.TempDir(), "foo")
	ebuildPath = filepath.Join(pkgDir, "foo-1.0.0.ebuild")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\nCOMMIT=\"aaaa\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	larryDir := filepath.Join(pkgDir, ".larry")
	if err := os.MkdirAll(larryDir, 0755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(larryDir, "prepare.sh")
	if err := os.WriteFile(scriptPath, []byte(scriptBody), 0755); err != nil {
		t.Fatal(err)
	}

	rec = newRecorder()
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		rec.record(bin, args, dir, env)
		// Write a valid Manifest so verifyManifestValid passes.
		manifestPath := filepath.Join(pkgDir, "Manifest")
		_ = os.WriteFile(manifestPath, []byte(
			"DIST foo-1.0.0.tar.gz 10 BLAKE2B aa SHA512 bb\n",
		), 0644) // best-effort; goroutine callback may outlive the test
		return nil
	}

	return ebuildPath, pkgDir, rec, func() { ManifestRunner = orig }
}

// TestCustomPrepare_RunsManifest verifies that runCustomPrepare runs the
// manifest after the script succeeds, passing the ebuild path (not cat/pkg).
// This avoids fetching distfiles for sibling ebuilds.
func TestCustomPrepare_RunsManifest(t *testing.T) {
	ebuildPath, _, rec, cleanup := fixtureCustomPrepare(t, "#!/bin/sh\nexit 0\n")
	defer cleanup()

	// Use the fixture's ebuild directly (already in proper dir structure).
	// Stub ManifestRunner to record calls and return success.
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		rec.record(bin, args, dir, env)
		// Write valid Manifest.
		_ = os.WriteFile(filepath.Join(filepath.Dir(ebuildPath), "Manifest"), []byte(
			"DIST foo-1.0.0.tar.gz 10 BLAKE2B aa SHA512 bb\n",
		), 0644) // best-effort; goroutine callback may outlive the test
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	// Remove the Manifest from the fixture's stub since our override will
	// write it fresh.
	_ = os.Remove(filepath.Join(filepath.Dir(ebuildPath), "Manifest"))

	ei := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "dev-util",
		Pkg:      "foo",
		PVVal:    "1.0.0",
		PVal:     "foo-1.0.0",
	}
	cfg := &PrepareConfig{
		Distdir: t.TempDir(),
		Overlay: t.TempDir(),
	}

	err := runCustomPrepare(ei, filepath.Join(filepath.Dir(ebuildPath), ".larry", "prepare.sh"), cfg)
	if err != nil {
		t.Fatalf("runCustomPrepare: %v", err)
	}
	if !rec.called() {
		t.Fatal("ManifestRunner was not called")
	}
	if rec.calls[0].args[3] != ebuildPath {
		t.Errorf("target arg = %q, want %q", rec.calls[0].args[3], ebuildPath)
	}
}

// TestCustomPrepare_RestoresOnScriptFailure verifies that when the script
// fails, the ebuild is restored to its original content.
func TestCustomPrepare_RestoresOnScriptFailure(t *testing.T) {
	ebuildPath, _, rec, cleanup := fixtureCustomPrepare(t,
		"#!/bin/sh\nsed -i 's/aaaa/bbbb/' foo-1.0.0.ebuild\nexit 1\n")
	defer cleanup()

	ei := &ebuild.EbuildInfo{
		Path:  ebuildPath,
		Pkg:   "foo",
		PVVal: "1.0.0",
		PVal:  "foo-1.0.0",
	}
	cfg := &PrepareConfig{
		Distdir: t.TempDir(),
	}

	err := runCustomPrepare(ei, filepath.Join(filepath.Dir(ebuildPath), ".larry", "prepare.sh"), cfg)
	if err == nil {
		t.Fatal("expected error from failed script")
	}
	// Ebuild should be restored to original.
	data, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("read ebuild: %v", err)
	}
	if string(data) != "EAPI=8\nCOMMIT=\"aaaa\"\n" {
		t.Errorf("ebuild = %q, want %q (should be restored)", string(data), "EAPI=8\nCOMMIT=\"aaaa\"\n")
	}
	// Manifest stub should NOT have been called.
	if rec.called() {
		t.Error("ManifestRunner should not be called after script failure")
	}
	// No .bak should remain.
	if _, err := os.Stat(ebuildPath + ".bak"); !os.IsNotExist(err) {
		t.Error(".bak file should not remain after rollback")
	}
}

// TestCustomPrepare_RestoresOnManifestFailure verifies that when the manifest
// fails, the ebuild is restored.
func TestCustomPrepare_RestoresOnManifestFailure(t *testing.T) {
	// Proper dir structure: <tmpdir>/foo/foo-1.0.0.ebuild
	pkgDir := filepath.Join(t.TempDir(), "foo")
	ebuildPath := filepath.Join(pkgDir, "foo-1.0.0.ebuild")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\nCOMMIT=\"aaaa\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	larryDir := filepath.Join(pkgDir, ".larry")
	if err := os.MkdirAll(larryDir, 0755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(larryDir, "prepare.sh")
	// Script modifies ebuild in place and exits 0.
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nsed -i 's/aaaa/bbbb/' foo-1.0.0.ebuild\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Stub ManifestRunner to fail.
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		return errors.New("boom")
	}
	t.Cleanup(func() { ManifestRunner = orig })

	ei := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "dev-util",
		Pkg:      "foo",
		PVVal:    "1.0.0",
		PVal:     "foo-1.0.0",
	}
	cfg := &PrepareConfig{
		Distdir: t.TempDir(),
		Overlay: t.TempDir(),
	}

	err := runCustomPrepare(ei, scriptPath, cfg)
	if err == nil {
		t.Fatal("expected error from manifest failure")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %v, want to contain 'boom'", err)
	}
	// Ebuild should be restored to original.
	restored, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("read ebuild: %v", err)
	}
	if string(restored) != "EAPI=8\nCOMMIT=\"aaaa\"\n" {
		t.Errorf("ebuild = %q, want %q", string(restored), "EAPI=8\nCOMMIT=\"aaaa\"\n")
	}
	if _, err := os.Stat(ebuildPath + ".bak"); !os.IsNotExist(err) {
		t.Error(".bak should not remain after manifest rollback")
	}
}

// TestCustomPrepare_DryRunDoesNotExecute verifies that dry-run mode skips
// everything — no script execution, no manifest, no backup.
func TestCustomPrepare_DryRunDoesNotExecute(t *testing.T) {
	ebuildPath, _, _, cleanup := fixtureCustomPrepare(t,
		"#!/bin/sh\necho ran > ran.txt\nexit 0\n")
	defer cleanup()

	ei := &ebuild.EbuildInfo{
		Path: ebuildPath,
		Pkg:  "foo",
		PVVal: "1.0.0",
		PVal:  "foo-1.0.0",
	}
	cfg := &PrepareConfig{
		DryRun:  true,
		Distdir: t.TempDir(),
	}

	err := runCustomPrepare(ei, filepath.Join(filepath.Dir(ebuildPath), ".larry", "prepare.sh"), cfg)
	if err != nil {
		t.Fatalf("runCustomPrepare: %v", err)
	}
	// No ran.txt was created.
	if _, err := os.Stat(filepath.Join(filepath.Dir(ebuildPath), "ran.txt")); !os.IsNotExist(err) {
		t.Error("ran.txt should not exist in dry-run mode")
	}
	// No .bak.
	if _, err := os.Stat(ebuildPath + ".bak"); !os.IsNotExist(err) {
		t.Error(".bak should not exist in dry-run mode")
	}
}

// TestCustomPrepare_QuietSuppressesWarning verifies that the sandbox warning
// is suppressed when the logger's quiet mode is enabled.
func TestCustomPrepare_QuietSuppressesWarning(t *testing.T) {
	ebuildPath, _, _, cleanup := fixtureCustomPrepare(t, "#!/bin/sh\nexit 0\n")
	defer cleanup()

	// Use a logger with injectable stderr.
	for _, quiet := range []bool{false, true} {
		label := "false"
		if quiet {
			label = "true"
		}
		t.Run("quiet="+label, func(t *testing.T) {
			var buf strings.Builder
			lgr, err := output.NewLogger(0, quiet, nil)
			if err != nil {
				t.Fatalf("NewLogger: %v", err)
			}
			lgr.SetErr(&buf)

			ei := &ebuild.EbuildInfo{
				Path: ebuildPath,
				Pkg:  "foo",
				PVVal: "1.0.0",
				PVal:  "foo-1.0.0",
			}
			cfg := &PrepareConfig{
				Distdir: t.TempDir(),
				Log:     lgr,
			}

			err = runCustomPrepare(ei, filepath.Join(filepath.Dir(ebuildPath), ".larry", "prepare.sh"), cfg)
			if err != nil {
				t.Fatalf("runCustomPrepare: %v", err)
			}

			if !quiet {
				if !strings.Contains(buf.String(), customPrepareSandboxWarning) {
					t.Errorf("quiet=false: stderr = %q, want %q", buf.String(), customPrepareSandboxWarning)
				}
			} else {
				if strings.Contains(buf.String(), "partial sandboxing") {
					t.Errorf("quiet=true: stderr = %q, should NOT contain 'partial sandboxing'", buf.String())
				}
			}
		})
	}
}

// TestCustomPrepare_RelativePaths verifies that a custom prepare script
// resolves when the ebuild path (and thus the script path and cmd.Dir) are
// relative to larry's working directory. The child process chdirs into the
// package dir before exec, so a relative script path must be absolutized
// first or fork/exec fails with ENOENT.
func TestCustomPrepare_RelativePaths(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "foo")
	if err := os.MkdirAll(filepath.Join(pkgDir, ".larry"), 0755); err != nil {
		t.Fatal(err)
	}
	ebuildPath := filepath.Join(pkgDir, "foo-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, ".larry", "prepare.sh"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	rec := newRecorder()
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		rec.record(bin, args, dir, env)
		_ = os.WriteFile(filepath.Join(pkgDir, "Manifest"), []byte(
			"DIST foo-1.0.0.tar.gz 10 BLAKE2B aa SHA512 bb\n",
		), 0644)
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	// Run larry from the fixture root with relative paths, the way
	// `larry prepare foo` behaves when the CLI keeps the argument relative.
	t.Chdir(root)

	ei := &ebuild.EbuildInfo{
		Path:     filepath.Join("foo", "foo-1.0.0.ebuild"),
		Category: "dev-util",
		Pkg:      "foo",
		PVVal:    "1.0.0",
		PVal:     "foo-1.0.0",
	}
	cfg := &PrepareConfig{
		Distdir: filepath.Join(root, "dist"),
		Overlay: root,
	}

	err := runCustomPrepare(ei, filepath.Join("foo", ".larry", "prepare.sh"), cfg)
	if err != nil {
		t.Fatalf("runCustomPrepare with relative paths: %v", err)
	}
	if !rec.called() {
		t.Fatal("ManifestRunner was not called")
	}
}

// ---------------------------------------------------------------------------
// Stale Manifest detection after custom hooks
// ---------------------------------------------------------------------------

// TestManifestEntriesStale covers staleness detection of DIST entries
// against the files actually present in distdir.
func TestManifestEntriesStale(t *testing.T) {
	distDir := t.TempDir()
	data := []byte("distfile-bytes")
	distfile := filepath.Join(distDir, "foo-1.0.0.tar.gz")
	if err := os.WriteFile(distfile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	goodSum := sha512.Sum512(data)
	goodLine := fmt.Sprintf("DIST foo-1.0.0.tar.gz %d BLAKE2B aa SHA512 %x\n",
		len(data), goodSum)

	manifestPath := filepath.Join(t.TempDir(), "Manifest")
	writeManifest := func(content string) {
		t.Helper()
		if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name    string
		manifest string
		want    bool
	}{
		{"matching entry", goodLine, false},
		{"size mismatch", "DIST foo-1.0.0.tar.gz 10 BLAKE2B aa SHA512 " + fmt.Sprintf("%x", goodSum) + "\n", true},
		{"hash mismatch", fmt.Sprintf("DIST foo-1.0.0.tar.gz %d BLAKE2B aa SHA512 bb\n", len(data)), true},
		{"file missing from distdir", "DIST other-1.0.0.tar.gz 10 BLAKE2B aa SHA512 bb\n", false},
		{"non-DIST lines ignored", "AUX files.patch 10 BLAKE2B aa SHA512 bb\n" + goodLine, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeManifest(tt.manifest)
			if got := manifestEntriesStale(manifestPath, distDir); got != tt.want {
				t.Errorf("manifestEntriesStale = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("missing manifest", func(t *testing.T) {
		if got := manifestEntriesStale(filepath.Join(t.TempDir(), "Manifest"), distDir); got {
			t.Error("manifestEntriesStale = true for missing Manifest, want false")
		}
	})
}

// fixtureCustomPrepareOverlay builds a real overlay tree:
// <root>/dev-util/foo/{foo-1.0.0.ebuild, .larry/prepare.sh} so that the
// manifest step's overlay-relative package dir matches the fixture dir.
func fixtureCustomPrepareOverlay(t *testing.T, scriptBody string) (root, ebuildPath, pkgDir string) {
	t.Helper()
	root = t.TempDir()
	pkgDir = filepath.Join(root, "dev-util", "foo")
	if err := os.MkdirAll(filepath.Join(pkgDir, ".larry"), 0o755); err != nil {
		t.Fatal(err)
	}
	ebuildPath = filepath.Join(pkgDir, "foo-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\nCOMMIT=\"aaaa\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, ".larry", "prepare.sh"), []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, ebuildPath, pkgDir
}

// TestCustomPrepare_StaleManifestForcesFullRegen verifies that when a
// distfile no longer matches its Manifest entry — e.g. the hook
// regenerated it — the manifest step targets the whole cat/pkg so the
// Manifest is dropped and regenerated, instead of merging stale entries.
func TestCustomPrepare_StaleManifestForcesFullRegen(t *testing.T) {
	root, ebuildPath, pkgDir := fixtureCustomPrepareOverlay(t, "#!/bin/sh\nexit 0\n")

	distDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(distDir, "foo-1.0.0.tar.gz"),
		[]byte("regenerated-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stale entry: records a size and hash that do not match the file.
	if err := os.WriteFile(filepath.Join(pkgDir, "Manifest"),
		[]byte("DIST foo-1.0.0.tar.gz 10 BLAKE2B aa SHA512 bb\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotTarget string
	var manifestPresentAtCall bool
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		gotTarget = args[3]
		_, err := os.Stat(filepath.Join(pkgDir, "Manifest"))
		manifestPresentAtCall = !os.IsNotExist(err)
		_ = os.WriteFile(filepath.Join(pkgDir, "Manifest"),
			[]byte("DIST foo-1.0.0.tar.gz 18 BLAKE2B aa SHA512 bb\n"), 0o644)
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	ei := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "dev-util",
		Pkg:      "foo",
		PVVal:    "1.0.0",
		PVal:     "foo-1.0.0",
	}
	cfg := &PrepareConfig{Distdir: distDir, Overlay: root}

	if err := runCustomPrepare(ei, filepath.Join(pkgDir, ".larry", "prepare.sh"), cfg); err != nil {
		t.Fatalf("runCustomPrepare: %v", err)
	}
	if gotTarget != "dev-util/foo" {
		t.Errorf("manifest target = %q, want cat/pkg %q (full regeneration)", gotTarget, "dev-util/foo")
	}
	if manifestPresentAtCall {
		t.Error("Manifest still present when pkgdev ran; want it removed for full regeneration")
	}
}

// TestCustomPrepare_MatchingManifestKeepsEbuildTarget verifies the merge
// path is kept when recorded entries match the distdir files, so sibling
// ebuilds' distfiles are not fetched unnecessarily.
func TestCustomPrepare_MatchingManifestKeepsEbuildTarget(t *testing.T) {
	root, ebuildPath, pkgDir := fixtureCustomPrepareOverlay(t, "#!/bin/sh\nexit 0\n")

	distDir := t.TempDir()
	data := []byte("distfile-bytes")
	if err := os.WriteFile(filepath.Join(distDir, "foo-1.0.0.tar.gz"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha512.Sum512(data)
	manifestLine := fmt.Sprintf("DIST foo-1.0.0.tar.gz %d BLAKE2B aa SHA512 %x\n", len(data), sum)
	if err := os.WriteFile(filepath.Join(pkgDir, "Manifest"), []byte(manifestLine), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotTarget string
	var manifestPresentAtCall bool
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		gotTarget = args[3]
		_, err := os.Stat(filepath.Join(pkgDir, "Manifest"))
		manifestPresentAtCall = !os.IsNotExist(err)
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	ei := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "dev-util",
		Pkg:      "foo",
		PVVal:    "1.0.0",
		PVal:     "foo-1.0.0",
	}
	cfg := &PrepareConfig{Distdir: distDir, Overlay: root}

	if err := runCustomPrepare(ei, filepath.Join(pkgDir, ".larry", "prepare.sh"), cfg); err != nil {
		t.Fatalf("runCustomPrepare: %v", err)
	}
	if gotTarget != ebuildPath {
		t.Errorf("manifest target = %q, want ebuild path %q (merge mode)", gotTarget, ebuildPath)
	}
	if !manifestPresentAtCall {
		t.Error("Manifest removed before pkgdev ran; want merge mode to preserve it")
	}
}
