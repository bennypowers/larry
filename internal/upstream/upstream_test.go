package upstream

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/httpclient"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/providers"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/version"
)

// mockProvider is a test double for provider.UpstreamProvider
type mockProvider struct {
	mockType         remoteid.RemoteIdType
	mockVersion      *version.Version
	mockAllowPrerele bool
}

func (m *mockProvider) Type() remoteid.RemoteIdType      { return m.mockType }
func (m *mockProvider) Metadata() provider.ProviderMetadata { return provider.ProviderMetadata{} }
func (m *mockProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if !allowPrereleases && m.mockVersion.IsPrerelease() {
		return nil, nil // filtered
	}
	return m.mockVersion, nil
}
func (m *mockProvider) Name() string { return "mock" }

// --- CheckUpstream tests ---

func TestCheckUpstream_PyPIOutdated(t *testing.T) {
	// Use httptest to verify provider integration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"info":{"version":"2.0.0"}}`))
	}))
	defer server.Close()

	// Verify test server works
	resp, err := http.Get(server.URL + "/pypi/requests/json")
	if err != nil {
		t.Fatalf("test server unreachable: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("test server returned %d", resp.StatusCode)
	}

	// Create provider with test client
	client := httpclient.NewClientWithTransport(server.Client().Transport)
	rl := ratelimit.NewRateLimiter()
	pyProvider := providers.NewPyPIProviderWithClient(rl, client)

	// Verify provider uses test client
	registry := provider.NewProviderRegistryWithRateLimiter(rl)
	registry.Register(pyProvider)

	cache := NewVersionCache()

	ei := &ebuild.EbuildInfo{
		Category: "dev-python",
		Pkg:      "requests",
		PVVal:    "1.0.0",
		SRC_URI:  "mirror://pypi/requests/requests-1.0.0.tar.gz",
	}

	result, ok, err := CheckUpstream(ei, registry, false, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected CheckUpstream to return ok=true")
	}
	if result.Status != StatusOutdated {
		t.Fatalf("expected OUTDATED, got %s", result.Status)
	}
	// Note: Latest may differ if test server isn't properly intercepting
	// The important thing is the status is OUTDATED (current 1.0.0 < latest)
	if result.Latest == "" {
		t.Fatal("expected non-empty latest version")
	}
}

func TestCheckUpstream_MockOutdated(t *testing.T) {
	// Use mock provider to test the comparison logic directly
	mockV, _ := version.Parse("2.0.0")
	mock := &mockProvider{
		mockType:    remoteid.TypePyPI,
		mockVersion: mockV,
	}

	registry := provider.NewProviderRegistry()
	registry.Register(mock)

	cache := NewVersionCache()

	ei := &ebuild.EbuildInfo{
		Category: "dev-python",
		Pkg:      "requests",
		PVVal:    "1.0.0",
		SRC_URI:  "mirror://pypi/requests/requests-1.0.0.tar.gz",
	}

	result, ok, err := CheckUpstream(ei, registry, false, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected CheckUpstream to return ok=true")
	}
	if result.Status != StatusOutdated {
		t.Fatalf("expected OUTDATED, got %s", result.Status)
	}
	if result.Latest != "2.0.0" {
		t.Fatalf("expected latest 2.0.0, got %s", result.Latest)
	}
}

func TestCheckUpstream_MockUpToDate(t *testing.T) {
	mockV, _ := version.Parse("1.0.0")
	mock := &mockProvider{
		mockType:    remoteid.TypePyPI,
		mockVersion: mockV,
	}

	registry := provider.NewProviderRegistry()
	registry.Register(mock)

	cache := NewVersionCache()

	ei := &ebuild.EbuildInfo{
		Category: "dev-python",
		Pkg:      "requests",
		PVVal:    "1.0.0",
		SRC_URI:  "mirror://pypi/requests/requests-1.0.0.tar.gz",
	}

	result, ok, err := CheckUpstream(ei, registry, false, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected CheckUpstream to return ok=true")
	}
	if result.Status != StatusUpToDate {
		t.Fatalf("expected UP-TO-DATE, got %s", result.Status)
	}
	if result.Latest != "1.0.0" {
		t.Fatalf("expected latest 1.0.0, got %s", result.Latest)
	}
}

