package github

import (
	"encoding/json"
	"os/exec"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient()
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.runner == nil {
		t.Fatal("Client.runner is nil")
	}
}

func TestNewClientWithRunner(t *testing.T) {
	stub := NewStubRunner()
	c := NewClientWithRunner(stub)
	if c.runner != stub {
		t.Fatal("Client.runner not set to stub")
	}
}

func TestLatestRelease(t *testing.T) {
	stub := NewStubRunner()
	releaseOutput := `{"tag_name":"v1.2.3","prerelease":false,"draft":false}`
	stub.Outputs["gh api repos/owner/repo/releases/latest"] = []byte(releaseOutput)

	c := NewClientWithRunner(stub)
	tag, prerelease, draft, err := c.LatestRelease("owner", "repo")
	if err != nil {
		t.Fatalf("LatestRelease(): %v", err)
	}
	if tag != "v1.2.3" {
		t.Errorf("tag = %q, want v1.2.3", tag)
	}
	if prerelease {
		t.Error("prerelease should be false")
	}
	if draft {
		t.Error("draft should be false")
	}
}

func TestLatestRelease_ParsesPrereleaseAndDraft(t *testing.T) {
	// Plan 17b test: assert the three returned values for prerelease+draft.
	stub := NewStubRunner()
	releaseOutput := `{"tag_name":"v1","prerelease":true,"draft":true}`
	stub.Outputs["gh api repos/o/r/releases/latest"] = []byte(releaseOutput)

	c := NewClientWithRunner(stub)
	tag, prerelease, draft, err := c.LatestRelease("o", "r")
	if err != nil {
		t.Fatalf("LatestRelease(): %v", err)
	}
	if tag != "v1" {
		t.Errorf("tag = %q, want v1", tag)
	}
	if !prerelease {
		t.Error("prerelease should be true")
	}
	if !draft {
		t.Error("draft should be true")
	}
}

func TestLatestRelease_NoOutput(t *testing.T) {
	stub := NewStubRunner()
	stub.Outputs["gh api repos/owner/repo/releases/latest"] = []byte(`{}`)

	c := NewClientWithRunner(stub)
	_, _, _, err := c.LatestRelease("owner", "repo")
	if err == nil {
		t.Error("LatestRelease() should return error for empty tag_name")
	}
}

func TestLatestRelease_GHError(t *testing.T) {
	stub := NewStubRunner()
	stub.Errors["gh api repos/owner/repo/releases/latest"] = &exec.ExitError{}

	c := NewClientWithRunner(stub)
	_, _, _, err := c.LatestRelease("owner", "repo")
	if err == nil {
		t.Error("LatestRelease() should return error when gh fails")
	}
}

func TestLatestRelease_MalformedJSON(t *testing.T) {
	stub := NewStubRunner()
	stub.Outputs["gh api repos/owner/repo/releases/latest"] = []byte("not valid json")

	c := NewClientWithRunner(stub)
	_, _, _, err := c.LatestRelease("owner", "repo")
	if err == nil {
		t.Error("LatestRelease() should return error for malformed JSON")
	}
}

func TestPrereleaseReleaseTags_ParsesFlags(t *testing.T) {
	stub := NewStubRunner()
	key := "gh release list -R o/r --limit 100 --json tagName,isPrerelease"
	stub.Outputs[key] = []byte(`[
		{"tagName":"1.0.0","isPrerelease":false},
		{"tagName":"2.0.0-rc1","isPrerelease":true},
		{"tagName":"9.9.9","isPrerelease":true}
	]`)

	c := NewClientWithRunner(stub)
	got, err := c.PrereleaseReleaseTags("o", "r")
	if err != nil {
		t.Fatalf("PrereleaseReleaseTags(): %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(set) = %d, want 2 (%v)", len(got), got)
	}
	if !got["2.0.0-rc1"] || !got["9.9.9"] {
		t.Errorf("set = %v, want 2.0.0-rc1 and 9.9.9", got)
	}
	if got["1.0.0"] {
		t.Error("1.0.0 should not be in the prerelease set")
	}
}

