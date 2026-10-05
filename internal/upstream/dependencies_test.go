package upstream

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/version"
)

// ─── Atom Extraction Tests ───────────────────────────────────────────────────

// TestExtractAtoms is one table-driven test with exact inputs and outputs.
func TestExtractAtoms(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{
			input:    ">=dev-libs/foo-1.2.3:0=[bar]",
			expected: []string{"dev-libs/foo"},
		},
		{
			input:    "|| ( a-cat/b c-cat/d )",
			expected: []string{"a-cat/b", "c-cat/d"},
		},
		{
			input:    "python? ( dev-python/x )",
			expected: []string{"dev-python/x"},
		},
		{
			input:    "dev-libs/foo dev-libs/foo",
			expected: []string{"dev-libs/foo"},
		},
		{
			input:    "dev-libs/libstdc++",
			expected: []string{"dev-libs/libstdc++"}, // D9: + and . in name
		},
		{
			input:    "dev-python/zope.interface",
			expected: []string{"dev-python/zope.interface"}, // D9
		},
		{
			input:    "app-text/docbook-xml-dtd",
			expected: []string{"app-text/docbook-xml-dtd"},
		},
		{
			input:    ">=dev-lang/python-exec-2.4.6",
			expected: []string{"dev-lang/python-exec"},
		},
		{
			input:    "sys-libs/db-5.3:=",
			expected: []string{"sys-libs/db"},
		},
		{
			input:    "",
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := extractAtoms(tc.input)
			if len(got) != len(tc.expected) {
				t.Fatalf("expected %v, got %v", tc.expected, got)
			}
			for i, exp := range tc.expected {
				if got[i] != exp {
					t.Errorf("got[%d] = %q, want %q", i, got[i], exp)
				}
			}
		})
	}
}

// TestExtractAtoms_Existing verifies the pre-existing test cases still work.
func TestExtractAtoms_Simple(t *testing.T) {
	input := ">=dev-lang/python-3.9"
	atoms := extractAtoms(input)
	if len(atoms) != 1 {
		t.Fatalf("expected 1 atom, got %d", len(atoms))
	}
	if atoms[0] != "dev-lang/python" {
		t.Errorf("expected 'dev-lang/python', got %q", atoms[0])
	}
}

func TestExtractAtoms_Multi(t *testing.T) {
	input := ">=dev-lang/python-3.9 dev-java/icedtea-web"
	atoms := extractAtoms(input)
	if len(atoms) != 2 {
		t.Fatalf("expected 2 atoms, got %d", len(atoms))
	}
	expected := []string{"dev-java/icedtea-web", "dev-lang/python"}
	for i, exp := range expected {
		if atoms[i] != exp {
			t.Errorf("atom[%d] = %q, want %q", i, atoms[i], exp)
		}
	}
}

func TestExtractAtoms_UseFlag(t *testing.T) {
	input := "python? ( dev-python/requests dev-python/urllib3 )"
	atoms := extractAtoms(input)
	if len(atoms) != 2 {
		t.Fatalf("expected 2 atoms from use-flag, got %d: %v", len(atoms), atoms)
	}
	found := map[string]bool{}
	for _, a := range atoms {
		found[a] = true
	}
	if !found["dev-python/requests"] || !found["dev-python/urllib3"] {
		t.Errorf("expected dev-python/requests and dev-python/urllib3, got %v", atoms)
	}
}

func TestExtractAtoms_Disjunction(t *testing.T) {
	input := "|| ( app-crypt/gpgme app-crypt/gpgmepp )"
	atoms := extractAtoms(input)
	if len(atoms) != 2 {
		t.Fatalf("expected 2 atoms from disjunction, got %d: %v", len(atoms), atoms)
	}
}

func TestExtractAtoms_BraceExpansion(t *testing.T) {
	input := "app-arch/bzip2[bzip2?] dev-libs/expat"
	atoms := extractAtoms(input)
	if len(atoms) != 2 {
		t.Fatalf("expected 2 atoms, got %d: %v", len(atoms), atoms)
	}
}

func TestExtractAtoms_VersionSpec(t *testing.T) {
	input := ">=dev-lang/perl-5.32.0"
	atoms := extractAtoms(input)
	if len(atoms) != 1 {
		t.Fatalf("expected 1 atom, got %d", len(atoms))
	}
	if atoms[0] != "dev-lang/perl" {
		t.Errorf("expected 'dev-lang/perl', got %q", atoms[0])
	}
}

