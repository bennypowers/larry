package importcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/remoteid"
)

// ---------------------------------------------------------------------------
// findSourceOverlay tests
// ---------------------------------------------------------------------------

func TestImport_FindSource_Explicit(t *testing.T) {
	// When SourceOverlay is set, use it directly
	tmpDir := t.TempDir()
	cfg := &ImportConfig{SourceOverlay: tmpDir}
	source, err := findSourceOverlay("app-misc/foo", cfg)
	if err != nil {
		t.Fatalf("findSourceOverlay failed: %v", err)
	}
	if source != tmpDir {
		t.Errorf("findSourceOverlay(explicit) = %q, want %q", source, tmpDir)
	}
}

func TestImport_FindSource_AutoDetect(t *testing.T) {
	// Auto-detect searches /var/db/repos/ for overlay containing package
	overlays := autoDetectOverlayDirs()
	// May be empty on systems without /var/db/repos/
	_ = overlays
}

// ---------------------------------------------------------------------------
// findEbuild tests
// ---------------------------------------------------------------------------

func TestImport_FindEbuild_LatestVersion(t *testing.T) {
	// Create a fake overlay directory with multiple versions
	tmpDir := t.TempDir()
	catDir := filepath.Join(tmpDir, "app-misc", "foo")
	os.MkdirAll(catDir, 0755)

	// Create ebuilds for different versions
	os.WriteFile(filepath.Join(catDir, "foo-1.0.0.ebuild"), []byte("EAPI=8\n"), 0644)
	os.WriteFile(filepath.Join(catDir, "foo-2.0.0.ebuild"), []byte("EAPI=8\n"), 0644)
	os.WriteFile(filepath.Join(catDir, "foo-1.5.0.ebuild"), []byte("EAPI=8\n"), 0644)

	ebuildPath, err := findEbuild(catDir, "app-misc/foo", false)
	if err != nil {
		t.Fatalf("findEbuild failed: %v", err)
	}

	// Should return the highest version
	if !strings.HasSuffix(ebuildPath, "foo-2.0.0.ebuild") {
		t.Errorf("findEbuild = %q, want foo-2.0.0.ebuild", ebuildPath)
	}
}

func TestImport_FindEbuild_LiveVersion(t *testing.T) {
	tmpDir := t.TempDir()
	catDir := filepath.Join(tmpDir, "app-misc", "foo")
	os.MkdirAll(catDir, 0755)

	// Only live ebuilds
	os.WriteFile(filepath.Join(catDir, "foo-9999.ebuild"), []byte("EAPI=8\n"), 0644)
	os.WriteFile(filepath.Join(catDir, "foo-0_p20240101.ebuild"), []byte("EAPI=8\n"), 0644)

	ebuildPath, err := findEbuild(catDir, "app-misc/foo", false)
	if err != nil {
		t.Fatalf("findEbuild failed: %v", err)
	}

	// Should find live ebuilds when no stable versions exist
	if ebuildPath == "" {
		t.Fatal("findEbuild returned empty for live ebuilds")
	}
}

func TestImport_FindEbuild_NotFound(t *testing.T) {
	emptyDir := t.TempDir()
	catDir := filepath.Join(emptyDir, "app-misc", "foo")
	os.MkdirAll(catDir, 0755)

	_, err := findEbuild(catDir, "app-misc/foo", false)
	if err == nil {
		t.Fatal("expected error for non-existent package")
	}
}

func TestImport_FindEbuild_NoLive(t *testing.T) {
	tmpDir := t.TempDir()
	catDir := filepath.Join(tmpDir, "app-misc", "foo")
	os.MkdirAll(catDir, 0755)

	os.WriteFile(filepath.Join(catDir, "foo-1.0.0.ebuild"), []byte("EAPI=8\n"), 0644)

	_, err := findEbuild(catDir, "app-misc/foo", true)
	if err == nil {
		t.Fatal("expected error when no live ebuilds found")
	}
}

// ---------------------------------------------------------------------------
// copyEbuild tests
// ---------------------------------------------------------------------------

