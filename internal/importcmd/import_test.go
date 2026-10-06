package importcmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// importFixture creates reposRoot/<repo>/<cat>/<pn>/ for each named repo and
// writes the given ebuild basenames as empty files. Returns reposRoot.
func importFixture(t *testing.T, repos map[string][]string, catPkg string) string {
	t.Helper()
	reposRoot := t.TempDir()

	parts := strings.SplitN(catPkg, "/", 2)
	if len(parts) != 2 {
		t.Fatalf("catPkg must be category/package, got %q", catPkg)
	}
	cat, pkg := parts[0], parts[1]

	for repoName, ebuilds := range repos {
		dir := filepath.Join(reposRoot, repoName, cat, pkg)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("create repo dir %s: %v", dir, err)
		}
		for _, eb := range ebuilds {
			if err := os.WriteFile(filepath.Join(dir, eb), nil, 0644); err != nil {
				t.Fatalf("write %s: %v", filepath.Join(dir, eb), err)
			}
		}
	}
	return reposRoot
}

// ---------------------------------------------------------------------------
// D1 — TargetPath: build from atom.ParseAtom, never from source path
// ---------------------------------------------------------------------------

func TestImport_TargetPath(t *testing.T) {
	reposRoot := importFixture(t, map[string][]string{
		"guru": {"foo-1.0.0.ebuild"},
	}, "dev-lisp/foo")

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   filepath.Join(reposRoot, "guru"),
		TargetOverlay:   targetOverlay,
		DryRun:          false,
		ReposRoot:       reposRoot,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	result, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	expected := filepath.Join(targetOverlay, "dev-lisp", "foo", "foo-1.0.0.ebuild")
	if result.EbuildPath != expected {
		t.Errorf("EbuildPath = %q, want %q", result.EbuildPath, expected)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected ebuild at %s: %v", expected, err)
	}

	// Regression: <target>/var must NOT exist
	if _, err := os.Stat(filepath.Join(targetOverlay, "var")); err == nil {
		t.Error("BUG D1: target/var should not exist")
	}
}

// ---------------------------------------------------------------------------
// D2, D3 — AutoSearchSkipsOwnOverlay
// ---------------------------------------------------------------------------

