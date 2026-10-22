package upstream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/larryconfig"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/version"
)

// mockProvider is a test double for provider.UpstreamProvider
type mockProvider struct {
	mockType    remoteid.RemoteIdType
	mockVersion *version.Version
	mockErr     error
}

func (m *mockProvider) Type() remoteid.RemoteIdType         { return m.mockType }
func (m *mockProvider) Metadata() provider.ProviderMetadata { return provider.ProviderMetadata{} }
func (m *mockProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if !allowPrereleases && m.mockVersion != nil && m.mockVersion.IsPrerelease() {
		return nil, nil
	}
	return m.mockVersion, m.mockErr
}
func (m *mockProvider) Name() string                         { return "mock" }
func (m *mockProvider) IsPrerelease(v *version.Version) bool { return v.IsPrerelease() }

// --- StatusHasRelease tests (D9) ---

func TestStatusHasRelease_IsNotSignificant(t *testing.T) {
	if StatusHasRelease.IsSignificant() {
		t.Error("StatusHasRelease.IsSignificant() should be false")
	}
	if StatusHasRelease.String() != "HAS-RELEASE" {
		t.Errorf("StatusHasRelease.String() = %q, want \"HAS-RELEASE\"", StatusHasRelease.String())
	}
}

func TestStatusIotaUnchanged(t *testing.T) {
	if StatusOutdated != 0 {
		t.Errorf("StatusOutdated = %d, want 0", StatusOutdated)
	}
	if StatusUpToDate != 1 {
		t.Errorf("StatusUpToDate = %d, want 1", StatusUpToDate)
	}
	if StatusAhead != 2 {
		t.Errorf("StatusAhead = %d, want 2", StatusAhead)
	}
	if StatusNewerTag != 3 {
		t.Errorf("StatusNewerTag = %d, want 3", StatusNewerTag)
	}
	if StatusLive != 4 {
		t.Errorf("StatusLive = %d, want 4", StatusLive)
	}
	if StatusDeprecated != 5 {
		t.Errorf("StatusDeprecated = %d, want 5", StatusDeprecated)
	}
	if StatusUnknown != 6 {
		t.Errorf("StatusUnknown = %d, want 6", StatusUnknown)
	}
	if StatusHasRelease != 7 {
		t.Errorf("StatusHasRelease = %d, want 7", StatusHasRelease)
	}
	if StatusCheckTag != 8 {
		t.Errorf("StatusCheckTag = %d, want 8", StatusCheckTag)
	}
	if StatusManual != 9 {
		t.Errorf("StatusManual = %d, want 9", StatusManual)
	}
}

// --- Discovery tests (D1, D2, D5, D9) ---

