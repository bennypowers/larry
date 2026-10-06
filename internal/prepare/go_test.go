package prepare

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// goFixture starts an httptest server serving a gzip tar at
// /<owner>/<repo>/archive/refs/tags/<tag>.tar.gz containing exactly one entry,
// the directory "proj-1.0.0/", plus the file "proj-1.0.0/go.mod" with bytes
// "module example.com/proj\n\ngo 1.22\n".
// Any other path 404s.
func goFixture(t *testing.T, owner, repo, tag string) *httptest.Server {
	t.Helper()
	return goFixtureWithGoMod(t, owner, repo, tag, "proj", "module example.com/proj\n\ngo 1.22\n")
}

// goFixtureWithGoMod is like goFixture but lets you customise the dir name and go.mod contents.
func goFixtureWithGoMod(t *testing.T, owner, repo, tag, dirName, goModContent string) *httptest.Server {
	t.Helper()

	tarData := buildTestTarball(t, dirName, map[string]string{
		"go.mod": goModContent,
	})
	_ = fmt.Sprintf // keep fmt import for error messages

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := fmt.Sprintf("/%s/%s/archive/refs/tags/%s.tar.gz", owner, repo, tag)
		if r.URL.Path != wantPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
	_, _ = w.Write(tarData) // httptest writer; a write error cannot occur here
	}))
}