func TestExtractAtoms_Empty(t *testing.T) {
	if len(extractAtoms("")) != 0 {
		t.Error("expected 0 atoms from empty string")
	}
	if len(extractAtoms("  ")) != 0 {
		t.Error("expected 0 atoms from whitespace")
	}
}

// ─── ParseEbuildInfo DEPEND tests ────────────────────────────────────────────

// TestParseEbuildInfo_PopulatesDepends verifies that ParseEbuildInfo fills DEPEND,
// RDEPEND, BDEPEND (D2). The ebuild body uses multi-line RDEPEND.
func TestParseEbuildInfo_PopulatesDepends(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-libs", "testpkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	ebuildPath := filepath.Join(pkgDir, "testpkg-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte(`EAPI=8
DESCRIPTION="Test"
SLOT="0"
DEPEND="dev-libs/foo"
RDEPEND="${DEPEND}
    dev-libs/bar
    dev-libs/baz"
BDEPEND="dev-util/qux"
`), 0644); err != nil {
		t.Fatal(err)
	}

	ei, err := ParseEbuildInfo(ebuildPath)
	if err != nil {
		t.Fatalf("ParseEbuildInfo: %v", err)
	}

	if ei.DEPEND != "dev-libs/foo" {
		t.Errorf("DEPEND = %q, want %q", ei.DEPEND, "dev-libs/foo")
	}
	if ei.BDEPEND != "dev-util/qux" {
		t.Errorf("BDEPEND = %q, want %q", ei.BDEPEND, "dev-util/qux")
	}
	// RDEPEND should contain all three references (multi-line expansion).
	// The multi-line value expands ${DEPEND} → "dev-libs/foo", so RDEPEND
	// should contain "dev-libs/foo", "dev-libs/bar", "dev-libs/baz".
	rdep := ei.RDEPEND
	if !strings.Contains(rdep, "dev-libs/foo") {
		t.Errorf("RDEPEND missing dev-libs/foo: %q", rdep)
	}
	if !strings.Contains(rdep, "dev-libs/bar") {
		t.Errorf("RDEPEND missing dev-libs/bar: %q", rdep)
	}
	if !strings.Contains(rdep, "dev-libs/baz") {
		t.Errorf("RDEPEND missing dev-libs/baz: %q", rdep)
	}
}

// ─── StripVersion tests ──────────────────────────────────────────────────────

func TestStripVersion(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{">=dev-libs/foo-1.2.3:0", "dev-libs/foo"}, // slot then version
		{"dev-libs/foo-1.0", "dev-libs/foo"},
		{"dev-libs/foo", "dev-libs/foo"},          // no version
		{"sys-libs/db-5.3", "sys-libs/db"},
		{"dev-lang/python-exec-2.4.6", "dev-lang/python-exec"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := stripVersion(strings.TrimLeft(tc.input, "<>="))
			if got != tc.expect {
				t.Errorf("stripVersion(%q) = %q, want %q", tc.input, got, tc.expect)
			}
		})
	}
}

// ─── ScanDependencies Integration Tests ──────────────────────────────────────

// setupFixtureOverlay creates a temp overlay with profiles/repo_name, a git repo,
// and the supplied ebuilds. ebuilds maps "category/pkg/pkg-1.0.ebuild" → content.
func setupFixtureOverlay(t *testing.T, ebuilds map[string]string) string {
	dir := t.TempDir()
	profilesDir := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profilesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "repo_name"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	// Write ebuilds
	for relPath, content := range ebuilds {
		parts := strings.Split(relPath, "/")
		pkgDir := filepath.Join(dir, parts[0], parts[1])
		if err := os.MkdirAll(pkgDir, 0755); err != nil {
			t.Fatal(err)
		}
		// basename is e.g. "pkg-1.0.ebuild"
		baseName := parts[len(parts)-1]
		if err := os.WriteFile(filepath.Join(pkgDir, baseName), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func mustParseVersion(t *testing.T, s string) *version.Version {
	t.Helper()
	v, err := version.Parse(s)
	if err != nil {
		t.Fatalf("version.Parse(%q): %v", s, err)
	}
	return v
}

// recordingProvider records every RemoteId passed to FetchLatestVersion.
type recordingProvider struct {
	mockType remoteid.RemoteIdType
	ver      *version.Version
	err      error
	got      []remoteid.RemoteId
}

func (p *recordingProvider) Type() remoteid.RemoteIdType         { return p.mockType }
func (p *recordingProvider) Name() string                        { return "rec" }
func (p *recordingProvider) Metadata() provider.ProviderMetadata { return provider.ProviderMetadata{Name: "rec"} }
func (p *recordingProvider) FetchLatestVersion(rid remoteid.RemoteId, _ bool) (*version.Version, error) {
	p.got = append(p.got, rid)
	if p.err != nil {
		return nil, p.err
	}
	return p.ver, nil
}
func (p *recordingProvider) IsPrerelease(v *version.Version) bool { return v.IsPrerelease() }

// TestScanDependencies_ReadsRealEbuild verifies that ScanDependencies reads the
// real ebuild from the overlay (D3): the dependency's version is not fabricated.
func TestScanDependencies_ReadsRealEbuild(t *testing.T) {
	ov := setupFixtureOverlay(t, map[string]string{
		"app-misc/root/root-1.0.0.ebuild": `EAPI=8
DESCRIPTION="Root"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="dev-libs/dep"
`,
		"dev-libs/dep/dep-2.5.0.ebuild": `EAPI=8
DESCRIPTION="Dep"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
	})

	registry := provider.NewProviderRegistry()
	cache := NewVersionCacheWithPath(t.TempDir(), 60)

	// Find the root ebuild
	rootEbuild, err := atom.FindLatest(filepath.Join(ov, "app-misc", "root"))
	if err != nil {
		t.Fatalf("FindLatest(root): %v", err)
	}

	visited := map[string]bool{}
	results := ScanDependencies(ov, rootEbuild, registry, false, cache, 0, visited)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Package != "dev-libs/dep" {
		t.Errorf("Package = %q, want %q", r.Package, "dev-libs/dep")
	}
	if r.Current != "2.5.0" {
		t.Errorf("Current = %q, want %q (not 1.0.0)", r.Current, "2.5.0")
	}
	if r.Depth != 1 {
		t.Errorf("Depth = %d, want 1", r.Depth)
	}
}

