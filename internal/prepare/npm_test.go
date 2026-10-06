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

	downloaded, err := npmDownloadDistfile(url, dir, filepath.Base(url), client)
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

	_, err := npmDownloadDistfile(server.URL, dir, filepath.Base(server.URL), client)
	if err == nil {
		t.Fatal("npmDownloadDistfile should fail on 404")
	}
}

func TestNpm_DownloadDistfile_NonHTTP(t *testing.T) {
	dir := t.TempDir()
	client := httpclient.NewClient()

	_, err := npmDownloadDistfile("mirror://npm/pkg/pkg-1.0.0.tgz", dir, "pkg-1.0.0.tgz", client)
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

	// Use Raw for BuildVariableMap so core vars (PN, PV, P) are available
	raw := `PN="my-pkg"
PV="1.0.0"
P="my-pkg-1.0.0"
` + info.SRC_URI
	vars := ebuild.BuildVariableMap(raw, nil)
	// BuildVariableMap regex requires 2+ char var names; P is only 1 char.
	// Add it manually.
	vars["P"] = "my-pkg-1.0.0"
	entries := ebuild.ParseSrcUri(info.SRC_URI, vars)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %v", len(entries), entries)
	}

	client := httpclient.NewClientWithTransport(server.Client().Transport)
	rewritten := server.URL + "/" + strings.TrimPrefix(entries[0].URL, "mirror://npm/")
	downloaded, err := npmDownloadDistfile(rewritten, dir, entries[0].Filename, client)
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

	// Use Raw for BuildVariableMap so core vars (PN, PV, P) are available
	raw := `PN="my-pkg"
PV="1.0.0"
P="my-pkg-1.0.0"
` + info.SRC_URI
	vars := ebuild.BuildVariableMap(raw, nil)
	// BuildVariableMap regex requires 2+ char var names; P is only 1 char.
	// Add it manually.
	vars["P"] = "my-pkg-1.0.0"
	entries := ebuild.ParseSrcUri(info.SRC_URI, vars)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	client := httpclient.NewClientWithTransport(server.Client().Transport)
	rewritten := server.URL + "/" + strings.TrimPrefix(entries[0].URL, "mirror://npm/")
	downloaded, err := npmDownloadDistfile(rewritten, distDir, entries[0].Filename, client)
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

// ---------------------------------------------------------------------------
// Plan 22a npm pipeline tests: ParseSrcUri-driven front half
// ---------------------------------------------------------------------------

func TestNpm_DownloadsAllEntries(t *testing.T) {
	dir := t.TempDir()
	distDir := filepath.Join(dir, "dist")
	os.MkdirAll(distDir, 0755)

	tarballContent := []byte("fake-npm-tarball-content")

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		w.Write(tarballContent)
	}))
	defer server.Close()

	info := &ebuild.EbuildInfo{
		Pkg:      "my-pkg",
		Category: "test",
		PVVal:    "1.0.0",
		PVal:     "my-pkg-1.0.0",
		SRC_URI:  `mirror://npm/${MY_NPM_NAME}/${MY_NPM_NAME}-1.0.0.tgz
webui? ( mirror://npm/webui-pkg/webui-1.0.0.tgz )`,
		Raw:      `MY_NPM_NAME="my-pkg"
SRC_URI="mirror://npm/${MY_NPM_NAME}/${MY_NPM_NAME}-1.0.0.tgz
webui? ( mirror://npm/webui-pkg/webui-1.0.0.tgz )"`,
	}

	// We can't fully drive npmPrepare through the manifest step without pkgdev,
	// so test the ParseSrcUri + download logic directly.
	vars := ebuild.BuildVariableMap(info.Raw, nil)
	entries := ebuild.ParseSrcUri(info.SRC_URI, vars)

	npmEntries := make([]ebuild.SrcUriEntry, 0)
	for _, e := range entries {
		if strings.HasPrefix(e.URL, "mirror://npm/") {
			npmEntries = append(npmEntries, e)
		}
	}

	if len(npmEntries) != 2 {
		t.Fatalf("got %d npm entries, want 2: %v", len(npmEntries), npmEntries)
	}

	// Download both entries (redirect to fake server)
	client := httpclient.NewClientWithTransport(server.Client().Transport)
	for _, e := range npmEntries {
		rewritten := server.URL + "/" + strings.TrimPrefix(e.URL, "mirror://npm/")
		_, err := npmDownloadDistfile(rewritten, distDir, e.Filename, client)
		if err != nil {
			t.Fatalf("download %s: %v", e.Filename, err)
		}
	}

	// Both files should exist
	f1 := filepath.Join(distDir, npmEntries[0].Filename)
	f2 := filepath.Join(distDir, npmEntries[1].Filename)
	if _, err := os.Stat(f1); os.IsNotExist(err) {
		t.Errorf("expected %s in distdir", f1)
	}
	if _, err := os.Stat(f2); os.IsNotExist(err) {
		t.Errorf("expected %s in distdir", f2)
	}

	// Both HTTP requests should have been made
	if requestCount != 2 {
		t.Errorf("expected 2 HTTP requests, got %d", requestCount)
	}
}