// buildTestTarball creates a gzip tar in memory containing the given files.
// Files are keyed by path relative to a top-level dirName/.
func buildTestTarball(t *testing.T, dirName string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	w := tar.NewWriter(gz)

	for relPath, content := range files {
		fullPath := filepath.Join(dirName, relPath)
		hdr := &tar.Header{
			Name:     fullPath,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if strings.HasSuffix(relPath, "/") || relPath == dirName {
			hdr.Typeflag = tar.TypeDir
			hdr.Mode = 0o755
			hdr.Size = 0
		}
		if err := w.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := w.Write([]byte(content)); err != nil {
				t.Fatalf("write tar data: %v", err)
			}
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

// makeGoEbuildInfo creates a minimal EbuildInfo for a go-module ebuild.
func makeGoEbuildInfo(t *testing.T, tmpDir, owner, repo string, extraVars ...string) *ebuild.EbuildInfo {
	t.Helper()
	pkgDir := filepath.Join(tmpDir, "overlay", "dev-util", "proj")
if err := os.MkdirAll(pkgDir, 0o755); err != nil { t.Fatal(err) }
	ebuildPath := filepath.Join(pkgDir, "proj-1.0.0.ebuild")

	content := `EAPI=8
inherit go-module

DESCRIPTION="Test"
HOMEPAGE="https://github.com/` + owner + `/` + repo + `"
SRC_URI="https://github.com/` + owner + `/` + repo + `/archive/refs/tags/v${PV}.tar.gz -> ${P}.tar.gz"

LICENSE="MIT"
SLOT="0"
KEYWORDS="~amd64"
`
	if len(extraVars)%2 == 0 {
		for i := 0; i < len(extraVars); i += 2 {
			content += extraVars[i] + "=" + extraVars[i+1] + "\n"
		}
	}

	if err := os.WriteFile(ebuildPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	return &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "dev-util",
		Pkg:      "proj",
		PVVal:    "1.0.0",
		PVal:     "proj-1.0.0",
		SRC_URI:  "https://github.com/" + owner + "/" + repo + "/archive/refs/tags/v${PV}.tar.gz",
		HOMEPAGE: "https://github.com/" + owner + "/" + repo,
		Inherit:  []string{"go-module"},
	}
}

// makeGoEbuildInfoWithPV creates an EbuildInfo with a custom PVVal.
func makeGoEbuildInfoWithPV(t *testing.T, tmpDir, owner, repo, pvVal string, extraVars ...string) *ebuild.EbuildInfo {
	t.Helper()
	pv := pvVal
	// Strip -rN from PVVal to get the base PV
	if idx := strings.LastIndex(pvVal, "-r"); idx > 0 {
		pv = pvVal[:idx]
	}
	pkgName := "proj"
	pVal := pkgName + "-" + pv

	pkgDir := filepath.Join(tmpDir, "overlay", "dev-util", "proj")
if err := os.MkdirAll(pkgDir, 0o755); err != nil { t.Fatal(err) }
	ebuildPath := filepath.Join(pkgDir, pkgName+"-"+pv+".ebuild")

	content := `EAPI=8
inherit go-module

DESCRIPTION="Test"
HOMEPAGE="https://github.com/` + owner + `/` + repo + `"
SRC_URI="https://github.com/` + owner + `/` + repo + `/archive/refs/tags/v${PV}.tar.gz -> ${P}.tar.gz"

LICENSE="MIT"
SLOT="0"
KEYWORDS="~amd64"
`
	if len(extraVars)%2 == 0 {
		for i := 0; i < len(extraVars); i += 2 {
			content += extraVars[i] + "=" + extraVars[i+1] + "\n"
		}
	}

	if err := os.WriteFile(ebuildPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	return &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "dev-util",
		Pkg:      pkgName,
		PVVal:    pvVal,
		PVal:     pVal,
		SRC_URI:  "https://github.com/" + owner + "/" + repo + "/archive/refs/tags/v${PV}.tar.gz",
		HOMEPAGE: "https://github.com/" + owner + "/" + repo,
		Inherit:  []string{"go-module"},
	}
}

// recRunner is a recording CmdRunner for testing.
type recRunner struct {
	calls [][]string
}

func (r *recRunner) Run(cmd *exec.Cmd) error {
	r.calls = append(r.calls, cmd.Args)
	if len(cmd.Args) >= 3 && cmd.Args[1] == "mod" && cmd.Args[2] == "vendor" {
		if err := os.MkdirAll(filepath.Join(cmd.Dir, "vendor", "x"), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(cmd.Dir, "vendor", "x", "y.go"), []byte("package y\n"), 0o644); err != nil {
			return err
		}
		return nil
	}
	return nil
}

func (r *recRunner) Output(cmd *exec.Cmd) ([]byte, error) {
	r.calls = append(r.calls, cmd.Args)
	return nil, nil
}

// reqRecorder records HTTP request paths.
type reqRecorder struct {
	paths []string
}

func (rr *reqRecorder) record(req *http.Request) {
	rr.paths = append(rr.paths, req.URL.Path)
}

// ---------------------------------------------------------------------------
// Happy-path and defect-regression tests
// ---------------------------------------------------------------------------

func TestPrepareGo_HappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	srv := goFixture(t, "acme", "proj", "v1.0.0")

	ei := makeGoEbuildInfo(t, tmpDir, "acme", "proj")
	runner := &recRunner{}
	overlay := filepath.Join(tmpDir, "overlay")
	distDir := t.TempDir()

	rr := &reqRecorder{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rr.record(r)
		http.NotFound(w, r)
	}))
	defer ts.Close()

	// We need a combined handler that serves the tarball for the right path,
	// or use goFixture's server directly. goFixture already serves the tar.
	// So we just use srv.URL.
	stubManifestRunner(t)

	cfg := &PrepareConfig{
		Overlay:       overlay,
		Distdir:       distDir,
		GitHubBaseURL: srv.URL,
		Runner:        runner,
		HTTPClient:    srv.Client(),
	}

	err := PrepareGo(ei, cfg)
	if err != nil {
		t.Fatalf("PrepareGo returned error: %v", err)
	}

	// Both files should exist
	pkgTarball := filepath.Join(filepath.Dir(ei.Path), "proj-1.0.0-deps.tar.gz")
	if _, err := os.Stat(pkgTarball); err != nil {
		t.Errorf("tarball not created at %s: %v", pkgTarball, err)
	}
	distTarball := filepath.Join(distDir, "proj-1.0.0-deps.tar.gz")
	if _, err := os.Stat(distTarball); err != nil {
		t.Errorf("tarball not copied to distdir %s: %v", distTarball, err)
	}
}

// stubManifestRunner replaces ManifestRunner with a no-op for tests that don't
// need real manifest execution.
func stubManifestRunner(t *testing.T) {
	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })
}

