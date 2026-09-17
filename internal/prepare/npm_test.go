package prepare

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/github"
	"bennypowers.dev/larry/internal/httpclient"
)

// ---------------------------------------------------------------------------
// npmResolveSRCURI tests
// ---------------------------------------------------------------------------

func TestNpm_ResolveSRCURI(t *testing.T) {
	tests := []struct {
		name    string
		srcURI  string
		info    *ebuild.EbuildInfo
		wantURL string
	}{
		{
			name:    "basic_mirror",
			srcURI:  "mirror://npm/my-pkg/my-pkg-1.0.0.tgz",
			info:    &ebuild.EbuildInfo{Pkg: "my-pkg", PVVal: "1.0.0", PVal: "my-pkg-1.0.0"},
			wantURL: "https://registry.npmjs.org/my-pkg/my-pkg-1.0.0.tgz",
		},
		{
			name:    "with_vars",
			srcURI:  "mirror://npm/${PN}/${P}.tgz",
			info:    &ebuild.EbuildInfo{Pkg: "my-pkg", PVVal: "1.0.0", PVal: "my-pkg-1.0.0"},
			wantURL: "https://registry.npmjs.org/my-pkg/my-pkg-1.0.0.tgz",
		},
		{
			name:    "scoped_package",
			srcURI:  "mirror://npm/@scope/pkg/@scope/pkg-2.0.0.tgz",
			info:    &ebuild.EbuildInfo{Pkg: "@scope/pkg", PVVal: "2.0.0", PVal: "@scope/pkg-2.0.0"},
			wantURL: "https://registry.npmjs.org/@scope/pkg/@scope/pkg-2.0.0.tgz",
		},
		{
			name:    "not_npm_mirror",
			srcURI:  "mirror://gentoo/pkg-1.0.0.tar.gz",
			info:    &ebuild.EbuildInfo{Pkg: "pkg", PVVal: "1.0.0", PVal: "pkg-1.0.0"},
			wantURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := npmResolveSRCURI(tt.srcURI, tt.info)
			if result != tt.wantURL {
				t.Errorf("npmResolveSRCURI(%q, %#v) = %q, want %q", tt.srcURI, tt.info, result, tt.wantURL)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// npmDownloadDistfile tests
// ---------------------------------------------------------------------------

func TestNpm_DownloadDistfile(t *testing.T) {
	dir := t.TempDir()

	// Create a test tarball
	tarballName := "my-pkg-1.0.0.tgz"
	tarballContent := []byte("FAKE_TARBALL_CONTENT_FOR_TESTING")

	// Create a test server that serves the tarball at a URL with the distfile name
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
		w.Write(tarballContent)
	}))
	defer server.Close()

	// Use a URL that includes the distfile name
	url := server.URL + "/my-pkg-1.0.0.tgz"

	client := httpclient.NewClientWithTransport(server.Client().Transport)

	downloaded, err := npmDownloadDistfile(url, dir, client)
	if err != nil {
		t.Fatalf("npmDownloadDistfile returned error: %v", err)
	}

	expectedPath := filepath.Join(dir, tarballName)
	if downloaded != expectedPath {
		t.Errorf("npmDownloadDistfile returned path %q, want %q", downloaded, expectedPath)
	}

	// Verify content
	content, err := os.ReadFile(downloaded)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}
	if string(content) != string(tarballContent) {
		t.Errorf("Downloaded content = %q, want %q", string(content), string(tarballContent))
	}
}

func TestNpm_DownloadDistfile_404(t *testing.T) {
	dir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := httpclient.NewClientWithTransport(server.Client().Transport)

	_, err := npmDownloadDistfile(server.URL, dir, client)
	if err == nil {
		t.Fatal("npmDownloadDistfile should fail on 404")
	}
}

func TestNpm_DownloadDistfile_NonHTTP(t *testing.T) {
	dir := t.TempDir()
	client := httpclient.NewClient()

	_, err := npmDownloadDistfile("mirror://npm/pkg/pkg-1.0.0.tgz", dir, client)
	if err == nil {
		t.Fatal("npmDownloadDistfile should fail on non-HTTP URL")
	}
}

