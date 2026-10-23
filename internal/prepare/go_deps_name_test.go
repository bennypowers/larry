package prepare

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"

	"bennypowers.dev/larry/internal/ebuild"
)

// makeGoEbuildInfoWithDeps writes a go-module ebuild whose SRC_URI carries a
// deps tarball entry, and returns the matching EbuildInfo.
func makeGoEbuildInfoWithDeps(t *testing.T, tmpDir, depsSrc string) *ebuild.EbuildInfo {
	t.Helper()
	pkgDir := filepath.Join(tmpDir, "overlay", "dev-util", "proj")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ebuildPath := filepath.Join(pkgDir, "proj-1.0.0.ebuild")
	content := `EAPI=8
inherit go-module

DESCRIPTION="Test"
HOMEPAGE="https://github.com/acme/proj"
SRC_URI="https://github.com/acme/proj/archive/refs/tags/v${PV}.tar.gz -> ${P}.tar.gz
	` + depsSrc + `"

LICENSE="MIT"
SLOT="0"
KEYWORDS="~amd64"
`
	if err := os.WriteFile(ebuildPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "dev-util",
		Pkg:      "proj",
		PVVal:    "1.0.0",
		PVal:     "proj-1.0.0",
		SRC_URI: "https://github.com/acme/proj/archive/refs/tags/v${PV}.tar.gz -> ${P}.tar.gz\n\t" +
			depsSrc,
		HOMEPAGE: "https://github.com/acme/proj",
		Inherit:  []string{"go-module"},
		Raw:      content,
	}
}

// TestPrepareGo_DepsFilenameMatchesSrcURI_Gz pins the bug behind a failed bump:
// the ebuild (and `pkgdev manifest`) expects ${P}-deps.tar.gz, but PrepareGo
// wrote ${P}-deps.tar.xz into DISTDIR. The mismatch made pkgdev fetch the
// missing .tar.gz over the network, where the not-yet-pushed version 404s.
func TestPrepareGo_DepsFilenameMatchesSrcURI_Gz(t *testing.T) {
	tmpDir := t.TempDir()
	srv := goFixture(t, "acme", "proj", "v1.0.0")

	ei := makeGoEbuildInfoWithDeps(t, tmpDir,
		"https://raw.githubusercontent.com/bennypowers/gentoo-overlay/main/dev-util/proj/${P}-deps.tar.gz")
	distDir := t.TempDir()
	stubManifestRunner(t)

	cfg := &PrepareConfig{
		Overlay:       filepath.Join(tmpDir, "overlay"),
		Distdir:       distDir,
		GitHubBaseURL: srv.URL,
		Runner:        &recRunner{},
		HTTPClient:    srv.Client(),
	}

	if err := PrepareGo(ei, cfg); err != nil {
		t.Fatalf("PrepareGo returned error: %v", err)
	}

	pkgTarball := filepath.Join(filepath.Dir(ei.Path), "proj-1.0.0-deps.tar.gz")
	data, err := os.ReadFile(pkgTarball)
	if err != nil {
		t.Fatalf("package-dir tarball not created at %s: %v", pkgTarball, err)
	}
	if !bytes.HasPrefix(data, []byte{0x1f, 0x8b}) {
		t.Errorf("%s is not gzip-compressed (magic = %x)", pkgTarball, data[:2])
	}
	if _, err := os.Stat(filepath.Join(distDir, "proj-1.0.0-deps.tar.gz")); err != nil {
		t.Errorf("distdir tarball not named proj-1.0.0-deps.tar.gz: %v", err)
	}
	if _, err := os.Stat(filepath.Join(distDir, "proj-1.0.0-deps.tar.xz")); err == nil {
		t.Error("unexpected proj-1.0.0-deps.tar.xz in distdir: ebuild asks for .tar.gz")
	}

	// The gzip stream must still be a valid vendor/ tarball.
	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("create gzip reader: %v", err)
	}
	defer func() { _ = gzr.Close() }()
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
		if hdr.Name == "vendor/x/y.go" {
			found = true
		}
	}
	if !found {
		t.Error("tarball does not contain vendor/x/y.go")
	}
}