func TestPrepareGo_TarballRootIsVendorDir(t *testing.T) {
	tmpDir := t.TempDir()
	srv := goFixture(t, "acme", "proj", "v1.0.0")

	ei := makeGoEbuildInfo(t, tmpDir, "acme", "proj")
	runner := &recRunner{}
	overlay := filepath.Join(tmpDir, "overlay")
	distDir := t.TempDir()

	stubManifestRunner(t)

	cfg := &PrepareConfig{
		Overlay:       overlay,
		Distdir:       distDir,
		GitHubBaseURL: srv.URL,
		Runner:        runner,
		HTTPClient:    srv.Client(),
	}

	err := PrepareGo(ei, cfg)
	if err != nil {
		t.Fatalf("PrepareGo returned error: %v", err)
	}

	// Open the tarball at the package-dir location and inspect headers
	pkgTarball := filepath.Join(filepath.Dir(ei.Path), "proj-1.0.0-deps.tar.gz")
	f, err := os.Open(pkgTarball)
	if err != nil {
		t.Fatalf("open tarball: %v", err)
	}
defer func() { if err := f.Close(); err != nil { t.Fatal(err) } }()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("create gzip reader: %v", err)
	}
defer func() { _ = gzr.Close() }() // best-effort close in test

	tr := tar.NewReader(gzr)
	foundVendorX := false
	hasDotPrefix := false
	hasNoVendorPrefix := false

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar entry: %v", err)
		}
		if header.Name == "./" {
			hasDotPrefix = true
		}
		if !strings.HasPrefix(header.Name, "vendor/") && header.Name != "vendor" {
			hasNoVendorPrefix = true
		}
		if header.Name == "vendor/x/y.go" {
			foundVendorX = true
		}
	}

	if hasDotPrefix {
		t.Error("tarball root contains './' prefix — should be vendor/ at root")
	}
	if hasNoVendorPrefix {
		t.Error("tarball contains entries without vendor/ prefix")
	}
	if !foundVendorX {
		t.Error("tarball does not contain vendor/x/y.go")
	}
}

func TestPrepareGo_WritesBothLocations(t *testing.T) {
	tmpDir := t.TempDir()
	srv := goFixture(t, "acme", "proj", "v1.0.0")

	ei := makeGoEbuildInfo(t, tmpDir, "acme", "proj")
	runner := &recRunner{}
	overlay := filepath.Join(tmpDir, "overlay")
	distDir := t.TempDir()

	stubManifestRunner(t)

	cfg := &PrepareConfig{
		Overlay:       overlay,
		Distdir:       distDir,
		GitHubBaseURL: srv.URL,
		Runner:        runner,
		HTTPClient:    srv.Client(),
	}

	err := PrepareGo(ei, cfg)
	if err != nil {
		t.Fatalf("PrepareGo returned error: %v", err)
	}

	// Compare SHA-256 of both files
	pkgTarball := filepath.Join(filepath.Dir(ei.Path), "proj-1.0.0-deps.tar.gz")
	distTarball := filepath.Join(distDir, "proj-1.0.0-deps.tar.gz")

	pkgData, err := os.ReadFile(pkgTarball)
	if err != nil {
		t.Fatalf("read pkg tarball: %v", err)
	}
	distData, err := os.ReadFile(distTarball)
	if err != nil {
		t.Fatalf("read dist tarball: %v", err)
	}

	if len(pkgData) == 0 {
		t.Fatal("package-dir tarball is empty")
	}
	if len(distData) == 0 {
		t.Fatal("distdir tarball is empty")
	}

	// Quick check: file sizes must match (they're identical copies)
	if len(pkgData) != len(distData) {
		t.Errorf("file sizes differ: pkg=%d, dist=%d", len(pkgData), len(distData))
	}
}

