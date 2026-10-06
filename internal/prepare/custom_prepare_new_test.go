package prepare

import (
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
//   - <tmpdir>/foo/files/prepare.sh (mode 0o755)
//   - stubs ManifestRunner to record (bin, args, dir, env) calls.
// Returns (ebuildPath, pkgDir, stubRecorder, cleanup).
func fixtureCustomPrepare(t *testing.T, scriptBody string) (ebuildPath, pkgDir string, rec *callRecorder, cleanup func()) {
	t.Helper()
	pkgDir = filepath.Join(t.TempDir(), "foo")
	ebuildPath = filepath.Join(pkgDir, "foo-1.0.0.ebuild")
	os.MkdirAll(pkgDir, 0755)
	os.WriteFile(ebuildPath, []byte("EAPI=8\nCOMMIT=\"aaaa\"\n"), 0644)

	filesDir := filepath.Join(pkgDir, "files")
	os.MkdirAll(filesDir, 0755)
	scriptPath := filepath.Join(filesDir, "prepare.sh")
	os.WriteFile(scriptPath, []byte(scriptBody), 0755)

	rec = newRecorder()
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		rec.record(bin, args, dir, env)
		// Write a valid Manifest so verifyManifestValid passes.
		manifestPath := filepath.Join(pkgDir, "Manifest")
		os.WriteFile(manifestPath, []byte(
			"DIST foo-1.0.0.tar.gz 10 BLAKE2B aa SHA512 bb\n",
		), 0644)
		return nil
	}

	return ebuildPath, pkgDir, rec, func() { ManifestRunner = orig }
}

// TestCustomPrepare_RunsManifest verifies that runCustomPrepare runs the
// manifest after the script succeeds, with catPkg (not a file path).
func TestCustomPrepare_RunsManifest(t *testing.T) {
	ebuildPath, _, rec, cleanup := fixtureCustomPrepare(t, "#!/bin/sh\nexit 0\n")
	defer cleanup()

	// Use the fixture's ebuild directly (already in proper dir structure).
	// Stub ManifestRunner to record calls and return success.
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		rec.record(bin, args, dir, env)
		// Write valid Manifest.
		os.WriteFile(filepath.Join(filepath.Dir(ebuildPath), "Manifest"), []byte(
			"DIST foo-1.0.0.tar.gz 10 BLAKE2B aa SHA512 bb\n",
		), 0644)
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

	err := runCustomPrepare(ei, filepath.Join(filepath.Dir(ebuildPath), "files", "prepare.sh"), cfg)
	if err != nil {
		t.Fatalf("runCustomPrepare: %v", err)
	}
	if !rec.called() {
		t.Fatal("ManifestRunner was not called")
	}
	if rec.calls[0].args[3] != "dev-util/foo" {
		t.Errorf("catPkg arg = %q, want %q", rec.calls[0].args[3], "dev-util/foo")
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

	err := runCustomPrepare(ei, filepath.Join(filepath.Dir(ebuildPath), "files", "prepare.sh"), cfg)
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
	os.MkdirAll(pkgDir, 0755)
	os.WriteFile(ebuildPath, []byte("EAPI=8\nCOMMIT=\"aaaa\"\n"), 0644)
	filesDir := filepath.Join(pkgDir, "files")
	os.MkdirAll(filesDir, 0755)
	scriptPath := filepath.Join(filesDir, "prepare.sh")
	// Script modifies ebuild in place and exits 0.
	os.WriteFile(scriptPath, []byte("#!/bin/sh\nsed -i 's/aaaa/bbbb/' foo-1.0.0.ebuild\nexit 0\n"), 0755)

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

	err := runCustomPrepare(ei, filepath.Join(filepath.Dir(ebuildPath), "files", "prepare.sh"), cfg)
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

			err = runCustomPrepare(ei, filepath.Join(filepath.Dir(ebuildPath), "files", "prepare.sh"), cfg)
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
