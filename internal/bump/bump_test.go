package bump

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/version"
)

// ─── findLatestEbuild Tests ──────────────────────────────────────────────────

func TestFindLatestEbuild_SingleEbuild(t *testing.T) {
	dir := t.TempDir()
	ebuildPath := filepath.Join(dir, "my-pkg-1.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	path, err := findLatestEbuild(dir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if path != ebuildPath {
		t.Errorf("expected %q, got %q", ebuildPath, path)
	}
}

func TestFindLatestEbuild_MultipleEbuilds(t *testing.T) {
	dir := t.TempDir()
	// Create ebuilds of different versions
	ebuilds := []string{
		"my-pkg-1.0.ebuild",
		"my-pkg-2.0.ebuild",
		"my-pkg-1.5.ebuild",
	}
	for _, name := range ebuilds {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("EAPI=8"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	path, err := findLatestEbuild(dir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	expected := filepath.Join(dir, "my-pkg-2.0.ebuild")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestFindLatestEbuild_NoEbuilds(t *testing.T) {
	dir := t.TempDir()

	_, err := findLatestEbuild(dir)
	if err == nil {
		t.Fatal("expected error for no ebuilds, got nil")
	}
}

func TestFindLatestEbuild_LiveExcluded(t *testing.T) {
	dir := t.TempDir()
	// Live version ebuilds should be excluded
	if err := os.WriteFile(filepath.Join(dir, "my-pkg-9999.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "my-pkg-1.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	path, err := findLatestEbuild(dir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	expected := filepath.Join(dir, "my-pkg-1.0.ebuild")
	if path != expected {
		t.Errorf("expected %q (live excluded), got %q", expected, path)
	}
}

func TestFindLatestEbuild_MixedLiveStable(t *testing.T) {
	dir := t.TempDir()
	// Multiple live + stable — should pick highest stable
	ebuilds := []string{
		"my-pkg-9999.ebuild",
		"my-pkg-1.0.ebuild",
		"my-pkg-2.0.ebuild",
		"my-pkg-1.0-r1.ebuild",
	}
	for _, name := range ebuilds {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("EAPI=8"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	path, err := findLatestEbuild(dir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	expected := filepath.Join(dir, "my-pkg-2.0.ebuild")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestFindLatestEbuild_RevisionsOnly(t *testing.T) {
	dir := t.TempDir()
	// Only revision bumps exist, no base ebuild
	if err := os.WriteFile(filepath.Join(dir, "my-pkg-1.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "my-pkg-1.0-r1.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "my-pkg-1.0-r3.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	path, err := findLatestEbuild(dir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// Should return highest revision
	expected := filepath.Join(dir, "my-pkg-1.0-r3.ebuild")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

// ─── incrementRevision Tests ─────────────────────────────────────────────────

func TestIncrementRevision_None(t *testing.T) {
	dir := t.TempDir()
	// Only one ebuild, no revisions
	if err := os.WriteFile(filepath.Join(dir, "my-pkg-1.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	n := incrementRevision(dir)
	if n != 1 {
		t.Errorf("expected 1, got %d", n)
	}
}

func TestIncrementRevision_Increment(t *testing.T) {
	dir := t.TempDir()
	// Has revisions up to -r3
	if err := os.WriteFile(filepath.Join(dir, "my-pkg-1.0.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "my-pkg-1.0-r1.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "my-pkg-1.0-r3.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	n := incrementRevision(dir)
	if n != 4 {
		t.Errorf("expected 4, got %d", n)
	}
}

func TestIncrementRevision_SingleRevision(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "my-pkg-1.0-r2.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	n := incrementRevision(dir)
	if n != 3 {
		t.Errorf("expected 3, got %d", n)
	}
}

// ─── bumpFromEbuild Tests ────────────────────────────────────────────────────

func TestBump_VersionBump(t *testing.T) {
	dir := t.TempDir()
	oldName := "my-pkg-1.0.0.ebuild"
	content := `EAPI=8
PV=1.0.0
SRC_URI="mirror://test/my-pkg-1.0.0.tar.gz"

cat() {
	tar xzf ${A}
}
` 
	if err := os.WriteFile(filepath.Join(dir, oldName), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Bump(BumpConfig{
		Dir:     dir,
		Mode:    BumpModeVersion,
		Version: "2.0.0",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.NewFile != filepath.Join(dir, "my-pkg-2.0.0.ebuild") {
		t.Errorf("expected new file %q, got %q", filepath.Join(dir, "my-pkg-2.0.0.ebuild"), result.NewFile)
	}
}

func TestBump_VersionBump_Variables(t *testing.T) {
	dir := t.TempDir()
	oldName := "my-pkg-1.0.0.ebuild"
	content := `EAPI=8
PV=1.0.0
SRC_URI="mirror://test/${P}.tar.gz"

MYVAR="1.0.0-locked"

src_unpack() {
	einfo "unpacking ${P}"
}
` 
	if err := os.WriteFile(filepath.Join(dir, oldName), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Bump(BumpConfig{
		Dir:     dir,
		Mode:    BumpModeVersion,
		Version: "2.0.0",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Read the new file
	newContent, err := os.ReadFile(result.NewFile)
	if err != nil {
		t.Fatalf("failed to read new file: %v", err)
	}

	// PV should be updated to new version
	if !strings.Contains(string(newContent), "PV=2.0.0") {
		t.Errorf("expected PV=2.0.0 in new file, got:\n%s", string(newContent))
	}

	// ${P} references in other places should be preserved (not replaced with version)
	if !strings.Contains(string(newContent), `${P}`) {
		t.Errorf("expected ${P} reference preserved in new file")
	}

	// ${P} should be preserved
	if !strings.Contains(string(newContent), `${P}`) {
		t.Errorf("expected ${P} reference preserved in new file")
	}

	// Hardcoded string "1.0.0-locked" should be untouched
	if !strings.Contains(string(newContent), `1.0.0-locked`) {
		t.Errorf("expected hardcoded string 1.0.0-locked to be preserved")
	}
}

func TestBump_RevisionBump(t *testing.T) {
	dir := t.TempDir()
	oldName := "my-pkg-1.0.0.ebuild"
	if err := os.WriteFile(filepath.Join(dir, oldName), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Bump(BumpConfig{
		Dir:  dir,
		Mode: BumpModeRevision,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// Should be -r1 since no existing revisions
	if result.NewFile != filepath.Join(dir, "my-pkg-1.0.0-r1.ebuild") {
		t.Errorf("expected my-pkg-1.0.0-r1.ebuild, got %q", result.NewFile)
	}
	if result.Revision != 1 {
		t.Errorf("expected revision 1, got %d", result.Revision)
	}
}

func TestBump_RevisionBump_Increment(t *testing.T) {
	dir := t.TempDir()
	// Existing ebuild with -r2
	oldName := "my-pkg-1.0.0-r2.ebuild"
	if err := os.WriteFile(filepath.Join(dir, oldName), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Bump(BumpConfig{
		Dir:  dir,
		Mode: BumpModeRevision,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.NewFile != filepath.Join(dir, "my-pkg-1.0.0-r3.ebuild") {
		t.Errorf("expected my-pkg-1.0.0-r3.ebuild, got %q", result.NewFile)
	}
	if result.Revision != 3 {
		t.Errorf("expected revision 3, got %d", result.Revision)
	}
}

func TestBump_LiveExcluded(t *testing.T) {
	dir := t.TempDir()
	// Only live version exists
	if err := os.WriteFile(filepath.Join(dir, "my-pkg-9999.ebuild"), []byte("EAPI=8"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Bump(BumpConfig{
		Dir:  dir,
		Mode: BumpModeAuto,
	})
	if err == nil {
		t.Fatal("expected error for live-only ebuild, got nil")
	}
}

func TestBump_NoEbuilds(t *testing.T) {
	dir := t.TempDir()

	_, err := Bump(BumpConfig{
		Dir:  dir,
		Mode: BumpModeRevision,
	})
	if err == nil {
		t.Fatal("expected error for empty directory, got nil")
	}
}

// ─── BumpConfig Valid Tests ──────────────────────────────────────────────────

func TestBumpConfig_Validation(t *testing.T) {
	_, err := Bump(BumpConfig{})
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}

// ─── Integration Tests ───────────────────────────────────────────────────────

// TestBump_AutoVersionWithProvider tests the full auto-version bump with a mock provider.
func TestBump_AutoVersionWithProvider(t *testing.T) {
	dir := t.TempDir()
	// Create a test ebuild with PV=1.0.0
	content := `EAPI=8
PV=1.0.0
P=${PN}-${PV}
SRC_URI="mirror://github/example/example-1.0.0.tar.gz"

cat() {
	tar xzf ${A}
}
` 
	if err := os.WriteFile(filepath.Join(dir, "example-1.0.0.ebuild"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a mock provider that reports latest version 2.0.0
	mockP := &mockUpstreamProvider{
		latestVersion: func(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
			return version.Parse("2.0.0")
		},
	}

	registry := provider.NewProviderRegistry()
	registry.Register(mockP)

	result, err := Bump(BumpConfig{
		Dir:             dir,
		Mode:            BumpModeAuto,
		ProviderReg:     registry,
		AllowPrerelease: false,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.NewFile != filepath.Join(dir, "example-2.0.0.ebuild") {
		t.Errorf("expected example-2.0.0.ebuild, got %q", result.NewFile)
	}
	if result.NewVersion != "2.0.0" {
		t.Errorf("expected new version 2.0.0, got %q", result.NewVersion)
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