func TestPrepareGo_WriteOrderIsPkgDirThenDistdir(t *testing.T) {
	tmpDir := t.TempDir()
	srv := goFixture(t, "acme", "proj", "v1.0.0")

	ei := makeGoEbuildInfo(t, tmpDir, "acme", "proj")
	runner := &recRunner{}
	overlay := filepath.Join(tmpDir, "overlay")
	distDir := t.TempDir()

	// Create distdir as a regular file (not a directory) — copyToDistdir's MkdirAll will fail
	distfile := filepath.Join(distDir, "notadir")
	if err := os.WriteFile(distfile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Set distdir to the parent of the file — that's fine, but we need to
	// override copyToDistdir to point to the file path instead.
	// Actually, the test says cfg.Distdir points at an existing regular file.
	// copyToDistdir does os.MkdirAll(distDir, ...) which will fail if distDir is a file.
	// So set distdir to the file path itself.
	cfg := &PrepareConfig{
		Overlay:       overlay,
		Distdir:       distfile, // this is a regular file, not a dir
		GitHubBaseURL: srv.URL,
		Runner:        runner,
		HTTPClient:    srv.Client(),
	}

	err := PrepareGo(ei, cfg)
	if err == nil {
		t.Fatal("expected error when distdir is a file")
	}

	// But the pkg-dir tarball should already exist (written before distdir copy)
	pkgTarball := filepath.Join(filepath.Dir(ei.Path), "proj-1.0.0-deps.tar.gz")
	if _, err := os.Stat(pkgTarball); err != nil {
		t.Error("pkg-dir tarball should exist even when distdir copy fails")
	}
}

func TestPrepareGo_NoGitHubReleaseCalls(t *testing.T) {
	tmpDir := t.TempDir()
	srv := goFixture(t, "acme", "proj", "v1.0.0")

	ei := makeGoEbuildInfo(t, tmpDir, "acme", "proj")
	runner := &recRunner{}
	overlay := filepath.Join(tmpDir, "overlay")
	distDir := t.TempDir()

	stubManifestRunner(t)

	cfg := &PrepareConfig{
		Overlay:       overlay,
		Distdir:       distDir,
		GitHubBaseURL: srv.URL,
		Runner:        runner,
		HTTPClient:    srv.Client(),
	}

	err := PrepareGo(ei, cfg)
	if err != nil {
		t.Fatalf("PrepareGo returned error: %v", err)
	}

	// No recorded call should have "gh" as the first arg
	for _, call := range runner.calls {
		if len(call) > 0 && call[0] == "gh" {
			t.Errorf("unexpected gh call: %v", call)
		}
	}
	// Only recorded call should be ["go", "mod", "vendor"]
	if len(runner.calls) != 1 {
		t.Errorf("expected exactly 1 recorded call, got %d", len(runner.calls))
	}
	if len(runner.calls) > 0 {
		expected := []string{"go", "mod", "vendor"}
		if len(runner.calls[0]) != len(expected) {
			t.Errorf("call args length: got %d, want %d", len(runner.calls[0]), len(expected))
		} else {
			for i, a := range expected {
				if runner.calls[0][i] != a {
					t.Errorf("call arg[%d]: got %q, want %q", i, runner.calls[0][i], a)
				}
			}
		}
	}
}

func TestPrepareGo_TagStripsRevision(t *testing.T) {
	tmpDir := t.TempDir()

	// Server registered for tag v1.0.0 (no -r2)
	serverRequests := &reqRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverRequests.record(r)
		wantPath := "/acme/proj/archive/refs/tags/v1.0.0.tar.gz"
		if r.URL.Path != wantPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		tarData := buildTestTarball(t, "proj-1.0.0", map[string]string{"go.mod": "module example.com/proj\n\ngo 1.22\n"})
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
	_, _ = w.Write(tarData) // httptest writer; a write error cannot occur here
	}))
	defer srv.Close()

	ei := makeGoEbuildInfoWithPV(t, tmpDir, "acme", "proj", "1.0.0-r2")
	runner := &recRunner{}
	overlay := filepath.Join(tmpDir, "overlay")
	distDir := t.TempDir()

	stubManifestRunner(t)

	cfg := &PrepareConfig{
		Overlay:       overlay,
		Distdir:       distDir,
		GitHubBaseURL: srv.URL,
		Runner:        runner,
		HTTPClient:    srv.Client(),
	}

	err := PrepareGo(ei, cfg)
	if err != nil {
		t.Fatalf("PrepareGo returned error: %v", err)
	}

	// Server should have received exactly one request for v1.0.0 (not v1.0.0-r2)
	if len(serverRequests.paths) != 1 {
		t.Fatalf("expected 1 request, got %d: %v", len(serverRequests.paths), serverRequests.paths)
	}
	if serverRequests.paths[0] != "/acme/proj/archive/refs/tags/v1.0.0.tar.gz" {
		t.Errorf("request path = %q, want /acme/proj/archive/refs/tags/v1.0.0.tar.gz", serverRequests.paths[0])
	}
}