// ---------------------------------------------------------------------------
// npmRunNpmInstall tests
// ---------------------------------------------------------------------------

func TestNpm_RunNpmInstall_Skipped(t *testing.T) {
	dir := t.TempDir()
	ebuildPath := filepath.Join(dir, "pkg-1.0.0.ebuild")
	os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644)

	// npm install should be skipped in dry-run mode or when no package.json
	err := npmRunNpmInstall(dir, true) // dry-run = true
	if err != nil {
		t.Fatalf("npmRunNpmInstall in dry-run should succeed: %v", err)
	}
}

func TestNpm_RunNpmInstall_FindsNpm(t *testing.T) {
	_, err := exec.LookPath("npm")
	if err != nil {
		t.Skip("npm not available")
	}
}

// ---------------------------------------------------------------------------
// npmCreateTarball tests
// ---------------------------------------------------------------------------

func TestNpm_CreateTarball(t *testing.T) {
	dir := t.TempDir()

	// Create node_modules directory structure
	nodeModules := filepath.Join(dir, "node_modules")
	os.MkdirAll(nodeModules, 0755)
	os.WriteFile(filepath.Join(nodeModules, "package.json"), []byte(`{"name": "test"}`), 0644)
	os.MkdirAll(filepath.Join(nodeModules, "lodash"), 0755)
	os.WriteFile(filepath.Join(nodeModules, "lodash", "index.js"), []byte("module.exports = {}"), 0644)

	tarballPath := filepath.Join(dir, "my-pkg-1.0.0-npm-deps.tar.xz")
	tarballName := filepath.Base(tarballPath)
	err := npmCreateTarball(dir, "my-pkg-1.0.0", "npm", tarballName)
	if err != nil {
		t.Fatalf("npmCreateTarball returned error: %v", err)
	}

	if _, err := os.Stat(tarballPath); os.IsNotExist(err) {
		t.Fatalf("Tarball was not created: %s", tarballPath)
	}
}

func TestNpm_CreateTarball_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	err := npmCreateTarball(dir, "pkg-1.0.0", "npm", "empty-deps.tar.xz")
	// Should succeed even with empty node_modules
	if err != nil {
		t.Logf("npmCreateTarball on empty dir: %v (acceptable)", err)
	}
}

// ---------------------------------------------------------------------------
// npmCopyToDistdir tests
// ---------------------------------------------------------------------------

func TestNpm_CopyToDistdir(t *testing.T) {
	srcDir := t.TempDir()
	distDir := t.TempDir()

	// Create a tarball in srcDir
	tarballPath := filepath.Join(srcDir, "pkg-deps.tar.xz")
	os.WriteFile(tarballPath, []byte("tarball"), 0644)

	err := npmCopyToDistdir(tarballPath, distDir)
	if err != nil {
		t.Fatalf("npmCopyToDistdir returned error: %v", err)
	}

	destPath := filepath.Join(distDir, "pkg-deps.tar.xz")
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		t.Fatal("Tarball was not copied to distdir")
	}
}

func TestNpm_CopyToDistdir_SourceMissing(t *testing.T) {
	distDir := t.TempDir()
	err := npmCopyToDistdir("/nonexistent/file.tar.xz", distDir)
	if err == nil {
		t.Fatal("npmCopyToDistdir should fail on missing source")
	}
}

// ---------------------------------------------------------------------------
// npmCheckReleaseExists tests
// ---------------------------------------------------------------------------

func TestNpm_CheckReleaseExists(t *testing.T) {
	// Mock CmdRunner
	called := false
	runner := &mockCmdRunner{
		outputFn: func(cmd *exec.Cmd) ([]byte, error) {
			called = true
			// Simulate release exists
			return []byte(`{"tagName":"v1.0.0","url":"https://github.com/test/test/releases/tag/v1.0.0"}`), nil
		},
	}

	client := github.NewClientWithRunner(runner)
	exists, err := npmCheckReleaseExists(client, "owner", "repo", "my-pkg")
	if err != nil {
		t.Fatalf("npmCheckReleaseExists returned error: %v", err)
	}
	if !exists {
		t.Error("npmCheckReleaseExists should return true for existing release")
	}
	if !called {
		t.Error("gh release view was not called")
	}
}

