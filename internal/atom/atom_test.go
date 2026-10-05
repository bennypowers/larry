package atom

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ─── StripRevision ───────────────────────────────────────────────────────────

func TestStripRevision(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"1.0-r1", "1.0"},
		{"1.0-r12", "1.0"},
		{"foo-rs", "foo-rs"},      // not anchored, -r is in the middle
		{"foo-release", "foo-release"}, // not -r\d+$
		{"1.0-rc1", "1.0-rc1"},    // not -r\d+$
		{"1.0", "1.0"},            // no revision
		{"-r1", "-r1"},            // nothing before -r, idx==0 → no match
	}
	for _, tt := range tests {
		got := StripRevision(tt.in)
		if got != tt.want {
			t.Errorf("StripRevision(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ─── SplitRevision ───────────────────────────────────────────────────────────

func TestSplitRevision(t *testing.T) {
	tests := []struct {
		pvr      string
		wantPV   string
		wantRev  int
	}{
		{"1.0-r3", "1.0", 3},
		{"1.0", "1.0", 0},
		{"9.0", "9.0", 0},
		{"1.0_rc1", "1.0_rc1", 0},
	}
	for _, tt := range tests {
		pv, rev := SplitRevision(tt.pvr)
		if pv != tt.wantPV {
			t.Errorf("SplitRevision(%q) pv = %q, want %q", tt.pvr, pv, tt.wantPV)
		}
		if rev != tt.wantRev {
			t.Errorf("SplitRevision(%q) rev = %d, want %d", tt.pvr, rev, tt.wantRev)
		}
	}
}

// ─── NextRevision ────────────────────────────────────────────────────────────

func TestNextRevision(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"1.0", "1.0-r1"},
		{"1.0-r3", "1.0-r4"},
		{"1.0-r9", "1.0-r10"},
		{"9.0", "9.0-r1"},
	}
	for _, tt := range tests {
		got := NextRevision(tt.in)
		if got != tt.want {
			t.Errorf("NextRevision(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ─── ParseEbuildPath ─────────────────────────────────────────────────────────

func TestParseEbuildPath_Simple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dev-util", "javy", "javy-9.1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	pe, err := ParseEbuildPath(path)
	if err != nil {
		t.Fatalf("ParseEbuildPath(%q) error = %v", path, err)
	}
	assertParsedEbuild(t, pe, "javy", "dev-util", "9.1.0", "9.1.0", 0, false)
}

func TestParseEbuildPath_HyphenatedName(t *testing.T) {
	// Regression for D1–D5: kornia-rs must not be split on "-r"
	dir := t.TempDir()
	path := filepath.Join(dir, "dev-python", "kornia-rs", "kornia-rs-0.1.14.ebuild")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	pe, err := ParseEbuildPath(path)
	if err != nil {
		t.Fatalf("ParseEbuildPath(%q) error = %v", path, err)
	}
	if pe.PN != "kornia-rs" {
		t.Errorf("PN = %q, want %q", pe.PN, "kornia-rs")
	}
	if pe.PV != "0.1.14" {
		t.Errorf("PV = %q, want %q", pe.PV, "0.1.14")
	}
}

func TestParseEbuildPath_HyphenatedNameWithRevision(t *testing.T) {
	// python-requests-2.31.0-r1 — the headline bug case
	dir := t.TempDir()
	path := filepath.Join(dir, "dev-python", "python-requests", "python-requests-2.31.0-r1.ebuild")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	pe, err := ParseEbuildPath(path)
	if err != nil {
		t.Fatalf("ParseEbuildPath(%q) error = %v", path, err)
	}
	if pe.PN != "python-requests" {
		t.Errorf("PN = %q, want %q", pe.PN, "python-requests")
	}
	if pe.PV != "2.31.0" {
		t.Errorf("PV = %q, want %q", pe.PV, "2.31.0")
	}
	if pe.Revision != 1 {
		t.Errorf("Revision = %d, want 1", pe.Revision)
	}
	if pe.PVR != "2.31.0-r1" {
		t.Errorf("PVR = %q, want %q", pe.PVR, "2.31.0-r1")
	}
}

func TestParseEbuildPath_Live(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dev-util", "foo", "foo-9999.ebuild")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	pe, err := ParseEbuildPath(path)
	if err != nil {
		t.Fatalf("ParseEbuildPath(%q) error = %v", path, err)
	}
	if !pe.Live {
		t.Errorf("Live = false, want true")
	}
	if pe.PV != "9999" {
		t.Errorf("PV = %q, want %q", pe.PV, "9999")
	}
}

func TestParseEbuildPath_UnderscoreSuffixes(t *testing.T) {
	// _p, _rc, _pre suffixes should survive — not mistaken for revisions
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-util", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		pv   string
	}{
		{"foo-6.18.46_p20260824.ebuild", "6.18.46_p20260824"},
		{"foo-1.0_rc1.ebuild", "1.0_rc1"},
		{"foo-1.0_pre2.ebuild", "1.0_pre2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(pkgDir, tt.name)
			if err := os.WriteFile(path, []byte("EAPI=8"), 0644); err != nil {
				t.Fatal(err)
			}
			pe, err := ParseEbuildPath(path)
			if err != nil {
				t.Fatalf("ParseEbuildPath(%q) error = %v", path, err)
			}
			if pe.PV != tt.pv {
				t.Errorf("PV = %q, want %q", pe.PV, tt.pv)
			}
		})
	}
}

func TestParseEbuildPath_Mismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dev-util", "foo", "bar-1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseEbuildPath(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNotAnEbuild) {
		t.Errorf("error = %v, want ErrNotAnEbuild", err)
	}
}

// assertParsedEbuild is a helper that checks all fields of ParsedEbuild.
func assertParsedEbuild(t *testing.T, pe *ParsedEbuild, pn, cat, pv, pvr string, rev int, live bool) {
	t.Helper()
	if pe.PN != pn {
		t.Errorf("PN = %q, want %q", pe.PN, pn)
	}
	if pe.Category != cat {
		t.Errorf("Category = %q, want %q", pe.Category, cat)
	}
	if pe.PV != pv {
		t.Errorf("PV = %q, want %q", pe.PV, pv)
	}
	if pe.PVR != pvr {
		t.Errorf("PVR = %q, want %q", pe.PVR, pvr)
	}
	if pe.Revision != rev {
		t.Errorf("Revision = %d, want %d", pe.Revision, rev)
	}
	if pe.Live != live {
		t.Errorf("Live = %v, want %v", pe.Live, live)
	}
}

// ─── ParseAtom ───────────────────────────────────────────────────────────────

func TestParseAtom_NoVersion(t *testing.T) {
	dir := t.TempDir()
	catDir := filepath.Join(dir, "dev-util")
	pkgDir := filepath.Join(catDir, "fish-lsp")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}

	a, err := ParseAtom(dir, "dev-util/fish-lsp")
	if err != nil {
		t.Fatalf("ParseAtom error = %v", err)
	}
	if a.PN != "fish-lsp" {
		t.Errorf("PN = %q, want %q", a.PN, "fish-lsp")
	}
	if a.Version != "" {
		t.Errorf("Version = %q, want empty", a.Version)
	}
}