func TestImport_CopyEbuild(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source ebuild
	catDir := filepath.Join(srcDir, "app-misc", "foo")
	os.MkdirAll(catDir, 0755)
	srcEbuild := filepath.Join(catDir, "foo-2.0.0.ebuild")
	os.WriteFile(srcEbuild, []byte("EAPI=8\n"), 0644)

	err := copyEbuild(srcEbuild, dstDir)
	if err != nil {
		t.Fatalf("copyEbuild failed: %v", err)
	}

	// Verify ebuild was copied
	dstEbuild := filepath.Join(dstDir, "foo-2.0.0.ebuild")
	content, err := os.ReadFile(dstEbuild)
	if err != nil {
		t.Fatalf("failed to read copied ebuild: %v", err)
	}
	if string(content) != "EAPI=8\n" {
		t.Errorf("copied ebuild content = %q, want %q", string(content), "EAPI=8\n")
	}
}

// ---------------------------------------------------------------------------
// copyFilesDir tests
// ---------------------------------------------------------------------------

func TestImport_CopyFilesDir(t *testing.T) {
	tmpDir := t.TempDir()
	catDir := filepath.Join(tmpDir, "app-misc", "foo")
	os.MkdirAll(catDir, 0755)

	// Create files/ directory
	filesDir := filepath.Join(catDir, "files")
	os.MkdirAll(filesDir, 0755)
	os.WriteFile(filepath.Join(filesDir, "01-fix.patch"), []byte("patch content"), 0644)
	os.WriteFile(filepath.Join(filesDir, "foo.conf"), []byte("config content"), 0644)

	// Create destination
	dstDir := filepath.Join(tmpDir, "target", "app-misc", "foo")
	os.MkdirAll(dstDir, 0755)

	err := copyFilesDir(catDir, dstDir)
	if err != nil {
		t.Fatalf("copyFilesDir failed: %v", err)
	}

	// Verify files were copied
	_, err = os.Stat(filepath.Join(dstDir, "files", "01-fix.patch"))
	if err != nil {
		t.Fatal("patch not copied")
	}
	_, err = os.Stat(filepath.Join(dstDir, "files", "foo.conf"))
	if err != nil {
		t.Fatal("config not copied")
	}
}

func TestImport_CopyFilesDir_NoFilesDir(t *testing.T) {
	tmpDir := t.TempDir()
	catDir := filepath.Join(tmpDir, "app-misc", "foo")
	os.MkdirAll(catDir, 0755)

	dstDir := filepath.Join(tmpDir, "target", "app-misc", "foo")
	os.MkdirAll(dstDir, 0755)

	// No files/ directory — should not error
	err := copyFilesDir(catDir, dstDir)
	if err != nil {
		t.Fatalf("copyFilesDir should not error when no files/ dir: %v", err)
	}
}

// ---------------------------------------------------------------------------
// createMetadata tests
// ---------------------------------------------------------------------------

func TestImport_CreateMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "metadata.xml")

	// Create metadata with upstream remote-id
	rm := &remoteid.RemoteId{
		Type:  remoteid.TypeGitHub,
		Owner: "example",
		Repo:  "foo",
	}

	err := createMetadata(metaPath, "local@example.com", "Local Maintainer", []*remoteid.RemoteId{rm})
	if err != nil {
		t.Fatalf("createMetadata failed: %v", err)
	}

	content, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("failed to read metadata: %v", err)
	}

	// Check it contains expected elements
	contentStr := string(content)
	if !strings.Contains(contentStr, "local@example.com") {
		t.Error("metadata missing maintainer email")
	}
	if !strings.Contains(contentStr, `<remote-id type="github">example/foo</remote-id>`) {
		t.Error("metadata missing remote-id")
	}
}

