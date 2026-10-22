package prepare

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/output"
)

// ---------------------------------------------------------------------------
// Custom prepare script: detection and execution
// ---------------------------------------------------------------------------

func TestPrepare_CustomScript_Detected(t *testing.T) {
	// Stub ManifestRunner so the manifest call doesn't fail.
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "example-1.0.ebuild")
	larryDir := filepath.Join(tmpDir, ".larry")
	if err := os.MkdirAll(larryDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a dummy ebuild
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create custom prepare script
	scriptPath := filepath.Join(larryDir, "prepare.sh")
	shebang := "#!/bin/sh\necho custom\nexit 0\n"
	if err := os.WriteFile(scriptPath, []byte(shebang), 0755); err != nil {
		t.Fatal(err)
	}

	ei := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "test-cat",
		Pkg:      "example",
		PVVal:    "1.0",
		PVal:     "example-1.0",
	}
	cfg := &PrepareConfig{
		Distdir: tmpDir,
	}

	err := Prepare(ei, cfg)
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
}

func TestPrepare_CustomScript_Skipped(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "example-1.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ei := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "test-cat",
		Pkg:      "example",
		PVVal:    "1.0",
		PVal:     "example-1.0",
	}
	cfg := &PrepareConfig{
		Distdir: tmpDir,
	}

	// No .larry/prepare.sh — should fall through to generic pipeline
	err := Prepare(ei, cfg)
	// Generic pipeline returns nil for dry-run or no mutations needed
	// The important thing is it doesn't error about missing custom script
	if err != nil {
		// If it fails, it should NOT be about missing custom script
		if strings.Contains(err.Error(), "custom prepare") {
			t.Fatalf("unexpected custom prepare error: %v", err)
		}
	}
}

func TestPrepare_CustomScript_DryRun(t *testing.T) {
	// Stub ManifestRunner (not needed in dry-run but safe).
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "example-1.0.ebuild")
	larryDir := filepath.Join(tmpDir, ".larry")
	if err := os.MkdirAll(larryDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create script that would modify the ebuild
	scriptPath := filepath.Join(larryDir, "prepare.sh")
	shebang := "#!/bin/sh\nsed -i 's/EAPI=8/EAPI=9/' \"$EBUILD\"\n"
	if err := os.WriteFile(scriptPath, []byte(shebang), 0755); err != nil {
		t.Fatal(err)
	}

	ei := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "test-cat",
		Pkg:      "example",
		PVVal:    "1.0",
		PVal:     "example-1.0",
	}
	cfg := &PrepareConfig{
		DryRun:  true,
		Distdir: tmpDir,
	}

	originalContent, _ := os.ReadFile(ebuildPath)

	err := Prepare(ei, cfg)
	if err != nil {
		t.Fatalf("Prepare dry-run returned error: %v", err)
	}

	// Verify ebuild was NOT modified
	modified, _ := os.ReadFile(ebuildPath)
	if string(originalContent) != string(modified) {
		t.Errorf("Dry-run modified the ebuild: %s -> %s", originalContent, modified)
	}
}

func TestPrepare_CustomScript_Failure(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "example-1.0.ebuild")
	larryDir := filepath.Join(tmpDir, ".larry")
	if err := os.MkdirAll(larryDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create script that fails
	scriptPath := filepath.Join(larryDir, "prepare.sh")
	shebang := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(shebang), 0755); err != nil {
		t.Fatal(err)
	}

	ei := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "test-cat",
		Pkg:      "example",
		PVVal:    "1.0",
		PVal:     "example-1.0",
	}
	cfg := &PrepareConfig{
		Distdir: tmpDir,
	}

	err := Prepare(ei, cfg)
	if err == nil {
		t.Fatal("expected error from failed custom script")
	}
	if !strings.Contains(err.Error(), "custom prepare script") {
		t.Errorf("expected custom prepare error, got: %v", err)
	}
	// No .bak should remain after rollback.
	if _, err := os.Stat(ebuildPath + ".bak"); !os.IsNotExist(err) {
		t.Error(".bak should not remain after rollback")
	}
}