func TestImport_AutoSearchSkipsOwnOverlay(t *testing.T) {
	reposRoot := importFixture(t, map[string][]string{
		"mine": {"foo-1.0.0.ebuild"},
		"guru": {"foo-2.0.0.ebuild"},
	}, "dev-lisp/foo")

	targetOverlay := filepath.Join(reposRoot, "mine")
	cfg := &ImportConfig{
		SourceOverlay:   "", // empty → auto-search
		TargetOverlay:   targetOverlay,
		ReposRoot:       reposRoot,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	result, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	// Source overlay should be guru (not mine)
	expectedSource := filepath.Join(reposRoot, "guru", "dev-lisp", "foo", "foo-2.0.0.ebuild")
	if result.SourcePath != expectedSource {
		t.Errorf("SourcePath = %q, want %q", result.SourcePath, expectedSource)
	}

	// Target should be mine, with 2.0.0
	expectedTarget := filepath.Join(targetOverlay, "dev-lisp", "foo", "foo-2.0.0.ebuild")
	if result.EbuildPath != expectedTarget {
		t.Errorf("EbuildPath = %q, want %q", result.EbuildPath, expectedTarget)
	}
}

func TestImport_AutoSearchNoneFound(t *testing.T) {
	reposRoot := importFixture(t, map[string][]string{
		"mine": {"foo-1.0.0.ebuild"},
	}, "dev-lisp/foo")

	targetOverlay := filepath.Join(reposRoot, "mine")
	cfg := &ImportConfig{
		SourceOverlay:   "",
		TargetOverlay:   targetOverlay,
		ReposRoot:       reposRoot,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err == nil {
		t.Fatal("expected error when own overlay is only source")
	}
	if !strings.Contains(err.Error(), "--from") {
		t.Errorf("error should mention --from: %v", err)
	}
}

// ---------------------------------------------------------------------------
// D4 — FromRepoName / FromAbsolutePath / FromUnknownRepoName
// ---------------------------------------------------------------------------

func TestImport_FromRepoName(t *testing.T) {
	reposRoot := importFixture(t, map[string][]string{
		"guru": {"foo-1.0.0.ebuild"},
	}, "dev-lisp/foo")

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   "guru",
		TargetOverlay:   targetOverlay,
		ReposRoot:       reposRoot,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	result, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.SourcePath == "" {
		t.Fatal("SourcePath is empty")
	}
}

func TestImport_FromAbsolutePath(t *testing.T) {
	reposRoot := importFixture(t, map[string][]string{
		"guru": {"foo-1.0.0.ebuild"},
	}, "dev-lisp/foo")

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   filepath.Join(reposRoot, "guru"),
		TargetOverlay:   targetOverlay,
		ReposRoot:       reposRoot,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	result, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.SourcePath == "" {
		t.Fatal("SourcePath is empty")
	}
}

func TestImport_FromUnknownRepoName(t *testing.T) {
	reposRoot := importFixture(t, map[string][]string{
		"guru": {"foo-1.0.0.ebuild"},
	}, "dev-lisp/foo")

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   "nosuch",
		TargetOverlay:   targetOverlay,
		ReposRoot:       reposRoot,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err == nil {
		t.Fatal("expected error for unknown repo")
	}
	if !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("error should mention 'nosuch': %v", err)
	}
}

// ---------------------------------------------------------------------------
// D5 — Live ebuild exclusion
// ---------------------------------------------------------------------------

func TestImport_ExcludesLiveEbuild(t *testing.T) {
	reposRoot := importFixture(t, map[string][]string{
		"guru": {"foo-1.0.0.ebuild", "foo-9999.ebuild"},
	}, "dev-lisp/foo")

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   filepath.Join(reposRoot, "guru"),
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	result, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if !strings.HasSuffix(result.EbuildPath, "foo-1.0.0.ebuild") {
		t.Errorf("Expected foo-1.0.0.ebuild, got %s", result.EbuildPath)
	}

	// Verify 9999 was NOT copied
	_, err = os.Stat(filepath.Join(targetOverlay, "dev-lisp", "foo", "foo-9999.ebuild"))
	if err == nil {
		t.Error("foo-9999.ebuild should not be copied")
	}
}

func TestImport_PostReleaseIsNotLive(t *testing.T) {
	reposRoot := importFixture(t, map[string][]string{
		"guru": {"foo-1.0_p20240101.ebuild"},
	}, "dev-lisp/foo")

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   filepath.Join(reposRoot, "guru"),
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	result, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if !strings.HasSuffix(result.EbuildPath, "foo-1.0_p20240101.ebuild") {
		t.Errorf("Expected foo-1.0_p20240101.ebuild, got %s", result.EbuildPath)
	}
}

func TestImport_PreReleaseIsNotLive(t *testing.T) {
	reposRoot := importFixture(t, map[string][]string{
		"guru": {"foo-1.0_pre1.ebuild"},
	}, "dev-lisp/foo")

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   filepath.Join(reposRoot, "guru"),
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	result, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if !strings.HasSuffix(result.EbuildPath, "foo-1.0_pre1.ebuild") {
		t.Errorf("Expected foo-1.0_pre1.ebuild, got %s", result.EbuildPath)
	}
}

// ---------------------------------------------------------------------------
// D6 — Version compare uses internal/version
// ---------------------------------------------------------------------------

func TestImport_VersionCompareUsesVersionPkg(t *testing.T) {
	reposRoot := importFixture(t, map[string][]string{
		"guru": {"foo-1.2.3.ebuild", "foo-1.2.3-r1.ebuild"},
	}, "dev-lisp/foo")

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   filepath.Join(reposRoot, "guru"),
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	result, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if !strings.HasSuffix(result.EbuildPath, "foo-1.2.3-r1.ebuild") {
		t.Errorf("Expected foo-1.2.3-r1.ebuild, got %s", result.EbuildPath)
	}
}

func TestImport_NumericOrdering(t *testing.T) {
	reposRoot := importFixture(t, map[string][]string{
		"guru": {"foo-9.0.ebuild", "foo-10.0.ebuild"},
	}, "dev-lisp/foo")

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   filepath.Join(reposRoot, "guru"),
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	result, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if !strings.HasSuffix(result.EbuildPath, "foo-10.0.ebuild") {
		t.Errorf("Expected foo-10.0.ebuild, got %s", result.EbuildPath)
	}
}

// ---------------------------------------------------------------------------
// D13 — Force guard
// ---------------------------------------------------------------------------

func TestImport_RefusesEqualOrNewerWithoutForce(t *testing.T) {
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)

	targetOverlay := t.TempDir()
	targetPkgDir := filepath.Join(targetOverlay, "dev-lisp", "foo")
	os.MkdirAll(targetPkgDir, 0755)
	os.WriteFile(filepath.Join(targetPkgDir, "foo-1.0.0.ebuild"), []byte("OLD\n"), 0644)

	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		Force:           false,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err == nil {
		t.Fatal("expected error for equal version without force")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should mention --force: %v", err)
	}

	// Verify target still has OLD
	data, _ := os.ReadFile(filepath.Join(targetPkgDir, "foo-1.0.0.ebuild"))
	if string(data) != "OLD\n" {
		t.Errorf("target ebuild should still be OLD\\n, got %q", string(data))
	}
}