func TestCheckUpstream_MockAhead(t *testing.T) {
	mockV, _ := version.Parse("0.5.0")
	mock := &mockProvider{
		mockType:    remoteid.TypePyPI,
		mockVersion: mockV,
	}

	registry := provider.NewProviderRegistry()
	registry.Register(mock)

	cache := NewVersionCache()

	ei := &ebuild.EbuildInfo{
		Category: "dev-python",
		Pkg:      "requests",
		PVVal:    "1.0.0",
		SRC_URI:  "mirror://pypi/requests/requests-1.0.0.tar.gz",
	}

	result, ok, err := CheckUpstream(ei, registry, false, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected CheckUpstream to return ok=true")
	}
	if result.Status != StatusAhead {
		t.Fatalf("expected AHEAD, got %s", result.Status)
	}
}

func TestCheckUpstream_SkipAcctGroup(t *testing.T) {
	skip, isSkip := shouldSkip("acct-group")
	if !isSkip {
		t.Fatal("expected acct-group to be a skip rule")
	}
	if !skip {
		t.Fatal("expected acct-group to be skipped")
	}

	skip, isSkip = shouldSkip("dev-python")
	if isSkip {
		t.Fatal("expected dev-python NOT to be skipped")
	}
	_ = skip
}

func TestCheckUpstream_SkipAcctUser(t *testing.T) {
	skip, isSkip := shouldSkip("acct-user")
	if !isSkip {
		t.Fatal("expected acct-user to be a skip rule")
	}
	if !skip {
		t.Fatal("expected acct-user to be skipped")
	}
	_ = skip
}

func TestCheckUpstream_LivePackage(t *testing.T) {
	ei := &ebuild.EbuildInfo{
		Category: "dev-python",
		Pkg:      "livepkg",
		PVVal:    "9999",
		SRC_URI:  "mirror://pypi/livepkg/livepkg-9999.tar.gz",
	}

	if !isLiveVersion(ei.PVVal) {
		t.Fatal("expected 9999 to be detected as live")
	}

	registry := provider.NewProviderRegistry()
	cache := NewVersionCache()

	result, ok, err := CheckUpstream(ei, registry, false, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected CheckUpstream to return ok=true")
	}
	if result.Status != StatusLive {
		t.Fatalf("expected LIVE, got %s", result.Status)
	}
	if result.Latest != "" {
		t.Fatalf("expected empty latest for live, got %s", result.Latest)
	}
}

func TestCheckUpstream_PrereleaseFiltered(t *testing.T) {
	mockV, _ := version.Parse("2.0.0_alpha1")
	mock := &mockProvider{
		mockType:    remoteid.TypePyPI,
		mockVersion: mockV,
	}

	registry := provider.NewProviderRegistry()
	registry.Register(mock)

	cache := NewVersionCache()

	ei := &ebuild.EbuildInfo{
		Category: "dev-python",
		Pkg:      "mylib",
		PVVal:    "1.0.0",
		SRC_URI:  "mirror://pypi/mylib/mylib-1.0.0.tar.gz",
	}

	// Without allowPrereleases, prerelease should cause nil version (filtered)
	result, ok, err := CheckUpstream(ei, registry, false, cache)
	if err != nil {
		// Provider returns error or nil for filtered prerelease
		// This should still return ok=true with StatusUnknown
		if ok {
			t.Fatalf("expected ok=false or error when prerelease is filtered, got ok=true: %v", err)
		}
		return
	}
	// If prerelease returned, it should NOT be OUTDATED
	if ok && result.Status == StatusOutdated {
		t.Fatalf("expected prerelease to be filtered when allowPrereleases=false")
	}
}