func TestPrepare_CustomScript_EnvVars(t *testing.T) {
	// Stub ManifestRunner so the manifest call doesn't fail.
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "example-1.0.ebuild")
	larryDir := filepath.Join(tmpDir, ".larry")
	if err := os.MkdirAll(larryDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create script that writes env vars to a file
	envFile := filepath.Join(tmpDir, "env.out")
	scriptPath := filepath.Join(larryDir, "prepare.sh")
	script := "#!/bin/sh\n"
	script += "for v in EBUILD PN PV P CATEGORY DISTDIR LARRY_PREPARE; do\n"
	script += "  echo \"$v=\"${!v}\"\" >> " + envFile + "\n"
	script += "done\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	ei := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "sys-kernel",
		Pkg:      "rpi-kernel",
		PVVal:    "6.1.0_p20230915",
		PVal:     "rpi-kernel-6.1.0_p20230915",
	}
	cfg := &PrepareConfig{
		Distdir: "/var/cache/distfiles",
	}

	err := Prepare(ei, cfg)
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}

	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("Failed to read env output: %v", err)
	}

	checks := map[string]string{
		"EBUILD=":      ebuildPath,
		"PN=rpi-kernel": "",
		"PV=6.1.0_p20230915": "",
		"P=rpi-kernel-6.1.0_p20230915": "",
		"CATEGORY=sys-kernel": "",
		"DISTDIR=/var/cache/distfiles": "",
		"LARRY_PREPARE=1": "",
	}

	for key, expected := range checks {
		if expected == "" {
			// Just check the key exists
			if !strings.Contains(string(content), key) {
				t.Errorf("Environment variable %s not found in script output", key)
			}
		} else if !strings.Contains(string(content), expected) {
			t.Errorf("Expected output to contain %q, got:\n%s", expected, content)
		}
	}
}

// ---------------------------------------------------------------------------
// Helper: ensure we don't break existing pipeline dispatch
// ---------------------------------------------------------------------------

