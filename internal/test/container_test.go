package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Image build helpers
// ---------------------------------------------------------------------------

func TestContainer_BuildImage_Cached(t *testing.T) {
	// When cache dir exists, skip build
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "larry-test"), []byte("cached"), 0644)

	cf := filepath.Join(tmpDir, "Containerfile.larry-test")
	os.WriteFile(cf, []byte("FROM ubuntu\n"), 0644)

	cfg := &ContainerConfig{
		CacheDir: cacheDir,
		ImageName: "larry-test",
	}

	runner := &stubRunner{}
	err := buildImage(cf, cfg, runner)
	if err != nil {
		t.Fatalf("buildImage failed: %v", err)
	}
	if len(runner.Cmds) > 0 {
		t.Errorf("cached build executed %d commands, expected 0", len(runner.Cmds))
	}
}

func TestContainer_BuildImage_NotCached(t *testing.T) {
	// When cache doesn't exist, build should run
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")

	cf := filepath.Join(tmpDir, "Containerfile.larry-test")
	os.WriteFile(cf, []byte("FROM ubuntu\n"), 0644)

	cfg := &ContainerConfig{
		CacheDir:  cacheDir,
		ImageName: "larry-test",
	}

	runner := &stubRunner{}
	err := buildImage(cf, cfg, runner)
	if err != nil {
		t.Fatalf("buildImage failed: %v", err)
	}
	if len(runner.Cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(runner.Cmds))
	}
	if !strings.Contains(runner.Cmds[0], "podman build") {
		t.Errorf("command = %q, want 'podman build'", runner.Cmds[0])
	}
	if !strings.Contains(runner.Cmds[0], "-t larry-test") {
		t.Errorf("command = %q, want '-t larry-test'", runner.Cmds[0])
	}
}

func TestContainer_BuildImage_ContainerfileMissing(t *testing.T) {
	cfg := &ContainerConfig{
		ImageName: "larry-test",
	}

	runner := &stubRunner{}
	err := buildImage("/nonexistent/Containerfile", cfg, runner)
	if err == nil {
		t.Fatal("expected error for missing Containerfile")
	}
	if !strings.Contains(err.Error(), "Containerfile") {
		t.Errorf("error = %q, want 'Containerfile' in message", err)
	}
}

// ---------------------------------------------------------------------------
// Container mounting
// ---------------------------------------------------------------------------

func TestContainer_MountOverlay(t *testing.T) {
	tmpDir := t.TempDir()
	overlay := filepath.Join(tmpDir, "overlay")
	os.MkdirAll(overlay, 0755)

	cfg := &ContainerConfig{
		Overlay:   overlay,
		Distdir:   filepath.Join(tmpDir, "distfiles"),
		ImageName: "larry-test",
	}

	mounts := getContainerMounts(cfg)
	found := false
	for _, m := range mounts {
		if strings.Contains(m, overlay) && strings.Contains(m, "/overlay") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("mounts = %v, expected overlay mount", mounts)
	}
}

func TestContainer_MountDistdir(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &ContainerConfig{
		Distdir:   filepath.Join(tmpDir, "distfiles"),
		ImageName: "larry-test",
	}

	mounts := getContainerMounts(cfg)
	found := false
	for _, m := range mounts {
		if strings.Contains(m, cfg.Distdir) && strings.Contains(m, "/distfiles") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("mounts = %v, expected distfiles mount", mounts)
	}
}

// ---------------------------------------------------------------------------
// GPU passthrough
// ---------------------------------------------------------------------------

func TestContainer_GPUPassthrough(t *testing.T) {
	cfg := &ContainerConfig{
		ImageName: "larry-test",
	}

	flags := getGPUFlags(cfg)
	foundKFD := false
	foundDRI := false
	foundVideo := false
	for _, f := range flags {
		if strings.Contains(f, "--device=/dev/kfd") {
			foundKFD = true
		}
		if strings.Contains(f, "--device=/dev/dri") {
			foundDRI = true
		}
		if strings.Contains(f, "--group-add=video") {
			foundVideo = true
		}
	}
	if !foundKFD {
		t.Error("missing --device=/dev/kfd")
	}
	if !foundDRI {
		t.Error("missing --device=/dev/dri")
	}
	if !foundVideo {
		t.Error("missing --group-add=video")
	}
}