func TestImport_ForceOverwrites(t *testing.T) {
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)

	targetOverlay := t.TempDir()
	targetPkgDir := filepath.Join(targetOverlay, "dev-lisp", "foo")
	os.MkdirAll(targetPkgDir, 0755)
	os.WriteFile(filepath.Join(targetPkgDir, "foo-1.0.0.ebuild"), []byte("OLD\n"), 0644)

	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		Force:           true,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(targetPkgDir, "foo-1.0.0.ebuild"))
	if string(data) != "SRC\n" {
		t.Errorf("target ebuild should be SRC\\n, got %q", string(data))
	}
}

func TestImport_AllowsNewerWithoutForce(t *testing.T) {
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-2.0.0.ebuild"), []byte("SRC\n"), 0644)

	targetOverlay := t.TempDir()
	targetPkgDir := filepath.Join(targetOverlay, "dev-lisp", "foo")
	os.MkdirAll(targetPkgDir, 0755)
	os.WriteFile(filepath.Join(targetPkgDir, "foo-1.0.0.ebuild"), []byte("OLD\n"), 0644)

	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		Force:           false,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	// Both should exist
	if _, err := os.Stat(filepath.Join(targetPkgDir, "foo-2.0.0.ebuild")); err != nil {
		t.Error("foo-2.0.0.ebuild should have been created")
	}
	if _, err := os.Stat(filepath.Join(targetPkgDir, "foo-1.0.0.ebuild")); err != nil {
		t.Error("foo-1.0.0.ebuild should still exist")
	}
}

// ---------------------------------------------------------------------------
// D12 — DryRunNoWrites
// ---------------------------------------------------------------------------

func TestImport_DryRunNoWrites(t *testing.T) {
	reposRoot := importFixture(t, map[string][]string{
		"guru": {"foo-1.0.0.ebuild"},
	}, "dev-lisp/foo")

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   filepath.Join(reposRoot, "guru"),
		TargetOverlay:   targetOverlay,
		DryRun:          true,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	result, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	// No files should have been written
	entries, err := os.ReadDir(targetOverlay)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries in target, got %d", len(entries))
	}

	// Result must have paths
	if result.EbuildPath == "" {
		t.Error("EbuildPath should be non-empty in dry-run")
	}
	if result.MetaPath == "" {
		t.Error("MetaPath should be non-empty in dry-run")
	}
}

// ---------------------------------------------------------------------------
// CopyFilesDir
// ---------------------------------------------------------------------------

func TestImport_CopiesFilesDir(t *testing.T) {
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)

	filesDir := filepath.Join(srcCat, "files")
	os.MkdirAll(filesDir, 0755)
	os.WriteFile(filepath.Join(filesDir, "foo.patch"), []byte("PATCH\n"), 0644)

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Log:             &ImportLogger{},
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	patchPath := filepath.Join(targetOverlay, "dev-lisp", "foo", "files", "foo.patch")
	data, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatalf("patch file not copied: %v", err)
	}
	if string(data) != "PATCH\n" {
		t.Errorf("patch content = %q, want %q", string(data), "PATCH\n")
	}
}