func TestParseAtom_WithVersion(t *testing.T) {
	dir := t.TempDir()
	catDir := filepath.Join(dir, "dev-util")
	pkgDir := filepath.Join(catDir, "javy")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}

	a, err := ParseAtom(dir, "dev-util/javy-8.1.1")
	if err != nil {
		t.Fatalf("ParseAtom error = %v", err)
	}
	if a.PN != "javy" {
		t.Errorf("PN = %q, want %q", a.PN, "javy")
	}
	if a.Version != "8.1.1" {
		t.Errorf("Version = %q, want %q", a.Version, "8.1.1")
	}
}

func TestParseAtom_HyphenWalkPrefersLongestDir(t *testing.T) {
	// Both dev-util/fish and dev-util/fish-lsp exist.
	// "dev-util/fish-lsp-1.1.3" should match fish-lsp, not fish.
	dir := t.TempDir()
	catDir := filepath.Join(dir, "dev-util")
	if err := os.MkdirAll(filepath.Join(catDir, "fish"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(catDir, "fish-lsp"), 0755); err != nil {
		t.Fatal(err)
	}

	a, err := ParseAtom(dir, "dev-util/fish-lsp-1.1.3")
	if err != nil {
		t.Fatalf("ParseAtom error = %v", err)
	}
	if a.PN != "fish-lsp" {
		t.Errorf("PN = %q, want %q", a.PN, "fish-lsp")
	}
	if a.Version != "1.1.3" {
		t.Errorf("Version = %q, want %q", a.Version, "1.1.3")
	}
}

func TestParseAtom_InvalidShape(t *testing.T) {
	dir := t.TempDir()
	for _, atom := range []string{"notanatom", "a/b/c", ""} {
		_, err := ParseAtom(dir, atom)
		if err == nil {
			t.Errorf("ParseAtom(%q) = nil, want error", atom)
			continue
		}
		if !errors.Is(err, ErrInvalidAtom) {
			t.Errorf("ParseAtom(%q) error = %v, want ErrInvalidAtom", atom, err)
		}
	}
}

func TestParseAtom_CategoryMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := ParseAtom(dir, "nonexistent/pkg-1.0")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("error = %v, want ErrCategoryNotFound", err)
	}
}

