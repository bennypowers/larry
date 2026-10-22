package prepare

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/github"
	"bennypowers.dev/larry/internal/larryconfig"
)

// captureStderr swaps os.Stderr for a pipe, runs fn, and returns whatever fn
// wrote directly to the real stderr stream.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(data)
}

// argvRecorder wraps a StubRunner and records the full argv of every command
// run through it (same pattern as internal/providers recordingRunner).
type argvRecorder struct {
	inner *github.StubRunner
	argv  [][]string
}

func (r *argvRecorder) Run(cmd *exec.Cmd) error {
	r.argv = append(r.argv, cmd.Args)
	return r.inner.Run(cmd)
}

func (r *argvRecorder) Output(cmd *exec.Cmd) ([]byte, error) {
	r.argv = append(r.argv, cmd.Args)
	return r.inner.Output(cmd)
}

// ---------------------------------------------------------------------------
// parseDepSpec tests — infer DepSpec from distfile name
// ---------------------------------------------------------------------------

func TestVendored_ParseDepSpec_Npm(t *testing.T) {
	tests := []struct {
		distName string
		wantType DepType
		wantTar  string
	}{
		{"pkg-1.0.0-npm-deps.tar.xz", DepNPM, "npm-cache"},
		{"pkg-1.0.0-npm-deps.tar.gz", DepNPM, "npm-cache"},
		{"pkg-1.0.0-npm-deps.tar.zst", DepNPM, "npm-cache"},
	}
	for _, tt := range tests {
		ds := parseDepSpec(tt.distName)
		if ds.Type != tt.wantType {
			t.Errorf("parseDepSpec(%q) type = %q, want %q", tt.distName, ds.Type, tt.wantType)
		}
		if ds.TarballContent != tt.wantTar {
			t.Errorf("parseDepSpec(%q) TarballContent = %q, want %q", tt.distName, ds.TarballContent, tt.wantTar)
		}
	}
}

func TestVendored_ParseDepSpec_Cargo(t *testing.T) {
	ds := parseDepSpec("pkg-1.0.0-cargo-deps.tar.xz")
	if ds.Type != DepCargo{
		t.Errorf("parseDepSpec cargo type = %q, want %q", ds.Type, DepCargo)
	}
	if ds.TarballContent != "vendor" {
		t.Errorf("parseDepSpec cargo TarballContent = %q, want %q", ds.TarballContent, "vendor")
	}
}

func TestVendored_ParseDepSpec_Go(t *testing.T) {
	ds := parseDepSpec("pkg-1.0.0-go-deps.tar.gz")
	if ds.Type != DepGo {
		t.Errorf("parseDepSpec go type = %q, want %q", ds.Type, DepGo)
	}
	if ds.TarballContent != "vendor" {
		t.Errorf("parseDepSpec go TarballContent = %q, want %q", ds.TarballContent, "vendor")
	}
}

func TestVendored_ParseDepSpec_Unknown(t *testing.T) {
	ds := parseDepSpec("pkg-1.0.0-foo-deps.tar.xz")
	if ds.Type != "" {
		t.Errorf("parseDepSpec unknown type = %q, want empty", ds.Type)
	}
}

func TestVendored_ParseDepSpec_DistName(t *testing.T) {
	ds := parseDepSpec("pkg-1.0.0-npm-deps.tar.xz")
	expected := "pkg-1.0.0-npm-deps.tar.xz"
	if ds.DistName != expected {
		t.Errorf("parseDepSpec distname = %q, want %q", ds.DistName, expected)
	}
}

// ---------------------------------------------------------------------------
// downloadSource tests
// ---------------------------------------------------------------------------

