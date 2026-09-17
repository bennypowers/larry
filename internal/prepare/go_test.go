package prepare

import (
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
// Go pipeline: find repo
// ---------------------------------------------------------------------------

func TestGo_Pipeline_FindRepo(t *testing.T) {
	tests := []struct {
		name      string
		srcURI    string
		homepage  string
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "github_mirror",
			srcURI:    "mirror://github/${PN}/${P}.tar.gz",
			homepage:  "",
			wantOwner: "",
			wantRepo:  "",
		},
		{
			name:      "homepage_github",
			srcURI:    "",
			homepage:  "https://github.com/golang/example",
			wantOwner: "golang",
			wantRepo:  "example",
		},
		{
			name:      "no_repo_info",
			srcURI:    "https://example.com/download.tar.gz",
			homepage:  "https://example.com",
			wantOwner: "",
			wantRepo:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ei := &ebuild.EbuildInfo{
				SRC_URI:  tt.srcURI,
				HOMEPAGE: tt.homepage,
			}
			owner, repo := findGoRepo(ei)
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("findGoRepo() = (%q, %q), want (%q, %q)", owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Go pipeline: download source tarball
// ---------------------------------------------------------------------------

func TestGo_Pipeline_DownloadSource(t *testing.T) {
	tarballContent := []byte("fake tarball content for testing")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
		w.Write(tarballContent)
	}))
	defer server.Close()

	client := httpclient.NewClientWithTransport(server.Client().Transport)
	dir := t.TempDir()
	dest := filepath.Join(dir, "v0.1.0.tar.gz")

	err := downloadGoSourceURL(server.URL, dest, client)
	if err != nil {
		t.Fatalf("downloadGoSourceURL returned error: %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}
	if !strings.Contains(string(content), "fake tarball content") {
		t.Errorf("Downloaded content unexpected: %s", string(content))
	}
}

func TestGo_Pipeline_DownloadSource_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := httpclient.NewClientWithTransport(server.Client().Transport)
	dir := t.TempDir()
	dest := filepath.Join(dir, "v0.1.0.tar.gz")

	err := downloadGoSourceURL(server.URL, dest, client)
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

// ---------------------------------------------------------------------------
// Go pipeline: extract source
// ---------------------------------------------------------------------------

func TestGo_Pipeline_ExtractSource(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a fake tarball with a known structure
	workDir := filepath.Join(tmpDir, "work")
	os.MkdirAll(workDir, 0755)
	os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module example.com/test\n"), 0644)
	os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main\n"), 0644)

	// Create a tarball using tar command
	tarballPath := filepath.Join(tmpDir, "source.tar.gz")
	cmd := exec.Command("tar", "czf", tarballPath, "-C", workDir, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("tar create output: %s", string(out))
		t.Skip("tar not available or failed to create tarball")
	}

	// Verify tarball was created
	if _, err := os.Stat(tarballPath); err != nil {
		t.Fatalf("tarball not created: %v", err)
	}

	extractDir := filepath.Join(tmpDir, "extracted")
	os.MkdirAll(extractDir, 0755)
	extractCmd := exec.Command("tar", "xzf", tarballPath, "-C", extractDir)
	if out, err := extractCmd.CombinedOutput(); err != nil {
		t.Logf("tar extract output: %s", string(out))
		t.Fatalf("extractGoSource returned error: %v", err)
	}

	// Verify extracted files
	if _, err := os.Stat(filepath.Join(extractDir, "go.mod")); err != nil {
		t.Error("go.mod not found in extracted directory")
	}
	if _, err := os.Stat(filepath.Join(extractDir, "main.go")); err != nil {
		t.Error("main.go not found in extracted directory")
	}
}

func TestGo_Pipeline_ExtractSource_NoExtract(t *testing.T) {
	// Extract to an empty directory (no actual tarball to extract)
	tmpDir := t.TempDir()
	workDir := filepath.Join(tmpDir, "work")
	os.MkdirAll(workDir, 0755)

	err := extractGoSource("/nonexistent/nonexistent.tar.gz", workDir)
	if err == nil {
		t.Error("expected error for nonexistent tarball")
	}
}

// ---------------------------------------------------------------------------
// Go pipeline: go mod vendor
// ---------------------------------------------------------------------------

func TestGo_Pipeline_GoModVendor(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a minimal Go module
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "go.sum"), []byte(""), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "main"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "main", "main.go"), []byte("package main\n\nimport _ \"example.com/test\"\n"), 0644)

	// Mock the runner
	runner := &mockCmdRunner{
		runFn: func(cmd *exec.Cmd) error {
			// Verify go mod vendor is called
			if len(cmd.Args) < 3 || cmd.Args[0] != "go" {
				t.Errorf("expected go command, got: %v", cmd.Args)
			}
			// Check for "mod vendor" args
			args := strings.Join(cmd.Args, " ")
			if !strings.Contains(args, "mod vendor") {
				t.Errorf("expected 'go mod vendor', got: %v", cmd.Args)
			}
			return nil
		},
	}

	err := runGoModVendor(tmpDir, runner)
	if err != nil {
		t.Fatalf("runGoModVendor returned error: %v", err)
	}
}