func TestPrereleaseReleaseTags_EmptyAndError(t *testing.T) {
	stub := NewStubRunner()
	key := "gh release list -R o/r --limit 100 --json tagName,isPrerelease"
	stub.Outputs[key] = []byte(`[]`)

	c := NewClientWithRunner(stub)
	got, err := c.PrereleaseReleaseTags("o", "r")
	if err != nil {
		t.Fatalf("PrereleaseReleaseTags() on empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("set = %v, want empty", got)
	}

	stubErr := NewStubRunner()
	stubErr.Errors[key] = &exec.ExitError{}
	if _, err := NewClientWithRunner(stubErr).PrereleaseReleaseTags("o", "r"); err == nil {
		t.Error("PrereleaseReleaseTags() should error when gh fails")
	}
}

func TestLatestReleaseTag_ExcludesPrereleases(t *testing.T) {
	// Plan 17b test: assert --exclude-pre-releases is in argv when allowPrereleases == false.
	stub := NewStubRunner()
	stub.Outputs["gh release list -R o/r --limit 100 --exclude-pre-releases --json tagName"] = []byte(`[{"tagName":"1.0.0"}]`)

	c := NewClientWithRunner(stub)
	tag, err := c.LatestReleaseTag("o", "r", false)
	if err != nil {
		t.Fatalf("LatestReleaseTag(): %v", err)
	}
	if tag != "1.0.0" {
		t.Errorf("tag = %q, want 1.0.0", tag)
	}
}

func TestLatestReleaseTag_AllowsPrereleases(t *testing.T) {
	stub := NewStubRunner()
	stub.Outputs["gh release list -R o/r --limit 100 --json tagName"] = []byte(`[{"tagName":"2.0.0-rc1"}]`)

	c := NewClientWithRunner(stub)
	tag, err := c.LatestReleaseTag("o", "r", true)
	if err != nil {
		t.Fatalf("LatestReleaseTag(): %v", err)
	}
	if tag != "2.0.0-rc1" {
		t.Errorf("tag = %q, want 2.0.0-rc1", tag)
	}
}

func TestLatestReleaseTag_EmptyOutput(t *testing.T) {
	stub := NewStubRunner()
	stub.Outputs["gh release list -R o/r --limit 100 --exclude-pre-releases --json tagName"] = []byte(`[]`)

	c := NewClientWithRunner(stub)
	_, err := c.LatestReleaseTag("o", "r", false)
	if err == nil {
		t.Error("LatestReleaseTag() should return error for empty output")
	}
}

func TestEnsureRelease_Exists(t *testing.T) {
	stub := NewStubRunner()
	// gh release view v1.0.0 --repo owner/repo succeeds
	stub.Outputs["gh release view v1.0.0 --repo owner/repo"] = []byte(`{"tagName":"v1.0.0"}`)

	c := NewClientWithRunner(stub)
	err := c.EnsureRelease("owner", "repo", "v1.0.0")
	if err != nil {
		t.Fatalf("EnsureRelease(): %v", err)
	}
}

func TestEnsureRelease_Create(t *testing.T) {
	stub := NewStubRunner()
	// gh release view fails (not found)
	stub.Errors["gh release view v2.0.0 --repo owner/repo"] = &exec.ExitError{}
	// gh release create v2.0.0 --title v2.0.0 --generate-notes --repo owner/repo succeeds

	c := NewClientWithRunner(stub)
	err := c.EnsureRelease("owner", "repo", "v2.0.0")
	if err != nil {
		t.Fatalf("EnsureRelease(): %v", err)
	}
}

func TestEnsureRelease_ExistsDifferentOwner(t *testing.T) {
	stub := NewStubRunner()
	stub.Outputs["gh release view v3.0.0 --repo foo/bar"] = []byte(`{"tagName":"v3.0.0"}`)

	c := NewClientWithRunner(stub)
	err := c.EnsureRelease("foo", "bar", "v3.0.0")
	if err != nil {
		t.Fatalf("EnsureRelease(): %v", err)
	}
}

func TestEnsureRelease_CreateDifferentOwner(t *testing.T) {
	stub := NewStubRunner()
	stub.Errors["gh release view v3.0.0 --repo foo/bar"] = &exec.ExitError{}

	c := NewClientWithRunner(stub)
	err := c.EnsureRelease("foo", "bar", "v3.0.0")
	if err != nil {
		t.Fatalf("EnsureRelease(): %v", err)
	}
}

func TestUploadAsset(t *testing.T) {
	stub := NewStubRunner()

	c := NewClientWithRunner(stub)
	err := c.UploadAsset("owner", "repo", "v1.0.0", "/path/to/file.tar.gz", "file.tar.gz", false)
	if err != nil {
		t.Fatalf("UploadAsset(): %v", err)
	}
}

func TestUploadAsset_Clobber(t *testing.T) {
	stub := NewStubRunner()

	c := NewClientWithRunner(stub)
	err := c.UploadAsset("owner", "repo", "v1.0.0", "/path/to/file.tar.gz", "file.tar.gz", true)
	if err != nil {
		t.Fatalf("UploadAsset(): %v", err)
	}
}

func TestDownloadSourceTarball(t *testing.T) {
	stub := NewStubRunner()

	c := NewClientWithRunner(stub)
	_, err := c.DownloadSourceTarball("owner", "repo", "v1.0.0", "/tmp")
	if err != nil {
		t.Fatalf("DownloadSourceTarball(): %v", err)
	}
}

func TestDownloadSourceTarball_NoDestDir(t *testing.T) {
	stub := NewStubRunner()

	c := NewClientWithRunner(stub)
	path, err := c.DownloadSourceTarball("owner", "repo", "v1.0.0", "")
	if err != nil {
		t.Fatalf("DownloadSourceTarball(): %v", err)
	}
	if path != "" {
		t.Errorf("path = %q, want empty string", path)
	}
}

func TestListReleaseAssets(t *testing.T) {
	stub := NewStubRunner()

	assets := []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}{
		{Name: "pkg-1.0.0.tar.gz", URL: "https://github.com/owner/repo/releases/download/v1.0.0/pkg-1.0.0.tar.gz"},
		{Name: "pkg-1.0.0-src.zip", URL: "https://github.com/owner/repo/releases/download/v1.0.0/pkg-1.0.0-src.zip"},
	}
	outJSON, _ := json.Marshal(map[string]interface{}{"assets": assets})
	stub.Outputs["gh release view v1.0.0 --json assets"] = outJSON

	c := NewClientWithRunner(stub)
	result, err := c.ListReleaseAssets("owner", "repo", "v1.0.0")
	if err != nil {
		t.Fatalf("ListReleaseAssets(): %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("ListReleaseAssets() returned %d assets, want 2", len(result))
	}
	if result[0].Name != "pkg-1.0.0.tar.gz" {
		t.Errorf("First asset name = %q, want pkg-1.0.0.tar.gz", result[0].Name)
	}
	if result[1].Name != "pkg-1.0.0-src.zip" {
		t.Errorf("Second asset name = %q, want pkg-1.0.0-src.zip", result[1].Name)
	}
}

func TestListReleaseAssets_ChecksumMatch(t *testing.T) {
	// Test case: asset exists with same checksum → skip
	stub := NewStubRunner()

	assets := []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}{
		{Name: "pkg-1.0.0.tar.gz", URL: "https://github.com/owner/repo/releases/download/v1.0.0/pkg-1.0.0.tar.gz"},
	}
	outJSON, _ := json.Marshal(map[string]interface{}{"assets": assets})
	stub.Outputs["gh release view v1.0.0 --json assets"] = outJSON

	c := NewClientWithRunner(stub)
	result, err := c.ListReleaseAssets("owner", "repo", "v1.0.0")
	if err != nil {
		t.Fatalf("ListReleaseAssets(): %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("ListReleaseAssets() returned %d assets, want 1", len(result))
	}
}

func TestListReleaseAssets_ChecksumMismatch(t *testing.T) {
	// Test case: asset exists with different checksum → clobber
	stub := NewStubRunner()

	assets := []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}{
		{Name: "pkg-1.0.0.tar.gz", URL: "https://github.com/owner/repo/releases/download/v1.0.0/pkg-1.0.0.tar.gz"},
	}
	outJSON, _ := json.Marshal(map[string]interface{}{"assets": assets})
	stub.Outputs["gh release view v1.0.0 --json assets"] = outJSON

	c := NewClientWithRunner(stub)
	_, err := c.ListReleaseAssets("owner", "repo", "v1.0.0")
	if err != nil {
		t.Fatalf("ListReleaseAssets(): %v", err)
	}
	// Upload with --clobber
	err = c.UploadAsset("owner", "repo", "v1.0.0", "/path/file.tar.gz", "file.tar.gz", true)
	if err != nil {
		t.Fatalf("UploadAsset(): %v", err)
	}
}