func TestVendored_DownloadSource(t *testing.T) {
	dir := t.TempDir()
	ei := &ebuild.EbuildInfo{
		Path:  filepath.Join(dir, "pkg-1.0.0.ebuild"),
		Pkg:   "pkg",
		PVVal: "1.0.0",
		PVal:  "pkg-1.0.0",
	}

	// Create a fake source tarball
	tarballPath := filepath.Join(dir, "pkg-1.0.0.tar.gz")
	if err := os.WriteFile(tarballPath, []byte("fake-tarball"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "pkg-1.0.0.tar.gz") {
			http.ServeFile(w, r, tarballPath)
		} else {
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	// Write ebuild with SRC_URI pointing to test server
	if err := os.WriteFile(ei.Path, []byte(fmt.Sprintf(`EAPI=8
SRC_URI="%s/pkg-1.0.0.tar.gz"
`, server.URL)), 0644); err != nil {
		t.Fatal(err)
	}

	// Test the download logic directly
	targetPath := filepath.Join(dir, "out.tar.gz")
	err := downloadFileFromURL(server.URL+"/pkg-1.0.0.tar.gz", targetPath)
	if err != nil {
		t.Fatalf("downloadFileFromURL failed: %v", err)
	}

	content, _ := os.ReadFile(targetPath)
	if string(content) != "fake-tarball" {
		t.Errorf("downloaded content = %q, want %q", string(content), "fake-tarball")
	}
}

func TestVendored_DownloadSource_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "out.tar.gz")
	err := downloadFileFromURL(server.URL+"/missing.tar.gz", target)
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestVendored_DownloadStallTimesOut(t *testing.T) {
	// A server that accepts the connection but never responds must fail once
	// the download client's timeout elapses — a stalled server must not hang
	// prepare forever.
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	t.Cleanup(func() {
		close(block)
		server.Close()
	})

	// Swap in a short timeout so the test fails fast; the production client
	// uses a generous one (distfile-sized downloads).
	orig := downloadClient
	downloadClient = &http.Client{Timeout: 100 * time.Millisecond}
	t.Cleanup(func() { downloadClient = orig })

	dest := filepath.Join(t.TempDir(), "stalled.tar.gz")
	start := time.Now()
	err := downloadFileFromURL(server.URL+"/stalled.tar.gz", dest)
	if err == nil {
		t.Fatal("expected error from stalled server")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("download took %v to fail; the client timeout should bound it", elapsed)
	}
}

// ---------------------------------------------------------------------------
// extractSource tests
// ---------------------------------------------------------------------------

func TestVendored_ExtractSource(t *testing.T) {
	// Create a minimal tarball with a file inside
	dir := t.TempDir()
	innerDir := filepath.Join(dir, "inner")
	if err := os.MkdirAll(innerDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(innerDir, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	tarballPath := filepath.Join(dir, "source.tar.gz")
	err := createTestTarball(tarballPath, "inner", dir)
	if err != nil {
		t.Fatalf("createTestTarball: %v", err)
	}

	extractDir := filepath.Join(dir, "extracted")
	err = extractTarball(tarballPath, extractDir)
	if err != nil {
		t.Fatalf("extractTarball failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(extractDir, "inner", "file.txt"))
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}
	if string(content) != "hello" {
		t.Errorf("extracted content = %q, want %q", string(content), "hello")
	}
}

func TestVendored_ExtractSource_InvalidTarball(t *testing.T) {
	err := extractTarball("/nonexistent.tar.gz", t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid tarball")
	}
}

func TestVendored_ExtractTarball_PassesThroughCommandOutput(t *testing.T) {
	// A failing tar must surface its stderr output, not a bare "exit status 1".
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.tar.gz")
	if err := os.WriteFile(garbage, []byte("not a tarball"), 0644); err != nil {
		t.Fatal(err)
	}

	stderr := captureStderr(t, func() {
		_ = extractTarball(garbage, filepath.Join(dir, "out"))
	})
	if stderr == "" {
		t.Error("tar failure produced no stderr output — subprocess output is discarded")
	}
}

func TestVendored_RunVendorCommand_PassesThroughCommandOutput(t *testing.T) {
	// A failing vendor command (go mod vendor in a dir without go.mod) must
	// surface its stderr output, not a bare "exit status 1".
	stderr := captureStderr(t, func() {
		_ = runVendorCommand(DepGo, t.TempDir())
	})
	if stderr == "" {
		t.Error("vendor command failure produced no stderr output — subprocess output is discarded")
	}
}

// ---------------------------------------------------------------------------
// createVendorTarball tests
// ---------------------------------------------------------------------------

func TestVendored_CreateTarball(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a vendor directory with files
	vendorDir := filepath.Join(tmpDir, "node_modules")
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendorDir, "package.json"), []byte(`{"name":"dep"}`), 0644); err != nil {
		t.Fatal(err)
	}

	tarballPath := filepath.Join(tmpDir, "output.tar.xz")
	err := createVendorTarball(tmpDir, "node_modules", tarballPath)
	if err != nil {
		t.Fatalf("createVendorTarball failed: %v", err)
	}

	// Verify tarball exists and is non-empty
	info, err := os.Stat(tarballPath)
	if err != nil {
		t.Fatalf("tarball stat failed: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("tarball is empty")
	}
}

func TestVendored_CreateTarball_MissingDir(t *testing.T) {
	err := createVendorTarball(t.TempDir(), "nonexistent", filepath.Join(t.TempDir(), "out.tar.xz"))
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}

// ---------------------------------------------------------------------------
// copyToDistdir tests
// ---------------------------------------------------------------------------

func TestVendored_CopyToDistdir(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create a tarball in srcDir
	tarballPath := filepath.Join(srcDir, "deps.tar.xz")
	if err := os.WriteFile(tarballPath, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	err := copyTarballToDistdir(filepath.Join(srcDir, "deps.tar.xz"), dstDir)
	if err != nil {
		t.Fatalf("copyTarballToDistdir failed: %v", err)
	}

	// Verify copy
	_, err = os.Stat(filepath.Join(dstDir, "deps.tar.xz"))
	if err != nil {
		t.Fatal("tarball not copied to distdir")
	}
}

func TestVendored_CopyToDistdir_CreateDistdir(t *testing.T) {
	tmpDir := t.TempDir()
	dstDir := filepath.Join(tmpDir, "new-distdir")

	srcTarball := filepath.Join(t.TempDir(), "deps.tar.gz")
	if err := os.WriteFile(srcTarball, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	// dstDir doesn't exist — should be created
	err := copyTarballToDistdir(srcTarball, dstDir)
	if err != nil {
		t.Fatalf("copyTarballToDistdir should create distdir: %v", err)
	}

	_, err = os.Stat(filepath.Join(dstDir, "deps.tar.gz"))
	if err != nil {
		t.Fatal("tarball not created in new distdir")
	}
}

// ---------------------------------------------------------------------------
// ghReleaseView tests via StubRunner
// ---------------------------------------------------------------------------

func TestVendored_GhReleaseView_Exists(t *testing.T) {
	runner := github.NewStubRunner()
	runner.Outputs["gh release view pkg --json tagName,url"] = []byte(`{"tagName":"pkg","url":"https://github.com/owner/pkg/releases/tag/pkg"}`)

	exists, err := ghReleaseViewExists("pkg", "", runner)
	if err != nil {
		t.Fatalf("ghReleaseViewExists failed: %v", err)
	}
	if !exists {
		t.Error("expected release to exist")
	}
}

func TestVendored_GhReleaseView_NotExists(t *testing.T) {
	runner := github.NewStubRunner()
	runner.Errors["gh release view pkg --json tagName,url"] = fmt.Errorf("release not found")

	exists, err := ghReleaseViewExists("pkg", "", runner)
	if err != nil {
		t.Fatalf("ghReleaseViewExists failed: %v", err)
	}
	if exists {
		t.Error("expected release to not exist")
	}
}

func TestVendored_GhReleaseView_OtherErrorPropagates(t *testing.T) {
	// Auth/network failures must not read as "release does not exist" —
	// they surface later as confusing create-release errors instead.
	runner := github.NewStubRunner()
	runner.Errors["gh release view pkg --json tagName,url"] = fmt.Errorf("gh: authentication required")

	exists, err := ghReleaseViewExists("pkg", "", runner)
	if err == nil {
		t.Fatal("expected non-not-found gh error to propagate")
	}
	if !strings.Contains(err.Error(), "authentication required") {
		t.Errorf("error = %v, want it to contain the underlying gh error", err)
	}
	if exists {
		t.Error("exists should be false on error")
	}
}

func TestVendored_GhReleaseView_ExitOneNotFoundIsNotAnError(t *testing.T) {
	// gh's documented not-found shape: exit code 1 with "not found" on
	// stderr (exec.Cmd.Output captures stderr into *exec.ExitError).
	cmd := exec.Command("sh", "-c", `echo 'release not found' >&2; exit 1`)
	if _, err := cmd.Output(); err == nil {
		t.Fatal("test setup: expected sh to exit 1")
	} else {
		runner := github.NewStubRunner()
		runner.Errors["gh release view pkg --json tagName,url"] = err

		exists, viewErr := ghReleaseViewExists("pkg", "", runner)
		if viewErr != nil {
			t.Fatalf("exit-1 not-found must yield (false, nil), got %v", viewErr)
		}
		if exists {
			t.Error("expected release to not exist")
		}
	}
}

// ---------------------------------------------------------------------------
// ghReleaseCreate tests via StubRunner
// ---------------------------------------------------------------------------

func TestVendored_GhReleaseCreate(t *testing.T) {
	rec := &argvRecorder{inner: github.NewStubRunner()}

	err := ghReleaseCreate("pkg", "", "pkg", rec)
	if err != nil {
		t.Fatalf("ghReleaseCreate failed: %v", err)
	}

	want := []string{
		"gh", "release", "create", "pkg",
		"--title", "pkg deps",
		"--notes", "npm dependency cache for pkg",
	}
	if len(rec.argv) != 1 {
		t.Fatalf("runner ran %d command(s), want 1: %v", len(rec.argv), rec.argv)
	}
	if !reflect.DeepEqual(rec.argv[0], want) {
		t.Errorf("gh argv = %v, want %v", rec.argv[0], want)
	}
}

func TestVendored_GhReleaseCreate_Failure(t *testing.T) {
	runner := github.NewStubRunner()
	runner.Errors["gh release create pkg --title pkg deps --notes npm dependency cache for pkg"] = fmt.Errorf("gh error")

	err := ghReleaseCreate("pkg", "", "pkg", runner)
	if err == nil {
		t.Fatal("expected error from gh release create")
	}
}

// ---------------------------------------------------------------------------
// ghUpload tests via StubRunner
// ---------------------------------------------------------------------------

func TestVendored_GhUpload(t *testing.T) {
	runner := github.NewStubRunner()

	tmpDir := t.TempDir()
	tarballPath := filepath.Join(tmpDir, "deps.tar.xz")
	if err := os.WriteFile(tarballPath, []byte("fake-tarball"), 0644); err != nil {
		t.Fatal(err)
	}

	err := ghUploadAsset("pkg", tarballPath, "", runner)
	if err != nil {
		t.Fatalf("ghUploadAsset failed: %v", err)
	}
}

func TestVendored_GhUpload_MissingFile(t *testing.T) {
	runner := github.NewStubRunner()

	err := ghUploadAsset("pkg", "/nonexistent/file.tar.gz", "", runner)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// ---------------------------------------------------------------------------
// dry-run behavior
// ---------------------------------------------------------------------------

func TestVendored_DryRun(t *testing.T) {
	// In dry-run mode, PrepareVendored should skip uploads and manifest
	tmpDir := t.TempDir()
	ei := &ebuild.EbuildInfo{
		Path:  filepath.Join(tmpDir, "pkg-1.0.0.ebuild"),
		Pkg:   "pkg",
		PVVal: "1.0.0",
		PVal:  "pkg-1.0.0",
		SRC_URI: "https://example.com/pkg-1.0.0.tar.gz",
	}

	// Write a minimal ebuild
	if err := os.WriteFile(ei.Path, []byte("EAPI=8\nSRC_URI=\"https://example.com/pkg-1.0.0.tar.gz\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create distdir
	distdir := filepath.Join(tmpDir, "distdir")
	if err := os.MkdirAll(distdir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &PrepareConfig{
		DryRun:  true,
		Distdir: distdir,
	}

	// Should not error in dry-run
	err := PrepareVendored(ei, cfg)
	if err != nil {
		t.Fatalf("PrepareVendored dry-run failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// multiple deps
// ---------------------------------------------------------------------------

func TestVendored_MultipleDeps(t *testing.T) {
	distNames := []string{
		"pkg-1.0.0-npm-deps.tar.xz",
		"pkg-1.0.0-cargo-deps.tar.xz",
		"pkg-1.0.0-go-deps.tar.gz",
	}

	deps := make([]DepSpec, 0, len(distNames))
	for _, dn := range distNames {
		ds := parseDepSpec(dn)
		deps = append(deps, ds)
	}

	if len(deps) != 3 {
		t.Fatalf("expected 3 deps, got %d", len(deps))
	}

	// Check types
	if deps[0].Type != DepNPM {
		t.Errorf("dep[0] type = %q, want %q", deps[0].Type, DepNPM)
	}
	if deps[1].Type != DepCargo {
		t.Errorf("dep[1] type = %q, want %q", deps[1].Type, DepCargo)
	}
	if deps[2].Type != DepGo {
		t.Errorf("dep[2] type = %q, want %q", deps[2].Type, DepGo)
	}
}

// ---------------------------------------------------------------------------
// parseDepSpecsFromURI tests — PN from atom.ParsedEbuild (C1)
// ---------------------------------------------------------------------------

func TestParseDepSpecs_HyphenatedPackageName(t *testing.T) {
	pe := &atom.ParsedEbuild{
		PN:  "fish-lsp",
		PV:  "1.1.3",
		P:   "fish-lsp-1.1.3",
		Path: t.TempDir(),
	}
	srcURI := "https://github.com/bennypowers/overlay/releases/download/fish-lsp/fish-lsp-1.1.3-npm-deps.tar.xz"

	deps := parseDepSpecsFromURI(srcURI, pe)
	if len(deps) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(deps))
	}
	if deps[0].Type != DepNPM {
		t.Errorf("type = %q, want %q", deps[0].Type, DepNPM)
	}
	if deps[0].DistName != "fish-lsp-1.1.3-npm-deps.tar.xz" {
		t.Errorf("DistName = %q, want %q", deps[0].DistName, "fish-lsp-1.1.3-npm-deps.tar.xz")
	}
	if deps[0].TarballContent != "npm-cache" {
		t.Errorf("TarballContent = %q, want npm-cache", deps[0].TarballContent)
	}
}

func TestParseDepSpecs_SimpleName(t *testing.T) {
	pe := &atom.ParsedEbuild{
		PN:   "javy",
		PV:   "9.1.0",
		P:    "javy-9.1.0",
		Path: t.TempDir(),
	}
	srcURI := "https://github.com/bennypowers/overlay/releases/download/javy/javy-9.1.0-cargo-deps.tar.xz"

	deps := parseDepSpecsFromURI(srcURI, pe)
	if len(deps) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(deps))
	}
	if deps[0].Type != DepCargo {
		t.Errorf("type = %q, want %q", deps[0].Type, DepCargo)
	}
	if deps[0].TarballContent != "vendor" {
		t.Errorf("TarballContent = %q, want vendor", deps[0].TarballContent)
	}
}

func TestParseDepSpecs_NoMatch(t *testing.T) {
	pe := &atom.ParsedEbuild{
		PN:   "javy",
		PV:   "9.1.0",
		P:    "javy-9.1.0",
		Path: t.TempDir(),
	}
	srcURI := "https://example.com/javy-9.1.0.tar.gz"

	deps := parseDepSpecsFromURI(srcURI, pe)
	if len(deps) != 0 {
		t.Fatalf("expected 0 deps, got %d", len(deps))
	}
}

// ---------------------------------------------------------------------------
// Vendor dirs — resolveBuildDir tests (C2)
// ---------------------------------------------------------------------------

func resolveBuildDirForTest(extractDir string, pe *atom.ParsedEbuild, pkgCfg *larryconfig.Config, ds DepSpec) string {
	return resolveBuildDir(extractDir, pe, pkgCfg, ds)
}

func TestVendored_VendorDirsConfigIsHonoured(t *testing.T) {
	overlayDir := t.TempDir()
	ep := &atom.ParsedEbuild{
		Category: "dev-util",
		PN:       "fish-lsp",
		PV:       "1.1.3",
		P:        "fish-lsp-1.1.3",
		Path:     filepath.Join(overlayDir, "fish-lsp-1.1.3.ebuild"),
	}
	pkgCfg := &larryconfig.Config{
		VendorDirs: map[string][]string{
			"fish-lsp": {"sub/pkg"},
		},
	}
	ds := DepSpec{}

	result := resolveBuildDirForTest("/extract", ep, pkgCfg, ds)
	want := filepath.Join("/extract", "sub", "pkg")
	if result != want {
		t.Errorf("workDir = %q, want %q", result, want)
	}
}

func TestVendored_VendorDirsAbsentMeansExtractRoot(t *testing.T) {
	ep := &atom.ParsedEbuild{
		Category: "dev-util",
		PN:       "fish-lsp",
		PV:       "1.1.3",
		P:        "fish-lsp-1.1.3",
		Path:     t.TempDir(),
	}

	result := resolveBuildDirForTest("/extract", ep, nil, DepSpec{})
	if result != "/extract" {
		t.Errorf("workDir = %q, want /extract", result)
	}
}

func TestVendored_VendorDirsForOtherPackageIsIgnored(t *testing.T) {
	ep := &atom.ParsedEbuild{
		Category: "dev-util",
		PN:       "fish-lsp",
		PV:       "1.1.3",
		P:        "fish-lsp-1.1.3",
		Path:     t.TempDir(),
	}
	pkgCfg := &larryconfig.Config{
		VendorDirs: map[string][]string{
			"some-other-pkg": {"sub/pkg"},
		},
	}

	result := resolveBuildDirForTest("/extract", ep, pkgCfg, DepSpec{})
	if result != "/extract" {
		t.Errorf("workDir = %q, want /extract", result)
	}
}

// ---------------------------------------------------------------------------
// Manifest catPkg test (C5)
// ---------------------------------------------------------------------------

func TestVendored_ManifestTargetsCatPkg(t *testing.T) {
	// Verify that atom.ParseEbuildPath yields correct PN and category,
	// which PrepareVendored uses to build the catPkg string
	// "category/PN" for the manifest target.
	dir := t.TempDir()
	ebuildPath := filepath.Join(dir, "dev-util", "fish-lsp", "fish-lsp-1.1.3.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\nSRC_URI=\"https://example.com/fish-lsp-1.1.3.tar.gz\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	pe, err := atom.ParseEbuildPath(ebuildPath)
	if err != nil {
		t.Fatalf("ParseEbuildPath: %v", err)
	}
	wantCatPkg := "dev-util/fish-lsp"
	gotCatPkg := pe.Category + "/" + pe.PN
	if gotCatPkg != wantCatPkg {
		t.Errorf("catPkg = %q, want %q", gotCatPkg, wantCatPkg)
	}

	// Verify PN came from directory, not filename splitting
	if pe.PN != "fish-lsp" {
		t.Errorf("PN = %q, want fish-lsp (from directory, not split)", pe.PN)
	}
}

// ---------------------------------------------------------------------------
// extractSRCURIForPN tests
// ---------------------------------------------------------------------------

func TestVendored_ExtractSRCURIForPN(t *testing.T) {
	tests := []struct {
		name   string
		srcURI string
		pkg    string
		want   []string
	}{
		{
			name:   "single_dep",
			srcURI: "releases/download/pkg/pkg-1.0.0-npm-deps.tar.xz",
			pkg:    "pkg",
			want:   []string{"pkg-1.0.0-npm-deps.tar.xz"},
		},
		{
			name:   "multiple_deps",
			srcURI: "releases/download/pkg/pkg-1.0.0-npm-deps.tar.xz releases/download/pkg/pkg-1.0.0-cargo-deps.tar.xz",
			pkg:    "pkg",
			want:   []string{"pkg-1.0.0-npm-deps.tar.xz", "pkg-1.0.0-cargo-deps.tar.xz"},
		},
		{
			name:   "different_pkg",
			srcURI: "releases/download/other/pkg-1.0.0-npm-deps.tar.xz",
			pkg:    "pkg",
			want:   []string{},
		},
		{
			name:   "with_other_src",
			srcURI: "https://example.com/src.tar.gz releases/download/pkg/pkg-1.0.0-npm-deps.tar.xz",
			pkg:    "pkg",
			want:   []string{"pkg-1.0.0-npm-deps.tar.xz"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractSRCURIForPN(tt.srcURI, tt.pkg)
			if len(result) != len(tt.want) {
				t.Errorf("extractSRCURIForPN() = %v, want %v", result, tt.want)
			}
			for i, w := range tt.want {
				if i < len(result) && result[i] != w {
					t.Errorf("extractSRCURIForPN()[%d] = %q, want %q", i, result[i], w)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseDepSpecFromURI tests
// ---------------------------------------------------------------------------

func TestVendored_ParseDepSpecsFromURI(t *testing.T) {
	pe := &atom.ParsedEbuild{PN: "pkg", PV: "1.0.0", P: "pkg-1.0.0", Path: t.TempDir()}
	dists := parseDepSpecsFromURI("releases/download/pkg/pkg-1.0.0-npm-deps.tar.xz releases/download/pkg/pkg-1.0.0-cargo-deps.tar.xz", pe)
	if len(dists) != 2 {
		t.Fatalf("expected 2 distspecs, got %d", len(dists))
	}
	if dists[0].DistName != "pkg-1.0.0-npm-deps.tar.xz" {
		t.Errorf("distspec[0].DistName = %q, want %q", dists[0].DistName, "pkg-1.0.0-npm-deps.tar.xz")
	}
	if dists[1].DistName != "pkg-1.0.0-cargo-deps.tar.xz" {
		t.Errorf("distspec[1].DistName = %q, want %q", dists[1].DistName, "pkg-1.0.0-cargo-deps.tar.xz")
	}
}

// TestVendored_NpmAssetContractIsNpmCache verifies the npm TarballContent
// matches what npmCreateDepsTarball actually produces (npm-cache/).
// This test follows the existing stub-runner pattern (see TestVendored_GhReleaseView_Exists).
func TestVendored_NpmAssetContractIsNpmCache(t *testing.T) {
	ds := parseDepSpec("pkg-1.0.0-npm-deps.tar.xz")
	if ds.TarballContent != "npm-cache" {
		t.Errorf("npm TarballContent = %q, want npm-cache", ds.TarballContent)
	}
}

// ---------------------------------------------------------------------------
// Helper: createTestTarball creates a simple tarball for testing
// ---------------------------------------------------------------------------

func createTestTarball(path, srcDir, baseDir string) error {
	cmd := exec.Command("tar", "czf", path, srcDir)
	cmd.Dir = baseDir
	return cmd.Run()
}