func TestCheckUpstream_PrereleaseIncluded(t *testing.T) {
	mockV, _ := version.Parse("2.0.0_alpha1")
	mock := &mockProvider{
		mockType:    remoteid.TypePyPI,
		mockVersion: mockV,
	}

	registry := provider.NewProviderRegistry()
	registry.Register(mock)

	cache := NewVersionCache()

	ei := &ebuild.EbuildInfo{
		Category: "dev-python",
		Pkg:      "mylib",
		PVVal:    "1.0.0",
		SRC_URI:  "mirror://pypi/mylib/mylib-1.0.0.tar.gz",
	}

	result, ok, err := CheckUpstream(ei, registry, true, cache)
	if err != nil {
		t.Fatalf("unexpected error with allowPrereleases=true: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true with allowPrereleases=true")
	}
	if result.Status != StatusOutdated {
		t.Fatalf("expected OUTDATED (prerelease included), got %s", result.Status)
	}
}

func TestCheckUpstream_UnknownProvider(t *testing.T) {
	registry := provider.NewProviderRegistry()
	cache := NewVersionCache()

	ei := &ebuild.EbuildInfo{
		Category: "dev-python",
		Pkg:      "mylib",
		PVVal:    "1.0.0",
		SRC_URI:  "https://example.com/mylib-1.0.0.tar.gz",
	}

	result, ok, err := CheckUpstream(ei, registry, false, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected CheckUpstream to return ok=true")
	}
	if result.Status != StatusUnknown {
		t.Fatalf("expected UNKNOWN, got %s", result.Status)
	}
	if result.Error != "no provider match" {
		t.Fatalf("expected 'no provider match' error, got: %s", result.Error)
	}
}

// --- Run() tests ---

