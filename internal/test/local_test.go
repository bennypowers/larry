package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Phase resolution helpers
// ---------------------------------------------------------------------------

func TestResolveEbuild_ExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644)

	path, err := resolveEbuild(ebuildPath)
	if err != nil {
		t.Fatalf("resolveEbuild(%q) = %v", ebuildPath, err)
	}
	if path != ebuildPath {
		t.Errorf("resolveEbuild = %q, want %q", path, ebuildPath)
	}
}

func TestResolveEbuild_CatPkg(t *testing.T) {
	// When given a category/package string, find the latest ebuild
	tmpDir := t.TempDir()
	catDir := filepath.Join(tmpDir, "app-misc", "foo")
	os.MkdirAll(catDir, 0755)
	os.WriteFile(filepath.Join(catDir, "foo-1.0.0.ebuild"), []byte("EAPI=8\n"), 0644)
	os.WriteFile(filepath.Join(catDir, "foo-2.0.0.ebuild"), []byte("EAPI=8\n"), 0644)

	path, err := resolveEbuildFromCatPkg("app-misc/foo", tmpDir)
	if err != nil {
		t.Fatalf("resolveEbuildFromCatPkg failed: %v", err)
	}
	if !strings.HasSuffix(path, "foo-2.0.0.ebuild") {
		t.Errorf("resolved = %q, want foo-2.0.0.ebuild", path)
	}
}

func TestResolveEbuild_CatPkgNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := resolveEbuildFromCatPkg("app-misc/nonexistent", tmpDir)
	if err == nil {
		t.Fatal("expected error for missing cat/pkg")
	}
	if !strings.Contains(err.Error(), "no ebuilds found") {
		t.Errorf("error = %q, want 'no ebuilds found'", err)
	}
}

// ---------------------------------------------------------------------------
// Phase validation
// ---------------------------------------------------------------------------

func TestValidatePhase_Valid(t *testing.T) {
	validPhases := []string{"clean", "fetch", "unpack", "prepare", "configure", "compile", "postunpack", "install", "qmerge", "digest", "manifest"}
	for _, phase := range validPhases {
		if !isValidPhase(phase) {
			t.Errorf("isValidPhase(%q) = false, want true", phase)
		}
	}
}

func TestValidatePhase_Invalid(t *testing.T) {
	invalidPhases := []string{"build", "test", "run", "invalid", "clean-fetch"}
	for _, phase := range invalidPhases {
		if isValidPhase(phase) {
			t.Errorf("isValidPhase(%q) = true, want false", phase)
		}
	}
	// FETCH is case-insensitive match for "fetch"
	if !isValidPhase("FETCH") {
		t.Error("isValidPhase(\"FETCH\") = false, want true (case-insensitive)")
	}
}

// ---------------------------------------------------------------------------
// BuildPhaseList — assemble phases from flags
// ---------------------------------------------------------------------------

func TestBuildPhaseList_Default(t *testing.T) {
	phases := buildPhaseList(false, false, false)
	if len(phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(phases))
	}
	if phases[0] != "clean" {
		t.Errorf("phase = %q, want clean", phases[0])
	}
}

func TestBuildPhaseList_WithCompile(t *testing.T) {
	phases := buildPhaseList(true, false, false)
	if len(phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(phases))
	}
	if phases[0] != "clean" || phases[1] != "compile" {
		t.Errorf("phases = %v, want [clean compile]", phases)
	}
}

func TestBuildPhaseList_WithMerge(t *testing.T) {
	phases := buildPhaseList(false, true, false)
	expected := []string{"clean", "fetch", "unpack", "compile", "install", "qmerge"}
	if len(phases) != len(expected) {
		t.Fatalf("expected %d phases, got %d", len(expected), len(phases))
	}
	for i, exp := range expected {
		if phases[i] != exp {
			t.Errorf("phases[%d] = %q, want %q", i, phases[i], exp)
		}
	}
}

func TestBuildPhaseList_CustomPhases(t *testing.T) {
	phases := buildPhaseList(false, false, false)
	// Custom phases override default
	phases = []string{"fetch", "unpack"}
	if len(phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(phases))
	}
	if phases[0] != "fetch" || phases[1] != "unpack" {
		t.Errorf("phases = %v, want [fetch unpack]", phases)
	}
}

// ---------------------------------------------------------------------------
// runPhase — execute ebuild phase (stub runner)
// ---------------------------------------------------------------------------