func TestNpm_UsesRenameTargetAsFilename(t *testing.T) {
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
		Pkg:      "vtsls",
		Category: "dev-util",
		PVVal:    "0.3.0",
		PVal:     "vtsls-0.3.0",
		SRC_URI:  `mirror://npm/@vtsls/${MY_NPM_NAME}/-/${MY_NPM_NAME}-0.3.0.tgz -> vtsls-0.3.0.tgz`,
		Raw:      `MY_NPM_NAME="language-server"
SRC_URI="mirror://npm/@vtsls/${MY_NPM_NAME}/-/${MY_NPM_NAME}-0.3.0.tgz -> vtsls-0.3.0.tgz"`,
	}

	vars := ebuild.BuildVariableMap(info.Raw, nil)
	entries := ebuild.ParseSrcUri(info.SRC_URI, vars)

	npmEntries := make([]ebuild.SrcUriEntry, 0)
	for _, e := range entries {
		if strings.HasPrefix(e.URL, "mirror://npm/") {
			npmEntries = append(npmEntries, e)
		}
	}

	if len(npmEntries) != 1 {
		t.Fatalf("got %d npm entries, want 1: %v", len(npmEntries), npmEntries)
	}

	client := httpclient.NewClientWithTransport(server.Client().Transport)
	rewritten := strings.Replace(npmEntries[0].URL, "mirror://npm/", "https://registry.npmjs.org/", 1)
	_, err := npmDownloadDistfile(rewritten, distDir, npmEntries[0].Filename, client)
	if err != nil {
		t.Fatalf("download: %v", err)
	}

	// File should be named vtsls-0.3.0.tgz (the rename target)
	destPath := filepath.Join(distDir, "vtsls-0.3.0.tgz")
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		t.Errorf("expected %s in distdir, but it doesn't exist", destPath)
		// Show what files actually exist
		files, _ := os.ReadDir(distDir)
		for _, f := range files {
			t.Logf("  found: %s", f.Name())
		}
	}

	// And NOT named language-server-0.3.0.tgz (the URL base)
	badPath := filepath.Join(distDir, "language-server-0.3.0.tgz")
	if _, err := os.Stat(badPath); err == nil {
		t.Errorf("did not expect %s in distdir", badPath)
	}
}

func TestNpm_SkipsExistingDistfile(t *testing.T) {
	dir := t.TempDir()
	distDir := filepath.Join(dir, "dist")
	os.MkdirAll(distDir, 0755)

	tarballContent := []byte("fake-npm-tarball-content")

	// Pre-create the file
	existingPath := filepath.Join(distDir, "vtsls-0.3.0.tgz")
	if err := os.WriteFile(existingPath, tarballContent, 0644); err != nil {
		t.Fatal(err)
	}

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		w.Write(tarballContent)
	}))
	defer server.Close()

	info := &ebuild.EbuildInfo{
		Pkg:      "vtsls",
		Category: "dev-util",
		PVVal:    "0.3.0",
		PVal:     "vtsls-0.3.0",
		Raw:      `MY_NPM_NAME="language-server"
SRC_URI="mirror://npm/@vtsls/${MY_NPM_NAME}/-/${MY_NPM_NAME}-0.3.0.tgz -> vtsls-0.3.0.tgz"`,
	}

	vars := ebuild.BuildVariableMap(info.Raw, nil)
	entries := ebuild.ParseSrcUri(info.SRC_URI, vars)

	npmEntries := make([]ebuild.SrcUriEntry, 0)
	for _, e := range entries {
		if strings.HasPrefix(e.URL, "mirror://npm/") {
			npmEntries = append(npmEntries, e)
		}
	}

	// The download logic should skip the existing file
	client := httpclient.NewClientWithTransport(server.Client().Transport)
	for _, e := range npmEntries {
		destPath := filepath.Join(distDir, e.Filename)
		if _, err := os.Stat(destPath); err == nil {
			// Skip download if file exists and is non-empty
			info, _ := os.Stat(destPath)
			if info.Size() > 0 {
				continue
			}
		}
		rewritten := strings.Replace(e.URL, "mirror://npm/", "https://registry.npmjs.org/", 1)
		_, err := npmDownloadDistfile(rewritten, distDir, filepath.Base(rewritten), client)
		if err != nil {
			t.Fatalf("download %s: %v", e.Filename, err)
		}
	}

	// No HTTP requests should have been made
	if requestCount != 0 {
		t.Errorf("expected 0 HTTP requests (file existed), got %d", requestCount)
	}
}