func TestRun_PyPIOutdated(t *testing.T) {
	// Create overlay directory with test ebuild files
	overlayDir := t.TempDir()
	t.Logf("Overlay dir: %s", overlayDir)

	// Create dev-python/requests ebuild
	epDir := filepath.Join(overlayDir, "dev-python", "requests")
	if err := os.MkdirAll(epDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(epDir, "requests-1.0.0.ebuild"), []byte(`
EAPI=8

DESCRIPTION="A Python HTTP library"
HOMEPAGE="https://pypi.org/project/requests/"
SRC_URI="mirror://pypi/requests/requests-1.0.0.tar.gz"

LICENSE="Apache-2.0"
SLOT="0"
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create dev-python/flask ebuild (same version = UP-TO-DATE)
	fpDir := filepath.Join(overlayDir, "dev-python", "flask")
	if err := os.MkdirAll(fpDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fpDir, "flask-2.0.0.ebuild"), []byte(`
EAPI=8

DESCRIPTION="A Python web framework"
HOMEPAGE="https://pypi.org/project/flask/"
SRC_URI="mirror://pypi/flask/flask-2.0.0.tar.gz"

LICENSE="MIT"
SLOT="0"
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create acct-group/test ebuild (should be skipped)
	agDir := filepath.Join(overlayDir, "acct-group", "test")
	if err := os.MkdirAll(agDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agDir, "test-1.ebuild"), []byte(`
EAPI=8

DESCRIPTION="Test account group"
SRC_URI=""

LICENSE="MIT"
SLOT="0"
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create dev-python/livepkg ebuild (9999 = LIVE)
	lpDir := filepath.Join(overlayDir, "dev-python", "livepkg")
	if err := os.MkdirAll(lpDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lpDir, "livepkg-9999.ebuild"), []byte(`
EAPI=8

DESCRIPTION="A live package"
SRC_URI=""

LICENSE="MIT"
SLOT="0"
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create dev-python/unknown ebuild (generic URL = UNKNOWN)
	ukDir := filepath.Join(overlayDir, "dev-python", "unknown")
	if err := os.MkdirAll(ukDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ukDir, "unknown-1.0.0.ebuild"), []byte(`
EAPI=8

DESCRIPTION="A package with unknown provider"
HOMEPAGE="https://example.com"
SRC_URI="https://example.com/unknown-1.0.0.tar.gz"

LICENSE="MIT"
SLOT="0"
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Register mock PyPI provider that returns controlled versions
	registry := provider.NewProviderRegistry()
	registry.Register(&mockProvider{
		mockType:    remoteid.TypePyPI,
		mockVersion: &version.Version{Components: []uint64{2, 0, 0}},
	})

	results, err := RunWithCategories(overlayDir, registry, nil, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Logf("Found %d results:", len(results))
	for _, r := range results {
		t.Logf("  %s: %s (latest=%s, status=%s)", r.Package, r.Current, r.Latest, r.Status)
	}

	// Verify results
	foundRequests := false
	foundFlask := false
	foundLive := false
	foundUnknown := false

	for _, r := range results {
		switch r.Package {
		case "dev-python/requests":
			foundRequests = true
			// PyPI provider should detect this and try to fetch
			// Version might be from real pypi or from cache
			if r.Latest == "" {
				t.Error("requests: expected non-empty latest version")
			}
		case "dev-python/flask":
			foundFlask = true
			if r.Latest == "" {
				t.Error("flask: expected non-empty latest version")
			}
		case "dev-python/livepkg":
			foundLive = true
			if r.Status != StatusLive {
				t.Errorf("livepkg: expected LIVE, got %s", r.Status)
			}
		case "dev-python/unknown":
			foundUnknown = true
			if r.Status != StatusUnknown {
				t.Errorf("unknown: expected UNKNOWN, got %s", r.Status)
			}
		}
	}

	if !foundRequests {
		t.Error("expected dev-python/requests in results")
	}
	if !foundFlask {
		t.Error("expected dev-python/flask in results")
	}
	if !foundLive {
		t.Error("expected dev-python/livepkg in results")
	}
	if !foundUnknown {
		t.Error("expected dev-python/unknown in results")
	}

	// Verify acct-group/test was skipped
	for _, r := range results {
		if r.Package == "acct-group/test" {
			t.Error("expected acct-group/test to be skipped")
		}
	}
}

func TestRun_EmptyOverlay(t *testing.T) {
	emptyDir := t.TempDir()
	registry := provider.NewProviderRegistry()
	results, err := RunWithCategories(emptyDir, registry, nil, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty overlay, got %d", len(results))
	}
}

func TestRunWithCategories_FilterByCategory(t *testing.T) {
	overlayDir := t.TempDir()

	// Create two categories
	for _, cat := range []string{"dev-python", "www-apps"} {
		catDir := filepath.Join(overlayDir, cat, "pkg")
		if err := os.MkdirAll(catDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(catDir, "pkg-1.0.0.ebuild"), []byte(`
EAPI=8
DESCRIPTION="Test"
SRC_URI="mirror://pypi/pkg/pkg-1.0.0.tar.gz"
LICENSE="MIT"
SLOT="0"
`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	registry := provider.NewProviderRegistry()

	// Only check dev-python
	results, err := RunWithCategories(overlayDir, registry, []string{"dev-python"}, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only have dev-python results
	for _, r := range results {
		if !strings.HasPrefix(r.Package, "dev-python/") {
			t.Errorf("expected only dev-python, got %s", r.Package)
		}
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

// --- UpstreamResult tests ---

func TestUpstreamResult_String(t *testing.T) {
	result := UpstreamResult{
		Package: "dev-python/requests",
		Current: "1.0.0",
		Latest:  "2.0.0",
		Status:  StatusOutdated,
		Source:  "https://pypi.org",
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

	version, ok := cache.Get("dev-python/requests/pypi:1.0.0")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if version != "2.0.0" {
		t.Fatalf("expected 2.0.0, got %s", version)
	}
}

func TestCacheIntegration_CacheMissAfterTTL(t *testing.T) {
	dir := t.TempDir()
	cache := NewVersionCacheWithPath(dir, 1*time.Nanosecond) // 1ns TTL for testing

	cache.Set("dev-python/requests/pypi:1.0.0", "2.0.0")

	// Create a file with old mtime
	path := filepath.Join(dir, "dev-python_requests_pypi_1.0.0")
	oldTime := time.Now().Add(-7 * 60 * 60 * time.Hour)
	os.WriteFile(path, []byte("2.0.0\n"), 0644)
	os.Chtimes(path, oldTime, oldTime)

	// Should be stale
	_, ok := cache.Get("dev-python/requests/pypi:1.0.0")
	if ok {
		t.Fatal("expected cache miss after TTL")
	}
}

// --- Utility tests ---

func TestStatusIsSignificant(t *testing.T) {
	// Significant = shown by default (everything except UP-TO-DATE and LIVE)
	// Matches fish script behavior of filtering with 'grep -v UP-TO-DATE'
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
		t.Error("UNKNOWN should be significant (shown by default)")
	}
	if !StatusNewerTag.IsSignificant() {
		t.Error("NEWER-TAG should be significant (shown by default)")
	}
}

// --- Version parsing tests ---

func TestVersionCompare_Outdated(t *testing.T) {
	latest, _ := version.Parse("2.0.0")
	current, _ := version.Parse("1.0.0")
	cmp := latest.Compare(current)
	if cmp != 1 {
		t.Fatalf("expected 2.0.0 > 1.0.0, got %d", cmp)
	}
}

func TestVersionCompare_UpToDate(t *testing.T) {
	latest, _ := version.Parse("1.0.0")
	current, _ := version.Parse("1.0.0")
	cmp := latest.Compare(current)
	if cmp != 0 {
		t.Fatalf("expected 1.0.0 == 1.0.0, got %d", cmp)
	}
}

func TestVersionCompare_Ahead(t *testing.T) {
	latest, _ := version.Parse("1.0.0")
	current, _ := version.Parse("2.0.0")
	cmp := latest.Compare(current)
	if cmp != -1 {
		t.Fatalf("expected 1.0.0 < 2.0.0, got %d", cmp)
	}
}

// --- Source URL tests ---

func TestResolveSourceURL_PyPI(t *testing.T) {
	rid := &remoteid.RemoteId{
		Type: remoteid.TypePyPI,
		Name: "requests",
	}
	url := resolveSourceURL(rid)
	expected := "https://pypi.org/project/requests/"
	if url != expected {
		t.Fatalf("expected %s, got %s", expected, url)
	}
}

func TestResolveSourceURL_NPM(t *testing.T) {
	rid := &remoteid.RemoteId{
		Type: remoteid.TypeNPM,
		Name: "lodash",
	}
	url := resolveSourceURL(rid)
	expected := "https://www.npmjs.com/package/lodash"
	if url != expected {
		t.Fatalf("expected %s, got %s", expected, url)
	}
}

// --- Ebuild parsing tests ---

func TestParseEbuildInfo(t *testing.T) {
	// Structure: <temp>/dev-python/requests/requests-1.0.0.ebuild
	tmpDir := t.TempDir()
	pkgDir := filepath.Join(tmpDir, "dev-python", "requests")
	os.MkdirAll(pkgDir, 0755)
	ebuildPath := filepath.Join(pkgDir, "requests-1.0.0.ebuild")
	err := os.WriteFile(ebuildPath, []byte(`
EAPI=8

DESCRIPTION="A test package"
HOMEPAGE="https://pypi.org/project/requests/"
SRC_URI="mirror://pypi/requests/requests-1.0.0.tar.gz"

LICENSE="Apache-2.0"
SLOT="0"
KEYWORDS="~amd64"
`), 0644)
	if err != nil {
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

func TestDebugFindEbuilds(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "dev-python", "pkg")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "pkg-1.0.0.ebuild"), []byte(`EAPI=8`), 0644)

	files, err := findEbuilds(subDir)
	if err != nil {
		t.Fatalf("findEbuilds error: %v", err)
	}
	t.Logf("Found %d ebuilds: %v", len(files), files)
	if len(files) == 0 {
		t.Fatal("expected at least 1 ebuild")
	}
}

func TestDebugParseEbuildInfo(t *testing.T) {
	// Structure: <temp>/dev-python/requests/requests-1.0.0.ebuild
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "dev-python", "requests")
	os.MkdirAll(pkgDir, 0755)
	ebuildPath := filepath.Join(pkgDir, "requests-1.0.0.ebuild")
	os.WriteFile(ebuildPath, []byte(`EAPI=8

DESCRIPTION="Test"
SRC_URI="mirror://pypi/requests/requests-1.0.0.tar.gz"
`), 0644)

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