// ---------------------------------------------------------------------------
// Struct sanity — ImportConfig and ImportResult
// ---------------------------------------------------------------------------

func TestImportConfig_Struct(t *testing.T) {
	cfg := &ImportConfig{
		SourceOverlay: "/src",
		TargetOverlay: "/dst",
		DryRun:        true,
		Force:         true,
		ReposRoot:     "/var/db/repos",
	}
	if cfg.ReposRoot != "/var/db/repos" {
		t.Error("ReposRoot not set")
	}
}

func TestImportResult_Struct(t *testing.T) {
	r := &ImportResult{
		EbuildPath: "/dst/foo-1.0.0.ebuild",
		MetaPath:   "/dst/metadata.xml",
		SourcePath: "/src/foo-1.0.0.ebuild",
	}
	if r.EbuildPath == "" || r.MetaPath == "" || r.SourcePath == "" {
		t.Error("ImportResult fields not set")
	}
}

// ---------------------------------------------------------------------------
// D7 — writeMetadata: byte-exact golden (pkgmetadata, DOCTYPE, tabs)
// ---------------------------------------------------------------------------

func TestImport_MetadataShape(t *testing.T) {
	// Source metadata with upstream remote-id.
	srcMeta := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkgmetadata SYSTEM "https://www.gentoo.org/dtd/metadata.dtd">
<pkgmetadata>
  <maintainer type="person">
    <email>upstream@example.com</email>
    <name>Upstream Maintainer</name>
  </maintainer>
  <upstream>
    <remote-id type="github">owner/repo</remote-id>
  </upstream>
</pkgmetadata>
`

	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)
	os.WriteFile(filepath.Join(srcCat, "metadata.xml"), []byte(srcMeta), 0644)

	targetOverlay := t.TempDir()

	log := &ImportLogger{}

	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Prepare:         nil, // no-op
		Log:             log,
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(targetOverlay, "dev-lisp", "foo", "metadata.xml"))
	if err != nil {
		t.Fatalf("read metadata.xml: %v", err)
	}

	want := testdataImportedGolden()
	if !bytes.Equal(got, want) {
		t.Errorf("metadata.xml does not match golden\nwant: %q\ngot:  %q", want, got)
	}
}

func TestImport_MetadataShape_NoRemoteIds(t *testing.T) {
	// No source metadata.xml.
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)

	targetOverlay := t.TempDir()

	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Prepare:         nil,
		Log:             &ImportLogger{},
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(targetOverlay, "dev-lisp", "foo", "metadata.xml"))
	if err != nil {
		t.Fatalf("read metadata.xml: %v", err)
	}

	want := testdataImportedNoUpstreamGolden()
	if !bytes.Equal(got, want) {
		t.Errorf("metadata.xml does not match no-upstream golden\nwant: %q\ngot:  %q", want, got)
	}

	// Also assert no <upstream> substring at all
	if strings.Contains(string(got), "<upstream>") {
		t.Error("metadata.xml must not contain <upstream> when there are no remote-ids")
	}
}

// ---------------------------------------------------------------------------
// D8 — Preserve upstream remote-id / error on malformed / missing is ok
// ---------------------------------------------------------------------------

func TestImport_PreservesUpstreamRemoteId(t *testing.T) {
	srcMeta := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkgmetadata SYSTEM "https://www.gentoo.org/dtd/metadata.dtd">
<pkgmetadata>
  <upstream>
    <remote-id type="github">owner/repo</remote-id>
  </upstream>
</pkgmetadata>
`
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)
	os.WriteFile(filepath.Join(srcCat, "metadata.xml"), []byte(srcMeta), 0644)

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Prepare:         nil,
		Log:             &ImportLogger{},
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(targetOverlay, "dev-lisp", "foo", "metadata.xml"))
	if err != nil {
		t.Fatalf("read metadata.xml: %v", err)
	}

	want := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkgmetadata SYSTEM "https://www.gentoo.org/dtd/metadata.dtd">
