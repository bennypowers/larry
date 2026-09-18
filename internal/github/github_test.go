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
	releaseOutput := []struct {
		TagName string `json:"tagName"`
		Name    string `json:"name"`
	}{{
		TagName: "v1.2.3",
		Name:    "Release v1.2.3",
	}}

	outJSON, _ := json.Marshal(releaseOutput)
	// The command is: gh release list --limit 1 --json tagName,name --repo owner/repo
	stub.Outputs["gh release list --limit 1 --json tagName,name --repo owner/repo"] = outJSON

	c := NewClientWithRunner(stub)
	tag, url, err := c.LatestRelease("owner", "repo")
	if err != nil {
		t.Fatalf("LatestRelease(): %v", err)
	}
	if tag != "v1.2.3" {
		t.Errorf("tag = %q, want v1.2.3", tag)
	}
	if url != "https://github.com/owner/repo/releases/tag/v1.2.3" {
		t.Errorf("url = %q, want https://github.com/owner/repo/releases/tag/v1.2.3", url)
	}
}

func TestLatestRelease_NoOutput(t *testing.T) {
	stub := NewStubRunner()
	stub.Outputs["gh release list --limit 1 --json tagName,name --repo owner/repo"] = []byte("[]")

	c := NewClientWithRunner(stub)
	_, _, err := c.LatestRelease("owner", "repo")
	if err == nil {
		t.Error("LatestRelease() should return error for empty output")
	}
}

func TestLatestRelease_GHError(t *testing.T) {
	stub := NewStubRunner()
	stub.Errors["gh release list --limit 1 --json tagName,name --repo owner/repo"] = &exec.ExitError{}

	c := NewClientWithRunner(stub)
	_, _, err := c.LatestRelease("owner", "repo")
	if err == nil {
		t.Error("LatestRelease() should return error when gh fails")
	}
}

func TestLatestRelease_ParsesJSON(t *testing.T) {
	stub := NewStubRunner()
	releaseOutput := []struct {
		TagName string `json:"tagName"`
		Name    string `json:"name"`
	}{{
		TagName: "v2.0.0",
		Name:    "Release v2.0.0",
	}}

	outJSON, _ := json.Marshal(releaseOutput)
	stub.Outputs["gh release list --limit 1 --json tagName,name --repo test-owner/test-repo"] = outJSON

	c := NewClientWithRunner(stub)
	tag, url, err := c.LatestRelease("test-owner", "test-repo")
	if err != nil {
		t.Fatalf("LatestRelease(): %v", err)
	}
	if tag != "v2.0.0" {
		t.Errorf("tag = %q, want v2.0.0", tag)
	}
	if url != "https://github.com/test-owner/test-repo/releases/tag/v2.0.0" {
		t.Errorf("url = %q, want https://github.com/test-owner/test-repo/releases/tag/v2.0.0", url)
	}
}

func TestLatestRelease_MalformedJSON(t *testing.T) {
	stub := NewStubRunner()
	// Invalid JSON
	stub.Outputs["gh release list --limit 1 --json tagName,name --repo owner/repo"] = []byte("not valid json")

	c := NewClientWithRunner(stub)
	_, _, err := c.LatestRelease("owner", "repo")
	if err == nil {
		t.Error("LatestRelease() should return error for malformed JSON")
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