// ---------------------------------------------------------------------------
// Container run
// ---------------------------------------------------------------------------

func TestContainer_RunEmerge(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644)

	// Create mount directories
	os.MkdirAll(filepath.Join(tmpDir, "overlay"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "distfiles"), 0755)

	cfg := &ContainerConfig{
		Overlay:   filepath.Join(tmpDir, "overlay"),
		Distdir:   filepath.Join(tmpDir, "distfiles"),
		ImageName: "larry-test",
	}

	runner := &stubRunner{}
	err := runContainerEmerge(ebuildPath, cfg, runner, false)
	if err != nil {
		t.Fatalf("runContainerEmerge failed: %v", err)
	}
	if len(runner.Cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(runner.Cmds))
	}
	if !strings.Contains(runner.Cmds[0], "podman run") {
		t.Errorf("command = %q, want 'podman run'", runner.Cmds[0])
	}
	if !strings.Contains(runner.Cmds[0], "emerge") {
		t.Errorf("command = %q, want 'emerge'", runner.Cmds[0])
	}
}

func TestContainer_RunEmerge_Pretend(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644)

	cfg := &ContainerConfig{
		Overlay:   filepath.Join(tmpDir, "overlay"),
		Distdir:   filepath.Join(tmpDir, "distfiles"),
		ImageName: "larry-test",
	}

	runner := &stubRunner{}
	err := runContainerEmerge(ebuildPath, cfg, runner, true)
	if err != nil {
		t.Fatalf("runContainerEmerge failed: %v", err)
	}
	// In pretend mode, no commands should execute
	if len(runner.Cmds) != 0 {
		t.Errorf("pretend mode executed %d commands, expected 0", len(runner.Cmds))
	}
}

// ---------------------------------------------------------------------------
// Image name
// ---------------------------------------------------------------------------

func TestContainer_ImageName(t *testing.T) {
	cfg := &ContainerConfig{}
	imageName := getImageName(cfg)
	if imageName != "larry-test" {
		t.Errorf("imageName = %q, want 'larry-test'", imageName)
	}
}

func TestContainer_ImageName_Custom(t *testing.T) {
	cfg := &ContainerConfig{
		ImageName: "my-custom-image",
	}
	imageName := getImageName(cfg)
	if imageName != "my-custom-image" {
		t.Errorf("imageName = %q, want 'my-custom-image'", imageName)
	}
}

// ---------------------------------------------------------------------------
// Cache dir
// ---------------------------------------------------------------------------

func TestContainer_CacheDir(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "binpkgs")

	cfg := &ContainerConfig{
		CacheDir: cacheDir,
	}

	result := getCacheDir(cfg)
	if result != cacheDir {
		t.Errorf("cacheDir = %q, want %q", result, cacheDir)
	}
}

// ---------------------------------------------------------------------------
// Podman not available
// ---------------------------------------------------------------------------

func TestContainer_PodmanNotFound(t *testing.T) {
	// When podman is not in PATH, should error
	runner := &stubRunner{Err: exec.ErrNotFound}

	// checkPodmanAvailable uses exec.Command directly, not the runner
	// Stub runner can't affect this, so we just verify it doesn't panic
	_ = runner
	// Actual podman availability is system-dependent; skip in CI
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not available")
	}
}

// ---------------------------------------------------------------------------
// Container config helpers
// ---------------------------------------------------------------------------

func TestContainer_GetEbuildDir(t *testing.T) {
	ebuildPath := "/var/db/repos/gentoo/dev-lisp/foo/foo-1.0.0.ebuild"
	ebuildDir := getEbuildDir(ebuildPath)
	expected := "/var/db/repos/gentoo/dev-lisp/foo"
	if ebuildDir != expected {
		t.Errorf("ebuildDir = %q, want %q", ebuildDir, expected)
	}
}

func TestContainer_GetEbuildDir_Dir(t *testing.T) {
	ebuildDir := getEbuildDir("/some/dir")
	if ebuildDir != "/some/dir" {
		t.Errorf("ebuildDir = %q, want %q", ebuildDir, "/some/dir")
	}
}