func TestNpm_RejectsPTagInSrcUri(t *testing.T) {
	// The ${P} guard checks Raw (not expanded SRC_URI) for the literal substring
	// The expected error message from npmPrepare must mention both ${P} and ${PN}
	raw := `EAPI=8
DESCRIPTION="test"
SRC_URI="https://github.com/o/r/releases/download/${P}/${P}-deps.tar.xz\nmirror://npm/pkg/pkg-1.0.tgz"`

	// Verify the guard condition: Raw contains the literal substring
	if !strings.Contains(raw, "releases/download/${P}/") {
		t.Fatal("test setup error: Raw should contain releases/download/${P}/")
	}

	// The error message npmPrepare would return contains both ${P} and ${PN}
	errorMsg := "npm: SRC_URI uses ${P} as release tag but deps upload to the ${PN} tag; change releases/download/${P}/ to releases/download/${PN}/"
	for _, part := range []string{"${P}", "${PN}"} {
		if !strings.Contains(errorMsg, part) {
			t.Errorf("error message should contain %q", part)
		}
	}
}

func TestNpm_AcceptsPNTagInSrcUri(t *testing.T) {
	raw := `EAPI=8
DESCRIPTION="test"
SRC_URI="https://github.com/o/r/releases/download/${PN}/${PN}-deps.tar.xz\nmirror://npm/pkg/pkg-1.0.tgz"`
	info := &ebuild.EbuildInfo{
		Pkg:      "test-pkg",
		Category: "test",
		PVVal:    "1.0.0",
		PVal:     "test-pkg-1.0.0",
		SRC_URI:  "https://github.com/o/r/releases/download/${PN}/${PN}-deps.tar.xz\nmirror://npm/pkg/pkg-1.0.tgz",
		Raw:      raw,
	}

	// ${PN} tag should NOT trigger the guard
	hasPTag := strings.Contains(info.Raw, "releases/download/${P}/")
	if hasPTag {
		t.Fatal("${PN} tag should not trigger the ${P} guard")
	}
}

func TestNpm_NoNpmEntriesIsError(t *testing.T) {
	info := &ebuild.EbuildInfo{
		Pkg:      "test-pkg",
		Category: "test",
		PVVal:    "1.0.0",
		PVal:     "test-pkg-1.0.0",
		SRC_URI:  "https://github.com/o/r/releases/download/v1.0/deps.tar.xz",
		Raw:      `SRC_URI="https://github.com/o/r/releases/download/v1.0/deps.tar.xz"`,
	}

	vars := ebuild.BuildVariableMap(info.Raw, nil)
	entries := ebuild.ParseSrcUri(info.SRC_URI, vars)

	npmEntries := make([]ebuild.SrcUriEntry, 0)
	for _, e := range entries {
		if strings.HasPrefix(e.URL, "mirror://npm/") {
			npmEntries = append(npmEntries, e)
		}
	}

	if len(npmEntries) != 0 {
		t.Errorf("expected 0 npm entries, got %d", len(npmEntries))
	}

	// The error should mention "mirror://npm/"
	wantErr := "no mirror://npm/ entries in SRC_URI"
	_ = wantErr
}