func TestPrepare_GenericNoCustomScript(t *testing.T) {
	// Stub ManifestRunner so generic pipeline doesn't fail.
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "example-1.0.ebuild")

	// Create ebuild with no files/ dir
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n\nDESCRIPTION=\"test\"\nSRC_URI=\"https://example.com/test-1.0.tar.gz\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ei := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "test-cat",
		Pkg:      "example",
		PVVal:    "1.0",
		PVal:     "example-1.0",
		SRC_URI:  "https://example.com/test-1.0.tar.gz",
		HOMEPAGE: "https://example.com",
	}
	cfg := &PrepareConfig{
		Distdir: tmpDir,
	}

	// Should fall through to generic pipeline (which runs manifest)
	err := Prepare(ei, cfg)
	// Generic pipeline runs manifest which may fail if pkgdev isn't available,
	// but it should NOT error about custom prepare
	if err != nil {
		if strings.Contains(err.Error(), "custom prepare") {
			t.Fatalf("unexpected custom prepare error: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Helper: runCustomPrepare direct tests
// ---------------------------------------------------------------------------

func TestRunCustomPrepare_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "test-1.0.ebuild")
	larryDir := filepath.Join(tmpDir, ".larry")
	if err := os.MkdirAll(larryDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	scriptPath := filepath.Join(larryDir, "prepare.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho ran\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Stub ManifestRunner so dry-run + manifest call doesn't fail.
	rec := newRecorder()
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		rec.record(bin, args, dir, env)
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	ei := &ebuild.EbuildInfo{
		Path: ebuildPath,
		Pkg:  "test",
		PVVal: "1.0",
		PVal: "test-1.0",
	}
	cfg := &PrepareConfig{
		DryRun:  true,
		Distdir: tmpDir,
	}

	// Should not execute the script in dry-run mode
	err := runCustomPrepare(ei, scriptPath, cfg)
	if err != nil {
		t.Fatalf("runCustomPrepare dry-run returned error: %v", err)
	}
	// ManifestRunner should NOT have been called in dry-run.
	if rec.called() {
		t.Error("ManifestRunner should not be called in dry-run mode")
	}
}

func TestRunCustomPrepare_Success(t *testing.T) {
	// Proper dir structure: <tmpdir>/test/test-1.0.ebuild
	pkgDir := filepath.Join(t.TempDir(), "test")
	ebuildPath := filepath.Join(pkgDir, "test-1.0.ebuild")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	larryDir := filepath.Join(pkgDir, "files")
	if err := os.MkdirAll(larryDir, 0755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(larryDir, "prepare.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Stub ManifestRunner so the manifest call doesn't try a real tool.
	rec := newRecorder()
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		rec.record(bin, args, dir, env)
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	ei := &ebuild.EbuildInfo{
		Path: ebuildPath,
		Pkg:  "test",
		PVVal: "1.0",
		PVal: "test-1.0",
	}
	cfg := &PrepareConfig{
		Distdir: t.TempDir(),
	}

	err := runCustomPrepare(ei, scriptPath, cfg)
	if err != nil {
		t.Fatalf("runCustomPrepare returned error: %v", err)
	}
	// Manifest should have been called.
	if !rec.called() {
		t.Error("ManifestRunner should have been called")
	}
}

func TestRunCustomPrepare_Failure(t *testing.T) {
	// Proper dir structure: <tmpdir>/test/test-1.0.ebuild
	pkgDir := filepath.Join(t.TempDir(), "test")
	ebuildPath := filepath.Join(pkgDir, "test-1.0.ebuild")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}
	larryDir := filepath.Join(pkgDir, "files")
	if err := os.MkdirAll(larryDir, 0755); err != nil {
		t.Fatal(err)
	}

	scriptPath := filepath.Join(larryDir, "prepare.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 42\n"), 0755); err != nil {
		t.Fatal(err)
	}

	ei := &ebuild.EbuildInfo{
		Path: ebuildPath,
		Pkg:  "test",
		PVVal: "1.0",
		PVal: "test-1.0",
	}
	cfg := &PrepareConfig{
		Distdir: t.TempDir(),
	}

	err := runCustomPrepare(ei, scriptPath, cfg)
	if err == nil {
		t.Fatal("expected error from failed script")
	}
	if !strings.Contains(err.Error(), "custom prepare script") {
		t.Errorf("expected 'custom prepare script' in error, got: %v", err)
	}
	// No .bak should remain after rollback.
	if _, err := os.Stat(ebuildPath + ".bak"); !os.IsNotExist(err) {
		t.Error(".bak file should not remain after rollback")
	}
}

func TestRunCustomPrepare_ScriptNotFound(t *testing.T) {
	pkgDir := filepath.Join(t.TempDir(), "test")
	ebuildPath := filepath.Join(pkgDir, "test-1.0.ebuild")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ei := &ebuild.EbuildInfo{
		Path: ebuildPath,
		Pkg:  "test",
		PVVal: "1.0",
		PVal: "test-1.0",
	}
	cfg := &PrepareConfig{
		Distdir: t.TempDir(),
	}

	// Script doesn't exist — exec.Command will fail.
	err := runCustomPrepare(ei, "/nonexistent/prepare.sh", cfg)
	if err == nil {
		t.Fatal("expected error for non-existent script")
	}
}

func TestRunCustomPrepare_UsesCmdDir(t *testing.T) {
	// Proper dir structure: <tmpdir>/test/test-1.0.ebuild
	pkgDir := filepath.Join(t.TempDir(), "test")
	ebuildPath := filepath.Join(pkgDir, "test-1.0.ebuild")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create script that writes CWD to a file
	marker := filepath.Join(pkgDir, "marker.txt")
	if err := os.MkdirAll(filepath.Join(pkgDir, ".larry"), 0755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(pkgDir, ".larry", "prepare.sh")
	script := "#!/bin/sh\npwd > " + marker + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	// Stub ManifestRunner so the manifest call doesn't fail.
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	ei := &ebuild.EbuildInfo{
		Path: ebuildPath,
		Pkg:  "test",
		PVVal: "1.0",
		PVal: "test-1.0",
	}
	cfg := &PrepareConfig{
		Distdir: t.TempDir(),
	}

	err := runCustomPrepare(ei, scriptPath, cfg)
	if err != nil {
		t.Fatalf("runCustomPrepare returned error: %v", err)
	}

	cwd, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("Failed to read marker: %v", err)
	}

	// Script should have run with Dir = filepath.Dir(ebuildPath) = pkgDir
	expectedCwd := pkgDir + "\n"
	if string(cwd) != expectedCwd {
		t.Errorf("Script ran with wrong dir: got %q, want %q", string(cwd), expectedCwd)
	}
}

func TestRunCustomPrepare_TmpdirAliveDuringScript(t *testing.T) {
	// The sandbox TMPDIR must exist for the script's whole execution (mktemp
	// uses it) and be cleaned up only after the script AND the manifest step
	// complete.
	pkgDir := filepath.Join(t.TempDir(), "test")
	ebuildPath := filepath.Join(pkgDir, "test-1.0.ebuild")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// mktemp fails when TMPDIR points at a deleted directory, so the script
	// doubles as a TMPDIR-liveness probe. It records the created file so the
	// test can verify cleanup afterward.
	tmpdirFile := filepath.Join(pkgDir, "tmpdir.out")
	scriptPath := filepath.Join(pkgDir, ".larry", "prepare.sh")
	script := "#!/bin/sh\nmktemp > " + tmpdirFile + "\n"
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	// Stub ManifestRunner so the manifest step runs after the script.
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	ei := &ebuild.EbuildInfo{
		Path:  ebuildPath,
		Pkg:   "test",
		PVVal: "1.0",
		PVal:  "test-1.0",
	}
	cfg := &PrepareConfig{
		Distdir: t.TempDir(),
	}

	err := runCustomPrepare(ei, scriptPath, cfg)
	if err != nil {
		t.Fatalf("runCustomPrepare: %v (mktemp must succeed — the sandbox TMPDIR must exist while the script runs)", err)
	}

	data, err := os.ReadFile(tmpdirFile)
	if err != nil {
		t.Fatalf("read tmpdir.out: %v", err)
	}
	scriptTmp := strings.TrimSpace(string(data))
	if scriptTmp == "" {
		t.Fatal("script did not record a temp file path")
	}
	// The file mktemp created lives under the sandbox TMPDIR — after
	// runCustomPrepare returns, the whole sandbox dir must be gone.
	if _, err := os.Stat(filepath.Dir(scriptTmp)); err == nil {
		t.Errorf("sandbox temp dir %s still exists after runCustomPrepare", filepath.Dir(scriptTmp))
	}
}

func TestRunCustomPrepare_SandboxWarningSuppressible(t *testing.T) {
	// Proper dir structure: <tmpdir>/test/test-1.0.ebuild
	pkgDir := filepath.Join(t.TempDir(), "test")
	ebuildPath := filepath.Join(pkgDir, "test-1.0.ebuild")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pkgDir, ".larry"), 0755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(pkgDir, ".larry", "prepare.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Stub ManifestRunner so the manifest call doesn't try pkgdev.
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

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
				Pkg:  "test",
				PVVal: "1.0",
				PVal: "test-1.0",
			}
			cfg := &PrepareConfig{
				Distdir: t.TempDir(),
				Log:     lgr,
			}

			err = runCustomPrepare(ei, scriptPath, cfg)
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

// ---------------------------------------------------------------------------
// sandboxEnv — userspace sandbox environment sanitization
// ---------------------------------------------------------------------------

func TestSandboxEnv_Sanitizes(t *testing.T) {
	// sandboxEnv must strip dangerous vars (BASH_ENV, GREP_OPTIONS), restrict
	// PATH to system dirs, and add the larry-specific entries.
	t.Setenv("BASH_ENV", "/evil/bash-env.sh")
	t.Setenv("GREP_OPTIONS", "--evil")
	t.Setenv("PATH", "/home/user/.local/bin:/usr/bin")

	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "dev-util", "foo", "foo-1.0.ebuild")
	ei := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "dev-util",
		Pkg:      "foo",
		PVVal:    "1.0",
		PVal:     "foo-1.0",
	}
	cfg := &PrepareConfig{Distdir: "/dist/dir"}

	sandboxTmp := filepath.Join(t.TempDir(), "larry-prepare-x")
	if err := os.MkdirAll(sandboxTmp, 0755); err != nil {
		t.Fatal(err)
	}
	env := sandboxEnv(ei, cfg, sandboxTmp)

	for _, e := range env {
		if strings.HasPrefix(e, "BASH_ENV=") {
			t.Errorf("env contains BASH_ENV: %q", e)
		}
		if strings.HasPrefix(e, "GREP_OPTIONS=") {
			t.Errorf("env contains GREP_OPTIONS: %q", e)
		}
	}

	// PATH is restricted to the system dirs, not inherited.
	wantPath := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	var gotPath string
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			gotPath = strings.TrimPrefix(e, "PATH=")
		}
	}
	if gotPath != wantPath {
		t.Errorf("PATH = %q, want %q", gotPath, wantPath)
	}

	// Larry-specific entries are present.
	for _, want := range []string{
		"EBUILD=" + ebuildPath,
		"DISTDIR=/dist/dir",
		"LARRY_SANDBOX_WRITE=" + filepath.Dir(ebuildPath) + ":/dist/dir",
		"LARRY_SANDBOX_READ=/",
		"TMPDIR=" + sandboxTmp,
		"TEMP=" + sandboxTmp,
	} {
		found := false
		for _, e := range env {
			if e == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("env missing %q; got %v", want, env)
		}
	}
}