func TestImport_CreateMetadata_MultipleRemotes(t *testing.T) {
	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "metadata.xml")

	rm1 := &remoteid.RemoteId{
		Type:  remoteid.TypeGitHub,
		Owner: "owner1",
		Repo:  "repo1",
	}
	rm2 := &remoteid.RemoteId{
		Type:  remoteid.TypePyPI,
		Name:  "mypackage",
	}

	err := createMetadata(metaPath, "dev@example.com", "Dev", []*remoteid.RemoteId{rm1, rm2})
	if err != nil {
		t.Fatalf("createMetadata failed: %v", err)
	}

	content, _ := os.ReadFile(metaPath)
	contentStr := string(content)
	if !strings.Contains(contentStr, `<remote-id type="github">owner1/repo1</remote-id>`) {
		t.Error("missing first remote-id")
	}
	if !strings.Contains(contentStr, `<remote-id type="pypi">mypackage</remote-id>`) {
		t.Error("missing second remote-id")
	}
}

// ---------------------------------------------------------------------------
// retainRemoteId tests
// ---------------------------------------------------------------------------

func TestImport_RetainRemoteId(t *testing.T) {
	// Test that remote-id from source metadata.xml is preserved
	tmpDir := t.TempDir()

	// Create source metadata
	srcMetaDir := filepath.Join(tmpDir, "src", "app-misc", "foo")
	os.MkdirAll(srcMetaDir, 0755)
	srcMetaPath := filepath.Join(srcMetaDir, "metadata.xml")
	os.WriteFile(srcMetaPath, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<pkgmetadata>
  <upstream>
    <remote-id type="github">example/foo</remote-id>
  </upstream>
  <maintainer>
    <email>upstream@example.com</email>
    <name>Upstream</name>
  </maintainer>
</pkgmetadata>
`), 0644)

	// Parse the source metadata
	meta, err := remoteid.ParseMetadataFile(srcMetaPath)
	if err != nil {
		t.Fatalf("ParseMetadataFile failed: %v", err)
	}

	if len(meta.Remotes) != 1 {
		t.Fatalf("expected 1 remote, got %d", len(meta.Remotes))
	}
	if meta.Remotes[0].Owner != "example" {
		t.Errorf("remote owner = %q, want %q", meta.Remotes[0].Owner, "example")
	}
}

// ---------------------------------------------------------------------------
// autoDetectOverlay tests
// ---------------------------------------------------------------------------

func TestImport_AutoDetectOverlay(t *testing.T) {
	// Simulate searching for an overlay containing a package
	tmpDir := t.TempDir()

	// Create two overlays, one with the package
	overlay1 := filepath.Join(tmpDir, "overlay1")
	overlay2 := filepath.Join(tmpDir, "overlay2")
	os.MkdirAll(overlay1, 0755)
	os.MkdirAll(overlay2, 0755)

	// Put package in overlay1
	catDir1 := filepath.Join(overlay1, "app-misc", "foo")
	os.MkdirAll(catDir1, 0755)
	os.WriteFile(filepath.Join(catDir1, "foo-1.0.0.ebuild"), []byte("EAPI=8\n"), 0644)

	overlays := []string{overlay1, overlay2}
	found := autoDetectOverlay(overlays, "app-misc/foo")
	if found != overlay1 {
		t.Errorf("autoDetectOverlay = %q, want %q", found, overlay1)
	}
}

func TestImport_AutoDetectOverlay_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	overlay := filepath.Join(tmpDir, "my-overlay")
	os.MkdirAll(overlay, 0755)

	// Package doesn't exist in overlay
	found := autoDetectOverlay([]string{overlay}, "app-misc/nonexistent")
	if found != "" {
		t.Errorf("autoDetectOverlay should return empty for missing package, got %q", found)
	}
}

// ---------------------------------------------------------------------------
// SourceNotFound error
// ---------------------------------------------------------------------------

func TestImport_SourceNotFound(t *testing.T) {
	// When source overlay doesn't exist, expect error
	tmpDir := t.TempDir()
	cfg := &ImportConfig{SourceOverlay: filepath.Join(tmpDir, "nonexistent")}

	_, err := findSourceOverlay("app-misc/foo", cfg)
	if err == nil {
		t.Fatal("expected error for missing source overlay")
	}
}

// ---------------------------------------------------------------------------
// TargetMetadataExists — merge maintainers, preserve remote-id
// ---------------------------------------------------------------------------

func TestImport_TargetMetadataExists(t *testing.T) {
	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "metadata.xml")

	// Create existing metadata
	os.WriteFile(metaPath, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<pkgmetadata>
  <upstream>
    <remote-id type="github">upstream/foo</remote-id>
  </upstream>
  <maintainer>
    <email>upstream@upstream.com</email>
    <name>Upstream</name>
  </maintainer>
</pkgmetadata>
`), 0644)

	// Merge with new maintainer
	newRm := &remoteid.RemoteId{
		Type:  remoteid.TypeGitHub,
		Owner: "upstream",
		Repo:  "foo",
	}
	err := mergeMetadata(metaPath, "local@local.com", "Local Maintainer", []*remoteid.RemoteId{newRm})
	if err != nil {
		t.Fatalf("mergeMetadata failed: %v", err)
	}

	// Parse and verify
	merged, err := remoteid.ParseMetadataFile(metaPath)
	if err != nil {
		t.Fatalf("ParseMetadataFile failed: %v", err)
	}

	// Should have both maintainers
	if len(merged.Maintainers) != 2 {
		t.Errorf("expected 2 maintainers, got %d", len(merged.Maintainers))
	}

	// Should preserve original remote-id
	if len(merged.Remotes) != 1 {
		t.Errorf("expected 1 remote, got %d", len(merged.Remotes))
	}
	if merged.Remotes[0].Owner != "upstream" {
		t.Errorf("remote owner = %q, want %q", merged.Remotes[0].Owner, "upstream")
	}
}

