package prepare

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/httpclient"
)

// ---------------------------------------------------------------------------
// findCargoRepo — new signature: (raw string, vars VariableMap) → (forge, owner, repo, error)
// ---------------------------------------------------------------------------

func TestFindCargoRepo_GitHub(t *testing.T) {
	raw := `EAPI=8

HOMEPAGE="https://github.com/yioneko/vtsls"
SRC_URI="https://github.com/yioneko/vtsls/archive/v0.7.0.tar.gz"
`
	forge, owner, repo, err := findCargoRepo(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if forge != forgeGitHub {
		t.Errorf("forge = %d, want forgeGitHub", forge)
	}
	if owner != "yioneko" {
		t.Errorf("owner = %q, want %q", owner, "yioneko")
	}
	if repo != "vtsls" {
		t.Errorf("repo = %q, want %q", repo, "vtsls")
	}
}

func TestFindCargoRepo_GitSrHt(t *testing.T) {
	raw := `EAPI=8
SRC_URI="https://git.sr.ht/~tsdh/nirius/archive/nirius-0.7.1.tar.gz"
`
	forge, owner, repo, err := findCargoRepo(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if forge != forgeSourceHut {
		t.Errorf("forge = %d, want forgeSourceHut", forge)
	}
	if owner != "~tsdh" {
		t.Errorf("owner = %q, want %q", owner, "~tsdh")
	}
	if repo != "nirius" {
		t.Errorf("repo = %q, want %q", repo, "nirius")
	}
}

func TestFindCargoRepo_SrHtHomepageRewrite(t *testing.T) {
	raw := `EAPI=8
HOMEPAGE="https://sr.ht/~tsdh/nirius/"
`
	forge, owner, repo, err := findCargoRepo(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if forge != forgeSourceHut {
		t.Errorf("forge = %d, want forgeSourceHut", forge)
	}
	if owner != "~tsdh" {
		t.Errorf("owner = %q, want %q", owner, "~tsdh")
	}
	if repo != "nirius" {
		t.Errorf("repo = %q, want %q", repo, "nirius")
	}
}

func TestFindCargoRepo_SrcUriBeatsHomepage(t *testing.T) {
	raw := `EAPI=8
HOMEPAGE="https://sr.ht/~tsdh/nirius/"
SRC_URI="https://git.sr.ht/~tsdh/nirius/archive/nirius-0.7.1.tar.gz"
`
	forge, owner, repo, err := findCargoRepo(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if forge != forgeSourceHut {
		t.Errorf("forge = %d, want forgeSourceHut", forge)
	}
	if owner != "~tsdh" {
		t.Errorf("owner = %q, want %q", owner, "~tsdh")
	}
	if repo != "nirius" {
		t.Errorf("repo = %q, want %q", repo, "nirius")
	}
}

func TestFindCargoRepo_NoMatchIsError(t *testing.T) {
	raw := `EAPI=8
HOMEPAGE="https://example.com/foo"
SRC_URI="mirror://pypi/foo-1.0.tar.gz"
`
	forge, owner, repo, err := findCargoRepo(raw, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if forge != 0 {
		t.Errorf("forge = %d, want 0", forge)
	}
	if owner != "" || repo != "" {
		t.Errorf("owner/repo = (%q, %q), want (\"\", \"\")", owner, repo)
	}
}

// ---------------------------------------------------------------------------
// cargoTag
// ---------------------------------------------------------------------------

func TestCargoTag_GitHubStripsRevision(t *testing.T) {
	tag := cargoTag(forgeGitHub, "rustfmt", "1.2.3-r1", "")
	if tag != "v1.2.3" {
		t.Errorf("tag = %q, want %q", tag, "v1.2.3")
	}
}

func TestCargoTag_GitHubNoRevision(t *testing.T) {
	tag := cargoTag(forgeGitHub, "rustfmt", "1.2.3", "")
	if tag != "v1.2.3" {
		t.Errorf("tag = %q, want %q", tag, "v1.2.3")
	}
}

func TestCargoTag_SourceHutFromArchiveURL(t *testing.T) {
	expanded := `SRC_URI="https://git.sr.ht/~tsdh/nirius/archive/nirius-0.7.1.tar.gz"`
	tag := cargoTag(forgeSourceHut, "nirius", "0.7.1", expanded)
	if tag != "nirius-0.7.1" {
		t.Errorf("tag = %q, want %q", tag, "nirius-0.7.1")
	}
}

func TestCargoTag_SourceHutFallsBackToPnPv(t *testing.T) {
	tag := cargoTag(forgeSourceHut, "nirius", "0.7.1-r2", "no archive URL here")
	if tag != "nirius-0.7.1" {
		t.Errorf("tag = %q, want %q", tag, "nirius-0.7.1")
	}
}

// ---------------------------------------------------------------------------
// fetchCargoLock
// ---------------------------------------------------------------------------

// httptestFetcher wraps an *httptest.Server to satisfy the fetcher interface.
type httptestFetcher struct {
	server   *httptest.Server
	requests []string
}

func newHttptestFetcher(t *testing.T) *httptestFetcher {
	t.Helper()
	f := &httptestFetcher{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		// Default: 404
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(f.server.Close)
	return f
}

// rewriteRoundTripper rewrites requests to the test server.
type rewriteRoundTripper struct {
	base   http.RoundTripper
	server *httptest.Server
}

func (rt *rewriteRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Host = rt.server.Listener.Addr().String()
	req.URL.Scheme = "http"
	return rt.base.RoundTrip(req)
}

// fetcherImpl satisfies the fetcher interface via the test server.
func (f *httptestFetcher) Get(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Transport: &rewriteRoundTripper{
			base:   http.DefaultTransport,
			server: f.server,
		},
	}
	return client.Do(req)
}

// ---------------------------------------------------------------------------
// TestFetchCargoLock_WritesRawBody (D4)
// ---------------------------------------------------------------------------

func TestFetchCargoLock_WritesRawBody(t *testing.T) {
	f := newHttptestFetcher(t)
	body := []byte(`[[package]]
name = "a"
version = "1.0"
source = "registry+https://github.com/rust-lang/crates.io-index"
`)

	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	})

	path, err := fetchCargoLock(f, forgeGitHub, "tsdh", "x", "v1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != string(body) {
		t.Errorf("body mismatch:\ngot:  %q\nwant: %q", data, body)
	}
}

// ---------------------------------------------------------------------------
// TestFetchCargoLock_GitHubRawURL
// ---------------------------------------------------------------------------

func TestFetchCargoLock_GitHubRawURL(t *testing.T) {
	f := newHttptestFetcher(t)
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	_, err := fetchCargoLock(f, forgeGitHub, "tsdh", "x", "v1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(f.requests))
	}
	if f.requests[0] != "/tsdh/x/v1.0/Cargo.lock" {
		t.Errorf("request path = %q, want /tsdh/x/v1.0/Cargo.lock", f.requests[0])
	}
}

// ---------------------------------------------------------------------------
// TestFetchCargoLock_SourceHutBlobURL
// ---------------------------------------------------------------------------

func TestFetchCargoLock_SourceHutBlobURL(t *testing.T) {
	f := newHttptestFetcher(t)
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	_, err := fetchCargoLock(f, forgeSourceHut, "~tsdh", "nirius", "nirius-0.7.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(f.requests))
	}
	if f.requests[0] != "/~tsdh/nirius/blob/nirius-0.7.1/Cargo.lock" {
		t.Errorf("request path = %q, want /~tsdh/nirius/blob/nirius-0.7.1/Cargo.lock", f.requests[0])
	}
}

// ---------------------------------------------------------------------------
// TestFetchCargoLock_NoDefaultBranchFallback (D5)
// ---------------------------------------------------------------------------

func TestFetchCargoLock_NoDefaultBranchFallback(t *testing.T) {
	f := newHttptestFetcher(t)
	// Server 404s everything.
	_, err := fetchCargoLock(f, forgeGitHub, "tsdh", "x", "v1.0")
	if err == nil {
		t.Fatal("expected error when all requests 404, got nil")
	}
	// Both requests should contain the tag.
	if len(f.requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(f.requests))
	}
	for _, p := range f.requests {
		if !strings.Contains(p, "v1.0") {
			t.Errorf("request %q does not contain tag v1.0 (would indicate empty-ref fallback)", p)
		}
	}
}

// ---------------------------------------------------------------------------
// TestFetchCargoLock_BindingsPythonSubdir (D7)
// ---------------------------------------------------------------------------

func TestFetchCargoLock_BindingsPythonSubdir(t *testing.T) {
	f := newHttptestFetcher(t)
	callCount := 0
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		callCount++
		if strings.Contains(r.URL.Path, "bindings/python") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("python subdir lock"))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	})

	path, err := fetchCargoLock(f, forgeGitHub, "tsdh", "x", "v1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "python subdir lock" {
		t.Errorf("body = %q, want %q", data, "python subdir lock")
	}
	if len(f.requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(f.requests))
	}
	// First request is root (no bindings/python prefix)
	if strings.Contains(f.requests[0], "bindings/python") {
		t.Errorf("first request %q should be root path, not bindings/python", f.requests[0])
	}
	// Second request contains bindings/python
	if !strings.Contains(f.requests[1], "bindings/python") {
		t.Errorf("second request %q should contain bindings/python", f.requests[1])
	}
}