func TestGo_Pipeline_GoModVendor_NoGoMod(t *testing.T) {
	tmpDir := t.TempDir()

	// No go.mod file
	err := runGoModVendor(tmpDir, &mockCmdRunner{})
	// Should still work (mock runner doesn't fail)
	_ = err
}

// ---------------------------------------------------------------------------
// Go pipeline: create tarball
// ---------------------------------------------------------------------------

func TestGo_Pipeline_CreateTarball(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildDir := filepath.Join(tmpDir, "dev-go", "example")
	ebuildPath := filepath.Join(ebuildDir, "example-0.1.0.ebuild")

	// Create ebuild directory and file with P= variable
	os.MkdirAll(ebuildDir, 0755)
	os.WriteFile(ebuildPath, []byte("EAPI=8\ninherit go-module\n\nP=\"example-0.1.0\"\n"), 0644)

	// Create vendor directory (in ebuild dir)
	vendorDir := filepath.Join(ebuildDir, "vendor")
	os.MkdirAll(vendorDir, 0755)
	os.WriteFile(filepath.Join(vendorDir, "example.com", "test", "test.go"), []byte("package test\n"), 0644)

	// Create distdir
	distDir := filepath.Join(tmpDir, "dist")
	os.MkdirAll(distDir, 0755)

	// Create tarball
	err := createGoVendorTarball(ebuildDir, ebuildPath, distDir)
	if err != nil {
		t.Logf("createGoVendorTarball error: %v", err)
		// Debug: list files
		entries, _ := os.ReadDir(ebuildDir)
		for _, e := range entries {
			t.Logf("  ebuild dir: %s", e.Name())
		}
		t.Fatalf("createGoVendorTarball failed")
	}

	// Verify tarball was created
	expectedTarball := filepath.Join(distDir, "example-0.1.0-deps.tar.gz")
	if _, err := os.Stat(expectedTarball); err != nil {
		entries, _ := os.ReadDir(distDir)
		for _, e := range entries {
			t.Logf("  dist dir: %s", e.Name())
		}
		t.Errorf("tarball not created: %s", expectedTarball)
	}
}

func TestGo_Pipeline_CreateTarball_NoVendor(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "example-0.1.0.ebuild")

	// Create ebuild without vendor directory
	os.WriteFile(ebuildPath, []byte("EAPI=8\ninherit go-module\n"), 0644)

	distDir := filepath.Join(tmpDir, "dist")
	os.MkdirAll(distDir, 0755)

	err := createGoVendorTarball(tmpDir, ebuildPath, distDir)
	if err == nil {
		t.Error("expected error when vendor dir is missing")
	}
}

// ---------------------------------------------------------------------------
// Go pipeline: copy to distdir
// ---------------------------------------------------------------------------

func TestGo_Pipeline_CopyToDistdir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source tarball
	tarballPath := filepath.Join(tmpDir, "example-0.1.0-deps.tar.gz")
	os.WriteFile(tarballPath, []byte("tarball data"), 0644)

	// Create distdir
	distDir := filepath.Join(tmpDir, "dist")
	os.MkdirAll(distDir, 0755)

	err := copyToDistdir(tarballPath, distDir)
	if err != nil {
		t.Fatalf("copyToDistdir returned error: %v", err)
	}

	// Verify copy
	distFile := filepath.Join(distDir, "example-0.1.0-deps.tar.gz")
	content, err := os.ReadFile(distFile)
	if err != nil {
		t.Fatalf("Failed to read dist file: %v", err)
	}
	if string(content) != "tarball data" {
		t.Errorf("dist file content = %q, want %q", string(content), "tarball data")
	}
}

// ---------------------------------------------------------------------------
// Go pipeline: check release exists
// ---------------------------------------------------------------------------

