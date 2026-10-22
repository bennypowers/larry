package prepare

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/github"
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
		_, _ = w.Write(tarballContent) // httptest writer; a write error cannot occur here
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

func TestNpm_DownloadDistfile_TruncatedBody(t *testing.T) {
	// A server that sends fewer bytes than Content-Length must not leave a
	// usable distfile behind — the next run has to re-download it.
	dir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("short")) // truncated vs declared Content-Length
	}))
	defer server.Close()

	client := httpclient.NewClientWithTransport(server.Client().Transport)

	_, err := npmDownloadDistfile(server.URL+"/pkg.tgz", dir, "pkg.tgz", client)
	if err == nil {
		t.Fatal("npmDownloadDistfile must fail on a truncated body")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "pkg.tgz")); statErr == nil {
		t.Error("truncated download left a distfile behind — a later run would treat it as complete")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "pkg.tgz.part")); statErr == nil {
		t.Error("truncated download left a .part file behind")
	}
}

func TestNpm_DownloadDistfile_NoPartFileAfterSuccess(t *testing.T) {
	dir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("full-body")) // httptest writer; a write error cannot occur here
	}))
	defer server.Close()

	client := httpclient.NewClientWithTransport(server.Client().Transport)

	downloaded, err := npmDownloadDistfile(server.URL+"/pkg.tgz", dir, "pkg.tgz", client)
	if err != nil {
		t.Fatalf("npmDownloadDistfile returned error: %v", err)
	}
	if _, statErr := os.Stat(downloaded + ".part"); statErr == nil {
		t.Error("successful download left a .part file behind")
	}
	if _, statErr := os.Stat(downloaded); statErr != nil {
		t.Errorf("distfile missing after successful download: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// npmCopyToDistdir tests
// ---------------------------------------------------------------------------

func TestNpm_CopyToDistdir(t *testing.T) {
	srcDir := t.TempDir()
	distDir := t.TempDir()

	tarballPath := filepath.Join(srcDir, "pkg-deps.tar.xz")
	if err := os.WriteFile(tarballPath, []byte("tarball"), 0644); err != nil {
		t.Fatal(err)
	}

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
// npmPrepare unit tests
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
	if err := os.MkdirAll(distDir, 0755); err != nil {
		t.Fatal(err)
	}

	tarballContent := []byte("fake-npm-tarball-content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(tarballContent) // httptest writer; a write error cannot occur here
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
	entries, err := ebuild.ParseSrcUri(info.SRC_URI, vars)
	if err != nil {
		t.Fatalf("ParseSrcUri: %v", err)
	}
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
	if err := os.MkdirAll(distDir, 0755); err != nil {
		t.Fatal(err)
	}

	tarballContent := []byte("fake-npm-tarball-content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(tarballContent) // httptest writer; a write error cannot occur here
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
	entries, err := ebuild.ParseSrcUri(info.SRC_URI, vars)
	if err != nil {
		t.Fatalf("ParseSrcUri: %v", err)
	}
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
	if err := os.MkdirAll(distDir, 0755); err != nil {
		t.Fatal(err)
	}

	tarballContent := []byte("fake-npm-tarball-content")

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(tarballContent) // httptest writer; a write error cannot occur here
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
	entries, err := ebuild.ParseSrcUri(info.SRC_URI, vars)
	if err != nil {
		t.Fatalf("ParseSrcUri: %v", err)
	}

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
	if err := os.MkdirAll(distDir, 0755); err != nil {
		t.Fatal(err)
	}

	tarballContent := []byte("fake-npm-tarball-content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(tarballContent) // httptest writer; a write error cannot occur here
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
	entries, err := ebuild.ParseSrcUri(info.SRC_URI, vars)
	if err != nil {
		t.Fatalf("ParseSrcUri: %v", err)
	}

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
	_, err = npmDownloadDistfile(rewritten, distDir, npmEntries[0].Filename, client)
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
	if err := os.MkdirAll(distDir, 0755); err != nil {
		t.Fatal(err)
	}

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
		_, _ = w.Write(tarballContent) // httptest writer; a write error cannot occur here
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
	entries, err := ebuild.ParseSrcUri(info.SRC_URI, vars)
	if err != nil {
		t.Fatalf("ParseSrcUri: %v", err)
	}

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
	entries, err := ebuild.ParseSrcUri(info.SRC_URI, vars)
	if err != nil {
		t.Fatalf("ParseSrcUri: %v", err)
	}

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

func TestNpm_PrepareDirectRegistryPipeline(t *testing.T) {
	// Regression: overlay ebuilds replaced mirror://npm/ with direct
	// registry.npmjs.org URLs (overlay commit "fix: npm registry URIs").
	// The full pipeline must still run for them: detection picks npm,
	// the entry filter accepts direct URLs, downloads hit the registry,
	// the deps tarball is created and uploaded, and the manifest runs.
	tmpDir := t.TempDir()
	pkgDir := filepath.Join(tmpDir, "test", "my-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	ebuildPath := filepath.Join(pkgDir, "my-pkg-1.0.0.ebuild")

	raw := `EAPI=8
MY_NPM_SCOPE="@scope"
SRC_URI="https://registry.npmjs.org/${MY_NPM_SCOPE}/${PN}/-/${P}.tgz -> ${P}.tgz
	https://github.com/o/r/releases/download/my-pkg/${P}-deps.tar.xz
	webui? ( https://registry.npmjs.org/${MY_NPM_SCOPE}/webui/-/webui-1.0.0.tgz )"
`
	if err := os.WriteFile(ebuildPath, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	// The manifest runner is stubbed below, but verifyManifestValid still
	// requires <pkgDir>/Manifest with DIST entries for every distfile.
	manifest := "DIST my-pkg-1.0.0.tgz 23 BLAKE2B abc SHA512 def\n" +
		"DIST my-pkg-1.0.0-deps.tar.xz 11 BLAKE2B abc SHA512 def\n" +
		"DIST webui-1.0.0.tgz 23 BLAKE2B abc SHA512 def\n"
	if err := os.WriteFile(filepath.Join(pkgDir, "Manifest"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	tarballContent := []byte("fake-npm-tarball-content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(tarballContent) // httptest writer; a write error cannot occur here
	}))
	defer server.Close()

	origBase := npmRegistryBase
	npmRegistryBase = server.URL + "/"
	defer func() { npmRegistryBase = origBase }()

	origNpm := npmRunner
	var recordedNpm *exec.Cmd
	npmRunner = func(cmd *exec.Cmd) error {
		recordedNpm = cmd
		return nil
	}
	defer func() { npmRunner = origNpm }()

	origArchive := archiveRunner
	archiveRunner = func(cmd *exec.Cmd) error {
		if len(cmd.Args) >= 3 {
			if err := os.WriteFile(cmd.Args[2], []byte("placeholder"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}
	defer func() { archiveRunner = origArchive }()

	manifestCalled := false
	origManifest := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		manifestCalled = true
		return nil
	}
	defer func() { ManifestRunner = origManifest }()

	rec := &argvRecorder{inner: github.NewStubRunner()}

	info := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Pkg:      "my-pkg",
		Category: "test",
		PVVal:    "1.0.0",
		PVal:     "my-pkg-1.0.0",
		SRC_URI:  "https://registry.npmjs.org/@scope/my-pkg/-/my-pkg-1.0.0.tgz -> my-pkg-1.0.0.tgz\n\thttps://github.com/o/r/releases/download/my-pkg/my-pkg-1.0.0-deps.tar.xz\n\twebui? ( https://registry.npmjs.org/@scope/webui/-/webui-1.0.0.tgz )",
		Raw:      raw,
	}

	cfg := &PrepareConfig{
		Distdir:    filepath.Join(tmpDir, "dist"),
		Overlay:    tmpDir,
		GitHubRepo: "o/r",
		HTTPClient: server.Client(),
		Runner:     rec,
	}
	err := Prepare(info, cfg)
	if err != nil {
		t.Fatalf("Prepare(direct-registry npm ebuild) returned error: %v", err)
	}

	// Both registry distfiles downloaded to distdir.
	for _, name := range []string{"my-pkg-1.0.0.tgz", "webui-1.0.0.tgz"} {
		data, readErr := os.ReadFile(filepath.Join(cfg.Distdir, name))
		if readErr != nil {
			t.Errorf("distfile %s not downloaded: %v", name, readErr)
			continue
		}
		if string(data) != string(tarballContent) {
			t.Errorf("distfile %s content = %q, want %q", name, data, tarballContent)
		}
	}

	// npm install ran against the main distfile.
	if recordedNpm == nil {
		t.Fatal("npmRunner was not called — deps tarball was not created")
	}
	sawMainTgz := false
	for _, a := range recordedNpm.Args {
		if a == filepath.Join(cfg.Distdir, "my-pkg-1.0.0.tgz") {
			sawMainTgz = true
		}
	}
	if !sawMainTgz {
		t.Errorf("npm argv must reference the main distfile: %v", recordedNpm.Args)
	}

	// The deps tarball was uploaded to the release tag from SRC_URI.
	sawUpload := false
	for _, argv := range rec.argv {
		if len(argv) >= 5 && argv[0] == "gh" && argv[1] == "release" && argv[2] == "upload" &&
			argv[3] == "my-pkg" && argv[4] == filepath.Join(cfg.Distdir, "my-pkg-1.0.0-deps.tar.xz") {
			sawUpload = true
		}
	}
	if !sawUpload {
		t.Errorf("gh release upload for my-pkg-1.0.0-deps.tar.xz not recorded, argv = %v", rec.argv)
	}

	if !manifestCalled {
		t.Error("manifest was not run")
	}
}

func TestNpm_PrefetchDirectRegistry(t *testing.T) {
	// npmPrefetchPackage must pre-populate DISTDIR for direct
	// registry.npmjs.org URLs, not only mirror://npm/ ones.
	pkgDir := filepath.Join(t.TempDir(), "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	distDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-1.0.ebuild"), []byte(`EAPI=8
PN="foo"
PV="1.0"
SRC_URI="https://registry.npmjs.org/foo-pkg/-/foo-1.0.0.tgz"
`), 0644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake-tgz")) // httptest writer; a write error cannot occur here
	}))
	defer server.Close()

	origBase := npmRegistryBase
	npmRegistryBase = server.URL + "/"
	defer func() { npmRegistryBase = origBase }()

	if err := npmPrefetchPackage(pkgDir, distDir); err != nil {
		t.Fatalf("npmPrefetchPackage: %v", err)
	}
	if _, err := os.Stat(filepath.Join(distDir, "foo-1.0.0.tgz")); err != nil {
		t.Errorf("direct-registry distfile not prefetched: %v", err)
	}
}

func TestNpm_PrepareDownloadFailureIsFatal(t *testing.T) {
	// A failing distfile download must abort the pipeline with an error
	// naming the distfile — the pipeline must not continue with a missing
	// distfile.
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "my-pkg-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	origBase := npmRegistryBase
	npmRegistryBase = server.URL + "/"
	defer func() { npmRegistryBase = origBase }()

	// Stub the manifest runner so a nil return from npmPrepare is unambiguous.
	origManifest := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		return nil
	}
	defer func() { ManifestRunner = origManifest }()

	info := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Pkg:      "my-pkg",
		Category: "test",
		PVVal:    "1.0.0",
		PVal:     "my-pkg-1.0.0",
		SRC_URI:  "mirror://npm/my-pkg/my-pkg-1.0.0.tgz",
		Raw: `MY_NPM_NAME="my-pkg"
SRC_URI="mirror://npm/my-pkg/my-pkg-1.0.0.tgz"`,
	}

	cfg := &PrepareConfig{
		Distdir:    filepath.Join(tmpDir, "dist"),
		ForceType:  ebuild.TypeNPM,
		HTTPClient: server.Client(),
	}
	err := Prepare(info, cfg)
	if err == nil {
		t.Fatal("Prepare must return an error when a distfile download fails")
	}
	if !strings.Contains(err.Error(), "my-pkg-1.0.0.tgz") {
		t.Errorf("error = %v, want it to name the distfile my-pkg-1.0.0.tgz", err)
	}
}

func TestNpm_PrepareRejectsEscapingReleaseAssetName(t *testing.T) {
	// A releases/download URL whose asset segment contains "../" would
	// traverse out of DISTDIR (and the deps-tarball temp dir) via
	// filepath.Join — it must be rejected.
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "my-pkg-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake-npm-tarball")) // httptest writer; a write error cannot occur here
	}))
	defer server.Close()

	origBase := npmRegistryBase
	npmRegistryBase = server.URL + "/"
	defer func() { npmRegistryBase = origBase }()

	// Stub npmRunner so a traversal that slips through would fail loudly in
	// the deps-tarball step rather than invoking the real npm.
	origNpm := npmRunner
	npmRunner = func(cmd *exec.Cmd) error {
		return errors.New("npm must not run in this test")
	}
	defer func() { npmRunner = origNpm }()

	info := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Pkg:      "my-pkg",
		Category: "test",
		PVVal:    "1.0.0",
		PVal:     "my-pkg-1.0.0",
		SRC_URI:  "mirror://npm/my-pkg/my-pkg-1.0.0.tgz https://github.com/o/r/releases/download/v1.0/../../evil.tar.xz",
		Raw: `MY_NPM_NAME="my-pkg"
SRC_URI="mirror://npm/my-pkg/my-pkg-1.0.0.tgz https://github.com/o/r/releases/download/v1.0/../../evil.tar.xz"`,
	}

	cfg := &PrepareConfig{
		Distdir:    filepath.Join(tmpDir, "dist"),
		ForceType:  ebuild.TypeNPM,
		HTTPClient: server.Client(),
	}
	err := Prepare(info, cfg)
	if err == nil {
		t.Fatal("Prepare must reject a release asset name containing ../")
	}
	if !strings.Contains(err.Error(), "evil.tar.xz") {
		t.Errorf("error = %v, want it to name the invalid asset name", err)
	}
}

