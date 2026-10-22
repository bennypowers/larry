package test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// D2 — emerge argv: atom not path, no stray "="
// ---------------------------------------------------------------------------

func TestContainerArgs_EmergeCommandLine(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "dev-util", "javy", "javy-9.1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	overlay := tmpDir
	cacheDir := filepath.Join(tmpDir, "cache")
	repo := "bennypowers"
	if err := os.MkdirAll(filepath.Join(overlay, "profiles"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "profiles", "repo_name"), []byte(repo), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	stub := newRecRunner()
	stub.fail["podman image exists larry-test"] = os.ErrNotExist

	cf := filepath.Join(tmpDir, "Containerfile.larry-test")
	if err := os.WriteFile(cf, []byte("FROM ubuntu\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &ContainerConfig{
		Overlay:   overlay,
		CacheDir:  cacheDir,
		ImageName: "larry-test",
	}

	err := TestContainer(ebuildPath, cfg, stub, false)
	if err != nil {
		t.Fatalf("TestContainer failed: %v", err)
	}

	// Find the podman run call
	var runArgs []string
	for _, c := range stub.calls {
		if c[0] == "podman" && len(c) > 1 && c[1] == "run" {
			runArgs = c
			break
		}
	}
	if len(runArgs) == 0 {
		t.Fatalf("no podman run call recorded; calls = %v", stub.calls)
	}

	// Find the emerge sub-args
	var emergeIdx int
	for i, a := range runArgs {
		if a == "emerge" {
			emergeIdx = i
			break
		}
	}
	if emergeIdx == 0 {
		t.Fatalf("no emerge in podman args; runArgs = %v", runArgs)
	}
	emergeArgs := runArgs[emergeIdx:]

	want := []string{"emerge", "-1", "--verbose", "--autounmask=y", "--autounmask-write=y", "=dev-util/javy-9.1.0"}
	if !reflect.DeepEqual(emergeArgs, want) {
		t.Errorf("emerge args = %v, want %v", emergeArgs, want)
	}
	// No element equals "=" and no element has .ebuild suffix
	for _, a := range emergeArgs {
		if a == "=" {
			t.Error("stray bare \"=\" element in emerge args")
		}
		if strings.HasSuffix(a, ".ebuild") {
			t.Errorf("element has .ebuild suffix: %q", a)
		}
	}
}

// D2 — revision suffix in ParsedEbuild.PF
func TestContainer_AtomForBareCategoryPackage(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "dev-util", "javy", "javy-9.1.0-r2.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	overlay := tmpDir
	cacheDir := filepath.Join(tmpDir, "cache")
	repo := "bennypowers"
	if err := os.MkdirAll(filepath.Join(overlay, "profiles"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "profiles", "repo_name"), []byte(repo), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	stub := newRecRunner()
	stub.fail["podman image exists larry-test"] = os.ErrNotExist

	cf := filepath.Join(tmpDir, "Containerfile.larry-test")
	if err := os.WriteFile(cf, []byte("FROM ubuntu\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &ContainerConfig{
		Overlay:   overlay,
		CacheDir:  cacheDir,
		ImageName: "larry-test",
	}

	err := TestContainer(ebuildPath, cfg, stub, false)
	if err != nil {
		t.Fatalf("TestContainer failed: %v", err)
	}

	var runArgs []string
	for _, c := range stub.calls {
		if c[0] == "podman" && len(c) > 1 && c[1] == "run" {
			runArgs = c
			break
		}
	}
	if len(runArgs) == 0 {
		t.Fatalf("no podman run call recorded")
	}

	var emergeIdx int
	for i, a := range runArgs {
		if a == "emerge" {
			emergeIdx = i
			break
		}
	}
	if emergeIdx == 0 {
		t.Fatalf("no emerge in args")
	}
	emergeArgs := runArgs[emergeIdx:]

	want := []string{"emerge", "-1", "--verbose", "--autounmask=y", "--autounmask-write=y", "=dev-util/javy-9.1.0-r2"}
	if !reflect.DeepEqual(emergeArgs, want) {
		t.Errorf("emerge args = %v, want %v", emergeArgs, want)
	}
}

// ---------------------------------------------------------------------------
// D3 — pretend adds --pretend flag
// ---------------------------------------------------------------------------

func TestContainerArgs_PretendAddsFlag(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "dev-util", "javy", "javy-9.1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	overlay := tmpDir
	cacheDir := filepath.Join(tmpDir, "cache")
	repo := "bennypowers"
	if err := os.MkdirAll(filepath.Join(overlay, "profiles"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "profiles", "repo_name"), []byte(repo), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stub := newRecRunner()

	cf := filepath.Join(tmpDir, "Containerfile.larry-test")
	if err := os.WriteFile(cf, []byte("FROM ubuntu\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &ContainerConfig{
		Overlay:   overlay,
		CacheDir:  cacheDir,
		ImageName: "larry-test",
	}

	// Suppress output
	origStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	err := TestContainer(ebuildPath, cfg, stub, true)
	// httptest writer; a close error cannot occur here.
	_ = w.Close()
	os.Stdout = origStdout

	if err != nil {
		t.Fatalf("TestContainer failed: %v", err)
	}
	if len(stub.calls) != 0 {
		t.Errorf("pretend mode recorded %d calls, expected 0; calls = %v", len(stub.calls), stub.calls)
	}
}

// ---------------------------------------------------------------------------
// D4 — pretend skips all podman operations (build, --version, etc.)
// ---------------------------------------------------------------------------

func TestContainer_PretendSkipsBuild(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "dev-util", "javy", "javy-9.1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	overlay := tmpDir
	cacheDir := filepath.Join(tmpDir, "cache")
	repo := "bennypowers"
	if err := os.MkdirAll(filepath.Join(overlay, "profiles"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "profiles", "repo_name"), []byte(repo), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stub := newRecRunner()

	cf := filepath.Join(tmpDir, "Containerfile.larry-test")
	if err := os.WriteFile(cf, []byte("FROM ubuntu\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &ContainerConfig{
		Overlay:   overlay,
		CacheDir:  cacheDir,
		ImageName: "larry-test",
	}

	// Suppress output
	origStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	err := TestContainer(ebuildPath, cfg, stub, true)
	// httptest writer; a close error cannot occur here.
	_ = w.Close()
	os.Stdout = origStdout

	if err != nil {
		t.Fatalf("TestContainer failed: %v", err)
	}
	// Stub recorded zero calls — in particular no podman build and no podman --version
	if len(stub.calls) != 0 {
		t.Errorf("pretend: expected zero calls, got %d: %v", len(stub.calls), stub.calls)
	}
}

// ---------------------------------------------------------------------------
// D5 — podman image exists (not sentinel file)
// ---------------------------------------------------------------------------

func TestContainer_UsesImageExists(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "dev-util", "javy", "javy-9.1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	overlay := tmpDir
	cacheDir := filepath.Join(tmpDir, "cache")
	repo := "bennypowers"
	if err := os.MkdirAll(filepath.Join(overlay, "profiles"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "profiles", "repo_name"), []byte(repo), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	stub := newRecRunner()
	// Stub says image exists (nil = present)
	stub.fail["podman image exists larry-test"] = nil

	cf := filepath.Join(tmpDir, "Containerfile.larry-test")
	if err := os.WriteFile(cf, []byte("FROM ubuntu\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &ContainerConfig{
		Overlay:   overlay,
		CacheDir:  cacheDir,
		ImageName: "larry-test",
	}

	err := TestContainer(ebuildPath, cfg, stub, false)
	if err != nil {
		t.Fatalf("TestContainer failed: %v", err)
	}

	// Recorded calls must include ["podman", "image", "exists", "larry-test"]
	foundExists := false
	for _, c := range stub.calls {
		if len(c) == 4 && c[0] == "podman" && c[1] == "image" && c[2] == "exists" && c[3] == "larry-test" {
			foundExists = true
			break
		}
	}
	if !foundExists {
		t.Errorf("expected podman image exists call; calls = %v", stub.calls)
	}
	// No podman build
	for _, c := range stub.calls {
		if len(c) > 1 && c[0] == "podman" && c[1] == "build" {
			t.Errorf("unexpected podman build call; calls = %v", stub.calls)
		}
	}
}

func TestContainer_RebuildsWhenImageAbsentDespiteStaleSentinel(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "dev-util", "javy", "javy-9.1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	overlay := tmpDir
	cacheDir := filepath.Join(tmpDir, "cache")
	repo := "bennypowers"
	if err := os.MkdirAll(filepath.Join(overlay, "profiles"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "profiles", "repo_name"), []byte(repo), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Pre-create a stale sentinel file
	sentinelFile := filepath.Join(cacheDir, "larry-test")
	if err := os.WriteFile(sentinelFile, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	stub := newRecRunner()
	// Stub says image does NOT exist (error = absent)
	stub.fail["podman image exists larry-test"] = fmt.Errorf("no such image")

	cf := filepath.Join(tmpDir, "Containerfile.larry-test")
	if err := os.WriteFile(cf, []byte("FROM ubuntu\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &ContainerConfig{
		Overlay:   overlay,
		CacheDir:  cacheDir,
		ImageName: "larry-test",
	}

	err := TestContainer(ebuildPath, cfg, stub, false)
	if err != nil {
		t.Fatalf("TestContainer failed: %v", err)
	}

	// A podman build call IS recorded
	foundBuild := false
	for _, c := range stub.calls {
		if len(c) > 1 && c[0] == "podman" && c[1] == "build" {
			foundBuild = true
			break
		}
	}
	if !foundBuild {
		t.Errorf("expected podman build call when image absent despite stale sentinel; calls = %v", stub.calls)
	}
}

// ---------------------------------------------------------------------------
// D6 — build context is overlay, not "."
// ---------------------------------------------------------------------------

func TestContainer_BuildContextIsOverlay(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "dev-util", "javy", "javy-9.1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	overlay := tmpDir
	cacheDir := filepath.Join(tmpDir, "cache")
	repo := "bennypowers"
	if err := os.MkdirAll(filepath.Join(overlay, "profiles"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "profiles", "repo_name"), []byte(repo), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	stub := newRecRunner()
	stub.fail["podman image exists larry-test"] = os.ErrNotExist

	cf := filepath.Join(tmpDir, "Containerfile.larry-test")
	if err := os.WriteFile(cf, []byte("FROM ubuntu\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &ContainerConfig{
		Overlay:   overlay,
		CacheDir:  cacheDir,
		ImageName: "larry-test",
	}

	err := TestContainer(ebuildPath, cfg, stub, false)
	if err != nil {
		t.Fatalf("TestContainer failed: %v", err)
	}

	// Find the build call
	var buildCall []string
	for _, c := range stub.calls {
		if len(c) > 1 && c[0] == "podman" && c[1] == "build" {
			buildCall = c
			break
		}
	}
	if len(buildCall) == 0 {
		t.Fatalf("no podman build call; calls = %v", stub.calls)
	}

	// The overlay is the context dir — it appears before --build-arg or at the end.
	// Check it's present as an argument (not "." or a flag value).
	foundOverlay := false
	for _, a := range buildCall {
		if a == overlay {
			foundOverlay = true
			break
		}
	}
	if !foundOverlay {
		t.Errorf("build call missing overlay path %q; call = %v", overlay, buildCall)
	}
	// Contains --build-arg followed by REPO=bennypowers
	foundRepo := false
	for i, a := range buildCall {
		if a == "--build-arg" && i+1 < len(buildCall) && buildCall[i+1] == "REPO=bennypowers" {
			foundRepo = true
			break
		}
	}
	if !foundRepo {
		t.Errorf("build call missing --build-arg REPO=bennypowers; call = %v", buildCall)
	}
}

// ---------------------------------------------------------------------------
// D7 — mount syntax: -v pairs, cache dirs, no --mount=, no DISTDIR/PKGDIR env
// ---------------------------------------------------------------------------

func TestContainerArgs_Mounts(t *testing.T) {
	cacheDir := "/c"
	repo := "bennypowers"

	// We need a real overlay dir for resolveRepoName to work
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "profiles"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "profiles", "repo_name"), []byte(repo), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &ContainerConfig{
		Overlay:   tmpDir,
		CacheDir:  cacheDir,
		ImageName: "larry-test",
	}

	mounts := getContainerMounts(cfg)
	want := []string{"-v", tmpDir + ":/var/db/repos/" + repo + ":ro", "-v", cacheDir + "/distfiles:/var/cache/distfiles", "-v", cacheDir + "/binpkgs:/var/cache/binpkgs"}
	if !reflect.DeepEqual(mounts, want) {
		t.Errorf("mounts = %v, want %v", mounts, want)
	}
}

func TestContainerArgs_NoMountTypeBindSyntax(t *testing.T) {
	cfg := &ContainerConfig{
		Overlay:   "/o",
		CacheDir:  "/c",
		ImageName: "larry-test",
	}

	mounts := getContainerMounts(cfg)
	for _, m := range mounts {
		if strings.Contains(m, "--mount=") {
			t.Errorf("mount element contains \"--mount=\": %q", m)
		}
	}
}

func TestContainerArgs_NoDistdirPkgdirEnv(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "dev-util", "javy", "javy-9.1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	overlay := tmpDir
	cacheDir := filepath.Join(tmpDir, "cache")
	repo := "bennypowers"
	if err := os.MkdirAll(filepath.Join(overlay, "profiles"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "profiles", "repo_name"), []byte(repo), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	stub := newRecRunner()
	stub.fail["podman image exists larry-test"] = nil

	cf := filepath.Join(tmpDir, "Containerfile.larry-test")
	if err := os.WriteFile(cf, []byte("FROM ubuntu\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &ContainerConfig{
		Overlay:   overlay,
		CacheDir:  cacheDir,
		ImageName: "larry-test",
	}

	err := TestContainer(ebuildPath, cfg, stub, false)
	if err != nil {
		t.Fatalf("TestContainer failed: %v", err)
	}

	// Find podman run call
	var runArgs []string
	for _, c := range stub.calls {
		if c[0] == "podman" && len(c) > 1 && c[1] == "run" {
			runArgs = c
			break
		}
	}

	for _, a := range runArgs {
		if a == "DISTDIR=/distfiles" || a == "PKGDIR=/binpkgs" {
			t.Errorf("found DISTDIR/PKGDIR env in args: %q", a)
		}
		if a == "-e" {
			t.Error("found \"-e\" in args (redundant with -v mounts)")
		}
	}
}

func TestContainer_CreatesCacheDirs(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "c")

	// Verify: call getContainerMounts which should mkdir
	cfg := &ContainerConfig{
		Overlay:   "/o",
		CacheDir:  cacheDir,
		ImageName: "larry-test",
	}

	// getContainerMounts should create cache dirs
	_ = getContainerMounts(cfg)

	if _, err := os.Stat(filepath.Join(cacheDir, "distfiles")); os.IsNotExist(err) {
		t.Error("distfiles dir not created")
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "binpkgs")); os.IsNotExist(err) {
		t.Error("binpkgs dir not created")
	}
}

// ---------------------------------------------------------------------------
// D8 — nil config guard at first statement
// ---------------------------------------------------------------------------

func TestContainer_NilConfigNoPanic(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "dev-util", "javy", "javy-9.1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stub := newRecRunner()

	// Recover from panic
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		_ = TestContainer(ebuildPath, nil, stub, false)
	}()

	if panicked {
		t.Fatal("TestContainer panicked with nil config — should not happen")
	}
}

// ---------------------------------------------------------------------------
// D10 — GPU flags
// ---------------------------------------------------------------------------

func TestContainer_GPUFlags(t *testing.T) {
	flags := getGPUFlags()
	want := []string{"--device", "/dev/kfd", "--device", "/dev/dri", "--group-add", "video"}
	if !reflect.DeepEqual(flags, want) {
		t.Errorf("GPU flags = %v, want %v", flags, want)
	}
	for _, f := range flags {
		if strings.Contains(f, "card0") {
			t.Errorf("GPU flag contains card0: %q", f)
		}
		if strings.Contains(f, "renderD128") {
			t.Errorf("GPU flag contains renderD128: %q", f)
		}
	}
}

// ---------------------------------------------------------------------------
// D11 — checkPodmanAvailable uses its runner
// ---------------------------------------------------------------------------

func TestContainer_PodmanCheckUsesRunner(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "dev-util", "javy", "javy-9.1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	overlay := tmpDir
	cacheDir := filepath.Join(tmpDir, "cache")
	repo := "bennypowers"
	if err := os.MkdirAll(filepath.Join(overlay, "profiles"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "profiles", "repo_name"), []byte(repo), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	stub := newRecRunner()
	// Stub runner fails for podman --version
	stub.fail["podman --version"] = fmt.Errorf("not found")
	// Image exists so we don't try to build
	stub.fail["podman image exists larry-test"] = nil

	cf := filepath.Join(tmpDir, "Containerfile.larry-test")
	if err := os.WriteFile(cf, []byte("FROM ubuntu\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &ContainerConfig{
		Overlay:   overlay,
		CacheDir:  cacheDir,
		ImageName: "larry-test",
	}

	err := TestContainer(ebuildPath, cfg, stub, false)
	if err == nil {
		t.Fatal("expected error from failed podman check")
	}
	if !strings.Contains(err.Error(), "podman not found") {
		t.Errorf("error = %q, want \"podman not found\"", err.Error())
	}

	// Stub recorded the podman --version call
	foundVersion := false
	for _, c := range stub.calls {
		if len(c) == 2 && c[0] == "podman" && c[1] == "--version" {
			foundVersion = true
			break
		}
	}
	if !foundVersion {
		t.Errorf("stub did not record podman --version call; calls = %v", stub.calls)
	}
}

// ---------------------------------------------------------------------------
// Helper tests — still valid after changes
// ---------------------------------------------------------------------------

func TestContainer_RunEmergeNonTUIErrorIncludesCommand(t *testing.T) {
	// The non-TUI podman failure must include the command line (the TUI
	// branch does) so the user can see what was actually run.
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "dev-util", "javy", "javy-9.1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "profiles"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "profiles", "repo_name"), []byte("bennypowers"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Stub podman on PATH that fails loudly.
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	stubScript := "#!/bin/sh\necho stub podman failure >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "podman"), []byte(stubScript), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", binDir)

	cfg := &ContainerConfig{
		Overlay:   tmpDir,
		CacheDir:  filepath.Join(tmpDir, "cache"),
		ImageName: "larry-test",
	}

	err := runContainerEmerge(ebuildPath, cfg, nil, false)
	if err == nil {
		t.Fatal("expected error from failing podman stub")
	}
	msg := err.Error()
	if !strings.Contains(msg, "podman run failed") {
		t.Errorf("error = %q, want prefix \"podman run failed\"", msg)
	}
	if !strings.Contains(msg, "=dev-util/javy-9.1.0") {
		t.Errorf("error = %q, want it to include the command line (emerge atom)", msg)
	}
}

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

func TestContainer_CacheDir(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")

	cfg := &ContainerConfig{
		CacheDir: cacheDir,
	}

	result := getCacheDir(cfg)
	if result != cacheDir {
		t.Errorf("cacheDir = %q, want %q", result, cacheDir)
	}
}

func TestContainer_EbuildNotFound(t *testing.T) {
	cfg := &ContainerConfig{
		Overlay:   "/tmp/overlay",
		ImageName: "larry-test",
	}

	stub := newRecRunner()
	err := TestContainer("/nonexistent/foo-1.0.0.ebuild", cfg, stub, false)
	if err == nil {
		t.Fatal("expected error for missing ebuild")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found' in message", err)
	}
}

func TestContainer_BuildImage_ContainerfileMissing(t *testing.T) {
	stub := newRecRunner()
	err := buildImage("/nonexistent/Containerfile", "/tmp/overlay", "larry-test", stub)
	if err == nil {
		t.Fatal("expected error for missing Containerfile")
	}
	if !strings.Contains(err.Error(), "Containerfile") {
		t.Errorf("error = %q, want 'Containerfile' in message", err)
	}
}

// ---------------------------------------------------------------------------
// Runners for testing
// ---------------------------------------------------------------------------

type recRunner struct {
	calls [][]string
	fail  map[string]error
}

func newRecRunner() *recRunner {
	return &recRunner{fail: make(map[string]error)}
}

func (r *recRunner) Run(name string, args ...string) error {
	key := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, append([]string{name}, args...))
	if err := r.fail[key]; err != nil {
		return err
	}
	return nil
}

func (r *recRunner) RunWithEnv(env []string, name string, args ...string) error {
	return r.Run(name, args...)
}

// ---------------------------------------------------------------------------
// --use: USE flags reach the container as -e USE=... (commas normalized)
// ---------------------------------------------------------------------------

func TestContainerArgs_UseFlags(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "dev-util", "javy", "javy-9.1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	overlay := tmpDir
	cacheDir := filepath.Join(tmpDir, "cache")
	if err := os.MkdirAll(filepath.Join(overlay, "profiles"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "profiles", "repo_name"), []byte("bennypowers"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stub := newRecRunner()
	stub.fail["podman image exists larry-test"] = os.ErrNotExist

	cf := filepath.Join(tmpDir, "Containerfile.larry-test")
	if err := os.WriteFile(cf, []byte("FROM ubuntu\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &ContainerConfig{
		Overlay:   overlay,
		CacheDir:  cacheDir,
		ImageName: "larry-test",
		UseFlags:  "cuda,rocm",
	}

	if err := TestContainer(ebuildPath, cfg, stub, false); err != nil {
		t.Fatalf("TestContainer failed: %v", err)
	}

	var runArgs []string
	for _, c := range stub.calls {
		if c[0] == "podman" && len(c) > 1 && c[1] == "run" {
			runArgs = c
			break
		}
	}
	if len(runArgs) == 0 {
		t.Fatalf("no podman run call recorded; calls = %v", stub.calls)
	}

	// -e USE=cuda rocm must appear before the image name.
	envIdx := indexOf(runArgs, "-e")
	if envIdx == -1 {
		t.Fatalf("no -e in podman args; runArgs = %v", runArgs)
	}
	if envIdx+1 >= len(runArgs) || runArgs[envIdx+1] != "USE=cuda rocm" {
		t.Errorf("-e value = %v, want USE=cuda rocm", runArgs[envIdx+1:])
	}
	imageIdx := indexOf(runArgs, "larry-test")
	if imageIdx == -1 || envIdx > imageIdx {
		t.Errorf("-e USE must precede image name; runArgs = %v", runArgs)
	}
}

func TestContainerArgs_NoUseFlagsNoEnv(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "dev-util", "javy", "javy-9.1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	overlay := tmpDir
	cacheDir := filepath.Join(tmpDir, "cache")
	if err := os.MkdirAll(filepath.Join(overlay, "profiles"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "profiles", "repo_name"), []byte("bennypowers"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stub := newRecRunner()
	stub.fail["podman image exists larry-test"] = os.ErrNotExist

	cf := filepath.Join(tmpDir, "Containerfile.larry-test")
	if err := os.WriteFile(cf, []byte("FROM ubuntu\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &ContainerConfig{
		Overlay:   overlay,
		CacheDir:  cacheDir,
		ImageName: "larry-test",
	}

	if err := TestContainer(ebuildPath, cfg, stub, false); err != nil {
		t.Fatalf("TestContainer failed: %v", err)
	}

	for _, c := range stub.calls {
		if c[0] == "podman" && len(c) > 1 && c[1] == "run" {
			if indexOf(c, "-e") != -1 {
				t.Errorf("unexpected -e in podman args when UseFlags unset; args = %v", c)
			}
			return
		}
	}
	t.Fatalf("no podman run call recorded")
}

func indexOf(slice []string, want string) int {
	for i, s := range slice {
		if s == want {
			return i
		}
	}
	return -1
}

func TestGetUseEnvArgs_SpaceForm(t *testing.T) {
	got := getUseEnvArgs(&ContainerConfig{UseFlags: "cuda rocm"})
	want := []string{"-e", "USE=cuda rocm"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("getUseEnvArgs space form = %v, want %v", got, want)
	}
	got = getUseEnvArgs(&ContainerConfig{UseFlags: "  cuda   rocm  "})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("getUseEnvArgs whitespace collapse = %v, want %v", got, want)
	}
	if got := getUseEnvArgs(&ContainerConfig{}); got != nil {
		t.Errorf("getUseEnvArgs empty = %v, want nil", got)
	}
	if got := getUseEnvArgs(nil); got != nil {
		t.Errorf("getUseEnvArgs nil cfg = %v, want nil", got)
	}
}