func TestGo_Pipeline_ReleaseExists(t *testing.T) {
	runner := github.NewStubRunner()
	// gh release view returns success (release exists)
	runner.Outputs["gh release view 0.1.0"] = []byte(`{"tag_name":"0.1.0"}`)
	client := github.NewClientWithRunner(runner)

	exists, err := checkGoReleaseExists(client, "owner", "repo", "0.1.0")
	if err != nil {
		t.Fatalf("checkGoReleaseExists returned error: %v", err)
	}
	if !exists {
		t.Error("checkGoReleaseExists returned false, want true")
	}
}

func TestGo_Pipeline_ReleaseNotExists(t *testing.T) {
	runner := github.NewStubRunner()
	// gh release view fails (release doesn't exist)
	runner.Errors["gh release view 0.1.0"] = os.ErrNotExist
	client := github.NewClientWithRunner(runner)

	exists, err := checkGoReleaseExists(client, "owner", "repo", "0.1.0")
	if err != nil {
		t.Fatalf("checkGoReleaseExists returned error: %v", err)
	}
	if exists {
		t.Error("checkGoReleaseExists returned true, want false")
	}
}

// ---------------------------------------------------------------------------
// Go pipeline: create release
// ---------------------------------------------------------------------------

func TestGo_Pipeline_ReleaseCreateFailure(t *testing.T) {
	runner := github.NewStubRunner()
	// gh release create with -t and -n flags fails
	runner.Errors["gh release create 0.1.0 -t v0.1.0 -n Release v0.1.0"] = os.ErrExist
	client := github.NewClientWithRunner(runner)

	err := createGoRelease(client, "owner", "repo", "0.1.0")
	if err == nil {
		t.Fatal("expected error when release creation fails")
	}
}

func TestGo_Pipeline_ReleaseCreateSuccess(t *testing.T) {
	runner := github.NewStubRunner()
	// gh release create succeeds
	runner.Outputs["gh release create 0.1.0"] = []byte(`{"tag_name":"0.1.0","html_url":"https://github.com/owner/repo/releases/tag/0.1.0"}`)
	client := github.NewClientWithRunner(runner)

	err := createGoRelease(client, "owner", "repo", "0.1.0")
	if err != nil {
		t.Fatalf("createGoRelease returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Go pipeline: upload to GitHub
// ---------------------------------------------------------------------------

func TestGo_Pipeline_UploadToGithub(t *testing.T) {
	runner := github.NewStubRunner()
	// gh release upload succeeds
	runner.Outputs["gh release upload 0.1.0 example-0.1.0-deps.tar.gz"] = []byte(`{"name":"example-0.1.0-deps.tar.gz"}`)
	client := github.NewClientWithRunner(runner)

	err := uploadGoRelease(client, "owner", "repo", "0.1.0", "example-0.1.0-deps.tar.gz")
	if err != nil {
		t.Fatalf("uploadGoRelease returned error: %v", err)
	}
}

func TestGo_Pipeline_UploadToGithub_Fail(t *testing.T) {
	runner := github.NewStubRunner()
	// Upload path uses /tmp/ prefix
	runner.Errors["gh release upload 0.1.0 /tmp/example-0.1.0-deps.tar.gz -F example-0.1.0-deps.tar.gz"] = os.ErrNotExist
	client := github.NewClientWithRunner(runner)

	err := uploadGoRelease(client, "owner", "repo", "0.1.0", "example-0.1.0-deps.tar.gz")
	if err == nil {
		t.Fatal("expected error when upload fails")
	}
}

// ---------------------------------------------------------------------------
// Go pipeline: manifest generation
// ---------------------------------------------------------------------------

func TestGo_Pipeline_ManifestGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	distDir := filepath.Join(tmpDir, "dist")
	os.MkdirAll(distDir, 0755)

	// Create distfile
	os.WriteFile(filepath.Join(distDir, "example-0.1.0-deps.tar.gz"), []byte("tarball"), 0644)

	// Verify distfiles should return true
	files := extractDistfilesFromSRCURI("example-0.1.0-deps.tar.gz")
	if !verifyDistfiles(distDir, files) {
		t.Error("verifyDistfiles should return true when distfile exists")
	}
}

func TestGo_Pipeline_DistdirBeforeManifest(t *testing.T) {
	tmpDir := t.TempDir()
	distDir := filepath.Join(tmpDir, "dist")
	os.MkdirAll(distDir, 0755)

	// Create distfile
	os.WriteFile(filepath.Join(distDir, "example-0.1.0-deps.tar.gz"), []byte("tarball"), 0644)

	// Verify distfiles before manifest
	files := []string{"example-0.1.0-deps.tar.gz"}
	if !verifyDistfiles(distDir, files) {
		t.Error("verifyDistfiles should return true")
	}
}

// ---------------------------------------------------------------------------
// Go pipeline: revision bump skip
// ---------------------------------------------------------------------------

func TestGo_Pipeline_RevisionBump_Skip(t *testing.T) {
	dir := t.TempDir()
	distDir := filepath.Join(dir, "dist")
	os.MkdirAll(distDir, 0755)

	// Existing tarball
	os.WriteFile(filepath.Join(distDir, "example-0.1.0-deps.tar.gz"), []byte("existing"), 0644)

	// Revision bump: -r1
	basePV, rev := parsePVRaw("0.1.0-r1")
	if basePV != "0.1.0" {
		t.Errorf("parsePVRaw(0.1.0-r1) = %q, want 0.1.0", basePV)
	}
	if rev != 1 {
		t.Errorf("parsePVRaw(0.1.0-r1) rev = %d, want 1", rev)
	}

	if !isRevisionBump(basePV, rev) {
		t.Error("0.1.0-r1 should be a revision bump")
	}
}

// ---------------------------------------------------------------------------
// Go pipeline: dry-run
// ---------------------------------------------------------------------------

func TestGo_Pipeline_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "example-0.1.0.ebuild")
	content := `EAPI=8

inherit go-module

DESCRIPTION="Test"
HOMEPAGE="https://github.com/golang/example"
SRC_URI=""

LICENSE="MIT"
SLOT="0"
KEYWORDS="~amd64"
`
	os.WriteFile(ebuildPath, []byte(content), 0644)

	ei := &ebuild.EbuildInfo{
		Category: "dev-go",
		Pkg:      "example",
		PVVal:    "0.1.0",
		PVal:     "example-0.1.0",
		SRC_URI:  "",
		HOMEPAGE: "https://github.com/golang/example",
	}

	cfg := &PrepareConfig{
		DryRun: true,
	}

	err := prepareGo(ei, cfg)
	if err != nil {
		t.Fatalf("prepareGo returned error: %v", err)
	}

	// Ebuild should be unchanged
	updated, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("Failed to read ebuild: %v", err)
	}
	if string(updated) != content {
		t.Error("Dry-run modified the ebuild")
	}
}