// An ebuild that asks for .tar.xz (release-asset style) must get xz output.
func TestPrepareGo_DepsFilenameMatchesSrcURI_Xz(t *testing.T) {
	tmpDir := t.TempDir()
	srv := goFixture(t, "acme", "proj", "v1.0.0")

	ei := makeGoEbuildInfoWithDeps(t, tmpDir,
		"https://github.com/bennypowers/gentoo-overlay/releases/download/${PN}/${P}-deps.tar.xz")
	distDir := t.TempDir()
	stubManifestRunner(t)

	cfg := &PrepareConfig{
		Overlay:       filepath.Join(tmpDir, "overlay"),
		Distdir:       distDir,
		GitHubBaseURL: srv.URL,
		Runner:        &recRunner{},
		HTTPClient:    srv.Client(),
	}

	if err := PrepareGo(ei, cfg); err != nil {
		t.Fatalf("PrepareGo returned error: %v", err)
	}

	pkgTarball := filepath.Join(filepath.Dir(ei.Path), "proj-1.0.0-deps.tar.xz")
	f, err := os.Open(pkgTarball)
	if err != nil {
		t.Fatalf("package-dir tarball not created at %s: %v", pkgTarball, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := xz.NewReader(f); err != nil {
		t.Fatalf("%s is not xz-compressed: %v", pkgTarball, err)
	}
	if _, err := os.Stat(filepath.Join(distDir, "proj-1.0.0-deps.tar.gz")); err == nil {
		t.Error("unexpected proj-1.0.0-deps.tar.gz in distdir: ebuild asks for .tar.xz")
	}
}

// A failed manifest run must not leave the freshly generated deps tarball
// behind; `larry bump` removes the new ebuild, so the tarball would be orphaned.
func TestPrepareGo_CleansUpNewTarballOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	srv := goFixture(t, "acme", "proj", "v1.0.0")

	ei := makeGoEbuildInfo(t, tmpDir, "acme", "proj")
	distDir := t.TempDir()

	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		return errors.New("manifest exploded")
	}
	t.Cleanup(func() { ManifestRunner = orig })

	cfg := &PrepareConfig{
		Overlay:          filepath.Join(tmpDir, "overlay"),
		Distdir:          distDir,
		GitHubBaseURL:    srv.URL,
		Runner:           &recRunner{},
		HTTPClient:       srv.Client(),
		CleanupOnFailure: true, // bump deletes the ebuild, so the tarball must go too
	}

	if err := PrepareGo(ei, cfg); err == nil {
		t.Fatalf("expected PrepareGo to fail when manifest fails")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(ei.Path), "proj-1.0.0-deps.tar.gz")); err == nil {
		t.Error("new deps tarball left behind after failed prepare")
	}
	if _, err := os.Stat(filepath.Join(distDir, "proj-1.0.0-deps.tar.gz")); err == nil {
		t.Error("new distdir tarball left behind after failed prepare")
	}
}

func TestCreateGoVendorTarball_GzipByExtension(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(filepath.Join(srcDir, "vendor", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "vendor", "x", "y.go"), []byte("package y\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(tmpDir, "out.tar.gz")
	if err := createGoVendorTarball(srcDir, outPath); err != nil {
		t.Fatalf("createGoVendorTarball returned error: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte{0x1f, 0x8b}) {
		t.Errorf("%s is not gzip-compressed (magic = %x)", outPath, data[:2])
	}
}

func TestCreateGoVendorTarball_RejectsUnknownExtension(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(filepath.Join(srcDir, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := createGoVendorTarball(srcDir, filepath.Join(tmpDir, "out.tar.zst"))
	if err == nil {
		t.Fatal("expected error for unsupported deps tarball extension")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error = %v, want it to mention unsupported archive format", err)
	}
}