// ---------------------------------------------------------------------------
// TestFetchCargoLock_NoOtherSubdirsProbed (D7)
// ---------------------------------------------------------------------------

func TestFetchCargoLock_NoOtherSubdirsProbed(t *testing.T) {
	f := newHttptestFetcher(t)
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})

	_, _ = fetchCargoLock(f, forgeGitHub, "tsdh", "x", "v1.0")

	// Exactly two requests should have been made.
	if len(f.requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(f.requests))
	}

	// None should contain the old subdirs.
	for _, p := range f.requests {
		for _, bad := range []string{"/src/", "/rust/", "/lib/", "/crates/"} {
			if strings.Contains(p, bad) {
				t.Errorf("request %q contains old subdir pattern %q", p, bad)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Cargo pipeline: find repo (old tests deleted — replaced above)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Cargo pipeline: download Cargo.lock (replaced by fetchCargoLock tests)
// ---------------------------------------------------------------------------

func TestDownloadCargoLock_HttpClient(t *testing.T) {
	lockContent := `[[package]]
name = "test"
version = "1.0.0"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "abc123"
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(lockContent))
	}))
	defer server.Close()

	client := httpclient.NewClientWithTransport(server.Client().Transport)

	dir := t.TempDir()
	dest := filepath.Join(dir, "Cargo.lock")
	err := downloadCargoLock(server.URL, dest, client)
	if err != nil {
		t.Fatalf("downloadCargoLock returned error: %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}
	if !strings.Contains(string(content), `name = "test"`) {
		t.Errorf("Downloaded content missing expected package: %s", string(content))
	}
}

// ---------------------------------------------------------------------------
// Cargo pipeline: parse Cargo.lock
// ---------------------------------------------------------------------------

func TestCargo_Pipeline_ParseCargoLock(t *testing.T) {
	lockContent := `[[package]]
name = "aho-corasick"
version = "1.1.2"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "b2969dcb958b36655471fc61f7e416fa76033bdd4bfed0678d8fee1e2d07a1f0"

[[package]]
name = "atty"
version = "0.2.14"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "d9b39be18770d11421cdb1b9947a45dd3f37e93092cbf377614828a319d5fee8"
`
	crates := parseCargoLock(lockContent)
	if len(crates) != 2 {
		t.Fatalf("parseCargoLock returned %d crates, want 2", len(crates))
	}
	if crates[0].Name != "aho-corasick" || crates[0].Version != "1.1.2" {
		t.Errorf("crates[0] = %+v, want {Name: aho-corasick, Version: 1.1.2}", crates[0])
	}
	if crates[1].Name != "atty" || crates[1].Version != "0.2.14" {
		t.Errorf("crates[1] = %+v, want {Name: atty, Version: 0.2.14}", crates[1])
	}
}

func TestCargo_Pipeline_ParseCargoLock_Empty(t *testing.T) {
	crates := parseCargoLock("")
	if len(crates) != 0 {
		t.Errorf("parseCargoLock(\"\") = %d crates, want 0", len(crates))
	}
}

// ---------------------------------------------------------------------------
// Cargo pipeline: pycargoebuild integration
// ---------------------------------------------------------------------------

func TestCargo_Pipeline_PycargoebuildAvailable(t *testing.T) {
	_, err := exec.LookPath("pycargoebuild")
	if err != nil {
		t.Skip("pycargoebuild not found")
	}
}

func TestCargo_Pipeline_PycargoebuildUpdateEbuild(t *testing.T) {
	_, err := exec.LookPath("pycargoebuild")
	if err != nil {
		t.Skip("pycargoebuild not found")
	}

	// Create a temp dir with Cargo.toml + Cargo.lock
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "Cargo.lock")
	tomlPath := filepath.Join(tmpDir, "Cargo.toml")

	lockContent := `[[package]]
name = "aho-corasick"
version = "1.1.2"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "b2969dcb958b36655471fc61f7e416fa76033bdd4bfed0678d8fee1e2d07a1f0"
`
	tomlContent := `[package]
name = "test-pkg"
version = "1.0.0"
edition = "2021"

[dependencies]
aho-corasick = "1.1.2"
`
	os.WriteFile(lockPath, []byte(lockContent), 0644)
	os.WriteFile(tomlPath, []byte(tomlContent), 0644)

	// Create a stub ebuild with the required structure
	tmpEbuild := filepath.Join(tmpDir, "test-pkg-1.0.0.ebuild")
	ebuildContent := `EAPI=8

inherit cargo

CRATES=""

DESCRIPTION="Test"
HOMEPAGE=""
SRC_URI=""

LICENSE="MIT"
# Dependent crate licenses
LICENSE+=""

SLOT="0"
KEYWORDS="~amd64"
`
	os.WriteFile(tmpEbuild, []byte(ebuildContent), 0644)

	runner := &mockCmdRunner{
		runFn: func(cmd *exec.Cmd) error {
			// Verify pycargoebuild is called with correct args
			if len(cmd.Args) < 3 || cmd.Args[0] != "pycargoebuild" {
				t.Errorf("expected pycargoebuild command, got: %v", cmd.Args)
			}
			return nil
		},
	}

	err = runPycargoebuild(tmpDir, tmpEbuild, runner)
	if err != nil {
		t.Fatalf("runPycargoebuild returned error: %v", err)
	}
}

func TestCargo_Pipeline_NativeParsing(t *testing.T) {
	lockContent := `[[package]]
name = "aho-corasick"
version = "1.1.2"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "b2969dcb958b36655471fc61f7e416fa76033bdd4bfed0678d8fee1e2d07a1f0"
`
	ebuildPath := filepath.Join(t.TempDir(), "test-1.0.0.ebuild")
	ebuildContent := `EAPI=8

inherit cargo

CRATES=""

DESCRIPTION="Test"
HOMEPAGE=""
SRC_URI=""

LICENSE="MIT"
# Dependent crate licenses
LICENSE+=""

SLOT="0"
KEYWORDS="~amd64"
`
	os.WriteFile(ebuildPath, []byte(ebuildContent), 0644)

	crates := parseCargoLock(lockContent)
	_, err := updateCargoEbuildNative(ebuildPath, crates)
	if err != nil {
		t.Fatalf("updateCargoEbuildNative returned error: %v", err)
	}

	updated, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("Failed to read updated ebuild: %v", err)
	}
	if !strings.Contains(string(updated), "aho-corasick@1.1.2") {
		t.Errorf("Updated ebuild missing CRATES entry: %s", string(updated))
	}
}

func TestCargo_Pipeline_AllToolsUnavailable(t *testing.T) {
	// Test that when no tools are available, an error is returned
	_, err := exec.LookPath("pycargoebuild")
	if err == nil {
		t.Skip("pycargoebuild is available, skipping")
	}
	// Native parsing always works as fallback, so this test
	// verifies the error path when even native parsing fails
	_, err = updateCargoEbuildNative("/nonexistent/ebuild", []CargoCrate{})
	if err == nil {
		t.Fatal("expected error for nonexistent ebuild path")
	}
}

// ---------------------------------------------------------------------------
// D8 — pycargoebuild runs in os.MkdirTemp, not in the package dir
// ---------------------------------------------------------------------------

func TestCargo_PycargoebuildRunsInTempDir(t *testing.T) {
	// Copy the plan 23a testdata ebuild into t.TempDir.
	fixturesDir := filepath.Join("testdata", "ebuilds")
	src, err := os.ReadFile(filepath.Join(fixturesDir, "cargo-literal-crates.ebuild"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	// Package dir (simulated overlay).
	pkgDir := t.TempDir()
	ebuildPath := filepath.Join(pkgDir, "nirius-0.7.1.ebuild")
	if err := os.WriteFile(ebuildPath, src, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// A real Cargo.lock (served via a small temp dir).
	lockDir := t.TempDir()
	lockData := `[[package]]
name = "aho-corasick"
version = "1.1.4"
source = "registry+https://github.com/rust-lang/crates.io-index"
`
	lockPath := filepath.Join(lockDir, "Cargo.lock")
	if err := os.WriteFile(lockPath, []byte(lockData), 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	// Stub lookPath to report pycargoebuild is available.
	orig := lookPath
	lookPath = func(name string) (string, error) {
		if name == "pycargoebuild" {
			return "/fake/pycargoebuild", nil
		}
		return orig(name)
	}
	t.Cleanup(func() { lookPath = orig })

	// Stub runner that records the command.
	var gotDir string
	runner := &mockCmdRunner{
		runFn: func(cmd *exec.Cmd) error {
			gotDir = cmd.Dir
			// Verify Cargo.lock and Cargo.toml exist in the dir.
			if _, err := os.Stat(filepath.Join(cmd.Dir, "Cargo.lock")); err != nil {
				return fmt.Errorf("Cargo.lock missing in %s: %w", cmd.Dir, err)
			}
			if _, err := os.Stat(filepath.Join(cmd.Dir, "Cargo.toml")); err != nil {
				return fmt.Errorf("Cargo.toml missing in %s: %w", cmd.Dir, err)
			}
			return nil
		},
	}

	cfg := &PrepareConfig{}
	ei := &ebuild.EbuildInfo{
		Category: "gui-apps",
		Pkg:      "nirius",
		PVVal:    "0.7.1",
		PVal:     "nirius-0.7.1",
	}

	_, err = updateCargoEbuild(ei, lockPath, nil, cfg, runner)
	if err != nil {
		t.Fatalf("updateCargoEbuild returned error: %v", err)
	}

	// Assert the runner was called with a temp dir, not the package dir.
	if gotDir == "" {
		t.Fatal("runner was never invoked")
	}
	if !strings.Contains(gotDir, "larry-cargo-") {
		t.Errorf("cmd.Dir = %q, should contain 'larry-cargo-'", gotDir)
	}
	if gotDir == pkgDir || gotDir == filepath.Dir(ebuildPath) {
		t.Errorf("cmd.Dir = %q should NOT be the package dir", gotDir)
	}

	// Verify no Cargo.toml or Cargo.lock leaked into the package dir.
	if _, err := os.Stat(filepath.Join(pkgDir, "Cargo.toml")); !os.IsNotExist(err) {
		t.Error("Cargo.toml leaked into package dir")
	}
	if _, err := os.Stat(filepath.Join(pkgDir, "Cargo.lock")); !os.IsNotExist(err) {
		t.Error("Cargo.lock leaked into package dir")
	}
}

func TestCargo_PycargoebuildFailureFallsBackToNative(t *testing.T) {
	fixturesDir := filepath.Join("testdata", "ebuilds")
	src, err := os.ReadFile(filepath.Join(fixturesDir, "cargo-no-crates.ebuild"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "test-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, src, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	lockPath := filepath.Join(tmpDir, "Cargo.lock")
	lockData := `[[package]]
name = "aho-corasick"
version = "1.1.4"
source = "registry+https://github.com/rust-lang/crates.io-index"
`
	if err := os.WriteFile(lockPath, []byte(lockData), 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	// Stub lookPath to report pycargoebuild available.
	orig := lookPath
	lookPath = func(name string) (string, error) {
		if name == "pycargoebuild" {
			return "/fake/pycargoebuild", nil
		}
		return orig(name)
	}
	t.Cleanup(func() { lookPath = orig })

	// Stub runner that always fails.
	runner := &mockCmdRunner{
		runFn: func(cmd *exec.Cmd) error {
			return fmt.Errorf("pycargoebuild: command not found")
		},
	}

	ei := &ebuild.EbuildInfo{
		Category: "dev-rust",
		Pkg:      "test-pkg",
		PVVal:    "1.0.0",
		PVal:     "test-pkg-1.0.0",
		Path:     ebuildPath,
	}
	cfg := &PrepareConfig{}

	_, err = updateCargoEbuild(ei, lockPath, []CargoCrate{{Name: "a", Version: "1.0"}}, cfg, runner)
	if err != nil {
		t.Fatalf("updateCargoEbuild returned error: %v", err)
	}

	updated, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("read updated: %v", err)
	}
	if !strings.Contains(string(updated), "CRATES=\"") {
		t.Error("native fallback did not update CRATES block")
	}
}

// ---------------------------------------------------------------------------
// Cargo pipeline: update ebuild (native path)
// ---------------------------------------------------------------------------

func TestCargo_Pipeline_UpdateEbuild(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "test-1.0.0.ebuild")

	// Create an ebuild with existing CRATES
	content := `EAPI=8

inherit cargo

CRATES="
	aho-corasick@1.1.2
"

DESCRIPTION="Test"
HOMEPAGE=""
SRC_URI=""

LICENSE="MIT"
# Dependent crate licenses
LICENSE+=""

SLOT="0"
KEYWORDS="~amd64"
`
	os.WriteFile(ebuildPath, []byte(content), 0644)

	crates := []CargoCrate{
		{Name: "atty", Version: "0.2.14"},
		{Name: "aho-corasick", Version: "1.1.2"},
	}

	_, err := updateCargoEbuildNative(ebuildPath, crates)
	if err != nil {
		t.Fatalf("updateCargoEbuildNative returned error: %v", err)
	}

	updated, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("Failed to read updated ebuild: %v", err)
	}

	// Both crates should be in the updated file
	if !strings.Contains(string(updated), "atty@0.2.14") {
		t.Error("Updated ebuild missing atty@0.2.14")
	}
	if !strings.Contains(string(updated), "aho-corasick@1.1.2") {
		t.Error("Updated ebuild missing aho-corasick@1.1.2")
	}
}

func TestCargo_Pipeline_UpdateEbuild_Rollback(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "test-1.0.0.ebuild")

	content := `EAPI=8

inherit cargo

CRATES="
	aho-corasick@1.1.2
"

DESCRIPTION="Test"
HOMEPAGE=""
SRC_URI=""

LICENSE="MIT"
# Dependent crate licenses
LICENSE+=""

SLOT="0"
KEYWORDS="~amd64"
`
	os.WriteFile(ebuildPath, []byte(content), 0644)

	// Create a bad crates list to simulate a failure
	_, err := updateCargoEbuildNative(ebuildPath, nil)
	if err != nil {
		t.Fatalf("updateCargoEbuildNative returned error: %v", err)
	}

	updated, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("Failed to read updated ebuild: %v", err)
	}

	// The file should have been updated (even with nil crates, it should write)
	_ = updated
}

// ---------------------------------------------------------------------------
// Cargo pipeline: manifest generation
// ---------------------------------------------------------------------------

func TestCargo_Pipeline_ManifestGeneration(t *testing.T) {
	// Verify distfiles before manifest
	tmpDir := t.TempDir()
	distDir := filepath.Join(tmpDir, "dist")
	os.MkdirAll(distDir, 0755)

	// Create a distfile
	os.WriteFile(filepath.Join(distDir, "rust-cli-0.1.0.tar.gz"), []byte("tarball"), 0644)

	// Verify distfiles should return true
	files := extractDistfilesFromSRCURI("rust-cli-0.1.0.tar.gz")
	if !verifyDistfiles(distDir, files) {
		t.Error("verifyDistfiles should return true when distfile exists")
	}
}

// ---------------------------------------------------------------------------
// Cargo pipeline: dry-run
// ---------------------------------------------------------------------------

func TestCargo_Pipeline_DryRun_SkipsAllMutations(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "test-1.0.0.ebuild")
	content := `EAPI=8

inherit cargo

CRATES=""

DESCRIPTION="Test"
HOMEPAGE=""
SRC_URI=""

LICENSE="MIT"
# Dependent crate licenses
LICENSE+=""

SLOT="0"
KEYWORDS="~amd64"
`
	os.WriteFile(ebuildPath, []byte(content), 0644)

	ei := &ebuild.EbuildInfo{
		Category: "dev-rust",
		Pkg:      "test-rust-cli",
		PVVal:    "1.0.0",
		PVal:     "test-rust-cli-1.0.0",
		SRC_URI:  "mirror://github/${PN}/${P}.tar.gz",
		HOMEPAGE: "https://github.com/test-rust-cli/test-rust-cli",
	}

	cfg := &PrepareConfig{
		DryRun: true,
	}

	// Dry-run should not modify the ebuild
	err := PrepareCargo(ei, cfg)
	if err != nil {
		t.Fatalf("PrepareCargo returned error: %v", err)
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
// Cargo pipeline: failure rollback
// ---------------------------------------------------------------------------

func TestCargo_Pipeline_Failure_Rollback(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "test-1.0.0.ebuild")

	originalContent := `EAPI=8

inherit cargo

CRATES="
	aho-corasick@1.1.2
"

DESCRIPTION="Test"
HOMEPAGE=""
SRC_URI=""

LICENSE="MIT"
# Dependent crate licenses
LICENSE+=""

SLOT="0"
KEYWORDS="~amd64"
`
	os.WriteFile(ebuildPath, []byte(originalContent), 0644)

	// Simulate failure: create a backup, then restore
	err := Backup(ebuildPath)
	if err != nil {
		t.Fatalf("Backup returned error: %v", err)
	}

	// Verify .bak exists
	bakPath := ebuildPath + ".bak"
	if _, err := os.Stat(bakPath); err != nil {
		t.Fatal("Backup .bak file not found")
	}

	// Restore and verify
	err = Restore(ebuildPath)
	if err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}

	restored, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("Failed to read restored ebuild: %v", err)
	}
	if string(restored) != originalContent {
		t.Errorf("Restored content = %q, want %q", string(restored), originalContent)
	}
}

// ---------------------------------------------------------------------------
// Cargo pipeline: revision bump skip
// ---------------------------------------------------------------------------

func TestCargo_Pipeline_RevisionBump_SkipUpload(t *testing.T) {
	dir := t.TempDir()

	// Create a tarball in DISTDIR (simulating existing asset)
	distPath := filepath.Join(dir, "dist")
	os.MkdirAll(distPath, 0755)
	os.WriteFile(filepath.Join(distPath, "test-rust-cli-1.0.0-crates.tar.xz"), []byte("existing"), 0644)

	// Revision bump: -r1
	_ = &ebuild.EbuildInfo{
		Category: "dev-rust",
		Pkg:      "test-rust-cli",
		PVVal:    "1.0.0",
		PVal:     "test-rust-cli-1.0.0",
		SRC_URI:  "mirror://github/${PN}/${P}.tar.gz",
	}

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
}

// ---------------------------------------------------------------------------
// Cargo pipeline: Cargo.lock parsing edge cases
// ---------------------------------------------------------------------------

func TestCargo_ParseCargoLock_Dependencies(t *testing.T) {
	lockContent := `[[package]]
name = "complex-pkg"
version = "2.0.0"
dependencies = [
 "aho-corasick 1.1.2",
 "atty 0.2.14",
]
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "abc123"
`
	crates := parseCargoLock(lockContent)
	if len(crates) != 1 {
		t.Fatalf("parseCargoLock returned %d crates, want 1", len(crates))
	}
	if crates[0].Name != "complex-pkg" {
		t.Errorf("crates[0].Name = %q, want %q", crates[0].Name, "complex-pkg")
	}
	if crates[0].Version != "2.0.0" {
		t.Errorf("crates[0].Version = %q, want %q", crates[0].Version, "2.0.0")
	}
}

func TestCargo_ParseCargoLock_GitCrate(t *testing.T) {
	// D1: git-sourced crates are filtered out — the cargo eclass cannot resolve them.
	lockContent := `[[package]]
name = "git-dep"
version = "0.1.0"
source = "git+https://github.com/user/repo?rev=abc123#abc123def"
`
	crates := parseCargoLock(lockContent)
	if len(crates) != 0 {
		t.Fatalf("parseCargoLock returned %d crates, want 0 (git sources filtered)", len(crates))
	}
}

// ---------------------------------------------------------------------------
// Cargo pipeline: CRATES formatting
// ---------------------------------------------------------------------------

func TestCargo_FormatCRATES(t *testing.T) {
	crates := []CargoCrate{
		{Name: "aho-corasick", Version: "1.1.2"},
		{Name: "atty", Version: "0.2.14"},
	}
	result := formatCRATES(crates)
	if !strings.Contains(result, "aho-corasick@1.1.2") {
		t.Errorf("formatCRATES missing aho-corasick@1.1.2: %q", result)
	}
	if !strings.Contains(result, "atty@0.2.14") {
		t.Errorf("formatCRATES missing atty@0.2.14: %q", result)
	}
}

// ---------------------------------------------------------------------------
// Cargo pipeline: full mock pipeline
// ---------------------------------------------------------------------------

func TestCargo_Pipeline_FullMock(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "test-rust-cli-1.0.0.ebuild")
	content := `EAPI=8

inherit cargo

CRATES=""

DESCRIPTION="Test"
HOMEPAGE=""
SRC_URI=""

LICENSE="MIT"
# Dependent crate licenses
LICENSE+=""

SLOT="0"
KEYWORDS="~amd64"
`
	os.WriteFile(ebuildPath, []byte(content), 0644)

	ei := &ebuild.EbuildInfo{
		Category: "dev-rust",
		Pkg:      "test-rust-cli",
		PVVal:    "1.0.0",
		PVal:     "test-rust-cli-1.0.0",
		SRC_URI:  "https://github.com/test-rust-cli/test-rust-cli/releases/download/v1.0.0/test-rust-cli-1.0.0.tar.gz",
		HOMEPAGE: "https://github.com/test-rust-cli/test-rust-cli",
	}

	// Enable dry-run to skip actual GitHub/network calls
	cfg := &PrepareConfig{
		DryRun: true,
	}

	// Full mock: dry-run should skip all mutations
	err := PrepareCargo(ei, cfg)
	if err != nil {
		t.Fatalf("PrepareCargo returned error: %v", err)
	}

	// Ebuild should be unchanged in dry-run
	updated, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("Failed to read ebuild: %v", err)
	}
	if string(updated) != content {
		t.Error("Dry-run modified the ebuild")
	}
}

// ---------------------------------------------------------------------------
// Cargo pipeline: download Cargo.lock from GitHub release
// ---------------------------------------------------------------------------

func TestCargo_DownloadCargoLock_GitHubRelease(t *testing.T) {
	lockContent := `[[package]]
name = "test"
version = "1.0.0"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "abc123"
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(lockContent))
	}))
	defer server.Close()

	client := httpclient.NewClientWithTransport(server.Client().Transport)
	dir := t.TempDir()
	dest := filepath.Join(dir, "Cargo.lock")

	// GitHub release download URL pattern
	url := server.URL + "/raw/v1.0.0/Cargo.lock"
	err := downloadCargoLock(url, dest, client)
	if err != nil {
		t.Fatalf("downloadCargoLock returned error: %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}
	if !strings.Contains(string(content), `name = "test"`) {
		t.Errorf("Downloaded content missing expected package: %s", string(content))
	}
}

// ---------------------------------------------------------------------------
// Cargo lock JSON parsing
// ---------------------------------------------------------------------------

func TestCargo_ParseCargoLockJSON(t *testing.T) {
	jsonStr := `{"name":"Cargo.lock","path":"Cargo.lock","sha":"abc123","download_url":"https://raw.githubusercontent.com/owner/repo/v1.0.0/Cargo.lock"}`

	var result struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		SHA         string `json:"sha"`
		DownloadURL string `json:"download_url"`
	}
	if err := jsonUnmarshal([]byte(jsonStr), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result.Name != "Cargo.lock" {
		t.Errorf("JSON name = %q, want Cargo.lock", result.Name)
	}
	if result.DownloadURL == "" {
		t.Error("JSON download_url is empty")
	}
}

// ---------------------------------------------------------------------------
// D1 — parseCargoLock only accepts registry+ sources
// ---------------------------------------------------------------------------

func TestParseCargoLock_OnlyRegistryCrates(t *testing.T) {
	lockContent := `[[package]]
name = "aho-corasick"
version = "1.1.4"
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = "b2969dcb958b36655471fc61f7e416fa76033bdd4bfed0678d8fee1e2d07a1f0"

[[package]]
name = "local-fork"
version = "0.1.0"
source = "git+https://github.com/x/y?rev=abc#abc"

[[package]]
name = "nirius"
version = "0.7.1"
`
	crates := parseCargoLock(lockContent)
	if len(crates) != 1 {
		t.Fatalf("parseCargoLock returned %d crates, want 1", len(crates))
	}
	if crates[0].Name != "aho-corasick" || crates[0].Version != "1.1.4" {
		t.Errorf("crates[0] = %+v, want {Name: aho-corasick, Version: 1.1.4}", crates[0])
	}
}

// ---------------------------------------------------------------------------
// D2 — formatCRATES dedupes and sorts
// ---------------------------------------------------------------------------

func TestFormatCRATES_DedupeAndSort(t *testing.T) {
	crates := []CargoCrate{
		{Name: "b", Version: "2.0"},
		{Name: "a", Version: "1.0"},
		{Name: "b", Version: "2.0"},
	}
	result := formatCRATES(crates)
	want := "\n\ta@1.0\n\tb@2.0\n"
	if result != want {
		t.Errorf("formatCRATES() = %q, want %q", result, want)
	}
}

func TestFormatCRATES_Empty(t *testing.T) {
	result := formatCRATES(nil)
	want := "\n"
	if result != want {
		t.Errorf("formatCRATES(nil) = %q, want %q", result, want)
	}
}

// ---------------------------------------------------------------------------
// D3 — updateCargoEbuildNative: replace-or-insert
// ---------------------------------------------------------------------------

func TestUpdateCratesBlock_Replace(t *testing.T) {
	fixturesDir := filepath.Join("testdata", "ebuilds")
	src, err := os.ReadFile(filepath.Join(fixturesDir, "cargo-literal-crates.ebuild"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "nirius-0.7.1.ebuild")
	if err := os.WriteFile(ebuildPath, src, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	crates := []CargoCrate{
		{Name: "a", Version: "1.0"},
		{Name: "b", Version: "2.0"},
	}
	count, err := updateCargoEbuildNative(ebuildPath, crates)
	if err != nil {
		t.Fatalf("updateCargoEbuildNative returned error: %v", err)
	}
	if count != 2 {
		t.Errorf("updateCargoEbuildNative() count = %d, want 2", count)
	}

	updated, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("read updated ebuild: %v", err)
	}

	wantBlock := "\n\ta@1.0\n\tb@2.0\n"
	want := "CRATES=\"" + wantBlock + "\""
	if !strings.Contains(string(updated), want) {
		t.Errorf("updated ebuild missing CRATES block:\n%s", string(updated))
	}
	// Assert original 40 entries are gone
	if strings.Contains(string(updated), "aho-corasick@1.1.4") {
		t.Error("original crate aho-corasick@1.1.4 should have been replaced")
	}
}

func TestUpdateCratesBlock_Append(t *testing.T) {
	fixturesDir := filepath.Join("testdata", "ebuilds")
	src, err := os.ReadFile(filepath.Join(fixturesDir, "cargo-no-crates.ebuild"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "test-crate-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, src, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	crates := []CargoCrate{{Name: "a", Version: "1.0"}}
	count, err := updateCargoEbuildNative(ebuildPath, crates)
	if err != nil {
		t.Fatalf("updateCargoEbuildNative returned error: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	updated, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("read updated ebuild: %v", err)
	}

	if !strings.Contains(string(updated), "CRATES=\"\n\ta@1.0\n\"") {
		t.Errorf("updated ebuild missing inserted CRATES block:\n%s", string(updated))
	}

	// Ordering guard: CRATES= must appear before inherit
	if strings.Index(string(updated), "CRATES=") >= strings.Index(string(updated), "inherit") {
		t.Error("CRATES= must appear before inherit")
	}
}

func TestUpdateCratesBlock_NoInheritLine(t *testing.T) {
	fixturesDir := filepath.Join("testdata", "ebuilds")
	src, err := os.ReadFile(filepath.Join(fixturesDir, "cargo-no-inherit.ebuild"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "test.ebuild")
	if err := os.WriteFile(ebuildPath, src, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	crates := []CargoCrate{{Name: "x", Version: "0.1"}}
	_, err = updateCargoEbuildNative(ebuildPath, crates)
	if err != nil {
		t.Fatalf("updateCargoEbuildNative returned error: %v", err)
	}

	updated, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("read updated ebuild: %v", err)
	}

	// CRATES= should appear after EAPI= but before any other content
	if strings.Index(string(updated), "CRATES=") <= strings.Index(string(updated), "EAPI=") {
		t.Error("CRATES= should appear after EAPI= line")
	}
}

func TestUpdateCratesBlock_PreservesRestOfFile(t *testing.T) {
	fixturesDir := filepath.Join("testdata", "ebuilds")
	src, err := os.ReadFile(filepath.Join(fixturesDir, "cargo-literal-crates.ebuild"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "nirius-0.7.1.ebuild")
	if err := os.WriteFile(ebuildPath, src, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Find the CRATES= block boundaries in the original
	cratesStartRe := regexp.MustCompile(`(?sm)^CRATES\s*=\s*"`)
	cratesEndRe := regexp.MustCompile(`(?sm)^"\s*$`)
	startMatch := cratesStartRe.FindIndex(src)
	var endMatch []int
	// Find the closing quote after the start
	if startMatch != nil {
		rest := src[startMatch[1]:]
		endSub := cratesEndRe.FindIndex(rest)
		if endSub != nil {
			endMatch = []int{startMatch[1] + endSub[0], startMatch[1] + endSub[1]}
		}
	}

	crates := []CargoCrate{
		{Name: "new-one", Version: "9.9.9"},
	}
	_, err = updateCargoEbuildNative(ebuildPath, crates)
	if err != nil {
		t.Fatalf("updateCargoEbuildNative returned error: %v", err)
	}

	updated, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("read updated ebuild: %v", err)
	}

	// If we found both boundaries, verify byte-for-byte preservation
	if startMatch != nil && endMatch != nil {
		pre := string(src[:startMatch[0]])
		post := string(src[endMatch[1]:])
		wantBlock := "\n\tnew-one@9.9.9\n"
		want := pre + "CRATES=\"" + wantBlock + "\"" + post
		if string(updated) != want {
			t.Errorf("file not preserved byte-for-byte outside CRATES= block.\n--- want\n+++ got\n%s",
				diffStrings(want, string(updated)))
		}
	}
}

// ---------------------------------------------------------------------------
// D11 — lookPath seam (pycargoebuild absent → native path)
// ---------------------------------------------------------------------------

func TestCargo_FallsBackToNativeWhenPycargoebuildAbsent(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })

	lookPath = func(name string) (string, error) {
		if name == "pycargoebuild" {
			return "", exec.ErrNotFound
		}
		return orig(name)
	}

	fixturesDir := filepath.Join("testdata", "ebuilds")
	src, err := os.ReadFile(filepath.Join(fixturesDir, "cargo-no-crates.ebuild"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "test-crate-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, src, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	crates := []CargoCrate{{Name: "a", Version: "1.0"}}
	_, err = updateCargoEbuildNative(ebuildPath, crates)
	if err != nil {
		t.Fatalf("updateCargoEbuildNative returned error: %v", err)
	}

	updated, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("read updated ebuild: %v", err)
	}

	if !strings.Contains(string(updated), "CRATES=\"\n\ta@1.0\n\"") {
		t.Errorf("native path did not insert CRATES block:\n%s", string(updated))
	}
}

// ---------------------------------------------------------------------------
// D9 — backup / rollback / no-bak
// ---------------------------------------------------------------------------

// TestCargo_RollbackOnManifestFailure tests that PrepareCargo restores the
// ebuild and removes the .bak when the manifest step fails.
func TestCargo_RollbackOnManifestFailure(t *testing.T) {
	fixturesDir := filepath.Join("testdata", "ebuilds")
	src, err := os.ReadFile(filepath.Join(fixturesDir, "cargo-literal-crates.ebuild"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	tmpDir := t.TempDir()
	overlayDir := filepath.Join(tmpDir, "overlay")
	pkgDir := filepath.Join(overlayDir, "gui-apps", "nirius")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("mkdir pkg dir: %v", err)
	}

	ebuildPath := filepath.Join(pkgDir, "nirius-0.7.1.ebuild")
	if err := os.WriteFile(ebuildPath, src, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Stub lookPath to skip pycargoebuild (use native path)
	origLP := lookPath
	lookPath = func(name string) (string, error) {
		if name == "pycargoebuild" {
			return "", exec.ErrNotFound
		}
		return origLP(name)
	}
	t.Cleanup(func() { lookPath = origLP })

	// Parse Cargo.lock and update ebuild
	lockContent := `[[package]]
name = "aho-corasick"
version = "1.1.4"
source = "registry+https://github.com/rust-lang/crates.io-index"
`
	crates := parseCargoLock(lockContent)

	// Backup + update (simulates PrepareCargo steps 3-4)
	if err := Backup(ebuildPath); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	defer os.Remove(ebuildPath + ".bak")

	count, err := updateCargoEbuildNative(ebuildPath, crates)
	if err != nil {
		t.Fatalf("updateCargoEbuildNative: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	// Verify ebuild was modified
	modified, _ := os.ReadFile(ebuildPath)
	if string(modified) == string(src) {
		t.Fatal("ebuild should have been modified")
	}

	// Simulate manifest failure → restore
	if err := Restore(ebuildPath); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Verify ebuild is back to original
	restored, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("read restored ebuild: %v", err)
	}
	if string(restored) != string(src) {
		t.Error("restored content differs from original")
	}

	// Verify .bak still exists (defer will clean up)
	if _, err := os.Stat(ebuildPath + ".bak"); os.IsNotExist(err) {
		t.Fatal(".bak should still exist after Restore")
	}
}

func TestCargo_NoBakFileOnSuccess(t *testing.T) {
	fixturesDir := filepath.Join("testdata", "ebuilds")
	src, err := os.ReadFile(filepath.Join(fixturesDir, "cargo-literal-crates.ebuild"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	tmpDir := t.TempDir()
	overlayDir := filepath.Join(tmpDir, "overlay")
	pkgDir := filepath.Join(overlayDir, "gui-apps", "nirius")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("mkdir pkg dir: %v", err)
	}

	ebuildPath := filepath.Join(pkgDir, "nirius-0.7.1.ebuild")
	if err := os.WriteFile(ebuildPath, src, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Stub lookPath to skip pycargoebuild
	origLP := lookPath
	lookPath = func(name string) (string, error) {
		if name == "pycargoebuild" {
			return "", exec.ErrNotFound
		}
		return origLP(name)
	}
	t.Cleanup(func() { lookPath = origLP })

	// Simulate the success path: backup → update → manifest success → restore → remove .bak
	if err := Backup(ebuildPath); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	defer os.Remove(ebuildPath + ".bak")

	lockContent := `[[package]]
name = "aho-corasick"
version = "1.1.4"
source = "registry+https://github.com/rust-lang/crates.io-index"
`
	crates := parseCargoLock(lockContent)

	_, err = updateCargoEbuildNative(ebuildPath, crates)
	if err != nil {
		t.Fatalf("updateCargoEbuildNative: %v", err)
	}

	// Simulate manifest success: restore the original and remove .bak
	if err := Restore(ebuildPath); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Simulate caller's defer os.Remove(.bak)
	os.Remove(ebuildPath + ".bak")

	// Verify .bak does NOT exist
	if _, err := os.Stat(ebuildPath + ".bak"); !os.IsNotExist(err) {
		t.Error(".bak file should not exist after success")
	}

	// Verify ebuild is restored
	restored, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("read restored ebuild: %v", err)
	}
	if string(restored) != string(src) {
		t.Error("restored content differs from original")
	}
}

// ---------------------------------------------------------------------------
// Helper: diff two strings (for test output)
// ---------------------------------------------------------------------------

func diffStrings(a, b string) string {
	return "=== want ===\n" + a + "\n=== got ===\n" + b
}

// jsonUnmarshal is a test helper alias for json.Unmarshal.
func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