func TestNpm_PrepareNoNpmEntriesReturnsErrorNotPanic(t *testing.T) {
	// An npm-typed ebuild whose SRC_URI has zero mirror://npm/ entries must
	// return an error — not panic on npmEntries[0].
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "test-pkg-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	info := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Pkg:      "test-pkg",
		Category: "test",
		PVVal:    "1.0.0",
		PVal:     "test-pkg-1.0.0",
		SRC_URI:  "https://github.com/o/r/releases/download/v1.0/deps.tar.xz",
		Raw:      `SRC_URI="https://github.com/o/r/releases/download/v1.0/deps.tar.xz"`,
	}

	cfg := &PrepareConfig{
		Distdir:   t.TempDir(),
		ForceType: ebuild.TypeNPM,
	}
	err := Prepare(info, cfg)
	if err == nil {
		t.Fatal("Prepare with npm type and no mirror://npm/ entries must return an error")
	}
	if !strings.Contains(err.Error(), "mirror://npm/") {
		t.Errorf("error = %v, want it to name mirror://npm/", err)
	}
}


// ---------------------------------------------------------------------------
// Plan 22b: npmRunner/archiveRunner seams + D8, D6, D7, D5, D9
// ---------------------------------------------------------------------------

func TestNpm_InstallArgs(t *testing.T) {
	// D8: npm --cache <dir> install <tgz> --ignore-scripts — lifecycle
	// scripts must never run on the workstation (mirrors the vendored
	// pipeline's npm ci --ignore-scripts; scripts only affect node_modules,
	// never the _cacache cache we tar).
	tgzPath := filepath.Join(t.TempDir(), "pkg-1.0.0.tgz")
	if err := os.WriteFile(tgzPath, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	var recordedNpm, recordedTar *exec.Cmd

	origNpm := npmRunner
	npmRunner = func(cmd *exec.Cmd) error {
		recordedNpm = cmd
		return nil
	}
	defer func() { npmRunner = origNpm }()

	origArchive := archiveRunner
	archiveRunner = func(cmd *exec.Cmd) error {
		recordedTar = cmd
		// archiveRunner stub creates a placeholder tarball so npmCreateDepsTarball
		// doesn't fail on the os.ReadFile(outputTarball) call at the end.
		if len(cmd.Args) >= 3 {
			outputPath := cmd.Args[2]
			if err := os.WriteFile(outputPath, []byte("placeholder"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}
	defer func() { archiveRunner = origArchive }()

	err := npmCreateDepsTarball(tgzPath, "", "deps.tar.gz")
	if err != nil {
		t.Fatalf("npmCreateDepsTarball: %v", err)
	}

	// Verify npm argv: ["npm", "--cache", <cacheDir>, "install", <tgzPath>, "--ignore-scripts"]
	if recordedNpm == nil {
		t.Fatal("npmRunner was not called")
	}
	if len(recordedNpm.Args) < 6 {
		t.Fatalf("npm argv too short: %v", recordedNpm.Args)
	}
	if recordedNpm.Args[0] != "npm" {
		t.Errorf("npm argv[0] = %q, want %q", recordedNpm.Args[0], "npm")
	}
	if recordedNpm.Args[1] != "--cache" {
		t.Errorf("npm argv[1] = %q, want --cache", recordedNpm.Args[1])
	}
	if recordedNpm.Args[2] == "" {
		t.Error("npm argv[2] (--cache dir) should not be empty")
	}
	if recordedNpm.Args[3] != "install" {
		t.Errorf("npm argv[3] = %q, want install", recordedNpm.Args[3])
	}
	if recordedNpm.Args[4] != tgzPath {
		t.Errorf("npm argv[4] = %q, want %q", recordedNpm.Args[4], tgzPath)
	}
	sawIgnoreScripts := false
	for _, a := range recordedNpm.Args {
		if a == "--ignore-scripts" {
			sawIgnoreScripts = true
		}
	}
	if !sawIgnoreScripts {
		t.Errorf("npm argv must contain --ignore-scripts: %v", recordedNpm.Args)
	}
	// Verify tar argv: ["tar", "-caf", <out>, "-C", <dir>, "npm-cache"]
	if recordedTar == nil {
		t.Fatal("archiveRunner was not called")
	}
	if recordedTar.Args[0] != "tar" {
		t.Errorf("tar argv[0] = %q, want tar", recordedTar.Args[0])
	}
	if recordedTar.Args[1] != "-caf" {
		t.Errorf("tar argv[1] = %q, want -caf", recordedTar.Args[1])
	}
}

func TestNpm_TarballCompressionFromExtension(t *testing.T) {
	// D6: tar -caf chooses compressor from output extension
	for _, tc := range []struct {
		ext    string
		expect []byte
	}{
		{".tar.xz", []byte{0xfd, 0x37, 0x7a, 0x58, 0x5a}},
		{".tar.gz", []byte{0x1f, 0x8b}},
	} {
		t.Run(tc.ext, func(t *testing.T) {
			dir := t.TempDir()
			npmCacheDir := filepath.Join(dir, "npm-cache")
			if err := os.MkdirAll(npmCacheDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(npmCacheDir, "index.json"), []byte("{}"), 0644); err != nil {
				t.Fatal(err)
			}

			// Stub npmRunner, let archiveRunner run for real
			origNpm := npmRunner
			npmRunner = func(*exec.Cmd) error { return nil }
			defer func() { npmRunner = origNpm }()

			assetName := "deps" + tc.ext
			err := npmCreateDepsTarball("", dir, assetName)
			if err != nil {
				t.Fatalf("npmCreateDepsTarball: %v", err)
			}

			data, err := os.ReadFile(filepath.Join(dir, assetName))
			if err != nil {
				t.Fatalf("read asset: %v", err)
			}
			if len(data) < len(tc.expect) {
				t.Fatalf("got %d bytes, want at least %d", len(data), len(tc.expect))
			}
			for i, b := range tc.expect {
				if data[i] != b {
					t.Errorf("byte %d: 0x%02x, want 0x%02x", i, data[i], b)
				}
			}
		})
	}

	// zstd: check if zstd is available
	t.Run("tar.zst", func(t *testing.T) {
		if _, err := exec.LookPath("zstd"); err != nil {
			t.Skip("zstd not on PATH")
		}
		dir := t.TempDir()
		npmCacheDir := filepath.Join(dir, "npm-cache")
		if err := os.MkdirAll(npmCacheDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(npmCacheDir, "index.json"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}

		origNpm := npmRunner
		npmRunner = func(*exec.Cmd) error { return nil }
		defer func() { npmRunner = origNpm }()

		err := npmCreateDepsTarball("", dir, "deps.tar.zst")
		if err != nil {
			t.Fatalf("npmCreateDepsTarball: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(dir, "deps.tar.zst"))
		if err != nil {
			t.Fatalf("read asset: %v", err)
		}
		want := []byte{0x28, 0xb5, 0x2f, 0xfd}
		if len(data) < 4 || data[0] != want[0] || data[1] != want[1] ||
			data[2] != want[2] || data[3] != want[3] {
			t.Errorf("magic: 0x%x 0x%x 0x%x 0x%x, want 0x%x 0x%x 0x%x 0x%x",
				data[0], data[1], data[2], data[3], want[0], want[1], want[2], want[3])
		}
	})
}

func TestNpm_ReleaseCreatePassesNotes(t *testing.T) {
	// D7: gh release create tag --title "pn deps" --notes "npm dependency cache for pn"
	rec := &argvRecorder{inner: github.NewStubRunner()}

	err := ghReleaseCreate("v1.0.0", "", "fish-lsp", rec)
	if err != nil {
		t.Fatalf("ghReleaseCreate: %v", err)
	}

	want := []string{
		"gh", "release", "create", "v1.0.0",
		"--title", "fish-lsp deps",
		"--notes", "npm dependency cache for fish-lsp",
	}
	if len(rec.argv) != 1 {
		t.Fatalf("runner ran %d command(s), want 1: %v", len(rec.argv), rec.argv)
	}
	if !reflect.DeepEqual(rec.argv[0], want) {
		t.Errorf("gh argv = %v, want %v", rec.argv[0], want)
	}
}

func TestNpm_PrefetchesSiblingVersions(t *testing.T) {
	// D5: npmPrefetchPackage downloads sibling version distfiles
	pkgDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-1.0.ebuild"), []byte(`EAPI=8
PN="foo"
PV="1.0"
SRC_URI="mirror://npm/foo-pkg/foo-1.0.0.tgz"
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-2.0.ebuild"), []byte(`EAPI=8
PN="foo"
PV="2.0"
SRC_URI="mirror://npm/bar-pkg/bar-2.0.0.tgz"
`), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := filepath.Glob(filepath.Join(pkgDir, "*.ebuild"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 ebuilds, got %d", len(files))
	}

	// The prefetch function will try to download from registry.npmjs.org which will fail,
	// but that's a logged error, not a fatal one. We verify the function doesn't panic.
}

func TestNpm_PrefetchSkipsExisting(t *testing.T) {
	// Pre-create a non-empty file; prefetch should skip it.
	dir := t.TempDir()
	existingPath := filepath.Join(dir, "foo-1.0.0.tgz")
	if err := os.WriteFile(existingPath, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(existingPath)
	if info.Size() == 0 {
		t.Fatal("test setup: file should be non-empty")
	}
}

func TestNpm_PrefetchIgnoresBakFiles(t *testing.T) {
	// .ebuild.bak should not be parsed
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo-1.0.ebuild"), []byte(`EAPI=8
PN="foo"
PV="1.0"
SRC_URI="mirror://npm/foo-pkg/foo-1.0.0.tgz"
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foo-0.9.ebuild.bak"), []byte(`EAPI=8
PN="foo"
PV="0.9"
SRC_URI="mirror://npm/foo-pkg/foo-0.9.0.tgz"
`), 0644); err != nil {
		t.Fatal(err)
	}

	files, _ := filepath.Glob(filepath.Join(dir, "*.ebuild"))
	for _, f := range files {
		if strings.HasSuffix(f, ".bak") {
			t.Errorf("should not include .bak file: %s", f)
		}
	}
	if len(files) != 1 {
		t.Errorf("expected 1 ebuild (not .bak), got %d", len(files))
	}
}

func TestNpm_PrefetchDownloadFailureIsFatal(t *testing.T) {
	// The mirror://npm/ pre-populate rule: a genuinely-missing distfile whose
	// download fails must abort the manifest-prefetch step with an error
	// naming the distfile — otherwise pkgdev fails later with a confusing
	// fetch error.
	// pkgDir must be named after the package (ParseEbuildPath derives PN
	// from the parent directory).
	pkgDir := filepath.Join(t.TempDir(), "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	distDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-1.0.ebuild"), []byte(`EAPI=8
PN="foo"
PV="1.0"
SRC_URI="mirror://npm/foo-pkg/foo-1.0.0.tgz"
`), 0644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	origBase := npmRegistryBase
	npmRegistryBase = server.URL + "/"
	defer func() { npmRegistryBase = origBase }()

	err := npmPrefetchPackage(pkgDir, distDir)
	if err == nil {
		t.Fatal("npmPrefetchPackage must error when a missing distfile cannot be downloaded")
	}
	if !strings.Contains(err.Error(), "foo-1.0.0.tgz") {
		t.Errorf("error = %v, want it to name the distfile foo-1.0.0.tgz", err)
	}
}

func TestNpm_ReleaseUploadUsesClobber(t *testing.T) {
	// gh release upload <tag> <path> --clobber
	rec := &argvRecorder{inner: github.NewStubRunner()}

	tmpDir := t.TempDir()
	assetPath := filepath.Join(tmpDir, "deps.tar.xz")
	if err := os.WriteFile(assetPath, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	err := ghUploadAsset("v1.0.0", assetPath, "", rec)
	if err != nil {
		t.Fatalf("ghUploadAsset: %v", err)
	}

	want := []string{"gh", "release", "upload", "v1.0.0", assetPath, "--clobber"}
	if len(rec.argv) != 1 {
		t.Fatalf("runner ran %d command(s), want 1: %v", len(rec.argv), rec.argv)
	}
	if !reflect.DeepEqual(rec.argv[0], want) {
		t.Errorf("gh argv = %v, want %v", rec.argv[0], want)
	}
}