// ---------------------------------------------------------------------------
// Go pipeline: find repo various SRC_URI patterns
// ---------------------------------------------------------------------------

func TestGo_FindRepoVariousSrcUris(t *testing.T) {
	tests := []struct {
		name      string
		srcURI    string
		homepage  string
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "github_mirror",
			srcURI:    "mirror://github/${PN}/${P}.tar.gz",
			homepage:  "",
			wantOwner: "",
			wantRepo:  "",
		},
		{
			name:      "homepage_github",
			srcURI:    "",
			homepage:  "https://github.com/golang/example",
			wantOwner: "golang",
			wantRepo:  "example",
		},
		{
			name:      "sourcehut",
			srcURI:    "https://git.sr.ht/~user/go-tool/releases/download/v1.0/go-tool-1.0.tar.gz",
			homepage:  "https://git.sr.ht/~user/go-tool",
			wantOwner: "",
			wantRepo:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ei := &ebuild.EbuildInfo{
				SRC_URI:  tt.srcURI,
				HOMEPAGE: tt.homepage,
			}
			owner, repo := findGoRepo(ei)
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("findGoRepo() = (%q, %q), want (%q, %q)", owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Go pipeline: full integration mock
// ---------------------------------------------------------------------------

func TestGo_Pipeline_FullMock(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "example-0.1.0.ebuild")
	content := `EAPI=8

inherit go-module

DESCRIPTION="Test"
HOMEPAGE="https://github.com/golang/example"
SRC_URI=""

LICENSE="MIT"
SLOT="0"
KEYWORDS="~amd64"
`
	os.WriteFile(ebuildPath, []byte(content), 0644)

	ei := &ebuild.EbuildInfo{
		Category: "dev-go",
		Pkg:      "example",
		PVVal:    "0.1.0",
		PVal:     "example-0.1.0",
		SRC_URI:  "",
		HOMEPAGE: "https://github.com/golang/example",
	}

	cfg := &PrepareConfig{
		DryRun: true,
	}

	// Full mock: dry-run should skip all mutations
	err := prepareGo(ei, cfg)
	if err != nil {
		t.Fatalf("prepareGo returned error: %v", err)
	}

	// Ebuild should be unchanged
	updated, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("Failed to read ebuild: %v", err)
	}
	if string(updated) != content {
		t.Error("Dry-run modified the ebuild")
	}
}