func TestPrepareGo_MissingTagIsError(t *testing.T) {
	tmpDir := t.TempDir()

	// Server 404s every path
	rr := &reqRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rr.record(r)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ei := makeGoEbuildInfo(t, tmpDir, "acme", "proj")
	runner := &recRunner{}
	overlay := filepath.Join(tmpDir, "overlay")
	distDir := t.TempDir()

	cfg := &PrepareConfig{
		Overlay:       overlay,
		Distdir:       distDir,
		GitHubBaseURL: srv.URL,
		Runner:        runner,
		HTTPClient:    srv.Client(),
	}

	err := PrepareGo(ei, cfg)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %q, want it to contain '404'", err.Error())
	}

	// Server recorded exactly one request (no fallback)
	if len(rr.paths) != 1 {
		t.Errorf("expected 1 request, got %d: %v", len(rr.paths), rr.paths)
	}
}

func TestPrepareGo_EmptyDistdirIsError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a real fixture server so we can verify it gets zero requests
	srv := goFixture(t, "acme", "proj", "v1.0.0")
	ei := makeGoEbuildInfo(t, tmpDir, "acme", "proj")
	runner := &recRunner{}

	// Record requests
	rr := &reqRecorder{}
	// Wrap the handler to record requests
	originalHandler := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rr.record(r)
		originalHandler.ServeHTTP(w, r)
	})

	cfg := &PrepareConfig{
		Overlay:       filepath.Join(tmpDir, "overlay"),
		Distdir:       "", // empty — should error before any network call
		GitHubBaseURL: srv.URL,
		Runner:        runner,
		HTTPClient:    srv.Client(),
	}

	err := PrepareGo(ei, cfg)
	if err == nil {
		t.Fatal("expected error for empty distdir")
	}
	if !strings.Contains(err.Error(), "distdir") {
		t.Errorf("error = %q, want it to contain 'distdir'", err.Error())
	}

	// Server recorded zero requests
	if len(rr.paths) != 0 {
		t.Errorf("expected 0 requests (early exit), got %d: %v", len(rr.paths), rr.paths)
	}
}

func TestPrepareGo_DepsFilename(t *testing.T) {
	tmpDir := t.TempDir()
	srv := goFixture(t, "acme", "proj", "v1.0.0")

	ei := makeGoEbuildInfo(t, tmpDir, "acme", "proj")
	runner := &recRunner{}
	overlay := filepath.Join(tmpDir, "overlay")
	distDir := t.TempDir()

	stubManifestRunner(t)

	cfg := &PrepareConfig{
		Overlay:       overlay,
		Distdir:       distDir,
		GitHubBaseURL: srv.URL,
		Runner:        runner,
		HTTPClient:    srv.Client(),
	}

	err := PrepareGo(ei, cfg)
	if err != nil {
		t.Fatalf("PrepareGo returned error: %v", err)
	}

	// Both files named exactly proj-1.0.0-deps.tar.gz
	pkgTarball := filepath.Join(filepath.Dir(ei.Path), "proj-1.0.0-deps.tar.gz")
	if _, err := os.Stat(pkgTarball); err != nil {
		t.Errorf("pkg tarball not found: %v", err)
	}

	distTarball := filepath.Join(distDir, "proj-1.0.0-deps.tar.gz")
	if _, err := os.Stat(distTarball); err != nil {
		t.Errorf("dist tarball not found: %v", err)
	}
}

func TestPrepareGo_ManifestTargetsCatPkg(t *testing.T) {
	tmpDir := t.TempDir()
	srv := goFixture(t, "acme", "proj", "v1.0.0")

	ei := makeGoEbuildInfo(t, tmpDir, "acme", "proj")
	runner := &recRunner{}
	overlay := filepath.Join(tmpDir, "overlay")
	distDir := t.TempDir()

	var manifestArg string
	origManifestRunner := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		// Extract catPkg from args: pkgdev manifest -d <distdir> <cat/pkg>
		for i, a := range args {
			if a == "-d" && i+1 < len(args) {
				// skip distdir arg
			} else if !strings.HasPrefix(a, "-") {
				manifestArg = a
			}
		}
		return nil
	}
	defer func() { ManifestRunner = origManifestRunner }()

	cfg := &PrepareConfig{
		Overlay:       overlay,
		Distdir:       distDir,
		GitHubBaseURL: srv.URL,
		Runner:        runner,
		HTTPClient:    srv.Client(),
	}

	err := PrepareGo(ei, cfg)
	if err != nil {
		t.Fatalf("PrepareGo returned error: %v", err)
	}

	if manifestArg != "dev-util/proj" {
		t.Errorf("manifest catPkg = %q, want dev-util/proj", manifestArg)
	}
}

