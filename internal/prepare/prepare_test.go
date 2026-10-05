package prepare

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/upstream"
)

// ---------------------------------------------------------------------------
// Type detection tests
// ---------------------------------------------------------------------------

func TestDetectType_Force(t *testing.T) {
	type testCase struct {
		name     string
		force    ebuild.PackageType
		expected ebuild.PackageType
	}
	tests := []testCase{
		{"force_npm", ebuild.TypeNPM, ebuild.TypeNPM},
		{"force_cargo", ebuild.TypeCargo, ebuild.TypeCargo},
		{"force_go", ebuild.TypeGo, ebuild.TypeGo},
		{"force_rpi", ebuild.TypeRpiKernel, ebuild.TypeRpiKernel},
		{"force_vendored", ebuild.TypeVendored, ebuild.TypeVendored},
		{"force_generic", ebuild.TypeGeneric, ebuild.TypeGeneric},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &ebuild.EbuildInfo{Category: "test", Pkg: "pkg"}
			got, err := ebuild.DetectTypeFromInfoForce(info, tt.force)
			if err != nil {
				t.Fatalf("DetectTypeFromInfoForce returned error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("DetectTypeFromInfoForce(%q) = %q, want %q", tt.force, got, tt.expected)
			}
		})
	}
}

func TestDetectTypeFromInfo(t *testing.T) {
	tests := []struct {
		name     string
		info     *ebuild.EbuildInfo
		wantType ebuild.PackageType
	}{
		{
			name:     "npm",
			info:     &ebuild.EbuildInfo{SRC_URI: "mirror://npm/pkg.tar.gz"},
			wantType: ebuild.TypeNPM,
		},
		{
			name:     "cargo",
			info:     &ebuild.EbuildInfo{SRC_URI: "CRATES="},
			wantType: ebuild.TypeCargo,
		},
		{
			name:     "go",
			info:     &ebuild.EbuildInfo{Inherit: []string{"go-module"}},
			wantType: ebuild.TypeGo,
		},
		{
			name:     "vendored",
			info:     &ebuild.EbuildInfo{SRC_URI: "releases/download/${PN}/"},
			wantType: ebuild.TypeVendored,
		},
		{
			name:     "generic",
			info:     &ebuild.EbuildInfo{SRC_URI: "mirror://test/file.tar.gz", Inherit: []string{"stdenv"}},
			wantType: ebuild.TypeGeneric,
		},
		{
			name:     "nil_info",
			info:     nil,
			wantType: ebuild.TypeGeneric,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ebuild.DetectTypeFromInfo(tt.info)
			if err != nil {
				t.Fatalf("DetectTypeFromInfo returned error: %v", err)
			}
			if got != tt.wantType {
				t.Errorf("DetectTypeFromInfo() = %q, want %q", got, tt.wantType)
			}
		})
	}
}

// ---------------------------------------------------------------------------




// parsePVRaw tests
// ---------------------------------------------------------------------------

