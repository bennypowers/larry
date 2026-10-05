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
	"bennypowers.dev/larry/internal/httpclient"
)

// ---------------------------------------------------------------------------
// npmResolveSRCURI tests
// npmDownloadDistfile tests
// ---------------------------------------------------------------------------

func TestNpm_DownloadDistfile(t *testing.T) {
	dir := t.TempDir()

	tarballName := "my-pkg-1.0.0.tgz"
	tarballContent := []byte("FAKE_TARBALL_CONTENT_FOR_TESTING")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
		w.Write(tarballContent)
	}))
	defer server.Close()

	url := server.URL + "/" + tarballName

	client := httpclient.NewClientWithTransport(server.Client().Transport)

	downloaded, err := npmDownloadDistfile(url, dir, client)
	if err != nil {
		t.Fatalf("npmDownloadDistfile returned error: %v", err)
	}

	expectedPath := filepath.Join(dir, tarballName)
	if downloaded != expectedPath {
		t.Errorf("npmDownloadDistfile returned path %q, want %q", downloaded, expectedPath)
	}

	content, err := os.ReadFile(downloaded)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}
	if string(content) != string(tarballContent) {
		t.Errorf("Downloaded content = %q, want %q", string(content), string(tarballContent))
	}
}

// ---------------------------------------------------------------------------
// mockCmdRunner — shared test helper implementing github.CmdRunner
// ---------------------------------------------------------------------------

type mockCmdRunner struct {
	runFn    func(cmd *exec.Cmd) error
	outputFn func(cmd *exec.Cmd) ([]byte, error)
}

func (m *mockCmdRunner) Run(cmd *exec.Cmd) error {
	if m.runFn != nil {
		return m.runFn(cmd)
	}
	return nil
}

func (m *mockCmdRunner) Output(cmd *exec.Cmd) ([]byte, error) {
	if m.outputFn != nil {
		return m.outputFn(cmd)
	}
	return nil, nil
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
// npmCopyToDistdir tests
// ---------------------------------------------------------------------------

func TestNpm_CopyToDistdir(t *testing.T) {
	srcDir := t.TempDir()
	distDir := t.TempDir()

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

func TestNpm_CopyToDistdir_EmptyDistdir(t *testing.T) {
	// Empty distdir should be a no-op
	err := npmCopyToDistdir("/some/file.tar", "")
	if err != nil {
		t.Errorf("npmCopyToDistdir with empty distdir should be no-op, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// extractVar tests
// npmExtractDistfileName tests
// ---------------------------------------------------------------------------

func TestNpm_ExtractDistfileName(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "scoped_package",
			url:  "https://registry.npmjs.org/@scope/pkg/-/pkg-1.0.0.tgz",
			want: "pkg-1.0.0.tgz",
		},
		{
			name: "regular_package",
			url:  "https://registry.npmjs.org/pkg/1.0.0/pkg-1.0.0.tgz",
			want: "pkg-1.0.0.tgz",
		},
		{
			name: "no_match",
			url:  "https://example.com/file.tar.gz",
			want: "file.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := npmExtractDistfileName(tt.url)
			if result != tt.want {
				t.Errorf("npmExtractDistfileName(%q) = %q, want %q", tt.url, result, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// npmDryRun tests
// ---------------------------------------------------------------------------

func TestNpm_DryRun_SkipsAll(t *testing.T) {
	_ = t.TempDir()
	info := &ebuild.EbuildInfo{
		Category: "test",
		Pkg:      "my-pkg",
		PVVal:    "1.0.0",
		PVal:     "my-pkg-1.0.0",
		SRC_URI:  "mirror://npm/${PN}/${P}.tgz",
	}

	cfg := &PrepareConfig{DryRun: true}
	err := npmPrepare(info, cfg)
	if err != nil {
		t.Fatalf("npmPrepare dry-run returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// npmPrepare pipeline integration
// ---------------------------------------------------------------------------

func TestNpm_Pipeline_DownloadAndCopy(t *testing.T) {
	dir := t.TempDir()
	distDir := filepath.Join(dir, "dist")
	os.MkdirAll(distDir, 0755)

	tarballContent := []byte("fake-npm-tarball-content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(tarballContent)
	}))
	defer server.Close()

	info := &ebuild.EbuildInfo{
		Pkg:     "my-pkg",
		PVVal:   "1.0.0",
		PVal:    "my-pkg-1.0.0",
		SRC_URI: "mirror://npm/${PN}/${P}.tgz",
	}

	resolvedURL := npmFirstRegistryURL(info)
	resolvedURL = strings.Replace(resolvedURL, "https://registry.npmjs.org", server.URL, 1)

	client := httpclient.NewClientWithTransport(server.Client().Transport)
	downloaded, err := npmDownloadDistfile(resolvedURL, dir, client)
	if err != nil {
		t.Fatalf("npmDownloadDistfile returned error: %v", err)
	}

	if err := npmCopyToDistdir(downloaded, distDir); err != nil {
		t.Fatalf("npmCopyToDistdir returned error: %v", err)
	}

	distPath := filepath.Join(distDir, "my-pkg-1.0.0.tgz")
	if _, err := os.Stat(distPath); os.IsNotExist(err) {
		t.Fatal("Tarball not found in distdir")
	}
}

// ---------------------------------------------------------------------------
// npmPrepare pipeline with download-to-distdir (no copy needed)
// ---------------------------------------------------------------------------

func TestNpm_Pipeline_DownloadToDistdir(t *testing.T) {
	dir := t.TempDir()
	distDir := filepath.Join(dir, "dist")
	os.MkdirAll(distDir, 0755)
	_ = dir // used by distDir

	tarballContent := []byte("fake-npm-tarball-content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(tarballContent)
	}))
	defer server.Close()

	info := &ebuild.EbuildInfo{
		Pkg:     "my-pkg",
		PVVal:   "1.0.0",
		PVal:    "my-pkg-1.0.0",
		SRC_URI: "mirror://npm/${PN}/${P}.tgz",
	}

	resolvedURL := npmFirstRegistryURL(info)
	resolvedURL = strings.Replace(resolvedURL, "https://registry.npmjs.org", server.URL, 1)

	client := httpclient.NewClientWithTransport(server.Client().Transport)
	downloaded, err := npmDownloadDistfile(resolvedURL, distDir, client)
	if err != nil {
		t.Fatalf("npmDownloadDistfile returned error: %v", err)
	}

	// Verify the file was downloaded directly to distdir (no copy needed)
	content, err := os.ReadFile(downloaded)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}
	if string(content) != string(tarballContent) {
		t.Errorf("Downloaded content = %q, want %q", string(content), string(tarballContent))
	}
}