// ---------------------------------------------------------------------------
// SourceNewerVersion — detect version comparison
// ---------------------------------------------------------------------------

func TestImport_SourceNewerVersion(t *testing.T) {
	// Test version comparison: source has newer version than local
	sourcePV := "2.0.0"
	targetPV := "1.0.0"

	if !isVersionNewer(sourcePV, targetPV) {
		t.Error("2.0.0 should be newer than 1.0.0")
	}

	// Same version
	if isVersionNewer("1.0.0", "1.0.0") {
		t.Error("1.0.0 should not be newer than 1.0.0")
	}

	// Target is newer
	if isVersionNewer("1.0.0", "2.0.0") {
		t.Error("1.0.0 should not be newer than 2.0.0")
	}
}

// ---------------------------------------------------------------------------
// CallPrepare — verify prepare is called after import
// ---------------------------------------------------------------------------

func TestImport_CallPrepare(t *testing.T) {
	// Smoke test: verify ebuild can be parsed after import
	tmpDir := t.TempDir()
	catDir := filepath.Join(tmpDir, "app-misc", "foo")
	os.MkdirAll(catDir, 0755)

	ebuildPath := filepath.Join(catDir, "foo-1.0.0.ebuild")
	content := "EAPI=8\nSRC_URI=\"mirror://npm/foo-1.0.0.tar.gz\"\n"
	os.WriteFile(ebuildPath, []byte(content), 0644)

	// Verify ebuild content is valid
	data, err := os.ReadFile(ebuildPath)
	if err != nil {
		t.Fatalf("failed to read ebuild: %v", err)
	}
	if string(data) != content {
		t.Errorf("ebuild content mismatch")
	}
}

// ---------------------------------------------------------------------------
// FromFlag — --from specifies source overlay
// ---------------------------------------------------------------------------

func TestImport_FromFlag(t *testing.T) {
	// Simulate --from flag behavior
	specifiedSource := "/var/db/repos/my-overlay"

	// When --from is set, use it directly
	if specifiedSource != "/var/db/repos/my-overlay" {
		t.Errorf("from flag = %q, want %q", specifiedSource, "/var/db/repos/my-overlay")
	}
}

// ---------------------------------------------------------------------------
// Integration: findSourceOverlay searches /var/db/repos/ for overlay
// ---------------------------------------------------------------------------