<pkgmetadata>
	<maintainer type="person">
		<email>web@bennypowers.com</email>
		<name>Benny Powers</name>
	</maintainer>
	<upstream>
		<remote-id type="github">owner/repo</remote-id>
	</upstream>
</pkgmetadata>
`
	if string(got) != want {
		t.Errorf("metadata.xml mismatch\nwant: %q\ngot:  %q", want, string(got))
	}
}

func TestImport_MalformedSourceMetadataIsReported(t *testing.T) {
	// Malformed XML — no closing tag.
	srcMeta := `<pkgmetadata><unclosed>`
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)
	os.WriteFile(filepath.Join(srcCat, "metadata.xml"), []byte(srcMeta), 0644)

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Prepare:         nil,
		Log:             &ImportLogger{},
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err == nil {
		t.Fatal("expected error for malformed source metadata")
	}
	if !strings.Contains(err.Error(), "parse source metadata") {
		t.Errorf("error should mention 'parse source metadata': %v", err)
	}

	// No metadata.xml should have been written
	metaPath := filepath.Join(targetOverlay, "dev-lisp", "foo", "metadata.xml")
	if _, err := os.Stat(metaPath); err == nil {
		t.Error("metadata.xml should not exist on error")
	}
}

func TestImport_MissingSourceMetadataIsNotAnError(t *testing.T) {
	// No metadata.xml in source at all.
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Prepare:         nil,
		Log:             &ImportLogger{},
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	// Should produce the no-upstream golden
	got, err := os.ReadFile(filepath.Join(targetOverlay, "dev-lisp", "foo", "metadata.xml"))
	if err != nil {
		t.Fatalf("read metadata.xml: %v", err)
	}
	want := testdataImportedNoUpstreamGolden()
	if !bytes.Equal(got, want) {
		t.Errorf("metadata.xml mismatch\nwant: %q\ngot:  %q", want, string(got))
	}
}

// ---------------------------------------------------------------------------
// D9 — Maintainer config required
// ---------------------------------------------------------------------------

func TestImport_MissingMaintainerConfigIsError(t *testing.T) {
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		MaintainerName:  "",
		MaintainerEmail: "x@y",
		Prepare:         nil,
		Log:             &ImportLogger{},
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err == nil {
		t.Fatal("expected error for missing maintainer name")
	}
	if !strings.Contains(err.Error(), "maintainer not configured") {
		t.Errorf("error should mention 'maintainer not configured': %v", err)
	}

	// Target overlay must have zero entries — nothing was written.
	entries, err := os.ReadDir(targetOverlay)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("target overlay should have 0 entries, got %d", len(entries))
	}
}

func TestImport_MissingMaintainerEmailIsError(t *testing.T) {
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		MaintainerName:  "X",
		MaintainerEmail: "",
		Prepare:         nil,
		Log:             &ImportLogger{},
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err == nil {
		t.Fatal("expected error for missing maintainer email")
	}
	if !strings.Contains(err.Error(), "maintainer not configured") {
		t.Errorf("error should mention 'maintainer not configured': %v", err)
	}
}

func TestImport_MaintainerFromConfig(t *testing.T) {
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)

	targetOverlay := t.TempDir()
	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Prepare:         nil,
		Log:             &ImportLogger{},
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(targetOverlay, "dev-lisp", "foo", "metadata.xml"))
	if err != nil {
		t.Fatalf("read metadata.xml: %v", err)
	}

	if !strings.Contains(string(got), `<email>web@bennypowers.com</email>`) {
		t.Errorf("missing expected email in metadata.xml\n%q", string(got))
	}
	if !strings.Contains(string(got), `<name>Benny Powers</name>`) {
		t.Errorf("missing expected name in metadata.xml\n%q", string(got))
	}
	// No empty <email></email> or <name></name>
	if strings.Contains(string(got), "<email></email>") {
		t.Error("should not contain empty <email> tag")
	}
	if strings.Contains(string(got), "<name></name>") {
		t.Error("should not contain empty <name> tag")
	}
}

// ---------------------------------------------------------------------------
// D11 — Prepare hook
// ---------------------------------------------------------------------------

func TestImport_RunsPrepare(t *testing.T) {
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)

	targetOverlay := t.TempDir()

	var prepareCalled bool
	var preparePath string
	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Prepare: func(ebuildPath string) error {
			prepareCalled = true
			preparePath = ebuildPath
			return nil
		},
		Log: &ImportLogger{},
	}

	result, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if !prepareCalled {
		t.Error("prepare stub was not called")
	}
	if preparePath != result.EbuildPath {
		t.Errorf("prepare called with path %q, want %q", preparePath, result.EbuildPath)
	}
	if !result.Prepared {
		t.Error("result.Prepared should be true")
	}
}

func TestImport_PrepareFailureIsReported(t *testing.T) {
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)

	targetOverlay := t.TempDir()

	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Prepare: func(p string) error {
			return fmt.Errorf("boom")
		},
		Log: &ImportLogger{},
	}

	result, err := Import("dev-lisp/foo", cfg)
	if err == nil {
		t.Fatal("expected error from prepare failure")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error should wrap 'boom': %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil on prepare failure")
	}
	if result.EbuildPath == "" {
		t.Error("result.EbuildPath should be populated")
	}
	if result.Prepared {
		t.Error("result.Prepared should be false on prepare failure")
	}
}

func TestImport_DryRunDoesNotPrepare(t *testing.T) {
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)

	var prepareCalled bool
	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   t.TempDir(),
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		DryRun:          true,
		Prepare: func(p string) error {
			prepareCalled = true
			return nil
		},
		Log: &ImportLogger{},
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if prepareCalled {
		t.Error("prepare stub should not be called in dry-run mode")
	}
}

// ---------------------------------------------------------------------------
// D14 — Provenance lines via logger
// ---------------------------------------------------------------------------

func TestImport_ProvenanceLines(t *testing.T) {
	srcDir := t.TempDir()
	srcCat := filepath.Join(srcDir, "dev-lisp", "foo")
	os.MkdirAll(srcCat, 0755)
	os.WriteFile(filepath.Join(srcCat, "foo-1.0.0.ebuild"), []byte("SRC\n"), 0644)

	targetOverlay := t.TempDir()

	log := &ImportLogger{}
	cfg := &ImportConfig{
		SourceOverlay:   srcDir,
		TargetOverlay:   targetOverlay,
		MaintainerName:  "Benny Powers",
		MaintainerEmail: "web@bennypowers.com",
		Prepare:         nil,
		Log:             log,
	}

	_, err := Import("dev-lisp/foo", cfg)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	// Check that provenance lines were logged
	msgs := log.Messages()
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "Importing dev-lisp/foo") && strings.Contains(m, "from ::") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'Importing dev-lisp/foo from ::...' in log messages. Got: %v", msgs)
	}
}

// ---------------------------------------------------------------------------
// D10 — Never calls GetRemoteIds on nil
// ---------------------------------------------------------------------------
// Covered by TestImport_MissingSourceMetadataIsNotAnError

// ---------------------------------------------------------------------------
// Test data helpers
// ---------------------------------------------------------------------------

func testdataImportedGolden() []byte {
	// Tab-indented metadata with upstream remote-id.
	return []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
		"<!DOCTYPE pkgmetadata SYSTEM \"https://www.gentoo.org/dtd/metadata.dtd\">\n" +
		"<pkgmetadata>\n" +
		"\t<maintainer type=\"person\">\n" +
		"\t\t<email>web@bennypowers.com</email>\n" +
		"\t\t<name>Benny Powers</name>\n" +
		"\t</maintainer>\n" +
		"\t<upstream>\n" +
		"\t\t<remote-id type=\"github\">owner/repo</remote-id>\n" +
		"\t</upstream>\n" +
		"</pkgmetadata>\n")
}

func testdataImportedNoUpstreamGolden() []byte {
	// Tab-indented metadata without upstream block.
	return []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
		"<!DOCTYPE pkgmetadata SYSTEM \"https://www.gentoo.org/dtd/metadata.dtd\">\n" +
		"<pkgmetadata>\n" +
		"\t<maintainer type=\"person\">\n" +
		"\t\t<email>web@bennypowers.com</email>\n" +
		"\t\t<name>Benny Powers</name>\n" +
		"\t</maintainer>\n" +
		"</pkgmetadata>\n")
}