func TestRunPhase_Success(t *testing.T) {
	runner := &stubRunner{}
	err := runPhase("/tmp/foo/foo-1.0.0.ebuild", "fetch", runner, false)
	if err != nil {
		t.Fatalf("runPhase failed: %v", err)
	}
	if len(runner.Cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(runner.Cmds))
	}
	expected := "ebuild /tmp/foo/foo-1.0.0.ebuild fetch"
	if runner.Cmds[0] != expected {
		t.Errorf("command = %q, want %q", runner.Cmds[0], expected)
	}
}

func TestRunPhase_Fail(t *testing.T) {
	runner := &stubRunner{Err: exec.ErrNotFound}
	err := runPhase("/tmp/foo/foo-1.0.0.ebuild", "fetch", runner, false)
	if err == nil {
		t.Fatal("expected error for failed command")
	}
	if !strings.Contains(err.Error(), "ebuild") {
		t.Errorf("error = %q, want 'ebuild' in message", err)
	}
}

func TestRunPhase_Pretend(t *testing.T) {
	var output strings.Builder
	runner := &stubRunner{}
	err := runPhase("/tmp/foo/foo-1.0.0.ebuild", "fetch", runner, true)
	if err != nil {
		t.Fatalf("runPhase failed: %v", err)
	}
	if len(runner.Cmds) != 0 {
		t.Errorf("pretend mode executed %d commands, expected 0", len(runner.Cmds))
	}
	_ = output // pretend prints to stdout, not the runner
}

// ---------------------------------------------------------------------------
// TestLocal — main entry point (stub runner tests)
// ---------------------------------------------------------------------------

func TestLocal_RunClean(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644)

	runner := &stubRunner{}
	err := TestLocal(ebuildPath, nil, runner, false, false)
	if err != nil {
		t.Fatalf("TestLocal failed: %v", err)
	}
	if len(runner.Cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(runner.Cmds))
	}
	if runner.Cmds[0] != "ebuild "+ebuildPath+" clean" {
		t.Errorf("command = %q, want 'ebuild %s clean'", runner.Cmds[0], ebuildPath)
	}
}

func TestLocal_RunMultiplePhases(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644)

	runner := &stubRunner{}
	err := TestLocal(ebuildPath, []string{"clean", "fetch", "unpack"}, runner, false, false)
	if err != nil {
		t.Fatalf("TestLocal failed: %v", err)
	}
	if len(runner.Cmds) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(runner.Cmds))
	}
}

func TestLocal_Pretend(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644)

	runner := &stubRunner{}
	err := TestLocal(ebuildPath, nil, runner, true, false)
	if err != nil {
		t.Fatalf("TestLocal failed: %v", err)
	}
	// In pretend mode, no commands should execute
	if len(runner.Cmds) != 0 {
		t.Errorf("pretend mode executed %d commands, expected 0", len(runner.Cmds))
	}
}

func TestLocal_InvalidPhase(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644)

	runner := &stubRunner{}
	err := TestLocal(ebuildPath, []string{"invalidphase"}, runner, false, false)
	if err == nil {
		t.Fatal("expected error for invalid phase")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("error = %q, want 'invalid' in message", err)
	}
}

func TestLocal_EbuildNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "nonexistent.ebuild")
	// Don't create the file

	runner := &stubRunner{}
	err := TestLocal(ebuildPath, nil, runner, false, false)
	if err == nil {
		t.Fatal("expected error for missing ebuild")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found' in message", err)
	}
}

func TestLocal_LivePackage(t *testing.T) {
	// When the ebuild is a live version (9999), use it directly (no fallback)
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-9999.ebuild")
	os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644)

	runner := &stubRunner{}
	err := TestLocal(ebuildPath, nil, runner, false, false)
	if err != nil {
		t.Fatalf("TestLocal for live package failed: %v", err)
	}
	if len(runner.Cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(runner.Cmds))
	}
}

// ---------------------------------------------------------------------------
// Environment setup helpers
// ---------------------------------------------------------------------------

func TestSetNetworkFeatures_Disabled(t *testing.T) {
	env := setNetworkFeatures(nil)
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, "FEATURES=") {
			found = true
			if !strings.Contains(e, "-network-sandbox") {
				t.Errorf("FEATURES missing -network-sandbox: %s", e)
			}
		}
	}
	if !found {
		t.Error("expected FEATURES env var")
	}
}

func TestSetNetworkFeatures_Existing(t *testing.T) {
	// When FEATURES already exists, should preserve or append
	env := []string{"FEATURES="}
	env = setNetworkFeatures(env)
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, "FEATURES=") {
			found = true
			if !strings.Contains(e, "-network-sandbox") {
				t.Errorf("FEATURES missing -network-sandbox: %s", e)
			}
		}
	}
	if !found {
		t.Error("expected FEATURES env var")
	}
}