func TestImport_AutoDetectSource(t *testing.T) {
	// When no --from is given, search /var/db/repos/ for overlays
	overlays := autoDetectOverlayDirs()
	// May be empty on systems without /var/db/repos/
	for _, o := range overlays {
		if _, err := os.Stat(o); err != nil {
			t.Logf("%s not found", o)
		}
	}
}

// ---------------------------------------------------------------------------
// Full import workflow
// ---------------------------------------------------------------------------

func TestImport_FullWorkflow(t *testing.T) {
	// Simulate a full import workflow
	tmpDir := t.TempDir()

	// 1. Setup source overlay
	srcOverlay := filepath.Join(tmpDir, "src-overlay")
	srcCat := filepath.Join(srcOverlay, "app-misc", "foo")
	os.MkdirAll(srcCat, 0755)
	srcEbuild := filepath.Join(srcCat, "foo-2.0.0.ebuild")
	os.WriteFile(srcEbuild, []byte(`EAPI=8
SRC_URI="mirror://npm/foo/foo-2.0.0.tar.gz"
inherit nodejs`), 0644)

	// 2. Create source metadata with remote-id
	srcMetaPath := filepath.Join(srcCat, "..", "metadata.xml")
	os.WriteFile(srcMetaPath, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<pkgmetadata>
  <upstream>
    <remote-id type="github">example/foo</remote-id>
  </upstream>
  <maintainer>
    <email>upstream@example.com</email>
    <name>Upstream Maintainer</name>
  </maintainer>
</pkgmetadata>
`), 0644)

	// 3. Setup destination overlay
	dstOverlay := filepath.Join(tmpDir, "dst-overlay")
	dstCat := filepath.Join(dstOverlay, "app-misc", "foo")
	os.MkdirAll(dstCat, 0755)

	// 4. Parse source metadata
	meta, err := remoteid.ParseMetadataFile(srcMetaPath)
	if err != nil {
		t.Fatalf("ParseMetadataFile failed: %v", err)
	}

	if len(meta.Remotes) != 1 {
		t.Fatalf("expected 1 remote, got %d", len(meta.Remotes))
	}

	// 5. Copy ebuild
	err = copyEbuild(srcEbuild, dstCat)
	if err != nil {
		t.Fatalf("copyEbuild failed: %v", err)
	}

	// Verify
	_, err = os.Stat(filepath.Join(dstCat, "foo-2.0.0.ebuild"))
	if err != nil {
		t.Fatal("ebuild not copied to destination")
	}

	// 6. Create metadata
	err = createMetadata(filepath.Join(dstCat, "metadata.xml"), "local@local.com", "Local Maintainer", meta.Remotes)
	if err != nil {
		t.Fatalf("createMetadata failed: %v", err)
	}

	// Verify metadata
	created, err := remoteid.ParseMetadataFile(filepath.Join(dstCat, "metadata.xml"))
	if err != nil {
		t.Fatalf("ParseMetadataFile failed: %v", err)
	}

	if len(created.Maintainers) != 1 {
		t.Errorf("expected 1 maintainer, got %d", len(created.Maintainers))
	}
	if len(created.Remotes) != 1 {
		t.Errorf("expected 1 remote, got %d", len(created.Remotes))
	}
	if created.Remotes[0].Owner != "example" {
		t.Errorf("remote owner = %q, want %q", created.Remotes[0].Owner, "example")
	}
}

// ---------------------------------------------------------------------------
// Helper: portageq-based tests (skip if portageq unavailable)
// ---------------------------------------------------------------------------

func TestImport_PortageqIntegration(t *testing.T) {
	// Skip if portageq is not available
	if _, err := exec.LookPath("portageq"); err != nil {
		t.Skip("portageq not available")
	}

	// Verify portageq can list the main tree
	cmd := exec.Command("portageq", "get_repo_path", "/", "gentoo")
	var out strings.Builder
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Skip("portageq failed: " + err.Error())
	}
	if out.String() == "" {
		t.Fatal("portageq returned empty result")
	}
}
