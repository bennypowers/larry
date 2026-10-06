package importcmd

import (
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
		SourceOverlay: filepath.Join(reposRoot, "guru"),
		TargetOverlay: targetOverlay,
		DryRun:        false,
		ReposRoot:     reposRoot,
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
		SourceOverlay: "", // empty → auto-search
		TargetOverlay: targetOverlay,
		ReposRoot:     reposRoot,
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
		SourceOverlay: "",
		TargetOverlay: targetOverlay,
		ReposRoot:     reposRoot,
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
		SourceOverlay: "guru",
		TargetOverlay: targetOverlay,
		ReposRoot:     reposRoot,
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
		SourceOverlay: filepath.Join(reposRoot, "guru"),
		TargetOverlay: targetOverlay,
		ReposRoot:     reposRoot,
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
		SourceOverlay: "nosuch",
		TargetOverlay: targetOverlay,
		ReposRoot:     reposRoot,
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
		SourceOverlay: filepath.Join(reposRoot, "guru"),
		TargetOverlay: targetOverlay,
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
		SourceOverlay: filepath.Join(reposRoot, "guru"),
		TargetOverlay: targetOverlay,
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
		SourceOverlay: filepath.Join(reposRoot, "guru"),
		TargetOverlay: targetOverlay,
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
		SourceOverlay: filepath.Join(reposRoot, "guru"),
		TargetOverlay: targetOverlay,
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
		SourceOverlay: filepath.Join(reposRoot, "guru"),
		TargetOverlay: targetOverlay,
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
		SourceOverlay: srcDir,
		TargetOverlay: targetOverlay,
		Force:         false,
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
		SourceOverlay: srcDir,
		TargetOverlay: targetOverlay,
		Force:         true,
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
		SourceOverlay: srcDir,
		TargetOverlay: targetOverlay,
		Force:         false,
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
		SourceOverlay: filepath.Join(reposRoot, "guru"),
		TargetOverlay: targetOverlay,
		DryRun:        true,
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
		SourceOverlay: srcDir,
		TargetOverlay: targetOverlay,
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