func setupTestOverlay(t *testing.T) string {
	dir := t.TempDir()
	profilesDir := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profilesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "repo_name"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeEbuild(t *testing.T, dir, name string, content string) {
	parts := strings.Split(strings.TrimSuffix(name, ".ebuild"), "/")
	// name = "cat/pkg/pkg-1.0.ebuild" → parts = ["cat", "pkg", "pkg-1.0"]
	// dir = overlay root → pkgDir = <dir>/cat/pkg
	pkgDir := filepath.Join(dir, strings.Join(parts[:len(parts)-1], "/"))
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Write file as just "pkg-1.0.ebuild" inside pkgDir
	baseName := parts[len(parts)-1] + ".ebuild"
	if err := os.WriteFile(filepath.Join(pkgDir, baseName), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestFindEbuilds_RevisionOnly(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-libs/bar/bar-3.0.0-r1.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	targets, err := discoverTargets(overlay)
	if err != nil {
		t.Fatalf("discoverTargets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	tgt := targets[0]
	if tgt.Ebuild.PVR != "3.0.0-r1" {
		t.Errorf("PVR = %q, want \"3.0.0-r1\"", tgt.Ebuild.PVR)
	}
	if tgt.Ebuild.PV != "3.0.0" {
		t.Errorf("PV = %q, want \"3.0.0\"", tgt.Ebuild.PV)
	}
	if tgt.LiveOnly {
		t.Error("LiveOnly should be false for a versioned ebuild")
	}
}

func TestFindEbuilds_LiveAndVersioned(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-libs/foo/foo-1.2.3.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	writeEbuild(t, overlay, "dev-libs/foo/foo-2.0.0-r1.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	writeEbuild(t, overlay, "dev-libs/foo/foo-9999.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	targets, err := discoverTargets(overlay)
	if err != nil {
		t.Fatalf("discoverTargets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	tgt := targets[0]
	if tgt.Ebuild.PVR != "2.0.0-r1" {
		t.Errorf("PVR = %q, want \"2.0.0-r1\"", tgt.Ebuild.PVR)
	}
	if tgt.LiveOnly {
		t.Error("LiveOnly should be false")
	}
}

func TestFindEbuilds_LiveOnly(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-libs/foo/foo-9999.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	targets, err := discoverTargets(overlay)
	if err != nil {
		t.Fatalf("discoverTargets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	tgt := targets[0]
	if tgt.Ebuild.PV != "9999" {
		t.Errorf("PV = %q, want \"9999\"", tgt.Ebuild.PV)
	}
	if !tgt.LiveOnly {
		t.Error("LiveOnly should be true for live-only package")
	}
}

func TestFindEbuilds_Deterministic(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-a/p1/p1-1.0.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	writeEbuild(t, overlay, "dev-a/p2/p2-1.0.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	writeEbuild(t, overlay, "dev-b/p1/p1-1.0.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	for i := 0; i < 3; i++ {
		targets, err := discoverTargets(overlay)
		if err != nil {
			t.Fatalf("discoverTargets: %v", err)
		}
		var keys []string
		for _, tgt := range targets {
			keys = append(keys, tgt.Atom.Category+"/"+tgt.Atom.PN)
		}
		want := []string{"dev-a/p1", "dev-a/p2", "dev-b/p1"}
		if len(keys) != len(want) {
			t.Errorf("iteration %d: got %d targets, want %d", i, len(keys), len(want))
			continue
		}
		for j, k := range keys {
			if k != want[j] {
				t.Errorf("iteration %d: keys[%d] = %q, want %q", i, j, k, want[j])
			}
		}
	}
}

func TestSkipsAcctPackages(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "acct-group/x/x-0.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	writeEbuild(t, overlay, "acct-user/x/x-0.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	writeEbuild(t, overlay, "dev-util/y/y-1.0.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	targets, err := discoverTargets(overlay)
	if err != nil {
		t.Fatalf("discoverTargets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Atom.PN != "y" {
		t.Errorf("got PN %q, want y", targets[0].Atom.PN)
	}
}

// --- Live-only with upstream release (D9) ---

func TestLiveOnly_Verbose_HasRelease(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "app-shells/zoxide/zoxide-9999.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI="https://github.com/ajeetdsouza/zoxide/archive/v${PV}.tar.gz"
LICENSE="MIT"
SLOT="0"
`)
	metaDir := filepath.Join(overlay, "app-shells", "zoxide")
	metaXML := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkgmetadata SYSTEM "https://www.gentoo.org/dtd/metadata.dtd">
<pkgmetadata>
<upstream>
<remote-id type="github">ajeetdsouza/zoxide</remote-id>
</upstream>
</pkgmetadata>
`
	if err := os.WriteFile(filepath.Join(metaDir, "metadata.xml"), []byte(metaXML), 0644); err != nil {
		t.Fatal(err)
	}
	registry := provider.NewProviderRegistry()
	mockV, _ := version.Parse("1.2.3")
	registry.Register(&mockProvider{mockType: remoteid.TypeGitHub, mockVersion: mockV})
	results, err := RunWithTargets(RunOptions{OverlayDir: overlay, Registry: registry})
	if err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Current != "9999 (live)" {
		t.Errorf("Current = %q, want \"9999 (live)\"", r.Current)
	}
	if r.Latest != "1.2.3" {
		t.Errorf("Latest = %q, want \"1.2.3\"", r.Latest)
	}
	if r.Status != StatusHasRelease {
		t.Errorf("Status = %s, want HAS-RELEASE", r.Status)
	}
}

func TestLiveOnly_Verbose_NoRelease(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "app-shells/zoxide/zoxide-9999.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	registry := provider.NewProviderRegistry()
	registry.Register(&mockProvider{
		mockType:    remoteid.TypeGitHub,
		mockVersion: nil,
		mockErr:     fmt.Errorf("no releases"),
	})
	results, err := RunWithTargets(RunOptions{OverlayDir: overlay, Registry: registry})
	if err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Current != "9999 (live)" {
		t.Errorf("Current = %q, want \"9999 (live)\"", r.Current)
	}
	if r.Latest != "" {
		t.Errorf("Latest = %q, want empty", r.Latest)
	}
	if r.Status != StatusLive {
		t.Errorf("Status = %s, want LIVE", r.Status)
	}
}

// --- PyPI name tests (D6) ---

func TestPypiName_MyPN(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-python/python-nmcli/python-nmcli-1.8.0.ebuild", `EAPI=8
MY_PN="nmcli"
DESCRIPTION="Test"
SRC_URI="mirror://pypi/n/nmcli/nmcli-1.8.0.tar.gz"
LICENSE="MIT"
SLOT="0"
`)
	targets, err := discoverTargets(overlay)
	if err != nil {
		t.Fatalf("discoverTargets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	einfo, err := ParseEbuildInfo(targets[0].Ebuild.Path)
	if err != nil {
		t.Fatalf("ParseEbuildInfo: %v", err)
	}
	rid, err := ResolveRemoteId(einfo, "pypi")
	if err != nil {
		t.Fatalf("ResolveRemoteId: %v", err)
	}
	if rid.Name != "nmcli" {
		t.Errorf("RemoteId.Name = %q, want \"nmcli\"", rid.Name)
	}
}

func TestPypiName_Precedence(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-python/foo/foo-1.0.ebuild", `EAPI=8
PYPI_PN="aaa"
MY_PN="bbb"
DESCRIPTION="Test"
SRC_URI="mirror://pypi/aaa/aaa-1.0.tar.gz"
LICENSE="MIT"
SLOT="0"
`)
	targets, err := discoverTargets(overlay)
	if err != nil {
		t.Fatalf("discoverTargets: %v", err)
	}
	einfo, err := ParseEbuildInfo(targets[0].Ebuild.Path)
	if err != nil {
		t.Fatalf("ParseEbuildInfo: %v", err)
	}
	rid, err := ResolveRemoteId(einfo, "pypi")
	if err != nil {
		t.Fatalf("ResolveRemoteId: %v", err)
	}
	if rid.Name != "aaa" {
		t.Errorf("RemoteId.Name = %q, want \"aaa\"", rid.Name)
	}
}

func TestPypiName_ExpandForms(t *testing.T) {
	tests := []struct {
		pn      string
		varLine string
		want    string
	}{
		{"foo-bar", `PYPI_PN="${PN//-/_}"`, "foo_bar"},
		{"a-b-c", `PYPI_PN="${PN/-/_}"`, "a_b-c"},
		{"foo", `PYPI_PN="${PN}"`, "foo"},
		{"foo-bar", "", "foo-bar"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			overlay := setupTestOverlay(t)
			fullPath := filepath.Join(overlay, "dev-python", "foo", "foo-1.0.ebuild")
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				t.Fatal(err)
			}
			content := fmt.Sprintf(`EAPI=8
DESCRIPTION="Test"
%s
SRC_URI="mirror://pypi/foo/foo-1.0.tar.gz"
LICENSE="MIT"
SLOT="0"
`, tc.varLine)
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			einfo := &ebuild.EbuildInfo{
				Path: fullPath,
				Pkg:  tc.pn,
				Raw:  content,
			}
			rid, err := ResolveRemoteId(einfo, "pypi")
			if err != nil {
				t.Fatalf("ResolveRemoteId: %v", err)
			}
			if rid.Name != tc.want {
				t.Errorf("RemoteId.Name = %q, want %q", rid.Name, tc.want)
			}
		})
	}
}

// --- npm name tests (D7, D8) ---

func TestNpmName_CustomVar(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-libs/widget/widget-1.0.0.ebuild", `EAPI=8
MY_SCOPE="@acme"
DESCRIPTION="Test"
SRC_URI="mirror://npm/${MY_SCOPE}/widget/-/widget-1.0.0.tgz"
LICENSE="MIT"
SLOT="0"
`)
	targets, err := discoverTargets(overlay)
	if err != nil {
		t.Fatalf("discoverTargets: %v", err)
	}
	einfo, err := ParseEbuildInfo(targets[0].Ebuild.Path)
	if err != nil {
		t.Fatalf("ParseEbuildInfo: %v", err)
	}
	rid, err := ResolveRemoteId(einfo, "npm")
	if err != nil {
		t.Fatalf("ResolveRemoteId: %v", err)
	}
	if rid.Name != "@acme/widget" {
		t.Errorf("RemoteId.Name = %q, want \"@acme/widget\"", rid.Name)
	}
}

func TestNpmName_ExpandsPVAndP(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-libs/widget/widget-1.0.0.ebuild", `EAPI=8
PV="1.0.0"
DESCRIPTION="Test"
SRC_URI="mirror://npm/${PN}/-/${P}.tgz"
LICENSE="MIT"
SLOT="0"
`)
	targets, err := discoverTargets(overlay)
	if err != nil {
		t.Fatalf("discoverTargets: %v", err)
	}
	einfo, err := ParseEbuildInfo(targets[0].Ebuild.Path)
	if err != nil {
		t.Fatalf("ParseEbuildInfo: %v", err)
	}
	rid, err := ResolveRemoteId(einfo, "npm")
	if err != nil {
		t.Fatalf("ResolveRemoteId: %v", err)
	}
	if rid.Name != "widget" {
		t.Errorf("RemoteId.Name = %q, want \"widget\"", rid.Name)
	}
}

func TestNpmName_FirstLineOnly(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-libs/widget/widget-1.0.0.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI="mirror://npm/widget/-/widget-1.0.0.tgz"
SRC_URI+=" webui? ( mirror://npm/other/-/other-2.0.0.tgz )"
LICENSE="MIT"
SLOT="0"
`)
	targets, err := discoverTargets(overlay)
	if err != nil {
		t.Fatalf("discoverTargets: %v", err)
	}
	einfo, err := ParseEbuildInfo(targets[0].Ebuild.Path)
	if err != nil {
		t.Fatalf("ParseEbuildInfo: %v", err)
	}
	rid, err := ResolveRemoteId(einfo, "npm")
	if err != nil {
		t.Fatalf("ResolveRemoteId: %v", err)
	}
	if rid.Name != "widget" {
		t.Errorf("RemoteId.Name = %q, want \"widget\"", rid.Name)
	}
}

func TestNpmName_ScopedNoDashDash(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-libs/widget/widget-1.0.0.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI="mirror://npm/@acme/widget/widget-1.0.0.tgz"
LICENSE="MIT"
SLOT="0"
`)
	targets, err := discoverTargets(overlay)
	if err != nil {
		t.Fatalf("discoverTargets: %v", err)
	}
	einfo, err := ParseEbuildInfo(targets[0].Ebuild.Path)
	if err != nil {
		t.Fatalf("ParseEbuildInfo: %v", err)
	}
	rid, err := ResolveRemoteId(einfo, "npm")
	if err != nil {
		t.Fatalf("ResolveRemoteId: %v", err)
	}
	// Scoped URL with no /-/: keep @scope/name (diverges from script).
	if rid.Name != "@acme/widget" {
		t.Errorf("RemoteId.Name = %q, want \"@acme/widget\"", rid.Name)
	}
}

func TestNpmName_UnscopedNoDashDash(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-libs/widget/widget-1.0.0.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI="mirror://npm/widget/widget-1.0.0.tgz"
LICENSE="MIT"
SLOT="0"
`)
	targets, err := discoverTargets(overlay)
	if err != nil {
		t.Fatalf("discoverTargets: %v", err)
	}
	einfo, err := ParseEbuildInfo(targets[0].Ebuild.Path)
	if err != nil {
		t.Fatalf("ParseEbuildInfo: %v", err)
	}
	rid, err := ResolveRemoteId(einfo, "npm")
	if err != nil {
		t.Fatalf("ResolveRemoteId: %v", err)
	}
	if rid.Name != "widget" {
		t.Errorf("RemoteId.Name = %q, want \"widget\"", rid.Name)
	}
}

// --- shouldSkip tests ---

func TestShouldSkip(t *testing.T) {
	if !shouldSkip("acct-group") {
		t.Error("acct-group should be skipped")
	}
	if !shouldSkip("acct-user") {
		t.Error("acct-user should be skipped")
	}
	if shouldSkip("dev-python") {
		t.Error("dev-python should NOT be skipped")
	}
}

// --- UpstreamResult tests ---

func TestUpstreamResult_String(t *testing.T) {
	result := UpstreamResult{
		Package: "dev-python/requests",
		Current: "1.0.0",
		Latest:  "2.0.0",
		Status:  StatusOutdated,
		Source:  SourceInfo{URL: "https://pypi.org"},
	}
	s := result.String()
	if !strings.Contains(s, "dev-python/requests") {
		t.Fatalf("String() should contain package name: %s", s)
	}
	if !strings.Contains(s, "OUTDATED") {
		t.Fatalf("String() should contain status: %s", s)
	}
}

// --- Cache integration tests ---

func TestCacheIntegration_CacheHit(t *testing.T) {
	dir := t.TempDir()
	cache := NewVersionCacheWithPath(dir, 6*time.Hour)
	cache.Set("dev-python/requests/pypi:1.0.0", "2.0.0")
	ver, ok := cache.Get("dev-python/requests/pypi:1.0.0")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if ver != "2.0.0" {
		t.Fatalf("expected 2.0.0, got %s", ver)
	}
}

func TestCacheIntegration_CacheMissAfterTTL(t *testing.T) {
	dir := t.TempDir()
	cache := NewVersionCacheWithPath(dir, 1*time.Nanosecond)
	cache.Set("dev-python/requests/pypi:1.0.0", "2.0.0")
	path := filepath.Join(dir, "dev-python_requests_pypi_1.0.0")
	oldTime := time.Now().Add(-7 * 60 * 60 * time.Hour)
	if err := os.WriteFile(path, []byte("2.0.0\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	_, ok := cache.Get("dev-python/requests/pypi:1.0.0")
	if ok {
		t.Fatal("expected cache miss after TTL")
	}
}

// --- Utility tests ---

func TestStatusIsSignificant(t *testing.T) {
	if !StatusOutdated.IsSignificant() {
		t.Error("OUTDATED should be significant")
	}
	if StatusUpToDate.IsSignificant() {
		t.Error("UP-TO-DATE should not be significant")
	}
	if !StatusAhead.IsSignificant() {
		t.Error("AHEAD should be significant")
	}
	if StatusLive.IsSignificant() {
		t.Error("LIVE should not be significant")
	}
	if !StatusUnknown.IsSignificant() {
		t.Error("UNKNOWN should be significant")
	}
	if StatusHasRelease.IsSignificant() {
		t.Error("HAS-RELEASE should not be significant")
	}
	if !StatusNewerTag.IsSignificant() {
		t.Error("NEWER-TAG should be significant")
	}
}

// --- Version parsing tests ---

func TestVersionCompare_Outdated(t *testing.T) {
	latest, _ := version.Parse("2.0.0")
	current, _ := version.Parse("1.0.0")
	if latest.Compare(current) != 1 {
		t.Fatalf("expected 2.0.0 > 1.0.0")
	}
}

func TestVersionCompare_UpToDate(t *testing.T) {
	a, _ := version.Parse("1.0.0")
	b, _ := version.Parse("1.0.0")
	if a.Compare(b) != 0 {
		t.Fatalf("expected 1.0.0 == 1.0.0")
	}
}

func TestVersionCompare_Ahead(t *testing.T) {
	a, _ := version.Parse("1.0.0")
	b, _ := version.Parse("2.0.0")
	if a.Compare(b) != -1 {
		t.Fatalf("expected 1.0.0 < 2.0.0")
	}
}

// --- Source URL tests ---

func TestResolveSourceURL_PyPI(t *testing.T) {
	rid := &remoteid.RemoteId{Type: remoteid.TypePyPI, Name: "requests"}
	url := resolveSourceURL(rid)
	if url != "https://pypi.org/project/requests/" {
		t.Fatalf("expected https://pypi.org/project/requests/, got %s", url)
	}
}

func TestResolveSourceURL_NPM(t *testing.T) {
	rid := &remoteid.RemoteId{Type: remoteid.TypeNPM, Name: "lodash"}
	url := resolveSourceURL(rid)
	if url != "https://www.npmjs.com/package/lodash" {
		t.Fatalf("expected https://www.npmjs.com/package/lodash, got %s", url)
	}
}

// --- Ebuild parsing tests ---

func TestParseEbuildInfo(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-python", "requests")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pkgDir): %v", err)
	}
	ebuildPath := filepath.Join(pkgDir, "requests-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte(`
EAPI=8
DESCRIPTION="A test package"
HOMEPAGE="https://pypi.org/project/requests/"
SRC_URI="mirror://pypi/requests/requests-1.0.0.tar.gz"
LICENSE="Apache-2.0"
SLOT="0"
`), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := ParseEbuildInfo(ebuildPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.PVVal != "1.0.0" {
		t.Errorf("expected PV 1.0.0, got %s", info.PVVal)
	}
	if info.Pkg != "requests" {
		t.Errorf("expected PN requests, got %s", info.Pkg)
	}
	if !strings.Contains(info.SRC_URI, "mirror://pypi") {
		t.Errorf("expected SRC_URI to contain mirror://pypi, got %s", info.SRC_URI)
	}
}

func TestDebugParseEbuildInfo(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-python", "requests")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pkgDir): %v", err)
	}
	ebuildPath := filepath.Join(pkgDir, "requests-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte(`EAPI=8
DESCRIPTION="Test"
SRC_URI="mirror://pypi/requests/requests-1.0.0.tar.gz"
`), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := ParseEbuildInfo(ebuildPath)
	if err != nil {
		t.Fatalf("parseEbuildInfo error: %v", err)
	}
	t.Logf("Parsed: PV=%s, PN=%s, Category=%s", info.PVVal, info.Pkg, info.Category)
	if info.PVVal == "" {
		t.Error("expected non-empty PV")
	}
	if info.Pkg == "" {
		t.Error("expected non-empty PN")
	}
	if info.Category == "" {
		t.Error("expected non-empty Category")
	}
}

// --- Helper function tests ---

func TestDetectProviderFromSRCURI_PyPI(t *testing.T) {
	tType := detectProviderFromSRCURI("mirror://pypi/requests/requests-1.0.0.tar.gz", "")
	if tType != "pypi" {
		t.Fatalf("expected pypi, got %s", tType)
	}
}

func TestDetectProviderFromSRCURI_NPM(t *testing.T) {
	tType := detectProviderFromSRCURI("mirror://npm/myapp/myapp-1.0.0.tgz", "")
	if tType != "npm" {
		t.Fatalf("expected npm, got %s", tType)
	}
}

func TestDetectProviderFromSRCURI_NPMDirectRegistry(t *testing.T) {
	// Direct registry URLs must classify as npm even when a GitHub releases
	// URL (the overlay's own deps tarball) sits in the same SRC_URI.
	srcURI := "https://registry.npmjs.org/@scope/myapp/-/myapp-1.0.0.tgz " +
		"https://github.com/o/r/releases/download/myapp/myapp-1.0.0-deps.tar.xz"
	tType := detectProviderFromSRCURI(srcURI, "")
	if tType != "npm" {
		t.Fatalf("expected npm, got %s", tType)
	}
}

func TestExtractNpmNameFromSRCURI_DirectRegistry(t *testing.T) {
	content := `MY_NPM_SCOPE="@earendil-works"
SRC_URI="https://registry.npmjs.org/${MY_NPM_SCOPE}/${PN}/-/${P}.tgz -> ${P}.tgz"
`
	got := extractNpmNameFromSRCURI("", content, "pi-coding-agent")
	if got != "@earendil-works/pi-coding-agent" {
		t.Fatalf("extractNpmNameFromSRCURI(direct) = %q, want %q", got, "@earendil-works/pi-coding-agent")
	}
}

func TestDetectProviderFromSRCURI_GitHub(t *testing.T) {
	tType := detectProviderFromSRCURI("mirror://github/owner/repo/archive/v1.0.0.tar.gz", "")
	if tType != "github" {
		t.Fatalf("expected github, got %s", tType)
	}
}

func TestDetectProviderFromSRCURI_Generic(t *testing.T) {
	tType := detectProviderFromSRCURI("https://example.com/pkg-1.0.0.tar.gz", "")
	if tType != "" {
		t.Fatalf("expected empty for generic, got %s", tType)
	}
}

// --- Atom parsing tests ---

func TestAtomParseAtom(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-util/cem/cem-1.0.0.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	a, err := atom.ParseAtom(overlay, "dev-util/cem")
	if err != nil {
		t.Fatalf("ParseAtom: %v", err)
	}
	if a.Category != "dev-util" {
		t.Errorf("Category = %q, want \"dev-util\"", a.Category)
	}
	if a.PN != "cem" {
		t.Errorf("PN = %q, want \"cem\"", a.PN)
	}
}

func TestAtomParseAtom_Unknown(t *testing.T) {
	overlay := setupTestOverlay(t)
	_, err := atom.ParseAtom(overlay, "dev-util/does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown package")
	}
	if !errors.Is(err, atom.ErrPackageNotFound) && !errors.Is(err, atom.ErrCategoryNotFound) {
		t.Errorf("error = %v, want ErrPackageNotFound or ErrCategoryNotFound", err)
	}
}

func TestAtomFindLatest(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-libs/bar/bar-1.0.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	writeEbuild(t, overlay, "dev-libs/bar/bar-2.0.0-r1.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	writeEbuild(t, overlay, "dev-libs/bar/bar-9999.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	pe, err := atom.FindLatest(filepath.Join(overlay, "dev-libs", "bar"))
	if err != nil {
		t.Fatalf("FindLatest: %v", err)
	}
	if pe.PVR != "2.0.0-r1" {
		t.Errorf("PVR = %q, want \"2.0.0-r1\"", pe.PVR)
	}
	if pe.Live {
		t.Error("FindLatest should not return live ebuilds")
	}
}

func TestAtomFindLatest_LiveOnly(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-libs/foo/foo-9999.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI=""
LICENSE="MIT"
SLOT="0"
`)
	_, err := atom.FindLatest(filepath.Join(overlay, "dev-libs", "foo"))
	if !errors.Is(err, atom.ErrNoEbuilds) {
		t.Fatalf("expected ErrNoEbuilds for live-only, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Plan 17a — fakeTagResolver + deriveTagStatus tests
// ---------------------------------------------------------------------------

// fakeTagResolver implements provider.ResolverWithTags with canned answers.
type fakeTagResolver struct {
	res *provider.Resolution
	err error
	ver *version.Version
}

func (f *fakeTagResolver) Type() remoteid.RemoteIdType { return remoteid.TypeGitHub }
func (f *fakeTagResolver) Name() string                { return "fake" }
func (f *fakeTagResolver) Metadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{Name: "fake"}
}
func (f *fakeTagResolver) FetchLatestVersion(rid remoteid.RemoteId, pre bool) (*version.Version, error) {
	if f.ver == nil {
		return nil, errors.New("fake: no version")
	}
	return f.ver, nil
}
func (f *fakeTagResolver) FetchResolution(rid remoteid.RemoteId, pre bool) (*provider.Resolution, error) {
	return f.res, f.err
}
func (f *fakeTagResolver) IsPrerelease(v *version.Version) bool { return v.IsPrerelease() }

var _ provider.ResolverWithTags = (*fakeTagResolver)(nil)

// --- deriveTagStatus unit tests ---

func TestStatus_NewerTag(t *testing.T) {
	// D4a: dated tag is newer than the ebuild date.
	status, latest := deriveTagStatus("6.18.46_p20260824", "6.18.47", "20270101")
	if status != StatusNewerTag {
		t.Errorf("status = %v, want %v", status, StatusNewerTag)
	}
	if latest != "6.18.47" {
		t.Errorf("latest = %q, want %q", latest, "6.18.47")
	}
}

func TestStatus_TagDateEqual(t *testing.T) {
	status, latest := deriveTagStatus("6.18.46_p20260824", "v6.18.46", "20260824")
	if status != StatusUpToDate {
		t.Errorf("status = %v, want %v", status, StatusUpToDate)
	}
	if latest != "6.18.46" {
		t.Errorf("latest = %q, want %q", latest, "6.18.46")
	}
}

func TestStatus_TagDateOlder(t *testing.T) {
	status, latest := deriveTagStatus("6.18.46_p20260824", "6.18.45", "20250101")
	if status != StatusAhead {
		t.Errorf("status = %v, want %v", status, StatusAhead)
	}
	if latest != "6.18.45" {
		t.Errorf("latest = %q, want %q", latest, "6.18.45")
	}
}

func TestStatus_CheckTag(t *testing.T) {
	// D4b: non-version-like tag → CHECK-TAG.
	status, latest := deriveTagStatus("1.2.3", "nightly-build", "20260101")
	if status != StatusCheckTag {
		t.Errorf("status = %v, want %v", status, StatusCheckTag)
	}
	if latest != "nightly-build" {
		t.Errorf("latest = %q, want %q", latest, "nightly-build")
	}
}

func TestStatus_ByDate_NoDatesFallsBackToVersionCompare(t *testing.T) {
	status, latest := deriveTagStatus("1.2.3", "v1.3.0", "")
	if status != StatusOutdated {
		t.Errorf("status = %v, want %v", status, StatusOutdated)
	}
	if latest != "1.3.0" {
		t.Errorf("latest = %q, want %q", latest, "1.3.0")
	}
}

func TestStatus_StripsVPrefixOnlyBeforeDigit(t *testing.T) {
	// vV only stripped when followed by a digit.
	status, latest := deriveTagStatus("1.2.3", "v1.3.0", "")
	if status != StatusOutdated {
		t.Errorf("v-prefixed tag: status = %v, want %v", status, StatusOutdated)
	}
	if latest != "1.3.0" {
		t.Errorf("v-prefixed tag: latest = %q, want %q", latest, "1.3.0")
	}

	status2, latest2 := deriveTagStatus("1.2.3", "verbose-1.0", "")
	// "verbose-1.0" starts with a letter, so isVersionLike(currentPV) && isVersionLike(tagName) is
	// true (both match ^[0-9][0-9.]* after stripping nothing). currentPV is "1.2.3" (version-like),
	// tagName is "verbose-1.0" — wait, "verbose-1.0" starts with 'v', not a digit, so isVersionLike
	// returns false. Thus we fall through to StatusCheckTag.
	if status2 != StatusCheckTag {
		t.Errorf("non-v-prefixed tag: status = %v, want %v", status2, StatusCheckTag)
	}
	if latest2 != "verbose-1.0" {
		t.Errorf("non-v-prefixed tag: latest = %q, want %q", latest2, "verbose-1.0")
	}
}

// --- Integration tests through the call site ---

func TestResolverWithTags_DrivesTagStatus(t *testing.T) {
	// D4c: ByDate=true triggers deriveTagStatus.
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "sys-kernel/rpi/rpi-6.18.46_p20260824.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI="https://github.com/raspberrypi/linux/archive/x.tar.gz"
LICENSE="MIT"
SLOT="0"
`)

	reg := provider.NewProviderRegistry()
	reg.Register(&fakeTagResolver{
		res: &provider.Resolution{Tag: "6.18.47", TagDate: "20270101", ByDate: true},
	})

	results, err := RunWithTargets(RunOptions{OverlayDir: overlay, Registry: reg, Fresh: true})
	if err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if r.Status != StatusNewerTag {
		t.Errorf("status = %v, want %v", r.Status, StatusNewerTag)
	}
	if r.Latest != "6.18.47" {
		t.Errorf("latest = %q, want %q", r.Latest, "6.18.47")
	}
}

func TestResolverWithTags_FallsBackToVersionWhenNotByDate(t *testing.T) {
	// ByDate=false → use res.Version, then version-compare.
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "sys-kernel/rpi/rpi-6.18.46_p20260824.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI="https://github.com/raspberrypi/linux/archive/x.tar.gz"
LICENSE="MIT"
SLOT="0"
`)

	reg := provider.NewProviderRegistry()
	reg.Register(&fakeTagResolver{
		res: &provider.Resolution{Version: "7.0", ByDate: false},
		ver: nil, // should not be reached
	})

	results, err := RunWithTargets(RunOptions{OverlayDir: overlay, Registry: reg, Fresh: true})
	if err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if r.Status != StatusOutdated {
		t.Errorf("status = %v, want %v", r.Status, StatusOutdated)
	}
	if r.Latest != "7.0" {
		t.Errorf("latest = %q, want %q", r.Latest, "7.0")
	}
}

func TestPlainProviderStillUsesFetchLatestVersion(t *testing.T) {
	// A provider that does NOT implement ResolverWithTags should still work.
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "sys-kernel/rpi/rpi-6.18.46_p20260824.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI="https://github.com/raspberrypi/linux/archive/x.tar.gz"
LICENSE="MIT"
SLOT="0"
`)

	reg := provider.NewProviderRegistry()
	// mockProvider does NOT implement ResolverWithTags.
	reg.Register(&mockProvider{
		mockType:    remoteid.TypeGitHub,
		mockVersion: mustVersion("7.0"),
	})

	results, err := RunWithTargets(RunOptions{OverlayDir: overlay, Registry: reg, Fresh: true})
	if err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if r.Status != StatusOutdated {
		t.Errorf("status = %v, want %v", r.Status, StatusOutdated)
	}
	if r.Latest != "7.0" {
		t.Errorf("latest = %q, want %q", r.Latest, "7.0")
	}
}

func mustVersion(s string) *version.Version {
	v, _ := version.Parse(s)
	return v
}

// TestParseEbuildInfo_InheritAccumulatesAllLines verifies that ParseEbuildInfo
// accumulates inherit lines from multiple `inherit` declarations.
func TestParseEbuildInfo_InheritAccumulatesAllLines(t *testing.T) {
	raw := `EAPI=8
inherit pam python-single-r1
DESCRIPTION="x"
inherit systemd tmpfiles
`
	dir := t.TempDir()
	ebuildPath := filepath.Join(dir, "test-pkg", "my-pkg-1.0.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	info, err := ParseEbuildInfo(ebuildPath)
	if err != nil {
		t.Fatalf("ParseEbuildInfo(%q) error: %v", ebuildPath, err)
	}
	want := []string{"pam", "python-single-r1", "systemd", "tmpfiles"}
	if len(info.Inherit) != len(want) {
		t.Errorf("Inherit = %v, want %v", info.Inherit, want)
		return
	}
	for i, w := range want {
		if info.Inherit[i] != w {
			t.Errorf("Inherit[%d] = %q, want %q", i, info.Inherit[i], w)
		}
	}
}

// ---------------------------------------------------------------------------
// Plan 34 — TestResolveOne_UnknownWithSource
// When the provider is resolved and a RemoteId is found, but version fetch
// fails, the Source must still be populated — not left empty ("unknown").
// ---------------------------------------------------------------------------
func TestResolveOne_UnknownWithSource(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-util/fixjson/fixjson-1.1.2-r1.ebuild", `
EAPI=8
DESCRIPTION="Text formatter for JSON files"
SRC_URI="mirror://github/rhysd/fixjson/archive/v1.1.2.tar.gz"
LICENSE="MIT"
SLOT="0"
`)
	metaDir := filepath.Join(overlay, "dev-util", "fixjson")
	metaXML := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkgmetadata SYSTEM "https://www.gentoo.org/dtd/metadata.dtd">
<pkgmetadata>
<upstream>
<remote-id type="github">rhysd/fixjson</remote-id>
</upstream>
</pkgmetadata>
`
	if err := os.WriteFile(filepath.Join(metaDir, "metadata.xml"), []byte(metaXML), 0644); err != nil {
		t.Fatal(err)
	}

	// Registry with a fake provider that returns an error — simulates
	// GitHub API 404 (no releases on the repo).
	reg := provider.NewProviderRegistry()
	reg.Register(&fakeTagResolver{
		res: nil,
		err: fmt.Errorf("gh api releases/latest: 404 Not Found"),
	})

	results, err := RunWithTargets(RunOptions{OverlayDir: overlay, Registry: reg, Fresh: true})
	if err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Package != "dev-util/fixjson" {
		t.Errorf("Package = %q, want \"dev-util/fixjson\"", r.Package)
	}
	if r.Status != StatusUnknown {
		t.Errorf("Status = %v, want %v", r.Status, StatusUnknown)
	}
	if r.Source.Label != "GitHub:rhysd/fixjson" {
		t.Errorf("Source.Label = %q, want \"GitHub:rhysd/fixjson\"", r.Source.Label)
	}
	if r.Source.URL != "https://github.com/rhysd/fixjson" {
		t.Errorf("Source.URL = %q, want \"https://github.com/rhysd/fixjson\"", r.Source.URL)
	}
	if r.Error == "" {
		t.Error("Error must be non-empty")
	}
}

func TestResolveOne_InvalidStrategyReportsError(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "dev-util/badcfg/badcfg-1.0.ebuild", `EAPI=8
DESCRIPTION="Test"
SRC_URI="https://github.com/o/r/archive/x.tar.gz"
LICENSE="MIT"
SLOT="0"
`)
	cfgDir := filepath.Join(overlay, "dev-util", "badcfg", ".larry")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("upstream:\n  strategy: bogus\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := provider.NewProviderRegistry()
	results, err := RunWithTargets(RunOptions{OverlayDir: overlay, Registry: reg, Fresh: true})
	if err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if r.Status != StatusUnknown {
		t.Errorf("status = %v, want %v", r.Status, StatusUnknown)
	}
	if !strings.Contains(r.Error, "strategy") {
		t.Errorf("Error = %q, want it to mention strategy", r.Error)
	}
}

func TestResolveOne_ManualCheckSkipsProvider(t *testing.T) {
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "net-print/vendorprint/vendorprint-3.5.1.ebuild", `EAPI=8
DESCRIPTION="Vendor-only binary driver"
HOMEPAGE="https://vendor.example/downloads"
SRC_URI="https://vendor.example/files/vendorprint-3.5.1.rpm"
LICENSE="vendor-eula"
SLOT="0"
`)
	cfgDir := filepath.Join(overlay, "net-print", "vendorprint", ".larry")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("upstream:\n  check: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Empty registry: if the manual branch did not short-circuit, this would
	// produce a "no provider" error instead.
	reg := provider.NewProviderRegistry()
	results, err := RunWithTargets(RunOptions{OverlayDir: overlay, Registry: reg, Fresh: true})
	if err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if r.Status != StatusManual {
		t.Errorf("status = %v, want %v", r.Status, StatusManual)
	}
	if r.Error != "" {
		t.Errorf("Error = %q, want empty", r.Error)
	}
	if r.Source.URL != "https://vendor.example/downloads" {
		t.Errorf("Source.URL = %q, want HOMEPAGE", r.Source.URL)
	}
}

func progressOverlay(t *testing.T) string {
	t.Helper()
	overlay := setupTestOverlay(t)
	writeEbuild(t, overlay, "app-shells/alpha/alpha-1.0.0.ebuild", `EAPI=8
DESCRIPTION="Alpha"
SRC_URI="https://github.com/acme/alpha/archive/v1.0.0.tar.gz"
LICENSE="MIT"
SLOT="0"
`)
	writeEbuild(t, overlay, "app-shells/beta/beta-2.0.0.ebuild", `EAPI=8
DESCRIPTION="Beta"
SRC_URI="https://github.com/acme/beta/archive/v2.0.0.tar.gz"
LICENSE="MIT"
SLOT="0"
`)
	writeMetadata(t, overlay, "app-shells/alpha", "acme/alpha")
	writeMetadata(t, overlay, "app-shells/beta", "acme/beta")
	return overlay
}

func writeMetadata(t *testing.T, overlay, relDir, remote string) {
	t.Helper()
	dir := filepath.Join(overlay, relDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkgmetadata SYSTEM "https://www.gentoo.org/dtd/metadata.dtd">
<pkgmetadata>
<upstream>
<remote-id type="github">` + remote + `</remote-id>
</upstream>
</pkgmetadata>
`
	if err := os.WriteFile(filepath.Join(dir, "metadata.xml"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunWithOptions_Progress(t *testing.T) {
	overlay := progressOverlay(t)
	reg := provider.NewProviderRegistry()
	reg.Register(&mockProvider{mockType: remoteid.TypeGitHub, mockVersion: mustVersion("3.0.0")})

	var events []Progress
	results, err := RunWithTargets(RunOptions{
		OverlayDir: overlay,
		Registry:   reg,
		Progress:   func(p Progress) { events = append(events, p) },
	})
	if err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if len(events) == 0 {
		t.Fatal("Progress was never called")
	}
	last := events[len(events)-1]
	if last.Done != last.Total || last.Total != 2 {
		t.Errorf("final progress = %+v, want Done=Total=2", last)
	}
	prev := -1
	for _, ev := range events {
		if ev.Done < prev {
			t.Errorf("Done went backwards: %+v", events)
		}
		prev = ev.Done
	}
	firstPackage := ""
	for _, ev := range events {
		if ev.Package != "" {
			firstPackage = ev.Package
			break
		}
	}
	if firstPackage == "" {
		t.Errorf("no progress event has a package: %+v", events)
	}
}

func TestRunWithOptions_CacheDirHonored(t *testing.T) {
	overlay := progressOverlay(t)
	reg := provider.NewProviderRegistry()
	reg.Register(&mockProvider{mockType: remoteid.TypeGitHub, mockVersion: mustVersion("3.0.0")})
	cacheDir := t.TempDir()

	if _, err := RunWithTargets(RunOptions{
		OverlayDir: overlay,
		Registry:   reg,
		CacheDir:   cacheDir,
	}); err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}

	var files int
	err := filepath.WalkDir(cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Errorf("no cache files written under %s", cacheDir)
	}
}

func TestRunWithOptions_ContextCanceled(t *testing.T) {
	overlay := progressOverlay(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := RunWithTargets(RunOptions{
		Context:    ctx,
		OverlayDir: overlay,
		Registry:   provider.NewProviderRegistry(),
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestRunWithOptions_NilProgressSafe(t *testing.T) {
	overlay := progressOverlay(t)
	reg := provider.NewProviderRegistry()
	reg.Register(&mockProvider{mockType: remoteid.TypeGitHub, mockVersion: mustVersion("3.0.0")})
	if _, err := RunWithTargets(RunOptions{OverlayDir: overlay, Registry: reg, Progress: nil}); err != nil {
		t.Fatalf("RunWithTargets with nil Progress: %v", err)
	}
}

// --- Cache policy isolation and batch conflict tests ---

type batchRecorder struct {
	calls int
	reqs  []provider.BatchRequest
}

func (b *batchRecorder) Type() remoteid.RemoteIdType         { return remoteid.TypeGitHub }
func (b *batchRecorder) Metadata() provider.ProviderMetadata { return provider.ProviderMetadata{} }
func (b *batchRecorder) Name() string                        { return "batch-recorder" }
func (b *batchRecorder) IsPrerelease(*version.Version) bool  { return false }
func (b *batchRecorder) FetchLatestVersion(remoteid.RemoteId, bool) (*version.Version, error) {
	return nil, nil
}
func (b *batchRecorder) BatchResolve(_ context.Context, reqs []provider.BatchRequest, _ bool) (map[string]*provider.Resolution, error) {
	b.calls++
	b.reqs = append(b.reqs, reqs...)
	return map[string]*provider.Resolution{}, nil
}

// sharedRepoOverlay writes two packages that point at the same GitHub repo.
func sharedRepoOverlay(t *testing.T) string {
	t.Helper()
	overlay := setupTestOverlay(t)
	for _, pkg := range []string{"alpha", "beta"} {
		writeEbuild(t, overlay, "app-shells/"+pkg+"/"+pkg+"-1.0.0.ebuild", `EAPI=8
DESCRIPTION="`+pkg+`"
SRC_URI="https://github.com/acme/shared/archive/v1.0.0.tar.gz"
LICENSE="MIT"
SLOT="0"
`)
		writeMetadata(t, overlay, "app-shells/"+pkg, "acme/shared")
	}
	return overlay
}

func sharedRepoNodes(t *testing.T, overlay string) []checkNode {
	t.Helper()
	var nodes []checkNode
	for _, pkg := range []string{"alpha", "beta"} {
		path := filepath.Join(overlay, "app-shells", pkg, pkg+"-1.0.0.ebuild")
		eb, err := atom.ParseEbuildPath(path)
		if err != nil {
			t.Fatalf("ParseEbuildPath(%s): %v", path, err)
		}
		nodes = append(nodes, checkNode{Package: "app-shells/" + pkg, Ebuild: eb})
	}
	return nodes
}

func TestPrefetchBatches_SharedRepoBatched(t *testing.T) {
	overlay := sharedRepoOverlay(t)
	rec := &batchRecorder{}
	reg := provider.NewProviderRegistry()
	reg.Register(rec)

	prefetchBatches(context.Background(), sharedRepoNodes(t, overlay), reg, false)

	if rec.calls != 1 {
		t.Fatalf("BatchResolve calls = %d, want 1", rec.calls)
	}
	if len(rec.reqs) != 2 {
		t.Errorf("batched %d requests, want 2", len(rec.reqs))
	}
}

func TestPrefetchBatches_DivergentRulesNotBatched(t *testing.T) {
	overlay := sharedRepoOverlay(t)
	cfgDir := filepath.Join(overlay, "app-shells", "beta", ".larry")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"),
		[]byte("upstream:\n  strategy: tags\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := &batchRecorder{}
	reg := provider.NewProviderRegistry()
	reg.Register(rec)

	prefetchBatches(context.Background(), sharedRepoNodes(t, overlay), reg, false)

	if rec.calls != 0 {
		t.Errorf("BatchResolve calls = %d, want 0 for divergent per-package rules", rec.calls)
	}
}

func TestCachePolicyKey_SeparatesRules(t *testing.T) {
	plain := cachePolicyKey("cat", "pkg", "github", "1.0", nil, false)
	withTags := cachePolicyKey("cat", "pkg", "github", "1.0",
		&larryconfig.ResolveOptions{Strategy: "tags", Check: true}, false)
	prerelease := cachePolicyKey("cat", "pkg", "github", "1.0", nil, true)

	if plain == withTags {
		t.Error("cache keys collide for different upstream strategies")
	}
	if plain == prerelease {
		t.Error("cache keys collide for different prerelease settings")
	}
	if plain != cachePolicyKey("cat", "pkg", "github", "1.0", nil, false) {
		t.Error("cache key is not stable for identical inputs")
	}
}