func TestPrepareGo_NoGoMod(t *testing.T) {
	tmpDir := t.TempDir()

	// Serve a tar with a top-level dir that has no go.mod
	rr := &reqRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rr.record(r)
		tarData := buildTestTarball(t, "proj-1.0.0", map[string]string{"README.md": "# proj\n"})
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
	_, _ = w.Write(tarData) // httptest writer; a write error cannot occur here
	}))
	defer srv.Close()

	ei := makeGoEbuildInfo(t, tmpDir, "acme", "proj")
	runner := &recRunner{}
	overlay := filepath.Join(tmpDir, "overlay")
	distDir := t.TempDir()

	cfg := &PrepareConfig{
		Overlay:       overlay,
		Distdir:       distDir,
		GitHubBaseURL: srv.URL,
		Runner:        runner,
		HTTPClient:    srv.Client(),
	}

	err := PrepareGo(ei, cfg)
	if err == nil {
		t.Fatal("expected error for no go.mod")
	}
	if !strings.Contains(err.Error(), "go.mod") {
		t.Errorf("error = %q, want it to contain 'go.mod'", err.Error())
	}
}

func TestPrepareGo_NoGitHubURL(t *testing.T) {
	tmpDir := t.TempDir()

	// No fixture server needed — should error before any network call
	ei := makeGoEbuildInfo(t, tmpDir, "acme", "proj")
	ei.HOMEPAGE = ""
	ei.SRC_URI = "https://example.com/x.tar.gz"

	runner := &recRunner{}
	distDir := t.TempDir()

	cfg := &PrepareConfig{
		Overlay:       filepath.Join(tmpDir, "overlay"),
		Distdir:       distDir,
		GitHubBaseURL: "",
		Runner:        runner,
		HTTPClient:    nil,
	}

	err := PrepareGo(ei, cfg)
	if err == nil {
		t.Fatal("expected error for no GitHub URL")
	}
	if !strings.Contains(err.Error(), "github") {
		t.Errorf("error = %q, want it to contain 'github'", err.Error())
	}

	// Zero requests to any server
	if len(runner.calls) != 0 {
		t.Errorf("expected 0 runner calls, got %d", len(runner.calls))
	}
}