// TestScanDependencies_UsesDepSrcUri verifies the dep's own SRC_URI drives
// provider resolution (D4).
func TestScanDependencies_UsesDepSrcUri(t *testing.T) {
	ov := setupFixtureOverlay(t, map[string]string{
		"app-misc/root/root-1.0.0.ebuild": `EAPI=8
DESCRIPTION="Root"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="dev-libs/dep"
`,
		"dev-libs/dep/dep-2.5.0.ebuild": `EAPI=8
DESCRIPTION="Dep"
SRC_URI="https://github.com/acme/widget/archive/v${PV}.tar.gz"
LICENSE="MIT"
SLOT="0"
`,
	})

	// Create metadata.xml for the dep so ResolveRemoteId picks up the GitHub remote-id.
	metaDir := filepath.Join(ov, "dev-libs", "dep")
	metaXML := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkgmetadata SYSTEM "https://www.gentoo.org/dtd/metadata.dtd">
<pkgmetadata>
<upstream>
<remote-id type="github">acme/widget</remote-id>
</upstream>
</pkgmetadata>
`
	if err := os.WriteFile(filepath.Join(metaDir, "metadata.xml"), []byte(metaXML), 0644); err != nil {
		t.Fatal(err)
	}

	registry := provider.NewProviderRegistry()
	p := &recordingProvider{mockType: remoteid.TypeGitHub, ver: mustParseVersion(t, "3.0.0")}
	registry.Register(p)
	cache := NewVersionCacheWithPath(t.TempDir(), 60)

	rootEbuild, err := atom.FindLatest(filepath.Join(ov, "app-misc", "root"))
	if err != nil {
		t.Fatalf("FindLatest(root): %v", err)
	}

	visited := map[string]bool{}
	ScanDependencies(ov, rootEbuild, registry, false, cache, 0, visited)

	if len(p.got) != 1 {
		t.Fatalf("expected 1 RemoteId, got %d", len(p.got))
	}
	want := remoteid.RemoteId{Type: remoteid.TypeGitHub, Owner: "acme", Repo: "widget"}
	if p.got[0] != want {
		t.Errorf("got RemoteId %v, want %v", p.got[0], want)
	}
}

// TestScanDependencies_SkipsOutOfOverlay verifies that dependencies not present
// in the overlay are silently skipped (D7).
func TestScanDependencies_SkipsOutOfOverlay(t *testing.T) {
	ov := setupFixtureOverlay(t, map[string]string{
		"app-misc/root/root-1.0.0.ebuild": `EAPI=8
DESCRIPTION="Root"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="sys-libs/glibc"
`,
	})

	registry := provider.NewProviderRegistry()
	cache := NewVersionCacheWithPath(t.TempDir(), 60)

	rootEbuild, err := atom.FindLatest(filepath.Join(ov, "app-misc", "root"))
	if err != nil {
		t.Fatalf("FindLatest(root): %v", err)
	}

	visited := map[string]bool{}
	results := ScanDependencies(ov, rootEbuild, registry, false, cache, 0, visited)

	if len(results) != 0 {
		t.Errorf("expected 0 results (glibc not in overlay), got %d: %v", len(results), results)
	}
}

// TestScanDependencies_DepthLimit verifies that depth 3 is the hard cap (D6).
// Chain: a → b → c → d → e, each in its own category.
func TestScanDependencies_DepthLimit(t *testing.T) {
	ov := setupFixtureOverlay(t, map[string]string{
		"c1/a/a-1.0.ebuild": `EAPI=8
DESCRIPTION="A"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="c2/b"
`,
		"c2/b/b-1.0.ebuild": `EAPI=8
DESCRIPTION="B"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="c3/c"
`,
		"c3/c/c-1.0.ebuild": `EAPI=8
DESCRIPTION="C"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="c4/d"
`,
		"c4/d/d-1.0.ebuild": `EAPI=8
DESCRIPTION="D"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="c5/e"
`,
		"c5/e/e-1.0.ebuild": `EAPI=8
DESCRIPTION="E"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
	})

	registry := provider.NewProviderRegistry()
	cache := NewVersionCacheWithPath(t.TempDir(), 60)

	aEbuild, err := atom.FindLatest(filepath.Join(ov, "c1", "a"))
	if err != nil {
		t.Fatalf("FindLatest(a): %v", err)
	}

	visited := map[string]bool{}
	results := ScanDependencies(ov, aEbuild, registry, false, cache, 0, visited)

	// Expect exactly 3 rows: c2/b (depth 1), c3/c (depth 2), c4/d (depth 3).
	// c5/e should NOT appear (depth would be 4 > maxDepDepth).
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	expected := []struct {
		pkg   string
		depth int
	}{
		{"c2/b", 1},
		{"c3/c", 2},
		{"c4/d", 3},
	}
	for i, want := range expected {
		if results[i].Package != want.pkg {
			t.Errorf("result[%d].Package = %q, want %q", i, results[i].Package, want.pkg)
		}
		if results[i].Depth != want.depth {
			t.Errorf("result[%d].Depth = %d, want %d", i, results[i].Depth, want.depth)
		}
	}
	// Verify e is absent
	for _, r := range results {
		if r.Package == "c5/e" {
			t.Error("c5/e should not appear (depth 4 > max)")
		}
	}
}