func TestParseAtom_PackageMissing(t *testing.T) {
	dir := t.TempDir()
	catDir := filepath.Join(dir, "dev-util")
	if err := os.MkdirAll(catDir, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := ParseAtom(dir, "dev-util/nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrPackageNotFound) {
		t.Errorf("error = %v, want ErrPackageNotFound", err)
	}
}

// ─── ListEbuilds ─────────────────────────────────────────────────────────────

func TestListEbuilds_Deterministic(t *testing.T) {
	dir := t.TempDir()
	names := []string{"foo-1.0.ebuild", "foo-2.0.ebuild", "foo-1.5.ebuild"}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("EAPI=8"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	var first []*ParsedEbuild
	for run := 0; run < 3; run++ {
		ebuilds, err := ListEbuilds(dir)
		if err != nil {
			t.Fatalf("ListEbuilds error = %v", err)
		}
		if run == 0 {
			first = ebuilds
		} else {
			for i, pe := range ebuilds {
				if first[i].PV != pe.PV || first[i].Revision != pe.Revision {
					t.Errorf("Run %d: PV = %q, Revision = %d, want %q/%d",
						run, pe.PV, pe.Revision, first[i].PV, first[i].Revision)
				}
			}
		}
	}
}

func TestListEbuilds_KeepsUnparseableVersion(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-util", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	// nightly is unparseable by version.Parse
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-1.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-nightly.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	ebuilds, err := ListEbuilds(pkgDir)
	if err != nil {
		t.Fatalf("ListEbuilds error = %v", err)
	}
	if len(ebuilds) != 2 {
		t.Fatalf("len(ebuilds) = %d, want 2", len(ebuilds))
	}
	// Unparseable should sort before parseable
	if ebuilds[0].PV != "nightly" {
		t.Errorf("first PV = %q, want %q", ebuilds[0].PV, "nightly")
	}
}

func TestListEbuilds_SkipsForeignBasename(t *testing.T) {
	// A stray file not matching the dir PN should be skipped
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-util", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-1.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "other-1.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-nightly.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	ebuilds, err := ListEbuilds(pkgDir)
	if err != nil {
		t.Fatalf("ListEbuilds error = %v", err)
	}
	if len(ebuilds) != 2 {
		t.Fatalf("len(ebuilds) = %d, want 2 (stray 'other-1.0.ebuild' skipped)", len(ebuilds))
	}
}

// ─── FindLatest ──────────────────────────────────────────────────────────────

func TestFindLatest_NumericOrdering(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-util", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"foo-9.0.ebuild", "foo-10.0.ebuild", "foo-1.0.ebuild"} {
		if err := os.WriteFile(filepath.Join(pkgDir, n), []byte("EAPI=8"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	pe, err := FindLatest(pkgDir)
	if err != nil {
		t.Fatalf("FindLatest error = %v", err)
	}
	if pe.PV != "10.0" {
		t.Errorf("PV = %q, want %q", pe.PV, "10.0")
	}
}

func TestFindLatest_ExcludesLive(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-util", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-1.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-9999.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	pe, err := FindLatest(pkgDir)
	if err != nil {
		t.Fatalf("FindLatest error = %v", err)
	}
	if pe.PV != "1.0" {
		t.Errorf("PV = %q, want %q", pe.PV, "1.0")
	}
}

func TestFindLatest_LiveOnly(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-util", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-9999.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := FindLatest(pkgDir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNoEbuilds) {
		t.Errorf("error = %v, want ErrNoEbuilds", err)
	}
}

func TestFindLatest_RevisionTiebreak(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-util", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-1.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-1.0-r2.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	pe, err := FindLatest(pkgDir)
	if err != nil {
		t.Fatalf("FindLatest error = %v", err)
	}
	if pe.PVR != "1.0-r2" {
		t.Errorf("PVR = %q, want %q", pe.PVR, "1.0-r2")
	}
}

// ─── FindForAtom ─────────────────────────────────────────────────────────────

func TestFindForAtom_ExactVersion(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-util", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-1.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-2.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	a := &Atom{PkgDir: pkgDir, Version: "2.0"}
	pe, err := FindForAtom(a)
	if err != nil {
		t.Fatalf("FindForAtom error = %v", err)
	}
	if pe.PV != "2.0" {
		t.Errorf("PV = %q, want %q", pe.PV, "2.0")
	}
}

func TestFindForAtom_ExactVersionMissing(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-util", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-1.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	a := &Atom{PkgDir: pkgDir, Version: "3.0"}
	_, err := FindForAtom(a)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFindForAtom_LiveFallback(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-util", "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	// No non-live ebuilds — live should be last resort
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-9999.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	a := &Atom{PkgDir: pkgDir}
	pe, err := FindForAtom(a)
	if err != nil {
		t.Fatalf("FindForAtom error = %v", err)
	}
	if !pe.Live {
		t.Errorf("Live = false, want true (fallback)")
	}
}