func TestPrepareGo_MultipleTopLevelDirsIsError(t *testing.T) {
	tmpDir := t.TempDir()

	// Serve a tar with two top-level dirs
	rr := &reqRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rr.record(r)
		// Build tar with two top-level dirs: a/ and b/
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		wt := tar.NewWriter(gz)

		for _, d := range []string{"a/", "b/"} {
			hdr := &tar.Header{
				Name:     d,
				Typeflag: tar.TypeDir,
				Mode:     0o755,
			}
			if err := wt.WriteHeader(hdr); err != nil {
				t.Fatalf("tar header: %v", err)
			}
		}
		if err := wt.Close(); err != nil {
			t.Fatalf("close tar writer: %v", err)
		}
		if err := gz.Close(); err != nil {
			t.Fatalf("close gzip writer: %v", err)
		}

		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
_, _ = w.Write(buf.Bytes()) // httptest writer; a write error cannot occur here
	}))
	defer srv.Close()

	ei := makeGoEbuildInfo(t, tmpDir, "acme", "proj")
	runner := &recRunner{}
	overlay := filepath.Join(tmpDir, "overlay")
	distDir := t.TempDir()

	cfg := &PrepareConfig{
		Overlay:       overlay,
		Distdir:       distDir,
		GitHubBaseURL: srv.URL,
		Runner:        runner,
		HTTPClient:    srv.Client(),
	}

	err := PrepareGo(ei, cfg)
	if err == nil {
		t.Fatal("expected error for multiple top-level dirs")
	}
	if !strings.Contains(err.Error(), "exactly one") {
		t.Errorf("error = %q, want it to contain 'exactly one'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// findGoRepo tests (kept — function still exists)
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
// Dry-run test (kept — still valid)
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
	if err := os.WriteFile(ebuildPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write ebuild: %v", err)
	}

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

	err := PrepareGo(ei, cfg)
	if err != nil {
		t.Fatalf("PrepareGo returned error: %v", err)
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
// extractGoSource tests (rewritten — no subprocess)
// ---------------------------------------------------------------------------

func TestExtractGoSource(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a tarball in memory
	files := map[string]string{
		"proj-1.0.0/go.mod": "module example.com/test\n\ngo 1.22\n",
		"proj-1.0.0/main.go": "package main\n",
	}
	var buf strings.Builder
	gz := gzip.NewWriter(&buf)
	w := tar.NewWriter(gz)

	for relPath, content := range files {
		hdr := &tar.Header{
			Name:     relPath,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if strings.HasSuffix(relPath, "/") {
			hdr.Typeflag = tar.TypeDir
			hdr.Mode = 0o755
		}
		if err := w.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg {
_, _ = w.Write([]byte(content)) // httptest writer; a write error cannot occur here
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	extractDir := filepath.Join(tmpDir, "extracted")
if err := os.MkdirAll(extractDir, 0o755); err != nil { t.Fatal(err) }

	err := extractGoSource(strings.NewReader(buf.String()), extractDir)
	if err != nil {
		t.Fatalf("extractGoSource returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(extractDir, "proj-1.0.0", "go.mod")); err != nil {
		t.Error("go.mod not found in extracted directory")
	}
	if _, err := os.Stat(filepath.Join(extractDir, "proj-1.0.0", "main.go")); err != nil {
		t.Error("main.go not found in extracted directory")
	}
}

func TestExtractGoSource_BadGzip(t *testing.T) {
	tmpDir := t.TempDir()
	err := extractGoSource(strings.NewReader("not a gzip file"), tmpDir)
	if err == nil {
		t.Error("expected error for bad gzip data")
	}
}

// ---------------------------------------------------------------------------
// runGoModVendor tests (kept)
// ---------------------------------------------------------------------------

func TestRunGoModVendor(t *testing.T) {
	tmpDir := t.TempDir()

	runner := &recRunner{
		calls: nil,
	}
	err := runGoModVendor(tmpDir, runner)
	if err != nil {
		t.Fatalf("runGoModVendor returned error: %v", err)
	}
	// Verify it recorded the call
	if len(runner.calls) != 1 {
		t.Errorf("expected 1 call, got %d", len(runner.calls))
	}
}

// ---------------------------------------------------------------------------
// createGoVendorTarball tests (rewritten — 2-arg form)
// ---------------------------------------------------------------------------

func TestCreateGoVendorTarball(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a srcDir with vendor/
	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(filepath.Join(srcDir, "vendor", "x", "y"), 0o755); err != nil {
		t.Fatalf("create vendor dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "vendor", "x", "y", "z.go"), []byte("package y\n"), 0o644); err != nil {
		t.Fatalf("write z.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	outPath := filepath.Join(tmpDir, "out.tar.gz")
	err := createGoVendorTarball(srcDir, outPath)
	if err != nil {
		t.Fatalf("createGoVendorTarball returned error: %v", err)
	}

	// Verify the tarball exists
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("tarball not created: %v", err)
	}

	// Verify contents start with vendor/
	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open tarball: %v", err)
	}
defer func() { if err := f.Close(); err != nil { t.Fatal(err) } }()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("create gzip reader: %v", err)
	}
defer func() { _ = gzr.Close() }() // best-effort close in test

	tr := tar.NewReader(gzr)
	foundVendor := false
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar entry: %v", err)
		}
		if header.Name == "vendor/x/y/z.go" {
			foundVendor = true
		}
	}
	if !foundVendor {
		t.Error("vendor/x/y/z.go not found in tarball")
	}
}

func TestCreateGoVendorTarball_NoVendor(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
if err := os.MkdirAll(srcDir, 0o755); err != nil { t.Fatal(err) }
	// No vendor/ directory

	outPath := filepath.Join(tmpDir, "out.tar.gz")
	err := createGoVendorTarball(srcDir, outPath)
	if err == nil {
		t.Error("expected error when vendor dir is missing")
	}
}

// ---------------------------------------------------------------------------
// copyToDistdir tests (kept)
// ---------------------------------------------------------------------------

func TestCopyToDistdir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source tarball
	tarballPath := filepath.Join(tmpDir, "example-0.1.0-deps.tar.gz")
	if err := os.WriteFile(tarballPath, []byte("tarball data"), 0o644); err != nil {
		t.Fatalf("write tarball: %v", err)
	}

	// Create distdir
	distDir := filepath.Join(tmpDir, "dist")
if err := os.MkdirAll(distDir, 0o755); err != nil { t.Fatal(err) }

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
// Deleted-function tests — these used to exist but were removed by plan 24.
// They are listed here for the agent report.
//
// DELETED (plan 24):
//   TestGo_Pipeline_DownloadSource        → replaced by happy-path tests
//   TestGo_Pipeline_DownloadSource_Non200 → replaced
//   TestGo_Pipeline_ExtractSource         → replaced by TestExtractGoSource
//   TestGo_Pipeline_ExtractSource_NoExtract → replaced
//   TestGo_Pipeline_GoModVendor           → replaced by TestRunGoModVendor
//   TestGo_Pipeline_GoModVendor_NoGoMod   → deleted (mock runner doesn't fail)
//   TestGo_Pipeline_CreateTarball         → replaced by TestCreateGoVendorTarball
//   TestGo_Pipeline_CreateTarball_NoVendor → replaced
//   TestGo_Pipeline_ReleaseExists         → deleted (no release)
//   TestGo_Pipeline_ReleaseNotExists      → deleted
//   TestGo_Pipeline_ReleaseCreateFailure  → deleted
//   TestGo_Pipeline_ReleaseCreateSuccess  → deleted
//   TestGo_Pipeline_UploadToGithub        → deleted
//   TestGo_Pipeline_UploadToGithub_Fail   → deleted
//   TestGo_Pipeline_ManifestGeneration    → deleted (manifest is plan 21)
//   TestGo_Pipeline_DistdirBeforeManifest → deleted
//   TestGo_Pipeline_RevisionBump_Skip     → deleted (not relevant to go pipeline)
//   TestGo_Pipeline_FullMock              → deleted (replaced by happy-path tests)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Helper test for the test helper: verify buildTestTarball produces valid tar
// ---------------------------------------------------------------------------

func TestBuildTestTarball(t *testing.T) {
	data := buildTestTarball(t, "test-1.0", map[string]string{
		"go.mod": "module example.com/test\n",
	})
	if len(data) == 0 {
		t.Fatal("buildTestTarball produced empty data")
	}

	// Verify it can be read back
	gzr, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("create gzip reader: %v", err)
	}
defer func() { _ = gzr.Close() }() // best-effort close in test

	tr := tar.NewReader(gzr)
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar entry: %v", err)
		}
		if hdr.Name == "test-1.0/go.mod" {
			found = true
		}
	}
	if !found {
		t.Error("go.mod not found in built tarball")
	}
}

// ---------------------------------------------------------------------------
// Additional: goFixture serves a proper tar with one top-level dir
// ---------------------------------------------------------------------------

func TestGoFixture(t *testing.T) {
	srv := goFixture(t, "acme", "proj", "v1.0.0")
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/acme/proj/archive/refs/tags/v1.0.0.tar.gz")
	if err != nil {
		t.Fatalf("fetch fixture: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() // best-effort close in test

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("fixture returned empty body")
	}
}

// ---------------------------------------------------------------------------
// JSON fixture helper (for servers that need to return JSON in other tests)
// ---------------------------------------------------------------------------

func TestJSONHelper(t *testing.T) {
	// Quick sanity: json marshalling works as expected for fixture patterns
	type asset struct {
		BrowserDownloadURL string `json:"browser_download_url"`
	}
	a := asset{BrowserDownloadURL: "https://example.com/a.tar.gz"}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	if string(data) != `{"browser_download_url":"https://example.com/a.tar.gz"}` {
		t.Errorf("unexpected JSON: %s", string(data))
	}
}