// TestScanDependencies_PreOrder verifies pre-order traversal: root → {x, y}, x → x1.
// Expected order: x (depth 1), x1 (depth 2), y (depth 1).
func TestScanDependencies_PreOrder(t *testing.T) {
	ov := setupFixtureOverlay(t, map[string]string{
		"c/root/root-1.0.ebuild": `EAPI=8
DESCRIPTION="Root"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="cx/x cy/y"
`,
		"cx/x/x-1.0.ebuild": `EAPI=8
DESCRIPTION="X"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="cx1/x1"
`,
		"cx1/x1/x1-1.0.ebuild": `EAPI=8
DESCRIPTION="X1"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
		"cy/y/y-1.0.ebuild": `EAPI=8
DESCRIPTION="Y"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
	})

	registry := provider.NewProviderRegistry()
	cache := NewVersionCacheWithPath(t.TempDir(), 60)

	rootEbuild, err := atom.FindLatest(filepath.Join(ov, "c", "root"))
	if err != nil {
		t.Fatalf("FindLatest(root): %v", err)
	}

	visited := map[string]bool{}
	results := ScanDependencies(ov, rootEbuild, registry, false, cache, 0, visited)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Pre-order: x → x1 → y
	if results[0].Package != "cx/x" || results[0].Depth != 1 {
		t.Errorf("expected cx/x at depth 1, got %s at depth %d", results[0].Package, results[0].Depth)
	}
	if results[1].Package != "cx1/x1" || results[1].Depth != 2 {
		t.Errorf("expected cx1/x1 at depth 2, got %s at depth %d", results[1].Package, results[1].Depth)
	}
	if results[2].Package != "cy/y" || results[2].Depth != 1 {
		t.Errorf("expected cy/y at depth 1, got %s at depth %d", results[2].Package, results[2].Depth)
	}
}