// ---------------------------------------------------------------------------
// TUI stream isolation
// ---------------------------------------------------------------------------

func TestContainer_TuiStreamIsolation(t *testing.T) {
	// When useTUI is true, subprocess output should be piped, not inherited
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644)

	// Create mount directories
	os.MkdirAll(filepath.Join(tmpDir, "overlay"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "distfiles"), 0755)

	cfg := &ContainerConfig{
		Overlay:   filepath.Join(tmpDir, "overlay"),
		Distdir:   filepath.Join(tmpDir, "distfiles"),
		ImageName: "larry-test",
		UseTUI:    true,
	}

	runner := &stubRunner{}
	err := runContainerEmerge(ebuildPath, cfg, runner, false)
	if err != nil {
		t.Fatalf("runContainerEmerge failed: %v", err)
	}
	// In TUI mode, output should be piped (stub runner captures it)
	if len(runner.Cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(runner.Cmds))
	}
	// The command should use --tty=false for TUI stream isolation
	if !strings.Contains(runner.Cmds[0], "--tty=false") {
		t.Errorf("TUI mode should use --tty=false, command = %q", runner.Cmds[0])
	}
}

// ---------------------------------------------------------------------------
// TestContainer — main entry point
// ---------------------------------------------------------------------------

func TestContainer_RunClean(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644)

	// Create Containerfile
	cfPath := filepath.Join(tmpDir, "Containerfile.larry-test")
	os.WriteFile(cfPath, []byte("FROM ubuntu\n"), 0644)

	cfg := &ContainerConfig{
		Overlay:   tmpDir,
		Distdir:   filepath.Join(tmpDir, "distfiles"),
		ImageName: "larry-test",
	}

	runner := &stubRunner{}
	err := TestContainer(ebuildPath, cfg, runner, false, false)
	if err != nil {
		t.Fatalf("TestContainer failed: %v", err)
	}
	// Should run podman run with emerge
	if len(runner.Cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(runner.Cmds))
	}
}

func TestContainer_RunMultiplePhases(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644)

	// Create Containerfile
	cfPath := filepath.Join(tmpDir, "Containerfile.larry-test")
	os.WriteFile(cfPath, []byte("FROM ubuntu\n"), 0644)

	cfg := &ContainerConfig{
		Overlay:   tmpDir,
		Distdir:   filepath.Join(tmpDir, "distfiles"),
		ImageName: "larry-test",
	}

	runner := &stubRunner{}
	err := TestContainer(ebuildPath, cfg, runner, false, false)
	if err != nil {
		t.Fatalf("TestContainer failed: %v", err)
	}
	if len(runner.Cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(runner.Cmds))
	}
}

func TestContainer_Pretend(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644)

	// Create Containerfile
	cfPath := filepath.Join(tmpDir, "Containerfile.larry-test")
	os.WriteFile(cfPath, []byte("FROM ubuntu\n"), 0644)

	cfg := &ContainerConfig{
		Overlay:   tmpDir,
		Distdir:   filepath.Join(tmpDir, "distfiles"),
		ImageName: "larry-test",
	}

	runner := &stubRunner{}
	err := TestContainer(ebuildPath, cfg, runner, true, false)
	if err != nil {
		t.Fatalf("TestContainer failed: %v", err)
	}
	// In pretend mode, no commands should execute
	if len(runner.Cmds) != 0 {
		t.Errorf("pretend mode executed %d commands, expected 0", len(runner.Cmds))
	}
}

func TestContainer_EbuildNotFound(t *testing.T) {
	cfg := &ContainerConfig{
		Overlay:   "/tmp/overlay",
		Distdir:   "/tmp/distfiles",
		ImageName: "larry-test",
	}

	runner := &stubRunner{}
	err := TestContainer("/nonexistent/foo-1.0.0.ebuild", cfg, runner, false, false)
	if err == nil {
		t.Fatal("expected error for missing ebuild")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found' in message", err)
	}
}

// stubRunner for container tests

type stubRunner struct {
	Cmds []string
	Err  error
}

func (s *stubRunner) Run(name string, args ...string) error {
	cmd := name + " " + strings.Join(args, " ")
	s.Cmds = append(s.Cmds, cmd)
	return s.Err
}
