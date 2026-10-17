package prepare

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/output"
)

// TestPrepare_WarnsMissingFilesdirPin verifies that Prepare warns when the
// ebuild references a version-pinned FILESDIR file that does not exist —
// src_install would die on it.
func TestPrepare_WarnsMissingFilesdirPin(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "dev-util", "foo")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ebuildPath := filepath.Join(pkgDir, "foo-1.0.0.ebuild")
	raw := "EAPI=8\nPATCHES=( \"${FILESDIR}/${P}-fix.patch\" )\n"
	if err := os.WriteFile(ebuildPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	// No files/ directory: the pinned reference cannot resolve.

	lg, err := output.NewLogger(0, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	var errw bytes.Buffer
	lg.SetErr(&errw)

	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	ei := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "dev-util",
		Pkg:      "foo",
		PVVal:    "1.0.0",
		PVal:     "foo-1.0.0",
	}
	cfg := &PrepareConfig{Distdir: t.TempDir(), Overlay: root, Log: lg}

	if err := Prepare(ei, cfg); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !strings.Contains(errw.String(), "foo-1.0.0-fix.patch") {
		t.Errorf("warnings = %q, want a warning about missing foo-1.0.0-fix.patch", errw.String())
	}
}

// TestPrepare_NoFilesdirWarningWhenPinResolves verifies that a pinned
// reference whose file exists stays silent at prepare time: the pattern is
// fragile but not broken, and nagging on every prepare would be noise.
func TestPrepare_NoFilesdirWarningWhenPinResolves(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "dev-util", "foo")
	filesDir := filepath.Join(pkgDir, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ebuildPath := filepath.Join(pkgDir, "foo-1.0.0.ebuild")
	raw := "EAPI=8\nPATCHES=( \"${FILESDIR}/${P}-fix.patch\" )\n"
	if err := os.WriteFile(ebuildPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filesDir, "foo-1.0.0-fix.patch"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}

	lg, err := output.NewLogger(0, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	var errw bytes.Buffer
	lg.SetErr(&errw)

	orig := ManifestRunner
	ManifestRunner = func(bin string, args []string, dir string, env []string) error {
		return nil
	}
	t.Cleanup(func() { ManifestRunner = orig })

	ei := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "dev-util",
		Pkg:      "foo",
		PVVal:    "1.0.0",
		PVal:     "foo-1.0.0",
	}
	cfg := &PrepareConfig{Distdir: t.TempDir(), Overlay: root, Log: lg}

	if err := Prepare(ei, cfg); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if strings.Contains(errw.String(), "fix.patch") {
		t.Errorf("warnings = %q, want no FILESDIR warning while the file exists", errw.String())
	}
}