// TestScanDependencies_GlobalDedup verifies that a diamond dependency is
// printed only once (D5).
func TestScanDependencies_GlobalDedup(t *testing.T) {
	ov := setupFixtureOverlay(t, map[string]string{
		"c/root/root-1.0.ebuild": `EAPI=8
DESCRIPTION="Root"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="cx/x cy/y"
`,
		"cx/x/x-1.0.ebuild": `EAPI=8
DESCRIPTION="X"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="cz/z"
`,
		"cy/y/y-1.0.ebuild": `EAPI=8
DESCRIPTION="Y"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="cz/z"
`,
		"cz/z/z-1.0.ebuild": `EAPI=8
DESCRIPTION="Z"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
	})

	registry := provider.NewProviderRegistry()
	cache := NewVersionCacheWithPath(t.TempDir(), 60)

	rootEbuild, err := atom.FindLatest(filepath.Join(ov, "c", "root"))
	if err != nil {
		t.Fatalf("FindLatest(root): %v", err)
	}

	visited := map[string]bool{}
	results := ScanDependencies(ov, rootEbuild, registry, false, cache, 0, visited)

	// Count how many times "cz/z" appears
	zCount := 0
	for _, r := range results {
		if r.Package == "cz/z" {
			zCount++
		}
	}
	if zCount != 1 {
		t.Errorf("expected cz/z exactly once, appeared %d times", zCount)
	}
}

// TestScanDependencies_Cycle verifies that a circular dependency terminates.
// a → b → a. a is seeded into visited; neither a nor b should appear.
func TestScanDependencies_Cycle(t *testing.T) {
	ov := setupFixtureOverlay(t, map[string]string{
		"c/a/a-1.0.ebuild": `EAPI=8
DESCRIPTION="A"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="c/b"
`,
		"c/b/b-1.0.ebuild": `EAPI=8
DESCRIPTION="B"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
RDEPEND="c/a"
`,
	})

	registry := provider.NewProviderRegistry()
	cache := NewVersionCacheWithPath(t.TempDir(), 60)

	aEbuild, err := atom.FindLatest(filepath.Join(ov, "c", "a"))
	if err != nil {
		t.Fatalf("FindLatest(a): %v", err)
	}

	// Seed "c/a" into visited (the caller does this).
	visited := map[string]bool{"c/a": true}
	results := ScanDependencies(ov, aEbuild, registry, false, cache, 0, visited)

	// Only b should appear (depth 1). a is already visited.
	// But wait: b's RDEPEND is c/a, which is already in visited, so b → a is blocked.
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Package != "c/b" {
		t.Errorf("expected c/b, got %s", results[0].Package)
	}
}

// TestScanDependencies_NoDepends verifies that an ebuild with no deps returns
// an empty result.
func TestScanDependencies_NoDepends(t *testing.T) {
	ov := setupFixtureOverlay(t, map[string]string{
		"c/empty/empty-1.0.ebuild": `EAPI=8
DESCRIPTION="Empty"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`,
	})

	registry := provider.NewProviderRegistry()
	cache := NewVersionCacheWithPath(t.TempDir(), 60)

	eb, err := atom.FindLatest(filepath.Join(ov, "c", "empty"))
	if err != nil {
		t.Fatalf("FindLatest: %v", err)
	}

	visited := map[string]bool{}
	results := ScanDependencies(ov, eb, registry, false, cache, 0, visited)

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}
