package bump

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

// ─── findLatestEbuild (delegated to atom.FindLatest) ────────────────────────

func TestFindLatestEbuild_SingleEbuild(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "my-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	ebuildPath := filepath.Join(pkgDir, "my-pkg-1.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	path, err := FindLatestEbuild(pkgDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if path != ebuildPath {
		t.Errorf("expected %q, got %q", ebuildPath, path)
	}
}

func TestFindLatestEbuild_MultipleEbuilds(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "my-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	ebuilds := []string{
		"my-pkg-1.0.ebuild",
		"my-pkg-2.0.ebuild",
		"my-pkg-1.5.ebuild",
	}
	for _, name := range ebuilds {
		if err := os.WriteFile(filepath.Join(pkgDir, name), []byte("EAPI=8"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	path, err := FindLatestEbuild(pkgDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	expected := filepath.Join(pkgDir, "my-pkg-2.0.ebuild")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestFindLatestEbuild_NoEbuilds(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "my-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := FindLatestEbuild(pkgDir)
	if err == nil {
		t.Fatal("expected error for no ebuilds, got nil")
	}
}

func TestFindLatestEbuild_LiveExcluded(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "my-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "my-pkg-9999.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "my-pkg-1.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	path, err := FindLatestEbuild(pkgDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	expected := filepath.Join(pkgDir, "my-pkg-1.0.ebuild")
	if path != expected {
		t.Errorf("expected %q (live excluded), got %q", expected, path)
	}
}

func TestFindLatestEbuild_MixedLiveStable(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "my-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	ebuilds := []string{
		"my-pkg-9999.ebuild",
		"my-pkg-1.0.ebuild",
		"my-pkg-2.0.ebuild",
		"my-pkg-1.0-r1.ebuild",
	}
	for _, name := range ebuilds {
		if err := os.WriteFile(filepath.Join(pkgDir, name), []byte("EAPI=8"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	path, err := FindLatestEbuild(pkgDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	expected := filepath.Join(pkgDir, "my-pkg-2.0.ebuild")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestFindLatestEbuild_RevisionsOnly(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "my-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "my-pkg-1.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "my-pkg-1.0-r1.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "my-pkg-1.0-r3.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	path, err := FindLatestEbuild(pkgDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	expected := filepath.Join(pkgDir, "my-pkg-1.0-r3.ebuild")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

// ─── Bump Tests ──────────────────────────────────────────────────────────────

func TestBump_VersionBump(t *testing.T) {
	pkgDir := filepath.Join(t.TempDir(), "my-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldName := "my-pkg-1.0.0.ebuild"
	content := `EAPI=8
PV=1.0.0
SRC_URI="mirror://test/my-pkg-1.0.0.tar.gz"

cat() {
	tar xzf ${A}
}
`
	if err := os.WriteFile(filepath.Join(pkgDir, oldName), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Bump(BumpConfig{
		Dir:     pkgDir,
		Mode:    BumpModeVersion,
		Version: "2.0.0",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.NewFile != filepath.Join(pkgDir, "my-pkg-2.0.0.ebuild") {
		t.Errorf("expected new file %q, got %q", filepath.Join(pkgDir, "my-pkg-2.0.0.ebuild"), result.NewFile)
	}
}

func TestBump_VersionBump_Variables(t *testing.T) {
	pkgDir := filepath.Join(t.TempDir(), "my-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldName := "my-pkg-1.0.0.ebuild"
	content := `EAPI=8
PV=1.0.0
P=${PN}-${PV}
SRC_URI="mirror://test/${P}.tar.gz"

MYVAR="1.0.0-locked"

src_unpack() {
	einfo "unpacking ${P}"
}
`
	if err := os.WriteFile(filepath.Join(pkgDir, oldName), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Bump(BumpConfig{
		Dir:     pkgDir,
		Mode:    BumpModeVersion,
		Version: "2.0.0",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	newContent, err := os.ReadFile(result.NewFile)
	if err != nil {
		t.Fatalf("failed to read new file: %v", err)
	}

	if !strings.Contains(string(newContent), "PV=2.0.0") {
		t.Errorf("expected PV=2.0.0 in new file, got:\n%s", string(newContent))
	}
	if !strings.Contains(string(newContent), `${P}`) {
		t.Errorf("expected ${P} reference preserved in new file")
	}
	if !strings.Contains(string(newContent), `1.0.0-locked`) {
		t.Errorf("expected hardcoded string 1.0.0-locked to be preserved")
	}
}

func TestBump_RevisionBump(t *testing.T) {
	pkgDir := filepath.Join(t.TempDir(), "my-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldName := "my-pkg-1.0.0.ebuild"
	if err := os.WriteFile(filepath.Join(pkgDir, oldName), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Bump(BumpConfig{
		Dir:  pkgDir,
		Mode: BumpModeRevision,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.NewFile != filepath.Join(pkgDir, "my-pkg-1.0.0-r1.ebuild") {
		t.Errorf("expected my-pkg-1.0.0-r1.ebuild, got %q", result.NewFile)
	}
	if result.Revision != 1 {
		t.Errorf("expected revision 1, got %d", result.Revision)
	}
}

func TestBump_RevisionBump_Increment(t *testing.T) {
	pkgDir := filepath.Join(t.TempDir(), "my-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldName := "my-pkg-1.0.0-r2.ebuild"
	if err := os.WriteFile(filepath.Join(pkgDir, oldName), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Bump(BumpConfig{
		Dir:  pkgDir,
		Mode: BumpModeRevision,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.NewFile != filepath.Join(pkgDir, "my-pkg-1.0.0-r3.ebuild") {
		t.Errorf("expected my-pkg-1.0.0-r3.ebuild, got %q", result.NewFile)
	}
	if result.Revision != 3 {
		t.Errorf("expected revision 3, got %d", result.Revision)
	}
}

func TestBump_LiveExcluded(t *testing.T) {
	pkgDir := filepath.Join(t.TempDir(), "my-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "my-pkg-9999.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Bump(BumpConfig{
		Dir:  pkgDir,
		Mode: BumpModeAuto,
	})
	if err == nil {
		t.Fatal("expected error for live-only ebuild, got nil")
	}
}

func TestBump_NoEbuilds(t *testing.T) {
	pkgDir := filepath.Join(t.TempDir(), "my-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := Bump(BumpConfig{
		Dir:  pkgDir,
		Mode: BumpModeRevision,
	})
	if err == nil {
		t.Fatal("expected error for empty directory, got nil")
	}
}

func TestBumpConfig_Validation(t *testing.T) {
	_, err := Bump(BumpConfig{})
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}

// ─── D8 Fix: Revision from latest, not directory max ─────────────────────────

func TestBump_NextRevisionFromLatestNotDirMax(t *testing.T) {
	// Temp package dir with foo-1.0.0-r3.ebuild and foo-2.0.0.ebuild.
	// A revision bump via Bump() should create foo-2.0.0-r1.ebuild
	// (revision of latest non-live = 2.0.0, not max over all files).
	pkgDir := filepath.Join(t.TempDir(), "foo")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-1.0.0-r3.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "foo-2.0.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Bump(BumpConfig{
		Dir:  pkgDir,
		Mode: BumpModeRevision,
	})
	if err != nil {
		t.Fatalf("Bump error = %v", err)
	}
	// Latest non-live is foo-2.0.0 (no revision), bump → foo-2.0.0-r1
	expected := filepath.Join(pkgDir, "foo-2.0.0-r1.ebuild")
	if result.NewFile != expected {
		t.Errorf("NewFile = %q, want %q", result.NewFile, expected)
	}
	if result.Revision != 1 {
		t.Errorf("Revision = %d, want 1", result.Revision)
	}
}

// ─── Integration Tests ───────────────────────────────────────────────────────

func TestBump_AutoVersionWithProvider(t *testing.T) {
	pkgDir := filepath.Join(t.TempDir(), "example")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `EAPI=8
PV=1.0.0
P=${PN}-${PV}
SRC_URI="mirror://github/example/example-1.0.0.tar.gz"

cat() {
	tar xzf ${A}
}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "example-1.0.0.ebuild"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	mockP := &mockUpstreamProvider{
		latestVersion: func(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
			return version.Parse("2.0.0")
		},
	}

	registry := provider.NewProviderRegistry()
	registry.Register(mockP)

	result, err := Bump(BumpConfig{
		Dir:             pkgDir,
		Mode:            BumpModeAuto,
		ProviderReg:     registry,
		AllowPrerelease: false,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.NewFile != filepath.Join(pkgDir, "example-2.0.0.ebuild") {
		t.Errorf("expected example-2.0.0.ebuild, got %q", result.NewFile)
	}
	if result.NewVersion != "2.0.0" {
		t.Errorf("expected new version 2.0.0, got %q", result.NewVersion)
	}
}

// ─── atom.Integration helper tests ──────────────────────────────────────────

func TestAtom_ParseEbuildPath_HyphenatedName(t *testing.T) {
	// Verify atom correctly handles hyphenated package names (D1-D5 regression)
	dir := t.TempDir()
	path := filepath.Join(dir, "dev-python", "kornia-rs", "kornia-rs-0.1.14.ebuild")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	pe, err := atom.ParseEbuildPath(path)
	if err != nil {
		t.Fatalf("ParseEbuildPath error = %v", err)
	}
	if pe.PN != "kornia-rs" {
		t.Errorf("PN = %q, want %q", pe.PN, "kornia-rs")
	}
	if pe.PV != "0.1.14" {
		t.Errorf("PV = %q, want %q", pe.PV, "0.1.14")
	}
}

// ─── .larry/config.yaml config loading test ────────────────────────────────────────

// mockResolverWithTags is a mock that implements ResolverWithTags for testing
// the bump command's version_template expansion path.
type mockResolverWithTags struct {
	latestVersion func(remoteid.RemoteId, bool) (*version.Version, error)
	resolution    *provider.Resolution
}

func (m *mockResolverWithTags) Name() string                        { return "mock-resolver" }
func (m *mockResolverWithTags) Type() remoteid.RemoteIdType        { return remoteid.TypeGitHub }
func (m *mockResolverWithTags) Metadata() provider.ProviderMetadata { return provider.ProviderMetadata{Name: "mock-resolver"} }
func (m *mockResolverWithTags) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if m.latestVersion != nil {
		return m.latestVersion(rid, allowPrereleases)
	}
	return version.Parse("1.0.0")
}
func (m *mockResolverWithTags) IsPrerelease(v *version.Version) bool { return v.IsPrerelease() }
func (m *mockResolverWithTags) FetchResolution(rid remoteid.RemoteId, allowPrereleases bool) (*provider.Resolution, error) {
	return m.resolution, nil
}

func TestBump_AutoVersionWithLarryConfig(t *testing.T) {
	// Test that the bump command loads .larry/config.yaml config and applies
	// version_template for date-based GitHub tags (regression test for
	// sys-kernel/raspberrypi-kernel bump producing wrong version).
	pkgDir := t.TempDir()
	pkgName := "my-pkg"
	pkgPath := filepath.Join(pkgDir, pkgName)
	if err := os.MkdirAll(pkgPath, 0755); err != nil {
		t.Fatal(err)
	}

	larryDir := filepath.Join(pkgPath, ".larry")
	if err := os.MkdirAll(larryDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create .larry/config.yaml with tag_match and version_template in the package dir
	yamlContent := `upstream:
  pv_match: '^(?P<BASE>\d+\.\d+\.\d+)(?:_p\d+)?$'
  tag_match: '^stable_(?P<DATE>\d{8})$'
  version_template: '${BASE}_p${DATE}'
`
	if err := os.WriteFile(filepath.Join(pkgPath, ".larry/config.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	content := `EAPI=8
PV=6.18.46_p20260824
SRC_URI="https://github.com/example/linux/archive/COMMIT.tar.gz"
`
	if err := os.WriteFile(filepath.Join(pkgPath, "my-pkg-6.18.46_p20260824.ebuild"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Mock provider that returns ByDate resolution (like GitHubProvider does
	// for raspberrypi/linux which has stable_YYYYMMDD tags)
	mockP := &mockResolverWithTags{
		resolution: &provider.Resolution{
			Tag:     "stable_20260911",
			TagDate: "20260911",
			ByDate:  true,
		},
	}

	registry := provider.NewProviderRegistry()
	registry.Register(mockP)

	result, err := Bump(BumpConfig{
		Dir:             pkgPath,
		Mode:            BumpModeAuto,
		ProviderReg:     registry,
		AllowPrerelease: false,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// With .larry/config.yaml config, the version_template should expand to:
	// BASE=6.18.46, DATE=20260911 → 6.18.46_p20260911
	if result.NewVersion != "6.18.46_p20260911" {
		t.Errorf("expected new version 6.18.46_p20260911, got %q", result.NewVersion)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

type mockUpstreamProvider struct {
	latestVersion func(remoteid.RemoteId, bool) (*version.Version, error)
}

func (m *mockUpstreamProvider) Name() string                        { return "mock" }
func (m *mockUpstreamProvider) Type() remoteid.RemoteIdType        { return remoteid.TypeGitHub }
func (m *mockUpstreamProvider) Metadata() provider.ProviderMetadata { return provider.ProviderMetadata{Name: "mock"} }
func (m *mockUpstreamProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if m.latestVersion != nil {
		return m.latestVersion(rid, allowPrereleases)
	}
	return version.Parse("1.0.0")
}
func (m *mockUpstreamProvider) IsPrerelease(v *version.Version) bool { return v.IsPrerelease() }

func TestBump_VersionBump_WarnsMissingFilesdirPin(t *testing.T) {
	pkgDir := filepath.Join(t.TempDir(), "my-pkg")
	filesDir := filepath.Join(pkgDir, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `EAPI=8
PV=1.0.0
PATCHES=( "${FILESDIR}/${P}-fix.patch" )
`
	if err := os.WriteFile(filepath.Join(pkgDir, "my-pkg-1.0.0.ebuild"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// The old-version patch exists; the bumped reference will not.
	if err := os.WriteFile(filepath.Join(filesDir, "my-pkg-1.0.0-fix.patch"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Bump(BumpConfig{Dir: pkgDir, Mode: BumpModeVersion, Version: "2.0.0"})
	if err != nil {
		t.Fatalf("Bump: %v", err)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", result.Warnings)
	}
	w := result.Warnings[0]
	if !strings.Contains(w, "my-pkg-2.0.0-fix.patch") || !strings.Contains(w, "does not exist") {
		t.Errorf("warning = %q, want it to report my-pkg-2.0.0-fix.patch missing", w)
	}
}

func TestBump_VersionBump_NoFilesdirWarningUnversioned(t *testing.T) {
	pkgDir := filepath.Join(t.TempDir(), "my-pkg")
	filesDir := filepath.Join(pkgDir, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `EAPI=8
PV=1.0.0
PATCHES=( "${FILESDIR}/fix.patch" )
`
	if err := os.WriteFile(filepath.Join(pkgDir, "my-pkg-1.0.0.ebuild"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filesDir, "fix.patch"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Bump(BumpConfig{Dir: pkgDir, Mode: BumpModeVersion, Version: "2.0.0"})
	if err != nil {
		t.Fatalf("Bump: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("warnings = %v, want none for unversioned ref", result.Warnings)
	}
}

func TestBump_RevisionBump_NoFilesdirWarnings(t *testing.T) {
	pkgDir := filepath.Join(t.TempDir(), "my-pkg")
	filesDir := filepath.Join(pkgDir, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `EAPI=8
PV=1.0.0
PATCHES=( "${FILESDIR}/${P}-fix.patch" )
`
	if err := os.WriteFile(filepath.Join(pkgDir, "my-pkg-1.0.0.ebuild"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filesDir, "my-pkg-1.0.0-fix.patch"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Bump(BumpConfig{Dir: pkgDir, Mode: BumpModeRevision})
	if err != nil {
		t.Fatalf("Bump: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("warnings = %v, want none on revision bump (refs unchanged)", result.Warnings)
	}
}