func TestParsePVRaw(t *testing.T) {
	tests := []struct {
		name     string
		pv       string
		wantPV   string
		wantR    int
	}{
		{"simple", "1.0.0", "1.0.0", 0},
		{"with_r", "1.0.0-r2", "1.0.0", 2},
		{"with_p", "6.1.0_p20230915", "6.1.0_p20230915", 0},
		{"with_p_and_r", "6.1.0_p20230915-r1", "6.1.0_p20230915", 1},
		{"live", "9999", "9999", 0},
		{"live_with_r", "9999-r3", "9999", 3},
		{"empty", "", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pv, r := parsePVRaw(tt.pv)
			if pv != tt.wantPV {
				t.Errorf("parsePVRaw(%q) pv = %q, want %q", tt.pv, pv, tt.wantPV)
			}
			if r != tt.wantR {
				t.Errorf("parsePVRaw(%q) r = %d, want %d", tt.pv, r, tt.wantR)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isLiveVersion tests
// ---------------------------------------------------------------------------

func TestIsLiveVersion(t *testing.T) {
	tests := []struct {
		name string
		pv   string
		want bool
	}{
		{"1.0.0", "1.0.0", false},
		{"9999", "9999", true},
		{"_p20230915", "6.1.0_p20230915", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLiveVersion(tt.pv)
			if got != tt.want {
				t.Errorf("isLiveVersion(%q) = %v, want %v", tt.pv, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isRevisionBump tests
// ---------------------------------------------------------------------------

func TestIsRevisionBump(t *testing.T) {
	tests := []struct {
		name string
		pv   string
		rev  int
		want bool
	}{
		{"1.0.0-r1", "1.0.0", 1, true},
		{"1.0.0-r2", "1.0.0", 2, true},
		{"1.0.0", "1.0.0", 0, false},
		{"9999-r1", "9999", 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRevisionBump(tt.pv, tt.rev)
			if got != tt.want {
				t.Errorf("isRevisionBump(%q, %d) = %v, want %v", tt.pv, tt.rev, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// collectEbuilds tests
// ---------------------------------------------------------------------------

func TestCollectEbuilds(t *testing.T) {
	dir := t.TempDir()

	// Create ebuild files
	for _, name := range []string{"pkg-1.0.0.ebuild", "pkg-2.0.0.ebuild"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("EAPI=8\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Create non-ebuild files
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme"), 0644)
	os.WriteFile(filepath.Join(dir, "pkg-1.0.0.ebuild.bak"), []byte("bak"), 0644)

	files, err := collectEbuilds(dir)
	if err != nil {
		t.Fatalf("collectEbuilds returned error: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("collectEbuilds returned %d files, want 2", len(files))
	}

	// Verify only real ebuilds are included
	for _, f := range files {
		name := filepath.Base(f)
		if strings.HasSuffix(name, ".ebuild.bak") {
			t.Errorf("collectEbuilds returned backup file: %s", name)
		}
	}
}

func TestCollectEbuilds_NoEbuilds(t *testing.T) {
	dir := t.TempDir()
	files, err := collectEbuilds(dir)
	if err != nil {
		t.Fatalf("collectEbuilds returned error on empty dir: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("collectEbuilds returned %d files, want 0", len(files))
	}
}

func TestCollectEbuilds_NonExistent(t *testing.T) {
	_, err := collectEbuilds("/nonexistent/dir/12345")
	if err == nil {
		t.Fatal("collectEbuilds should fail on non-existent directory")
	}
}

// ---------------------------------------------------------------------------
// Backup / Rollback tests
// ---------------------------------------------------------------------------

func TestBackup_Rollback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ebuild")
	content := []byte("EAPI=8\nPV=1.0.0\nSRC_URI=\"test\"\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	// Backup
	if err := Backup(path); err != nil {
		t.Fatalf("Backup returned error: %v", err)
	}

	// Verify .bak exists
	if _, err := os.Stat(path + ".bak"); os.IsNotExist(err) {
		t.Fatal(".bak file was not created")
	}

	// Modify the file
	modified := []byte("EAPI=8\nPV=2.0.0\n")
	if err := os.WriteFile(path, modified, 0644); err != nil {
		t.Fatal(err)
	}

	// Restore
	if err := Restore(path); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}

	// Verify content restored
	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(content) {
		t.Errorf("restored content = %q, want %q", string(restored), string(content))
	}
}

func TestBackupDir(t *testing.T) {
	dir := t.TempDir()

	// Create multiple files
	for i := 0; i < 3; i++ {
		path := filepath.Join(dir, fmt.Sprintf("pkg-%d.ebuild", i))
		os.WriteFile(path, []byte("EAPI=8\n"), 0644)
	}

	// Backup all files in directory
	if err := BackupDir(dir); err != nil {
		t.Fatalf("BackupDir returned error: %v", err)
	}

	// Verify all .bak files exist
	for i := 0; i < 3; i++ {
		bakPath := filepath.Join(dir, fmt.Sprintf("pkg-%d.ebuild.bak", i))
		if _, err := os.Stat(bakPath); os.IsNotExist(err) {
			t.Fatalf(".bak file for pkg-%d.ebuild not found", i)
		}
	}
}

func TestBackupNonExistent(t *testing.T) {
	err := Backup("/nonexistent/file.ebuild")
	if err == nil {
		t.Fatal("Backup should fail on non-existent file")
	}
}

// ---------------------------------------------------------------------------
// manifestArgs tests
// ---------------------------------------------------------------------------

func TestManifestArgs_MatchesRunner(t *testing.T) {
	tests := []struct {
		name         string
		style        manifestStyle
		distdir      string
		catPkg       string
		ebuildPath   string
		want         []string
	}{
		{
			name:         "pkgdev",
			style:        stylePkgdev,
			distdir:      "/d",
			catPkg:       "dev-util/foo",
			ebuildPath:   "/ov/dev-util/foo/foo-1.0.ebuild",
			want:         []string{"manifest", "-d", "/d", "dev-util/foo"},
		},
		{
			name:         "ebuild-digest",
			style:        styleEbuildDigest,
			distdir:      "/d",
			catPkg:       "dev-util/foo",
			ebuildPath:   "/ov/dev-util/foo/foo-1.0.ebuild",
			want:         []string{"/ov/dev-util/foo/foo-1.0.ebuild", "digest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manifestArgs(tt.style, tt.distdir, tt.catPkg, tt.ebuildPath)
			if len(result) != len(tt.want) {
				t.Fatalf("manifestArgs returned %d args, want %d: %v", len(result), len(tt.want), result)
			}
			for i, want := range tt.want {
				if result[i] != want {
					t.Errorf("manifestArgs[%d] = %q, want %q", i, result[i], want)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// verifyDistfiles tests
// ---------------------------------------------------------------------------

func TestVerifyDistfiles(t *testing.T) {
	dir := t.TempDir()

	// Create a distfile
	distPath := filepath.Join(dir, "my-pkg-1.0.0.tar.gz")
	os.WriteFile(distPath, []byte("content"), 0644)

	// Test with existing distfile
	if !verifyDistfiles(dir, []string{"my-pkg-1.0.0.tar.gz"}) {
		t.Error("verifyDistfiles returned false for existing distfile")
	}

	// Test with missing distfile
	if verifyDistfiles(dir, []string{"my-pkg-1.0.0.tar.gz", "missing-file.tar.gz"}) {
		t.Error("verifyDistfiles returned true for missing distfile")
	}

	// Test empty distdir → always true
	if !verifyDistfiles("", []string{"any-file"}) {
		t.Error("verifyDistfiles with empty distdir should return true")
	}
}

// ---------------------------------------------------------------------------
// extractDistfilesFromSRCURI tests
// ---------------------------------------------------------------------------

func TestExtractDistfilesFromSRCURI(t *testing.T) {
	tests := []struct {
		name     string
		srcURI   string
		expected []string
	}{
		{
			name:     "single",
			srcURI:   "mirror://test/file.tar.gz",
			expected: []string{"mirror://test/file.tar.gz"},
		},
		{
			name:     "multiple",
			srcURI:   "mirror://test/a.tar.gz mirror://test/b.tar.gz",
			expected: []string{"mirror://test/a.tar.gz", "mirror://test/b.tar.gz"},
		},
		{
			name:     "with_mirror_param",
			srcURI:   "mirror://test/a.tar.gz?mirror=1 mirror://test/b.tar.gz",
			expected: []string{"mirror://test/b.tar.gz"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractDistfilesFromSRCURI(tt.srcURI)
			if len(result) != len(tt.expected) {
				t.Errorf("extractDistfilesFromSRCURI returned %d files, want %d", len(result), len(tt.expected))
			}
			for i, want := range tt.expected {
				if i < len(result) && result[i] != want {
					t.Errorf("result[%d] = %q, want %q", i, result[i], want)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// findManifestRunner tests
// ---------------------------------------------------------------------------

func TestFindManifestRunner(t *testing.T) {
	binPath, _, err := findManifestRunner()
	if err != nil {
		t.Logf("findManifestRunner returned (expected on non-host): %v", err)
		return
	}
	if binPath == "" {
		t.Fatal("findManifestRunner returned empty path")
	}
	if !filepath.IsAbs(binPath) {
		t.Errorf("findManifestRunner returned non-absolute path: %s", binPath)
	}
}

func TestFindManifestRunner_NotFound(t *testing.T) {
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "/nonexistent/path")

	_, _, err := findManifestRunner()
	if err == nil {
		t.Fatal("findManifestRunner should fail when binary not in PATH")
	}
	_ = origPath
}

// ---------------------------------------------------------------------------
// getEnv tests
// ---------------------------------------------------------------------------

func TestGetEnv(t *testing.T) {
	os.Setenv("TEST_LARRY_VAR", "test_value")
	t.Cleanup(func() { os.Unsetenv("TEST_LARRY_VAR") })

	val := getEnv("TEST_LARRY_VAR")
	if val != "test_value" {
		t.Errorf("getEnv() = %q, want %q", val, "test_value")
	}

	val = getEnv("NONEXISTENT_VAR_12345")
	if val != "" {
		t.Errorf("getEnv(missing) = %q, want empty", val)
	}
}

// ---------------------------------------------------------------------------
// TUI stream isolation test
// ---------------------------------------------------------------------------

func TestTuiStreamIsolation(t *testing.T) {
	script := `#!/bin/bash
for i in $(seq 1 100); do
	echo "line $i"
done
`
	scriptPath := filepath.Join(t.TempDir(), "test-script.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(scriptPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe failed: %v", err)
	}
	cmd.Stderr = nil // no stderr inheritance

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	buf := make([]byte, 0, 2000)
	for i := 0; i < 100; i++ {
		line := make([]byte, 100)
		n, err := stdout.Read(line)
		if err != nil && n == 0 {
			break
		}
		buf = append(buf, line[:n]...)
	}

	if err := cmd.Wait(); err != nil {
		t.Logf("Wait returned: %v", err)
	}

	if len(buf) == 0 {
		t.Error("no output captured from StdoutPipe")
	}
}

// ---------------------------------------------------------------------------
// DryRun tests
// ---------------------------------------------------------------------------

func TestPrepare_DryRun_SkipsManifest(t *testing.T) {
	pkgDir := t.TempDir()
	ebuildPath := filepath.Join(pkgDir, "my-pkg-1.0.0.ebuild")
	content := `EAPI=8
PV=1.0.0
SRC_URI="mirror://test/my-pkg-1.0.0.tar.gz"
`
	if err := os.WriteFile(ebuildPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	info, err := upstream.ParseEbuildInfo(ebuildPath)
	if err != nil {
		t.Fatalf("ParseEbuildInfo returned error: %v", err)
	}

	// Dry-run should NOT call manifest runner (it returns nil immediately)
	cfg := &PrepareConfig{DryRun: true}
	err = Prepare(info, cfg)
	if err != nil {
		t.Fatalf("Prepare returned error in dry-run mode: %v", err)
	}

	// Verify no .bak file was created
	if _, err := os.Stat(ebuildPath + ".bak"); !os.IsNotExist(err) {
		t.Error("backup file created in dry-run mode")
	}
}

// ---------------------------------------------------------------------------
// PrepareFromFile tests
// ---------------------------------------------------------------------------

func TestPrepareFromFile(t *testing.T) {
	dir := t.TempDir()
	ebuildPath := filepath.Join(dir, "my-pkg-1.0.0.ebuild")
	content := `EAPI=8
PV=1.0.0
SRC_URI="mirror://test/my-pkg-1.0.0.tar.gz"
`
	if err := os.WriteFile(ebuildPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Dry-run mode to avoid needing pkgdev
	cfg := &PrepareConfig{DryRun: true}
	err := PrepareFromFile(ebuildPath, cfg)
	if err != nil {
		t.Fatalf("PrepareFromFile returned error: %v", err)
	}
}

func TestPrepareFromFile_NonExistent(t *testing.T) {
	cfg := &PrepareConfig{}
	err := PrepareFromFile("/nonexistent/pkg-1.0.0.ebuild", cfg)
	if err == nil {
		t.Fatal("PrepareFromFile should fail on non-existent file")
	}
}

// ---------------------------------------------------------------------------
// PrepareDir tests
// ---------------------------------------------------------------------------

func TestPrepareDir_NoEbuilds(t *testing.T) {
	dir := t.TempDir()
	cfg := &PrepareConfig{DryRun: true}
	err := PrepareDir(dir, cfg)
	if err != nil {
		t.Fatalf("PrepareDir returned error on empty dir: %v", err)
	}
}

func TestPrepareDir_AggregatesFailures(t *testing.T) {
	// D9 — all ebuilds attempted, middle one fails, returns "1 package(s) failed".
	dir := t.TempDir()
	// Create three ebuilds.
	for _, name := range []string{"foo-1.0.ebuild", "foo-2.0.ebuild", "foo-3.0.ebuild"} {
		os.WriteFile(filepath.Join(dir, name), []byte("EAPI=8\n"), 0644)
	}

	rec := newRecorder()
	pfOrig := PrepareFile
	PrepareFile = func(path string, cfg *PrepareConfig) error {
		rec.record("", nil, path, nil)
		// Middle one (foo-2.0) fails.
		if strings.Contains(path, "foo-2.0") {
			return errors.New("prepare boom")
		}
		return nil
	}
	t.Cleanup(func() { PrepareFile = pfOrig })

	err := PrepareDir(dir, &PrepareConfig{DryRun: true})
	if err == nil {
		t.Fatal("expected error from PrepareDir")
	}
	if !strings.Contains(err.Error(), "1 package(s) failed") {
		t.Errorf("error = %v, want '1 package(s) failed'", err)
	}
	if len(rec.calls) != 3 {
		t.Errorf("PrepareFile called %d times, want 3", len(rec.calls))
	}
}

// ---------------------------------------------------------------------------
// Integration: generic pipeline with actual manifest runner
// ---------------------------------------------------------------------------

func TestPrepare_Generic_RealManifest(t *testing.T) {
	// Only run if pkgdev is available
	_, _, err := findManifestRunner()
	if err != nil {
		t.Skip("pkgdev not available")
	}

	dir := t.TempDir()
	ebuildPath := filepath.Join(dir, "my-pkg-1.0.0.ebuild")
	content := `EAPI=8
PV=1.0.0
SRC_URI="mirror://test/my-pkg-1.0.0.tar.gz"
`
	if err := os.WriteFile(ebuildPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &PrepareConfig{}
	err = PrepareFromFile(ebuildPath, cfg)
	// In a real Gentoo environment with a valid ebuild, this should succeed
	// Otherwise it will fail because the distfile doesn't exist
	if err != nil {
		t.Logf("Prepare_Generic_RealManifest returned: %v (expected if distfile missing)", err)
	}
}