func TestListReleaseAssets_NewAsset(t *testing.T) {
	// Test case: no existing asset → upload
	stub := NewStubRunner()
	stub.Outputs["gh release view v1.0.0 --json assets"] = []byte(`{"assets":[]}`)

	c := NewClientWithRunner(stub)
	result, err := c.ListReleaseAssets("owner", "repo", "v1.0.0")
	if err != nil {
		t.Fatalf("ListReleaseAssets(): %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("ListReleaseAssets() returned %d assets, want 0", len(result))
	}

	// Upload new asset
	err = c.UploadAsset("owner", "repo", "v1.0.0", "/path/file.tar.gz", "file.tar.gz", false)
	if err != nil {
		t.Fatalf("UploadAsset(): %v", err)
	}
}

func TestListReleaseAssets_Empty(t *testing.T) {
	stub := NewStubRunner()
	stub.Outputs["gh release view v1.0.0 --json assets"] = []byte(`{"assets":[]}`)

	c := NewClientWithRunner(stub)
	result, err := c.ListReleaseAssets("owner", "repo", "v1.0.0")
	if err != nil {
		t.Fatalf("ListReleaseAssets(): %v", err)
	}
	if len(result) != 0 {
		t.Errorf("ListReleaseAssets() returned %d assets, want 0", len(result))
	}
}

func TestListReleaseAssets_GHError(t *testing.T) {
	stub := NewStubRunner()
	stub.Errors["gh release view v1.0.0 --json assets"] = &exec.ExitError{}

	c := NewClientWithRunner(stub)
	_, err := c.ListReleaseAssets("owner", "repo", "v1.0.0")
	if err == nil {
		t.Error("ListReleaseAssets() should return error when gh fails")
	}
}

func TestListReleaseAssets_MalformedJSON(t *testing.T) {
	stub := NewStubRunner()
	stub.Outputs["gh release view v1.0.0 --json assets"] = []byte("not json")

	c := NewClientWithRunner(stub)
	_, err := c.ListReleaseAssets("owner", "repo", "v1.0.0")
	if err == nil {
		t.Error("ListReleaseAssets() should return error for malformed JSON")
	}
}

// ---- Plan 31: LatestReleaseTag sorts by version, not creation date ----

func TestLatestReleaseTag_SortsByVersion(t *testing.T) {
	// cockpit-project/cockpit: 310.10 (newest by creation date) vs 368 (highest version)
	listJSON := `[` +
		`{"tagName":"310.10"},` +
		`{"tagName":"368"},` +
		`{"tagName":"356.4"}` +
		`]`
	stub := NewStubRunner()
	stub.Outputs["gh release list -R cockpit-project/cockpit --limit 100 --exclude-pre-releases --json tagName"] = []byte(listJSON)

	c := NewClientWithRunner(stub)
	tag, err := c.LatestReleaseTag("cockpit-project", "cockpit", false)
	if err != nil {
		t.Fatalf("LatestReleaseTag(): %v", err)
	}
	// 368 > 356.4 > 310.10 when compared as versions
	if tag != "368" {
		t.Errorf("tag = %q, want 368 (highest version, not 310.10)", tag)
	}
}

func TestLatestReleaseTag_SortsByVersion_Prefixed(t *testing.T) {
	// Tags with v prefix
	listJSON := `[` +
		`{"tagName":"v1.0.0"},` +
		`{"tagName":"v2.0.0"},` +
		`{"tagName":"v1.9.0"}` +
		`]`
	stub := NewStubRunner()
	stub.Outputs["gh release list -R o/r --limit 100 --json tagName"] = []byte(listJSON)

	c := NewClientWithRunner(stub)
	tag, err := c.LatestReleaseTag("o", "r", true)
	if err != nil {
		t.Fatalf("LatestReleaseTag(): %v", err)
	}
	if tag != "v2.0.0" {
		t.Errorf("tag = %q, want v2.0.0", tag)
	}
}

func TestLatestReleaseTag_EmptyReleases(t *testing.T) {
	stub := NewStubRunner()
	stub.Outputs["gh release list -R o/r --limit 100 --exclude-pre-releases --json tagName"] = []byte(`[]`)

	c := NewClientWithRunner(stub)
	_, err := c.LatestReleaseTag("o", "r", false)
	if err == nil {
		t.Fatal("LatestReleaseTag() should return error for empty releases")
	}
}

func TestLatestReleaseTag_NoVersionLike(t *testing.T) {
	listJSON := `[` +
		`{"tagName":"nightly"},` +
		`{"tagName":"dev"},` +
		`{"tagName":"main"}` +
		`]`
	stub := NewStubRunner()
	stub.Outputs["gh release list -R o/r --limit 100 --exclude-pre-releases --json tagName"] = []byte(listJSON)

	c := NewClientWithRunner(stub)
	_, err := c.LatestReleaseTag("o", "r", false)
	if err == nil {
		t.Fatal("LatestReleaseTag() should return error when no version-like tags exist")
	}
}

func TestLatestReleaseTag_SortsByVersion_Mixed(t *testing.T) {
	// Mix of version-like and non-version-like; should pick highest version
	listJSON := `[` +
		`{"tagName":"nightly"},` +
		`{"tagName":"v1.2.3"},` +
		`{"tagName":"v0.9.0"},` +
		`{"tagName":"latest"}` +
		`]`
	stub := NewStubRunner()
	stub.Outputs["gh release list -R o/r --limit 100 --exclude-pre-releases --json tagName"] = []byte(listJSON)

	c := NewClientWithRunner(stub)
	tag, err := c.LatestReleaseTag("o", "r", false)
	if err != nil {
		t.Fatalf("LatestReleaseTag(): %v", err)
	}
	if tag != "v1.2.3" {
		t.Errorf("tag = %q, want v1.2.3", tag)
	}
}