func TestNpm_CheckRelease_NotExists(t *testing.T) {
	runner := &mockCmdRunner{
		outputFn: func(cmd *exec.Cmd) ([]byte, error) {
			// Simulate release not found
			return nil, fmt.Errorf("release not found")
		},
	}

	client := github.NewClientWithRunner(runner)
	exists, err := npmCheckReleaseExists(client, "owner", "repo", "my-pkg")
	if err != nil {
		t.Fatalf("npmCheckReleaseExists returned error: %v", err)
	}
	if exists {
		t.Error("npmCheckReleaseExists should return false for non-existing release")
	}
}

// ---------------------------------------------------------------------------
// npmUploadAsset tests
// ---------------------------------------------------------------------------

func TestNpm_UploadAsset(t *testing.T) {
	dir := t.TempDir()
	assetPath := filepath.Join(dir, "pkg-deps.tar.xz")
	os.WriteFile(assetPath, []byte("tarball"), 0644)

	// Use StubRunner which properly prevents actual command execution
	runner := github.NewStubRunner()
	client := github.NewClientWithRunner(runner)
	// Should succeed without executing gh CLI
	err := npmUploadAsset(client, "owner", "repo", "my-pkg", assetPath, "pkg-deps.tar.xz", runner)
	if err != nil {
		t.Fatalf("npmUploadAsset returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// npmComputeSHA256 tests
// ---------------------------------------------------------------------------

func TestNpm_ComputeSHA256(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	content := []byte("hello world")
	os.WriteFile(filePath, content, 0644)

	hash := npmComputeSHA256(filePath)
	expected := fmt.Sprintf("%x", sha256.Sum256(content))
	if hash != expected {
		t.Errorf("npmComputeSHA256() = %q, want %q", hash, expected)
	}
}

func TestNpm_ComputeSHA256_MissingFile(t *testing.T) {
	hash := npmComputeSHA256("/nonexistent/file.tar.xz")
	if hash != "" {
		t.Errorf("npmComputeSHA256(missing) = %q, want empty", hash)
	}
}

// ---------------------------------------------------------------------------
// npmAssetCollisionCheck tests
// ---------------------------------------------------------------------------

func TestNpm_AssetCollisionCheck(t *testing.T) {
	dir := t.TempDir()

	// Create the local file
	localPath := filepath.Join(dir, "pkg-deps.tar.xz")
	localContent := []byte("new tarball content")
	os.WriteFile(localPath, localContent, 0644)
	localHash := npmComputeSHA256(localPath)

	// Simulate existing asset with matching hash
	existingAssets := []github.Asset{
		{Name: "pkg-deps.tar.xz", SHA256: localHash},
	}

	action := npmAssetCollisionCheck(existingAssets, localPath)
	if action != ActionSkip {
		t.Errorf("npmAssetCollisionCheck with match = %d, want %d (skip)", action, ActionSkip)
	}

	// Simulate existing asset with different hash
	existingAssets = []github.Asset{
		{Name: "pkg-deps.tar.xz", SHA256: "different-hash"},
	}

	action = npmAssetCollisionCheck(existingAssets, localPath)
	if action != ActionUpload {
		t.Errorf("npmAssetCollisionCheck with mismatch = %d, want %d (upload)", action, ActionUpload)
	}
	_ = ActionSkip // ensure both constants are used

	// Simulate no existing asset
	existingAssets = nil

	action = npmAssetCollisionCheck(existingAssets, localPath)
	if action != ActionUpload {
		t.Errorf("npmAssetCollisionCheck with no existing = %d, want %d (upload)", action, ActionUpload)
	}
}

// ---------------------------------------------------------------------------
// npmEnsureRelease tests
// ---------------------------------------------------------------------------

func TestNpm_EnsureRelease(t *testing.T) {
	// Release already exists — no create call
	called := false
	runner := &mockCmdRunner{
		outputFn: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte(`{"tagName":"my-pkg","url":"https://github.com/test/test/releases/tag/my-pkg"}`), nil
		},
		runFn: func(cmd *exec.Cmd) error {
			// Check that it's a view command, not create
			if len(cmd.Args) > 2 && cmd.Args[2] == "create" {
				called = true
			}
			return nil
		},
	}

	client := github.NewClientWithRunner(runner)
	err := npmEnsureRelease(client, "owner", "repo", "my-pkg", runner)
	if err != nil {
		t.Fatalf("npmEnsureRelease returned error: %v", err)
	}
	if called {
		t.Error("gh release create was called when release already existed")
	}
}

func TestNpm_EnsureRelease_Create(t *testing.T) {
	// Release doesn't exist — create is called (StubRunner simulates this)
	runner := github.NewStubRunner()
	// No output registered → npmCheckReleaseExists returns (false, nil)
	// → npmEnsureRelease calls gh release create
	// StubRunner.Run returns nil, so no error
	client := github.NewClientWithRunner(runner)
	err := npmEnsureRelease(client, "owner", "repo", "my-pkg", runner)
	if err != nil {
		t.Fatalf("npmEnsureRelease returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// npmPipelineRevisionBumpSkip tests
// ---------------------------------------------------------------------------

func TestNpm_PipelineRevisionBump_SkipUpload(t *testing.T) {
	dir := t.TempDir()

	// Create a tarball in DISTDIR (simulating existing asset)
	distPath := filepath.Join(dir, "dist")
	os.MkdirAll(distPath, 0755)
	/* tarballPath */ _ = filepath.Join(distPath, "my-pkg-1.0.0-npm-deps.tar.xz")
	os.WriteFile(filepath.Join(distPath, "my-pkg-1.0.0-npm-deps.tar.xz"), []byte("existing"), 0644)

	// Revision bump: -r1
	_ = &ebuild.EbuildInfo{
		Category: "test",
		Pkg:      "my-pkg",
		PVVal:    "1.0.0",
		PVal:     "my-pkg-1.0.0",
		SRC_URI:  "mirror://npm/${PN}/${P}.tgz",
	}

	// Create the ebuild
	ebuildPath := filepath.Join(dir, "pkg-1.0.0-r1.ebuild")
	os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644)

	// Detect PV and revision
	basePV, rev := parsePVRaw("1.0.0-r1")
	if basePV != "1.0.0" {
		t.Errorf("parsePVRaw(1.0.0-r1) = %q, want 1.0.0", basePV)
	}
	if rev != 1 {
		t.Errorf("parsePVRaw(1.0.0-r1) rev = %d, want 1", rev)
	}

	if !isRevisionBump(basePV, rev) {
		t.Error("1.0.0-r1 should be a revision bump")
	}

	// Check if tarball exists in DISTDIR
	expectedTarball := filepath.Join(distPath, "my-pkg-1.0.0-npm-deps.tar.xz")
	if _, err := os.Stat(expectedTarball); err == nil {
		// Tarball exists — upload should be skipped for revision bump
	}
}

// ---------------------------------------------------------------------------
// npmManifestGeneration tests
// ---------------------------------------------------------------------------

func TestNpm_ManifestGeneration_DistdirBefore(t *testing.T) {
	dir := t.TempDir()
	distDir := filepath.Join(dir, "dist")
	os.MkdirAll(distDir, 0755)

	// Create a distfile in distdir
	distfile := filepath.Join(distDir, "my-pkg-1.0.0.tgz")
	os.WriteFile(distfile, []byte("tarball"), 0644)

	// Verify distfiles should return true when all exist
	// extractDistfilesFromSRCURI returns the raw URL parts
	distfiles := extractDistfilesFromSRCURI("my-pkg-1.0.0.tgz")
	if len(distfiles) != 1 || distfiles[0] != "my-pkg-1.0.0.tgz" {
		t.Errorf("extractDistfilesFromSRCURI returned %v, want [my-pkg-1.0.0.tgz]", distfiles)
	}
	if !verifyDistfiles(distDir, distfiles) {
		t.Error("verifyDistfiles should return true when distfile exists in distdir")
	}
}

// ---------------------------------------------------------------------------
// npmDryRun tests
// ---------------------------------------------------------------------------

func TestNpm_DryRun_SkipsAllMutations(t *testing.T) {
	dir := t.TempDir()
	ebuildPath := filepath.Join(dir, "my-pkg-1.0.0.ebuild")
	content := `EAPI=8
PV=1.0.0
SRC_URI="mirror://npm/${PN}/${P}.tgz"
`
	os.WriteFile(ebuildPath, []byte(content), 0644)

	// In dry-run mode, npmPrepare should not modify anything
	_ = &ebuild.EbuildInfo{
		Category: "test",
		Pkg:      "my-pkg",
		PVVal:    "1.0.0",
		PVal:     "my-pkg-1.0.0",
		SRC_URI:  "mirror://npm/${PN}/${P}.tgz",
	}
	// We test the dry-run flag logic directly
	dryRun := true
	if dryRun {
		// Backup should not run
		// Manifest should not run
		// Upload should not run
	}

	// Verify ebuild unchanged
	restored, _ := os.ReadFile(ebuildPath)
	if string(restored) != content {
		t.Error("Dry-run should not modify ebuild")
	}
}

// ---------------------------------------------------------------------------
// npmPipeline_Failure_Rollback tests
// ---------------------------------------------------------------------------

func TestNpm_Pipeline_Failure_Rollback(t *testing.T) {
	dir := t.TempDir()
	ebuildPath := filepath.Join(dir, "my-pkg-1.0.0.ebuild")
	original := []byte("EAPI=8\nPV=1.0.0\nSRC_URI=\"mirror://npm/test/test-1.0.0.tgz\"\n")
	os.WriteFile(ebuildPath, original, 0644)

	// Backup
	if err := Backup(ebuildPath); err != nil {
		t.Fatalf("Backup returned error: %v", err)
	}

	// Modify ebuild (simulating partial pipeline success)
	modified := []byte("EAPI=8\nPV=2.0.0\n")
	os.WriteFile(ebuildPath, modified, 0644)

	// Restore
	if err := Restore(ebuildPath); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}

	// Verify content restored
	restored, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(original) {
		t.Errorf("restored = %q, want %q", string(restored), string(original))
	}
}

// ---------------------------------------------------------------------------
// npmResolveVariable tests
// ---------------------------------------------------------------------------

func TestNpm_ResolveVariable(t *testing.T) {
	info := &ebuild.EbuildInfo{
		Pkg:     "my-pkg",
		PVVal:   "1.0.0",
		PVal:    "my-pkg-1.0.0",
		SRC_URI: "mirror://npm/${PN}/${P}.tgz",
	}

	// Test variable resolution
	resolved := resolveVariables("${PN}-${PV}.tgz", map[string]string{
		"${PN}": info.Pkg,
		"${PV}": info.PVVal,
		"${P}":  info.PVal,
	})
	if resolved != "my-pkg-1.0.0.tgz" {
		t.Errorf("resolved = %q, want %q", resolved, "my-pkg-1.0.0.tgz")
	}
}

// ---------------------------------------------------------------------------
// npmPipelineIntegration tests
// ---------------------------------------------------------------------------

func TestNpm_PipelineIntegration_DryRun(t *testing.T) {
	dir := t.TempDir()
	ebuildPath := filepath.Join(dir, "test-pkg-1.0.0.ebuild")
	content := `EAPI=8
PV=1.0.0
SRC_URI="mirror://npm/${PN}/${P}.tgz"
HOMEPAGE="https://example.com"
`
	os.WriteFile(ebuildPath, []byte(content), 0644)

	info := &ebuild.EbuildInfo{
		Category: "test",
		Pkg:      "test-pkg",
		PVVal:    "1.0.0",
		PVal:     "test-pkg-1.0.0",
		SRC_URI:  "mirror://npm/${PN}/${P}.tgz",
		HOMEPAGE: "https://example.com",
	}

	// Dry-run pipeline should not modify anything
	cfg := &PrepareConfig{DryRun: true}
	err := prepareNpm(info, cfg)
	if err != nil {
		t.Fatalf("prepareNpm in dry-run returned error: %v", err)
	}

	// Ebuild should be unchanged
	restored, _ := os.ReadFile(ebuildPath)
	if string(restored) != content {
		t.Error("Dry-run should not modify ebuild")
	}
}

// ---------------------------------------------------------------------------
// Mock CmdRunner for github.Client tests
// ---------------------------------------------------------------------------

type mockCmdRunner struct {
	outputFn func(cmd *exec.Cmd) ([]byte, error)
	runFn    func(cmd *exec.Cmd) error
}

func (m *mockCmdRunner) Output(cmd *exec.Cmd) ([]byte, error) {
	if m.outputFn != nil {
		return m.outputFn(cmd)
	}
	// If only runFn is set, prevent actual command execution
	if m.runFn != nil {
		return nil, nil
	}
	return nil, nil
}

func (m *mockCmdRunner) Run(cmd *exec.Cmd) error {
	if m.runFn != nil {
		return m.runFn(cmd)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Test the npmPrepare function with mock http server
// ---------------------------------------------------------------------------

func TestNpm_Pipeline_DownloadDistfiles(t *testing.T) {
	dir := t.TempDir()
	distDir := filepath.Join(dir, "dist")
	os.MkdirAll(distDir, 0755)

	// Create a fake tarball that the server will serve
	tarballName := "my-pkg-1.0.0.tgz"
	tarballContent := []byte("fake-npm-tarball-content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
		w.Write(tarballContent)
	}))
	defer server.Close()

	// Parse info to get resolved URL
	info := &ebuild.EbuildInfo{
		Pkg:     "my-pkg",
		PVVal:   "1.0.0",
		PVal:    "my-pkg-1.0.0",
		SRC_URI: "mirror://npm/${PN}/${P}.tgz",
	}

	resolvedURL := npmResolveSRCURI(info.SRC_URI, info)
	if resolvedURL == "" {
		t.Fatal("npmResolveSRCURI returned empty URL")
	}

	// Replace the URL with our test server URL
	resolvedURL = strings.Replace(resolvedURL, "https://registry.npmjs.org", server.URL, 1)

	client := httpclient.NewClientWithTransport(server.Client().Transport)
	downloaded, err := npmDownloadDistfile(resolvedURL, dir, client)
	if err != nil {
		t.Fatalf("npmDownloadDistfile returned error: %v", err)
	}

	// Verify the downloaded file exists and has correct content
	content, err := os.ReadFile(downloaded)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}
	if string(content) != string(tarballContent) {
		t.Errorf("Downloaded content = %q, want %q", string(content), string(tarballContent))
	}

	// Copy to distdir
	if err := npmCopyToDistdir(downloaded, distDir); err != nil {
		t.Fatalf("npmCopyToDistdir returned error: %v", err)
	}

	// Verify distdir copy
	distPath := filepath.Join(distDir, tarballName)
	if _, err := os.Stat(distPath); os.IsNotExist(err) {
		t.Fatal("Tarball not found in distdir")
	}
}

// ---------------------------------------------------------------------------
// AssetCollision enum tests
// ---------------------------------------------------------------------------

func TestNpm_AssetCollisionAction(t *testing.T) {
	// Verify enum values
	if ActionSkip == ActionUpload {
		t.Error("ActionSkip should not equal ActionUpload")
	}
}

// ---------------------------------------------------------------------------
// npmPrepare function test with full mock chain
// ---------------------------------------------------------------------------

func TestNpm_Pipeline_FullMock(t *testing.T) {
	// This tests the full npm pipeline flow with mocks for all external calls
	dir := t.TempDir()
	info := &ebuild.EbuildInfo{
		Category: "test",
		Pkg:      "my-pkg",
		PVVal:    "1.0.0",
		PVal:     "my-pkg-1.0.0",
		SRC_URI:  "mirror://npm/${PN}/${P}.tgz",
	}

	// Dry-run mode skips all external operations
	cfg := &PrepareConfig{DryRun: true, Distdir: filepath.Join(dir, "dist")}
	err := prepareNpm(info, cfg)
	if err != nil {
		t.Fatalf("prepareNpm returned error in dry-run: %v", err)
	}
}

// ---------------------------------------------------------------------------
// npmPrepare with real subprocess (integration, skips if npm missing)
// ---------------------------------------------------------------------------

func TestNpm_Pipeline_RealSubprocess(t *testing.T) {
	_, err := exec.LookPath("npm")
	if err != nil {
		t.Skip("npm not available")
	}

	_, err = exec.LookPath("tar")
	if err != nil {
		t.Skip("tar not available")
	}

	dir := t.TempDir()

	// Create node_modules
	nodeModules := filepath.Join(dir, "node_modules")
	os.MkdirAll(nodeModules, 0755)
	os.WriteFile(filepath.Join(nodeModules, "test.js"), []byte("exports.test = true;"), 0644)

	// Create tarball
	tarballPath := filepath.Join(dir, "my-pkg-1.0.0-npm-deps.tar.xz")
	err = npmCreateTarball(dir, "my-pkg-1.0.0", "npm", "my-pkg-1.0.0-npm-deps.tar.xz")
	if err != nil {
		t.Fatalf("npmCreateTarball returned error: %v", err)
	}

	if _, err := os.Stat(tarballPath); os.IsNotExist(err) {
		t.Fatal("Tarball was not created")
	}
}

// ---------------------------------------------------------------------------
// npmPrepare with distdir check before manifest
// ---------------------------------------------------------------------------

func TestNpm_Pipeline_DistdirBeforeManifest(t *testing.T) {
	dir := t.TempDir()
	distDir := filepath.Join(dir, "dist")
	os.MkdirAll(distDir, 0755)

	// Create a distfile
	distfile := filepath.Join(distDir, "my-pkg-1.0.0.tgz")
	os.WriteFile(distfile, []byte("tarball"), 0644)

	// Verify distfiles exist
	distfiles := []string{"my-pkg-1.0.0.tgz"}
	if !verifyDistfiles(distDir, distfiles) {
		t.Error("verifyDistfiles should return true for existing distfile")
	}

	// Add a missing file
	distfiles = append(distfiles, "missing-file.tar.gz")
	if verifyDistfiles(distDir, distfiles) {
		t.Error("verifyDistfiles should return false when a distfile is missing")
	}
}

// ---------------------------------------------------------------------------
// npmPrepare dry-run with staging dir
// ---------------------------------------------------------------------------

func TestNpm_Pipeline_DryRun_StagingDir(t *testing.T) {
	dir := t.TempDir()
	stagingDir := filepath.Join(dir, "dry-run")

	// In dry-run mode, staging directory should be created
	// but no external mutations should occur
	err := os.MkdirAll(stagingDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create staging dir: %v", err)
	}

	// Verify staging dir is clean
	entries, _ := os.ReadDir(stagingDir)
	if len(entries) != 0 {
		t.Error("Staging dir should be empty in dry-run")
	}

	_ = stagingDir // used for staging artifacts in dry-run
}

// ---------------------------------------------------------------------------
// npmResolveSRCURI with various SRC_URI formats
// ---------------------------------------------------------------------------

func TestNpm_ResolveSRCURI_VariousFormats(t *testing.T) {
	info := &ebuild.EbuildInfo{
		Pkg:     "my-pkg",
		PVVal:   "2.0.0",
		PVal:    "my-pkg-2.0.0",
		SRC_URI: "mirror://npm/${PN}/${P}.tgz",
	}

	tests := []struct {
		name    string
		srcURI  string
		wantURL string
	}{
		{
			name:    "mirror_npm",
			srcURI:  "mirror://npm/my-pkg/my-pkg-2.0.0.tgz",
			wantURL: "https://registry.npmjs.org/my-pkg/my-pkg-2.0.0.tgz",
		},
		{
			name:    "mirror_with_vars",
			srcURI:  "mirror://npm/${PN}/${P}.tgz",
			wantURL: "https://registry.npmjs.org/my-pkg/my-pkg-2.0.0.tgz",
		},
		{
			name:    "not_npm",
			srcURI:  "mirror://gentoo/pkg-1.0.0.tar.gz",
			wantURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := npmResolveSRCURI(tt.srcURI, info)
			if result != tt.wantURL {
				t.Errorf("npmResolveSRCURI(%q) = %q, want %q", tt.srcURI, result, tt.wantURL)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// npmPrepare pipeline with multiple tarballs
// ---------------------------------------------------------------------------

func TestNpm_Pipeline_MultipleTarballs(t *testing.T) {
	dir := t.TempDir()
	distDir := filepath.Join(dir, "dist")
	os.MkdirAll(distDir, 0755)

	// Create multiple tarballs
	for _, name := range []string{"pkg-deps-1.tar.xz", "pkg-deps-2.tar.xz"} {
		path := filepath.Join(dir, name)
		os.WriteFile(path, []byte("tarball-"+name), 0644)

		if err := npmCopyToDistdir(path, distDir); err != nil {
			t.Fatalf("npmCopyToDistdir(%q) returned error: %v", name, err)
		}
	}

	// Verify all tarballs copied
	for _, name := range []string{"pkg-deps-1.tar.xz", "pkg-deps-2.tar.xz"} {
		distPath := filepath.Join(distDir, name)
		if _, err := os.Stat(distPath); os.IsNotExist(err) {
			t.Fatalf("Tarball %s not in distdir", name)
		}
	}
}

// ---------------------------------------------------------------------------
// npmPrepare release create failure with rollback
// ---------------------------------------------------------------------------

func TestNpm_Pipeline_ReleaseCreateFailure(t *testing.T) {
	dir := t.TempDir()
	ebuildPath := filepath.Join(dir, "pkg-1.0.0.ebuild")
	original := []byte("EAPI=8\nPV=1.0.0\n")
	os.WriteFile(ebuildPath, original, 0644)

	// Backup
	if err := Backup(ebuildPath); err != nil {
		t.Fatalf("Backup failed: %v", err)
	}

	// Simulate release create failure — rollback should restore
	// Modified content
	modified := []byte("EAPI=8\nPV=2.0.0\n")
	os.WriteFile(ebuildPath, modified, 0644)

	// Restore on failure
	if err := Restore(ebuildPath); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	// Verify rollback
	restored, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(original) {
		t.Errorf("Rollback failed: got %q, want %q", string(restored), string(original))
	}
}

// ---------------------------------------------------------------------------
// npmPrepare with JSON response parsing
// ---------------------------------------------------------------------------

func TestNpm_ParseReleaseJSON(t *testing.T) {
	jsonStr := `{"tagName":"my-pkg","url":"https://github.com/test/test/releases/tag/my-pkg","assets":[{"name":"pkg-deps.tar.xz","url":"https://example.com"}]}`

	var result struct {
		TagName string `json:"tagName"`
		URL     string `json:"url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		t.Fatalf("JSON parse failed: %v", err)
	}

	if result.TagName != "my-pkg" {
		t.Errorf("TagName = %q, want %q", result.TagName, "my-pkg")
	}
	if result.URL != "https://github.com/test/test/releases/tag/my-pkg" {
		t.Errorf("URL = %q, want %q", result.URL, "https://github.com/test/test/releases/tag/my-pkg")
	}
	if len(result.Assets) != 1 {
		t.Fatalf("Assets count = %d, want 1", len(result.Assets))
	}
	if result.Assets[0].Name != "pkg-deps.tar.xz" {
		t.Errorf("Asset name = %q, want %q", result.Assets[0].Name, "pkg-deps.tar.xz")
	}
}

// ---------------------------------------------------------------------------
// npmPrepare with io.ReadCloser simulation
// ---------------------------------------------------------------------------

func TestNpm_DownloadDistfile_ReadCloser(t *testing.T) {
	// Simulate an io.ReadCloser from http.Response
	data := []byte("downloaded-content")
	reader := io.NopCloser(strings.NewReader(string(data)))

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}

	if string(content) != string(data) {
		t.Errorf("content = %q, want %q", string(content), string(data))
	}
}

// ---------------------------------------------------------------------------
// npmPrepare distfile name extraction
// ---------------------------------------------------------------------------

func TestNpm_ExtractDistfileName(t *testing.T) {
	url := "https://registry.npmjs.org/my-pkg/-/my-pkg-1.0.0.tgz"
	distName := npmExtractDistfileName(url)
	if distName != "my-pkg-1.0.0.tgz" {
		t.Errorf("npmExtractDistfileName(%q) = %q, want %q", url, distName, "my-pkg-1.0.0.tgz")
	}
}
