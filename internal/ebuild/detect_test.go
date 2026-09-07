package ebuild

import (
	"os"
	"path/filepath"
	"testing"
)

// ---- setupTestEbuild: creates a temporary ebuild file and returns its path ----

func setupTestEbuild(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cat-pkg-1.0.ebuild")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("setupTestEbuild() write error: %v", err)
	}
	return path
}

// ---- TestDetectType_Npm: mirror://npm/ in SRC_URI detects npm ----

func TestDetectType_Npm(t *testing.T) {
	ebuild := `
EAPI=8
SRC_URI="mirror://npm/express/express-4.18.0.tgz"
`
	info := &EbuildInfo{SRC_URI: ebuild}
	typ, err := DetectTypeFromInfo(info)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo() error: %v", err)
	}
	if typ != TypeNPM {
		t.Errorf("DetectTypeFromInfo(npm ebuild) = %q, want %q", typ, TypeNPM)
	}

	// Also test via file path
	path := setupTestEbuild(t, ebuild)
	typ, err = DetectType(path)
	if err != nil {
		t.Fatalf("DetectType(%q) error: %v", path, err)
	}
	if typ != TypeNPM {
		t.Errorf("DetectType(npm file) = %q, want %q", typ, TypeNPM)
	}
}

// ---- TestDetectType_Cargo: CRATES= detects cargo ----

func TestDetectType_Cargo(t *testing.T) {
	ebuild := `
EAPI=8
CRATES="crates.io/aho-corasick/aho_corasick-1.1.2"
SRC_URI="mirror://github/owner/pkg/pkg-1.0.tar.gz"
`
	info := &EbuildInfo{SRC_URI: ebuild}
	typ, err := DetectTypeFromInfo(info)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo() error: %v", err)
	}
	if typ != TypeCargo {
		t.Errorf("DetectTypeFromInfo(cargo ebuild) = %q, want %q", typ, TypeCargo)
	}

	path := setupTestEbuild(t, ebuild)
	typ, err = DetectType(path)
	if err != nil {
		t.Fatalf("DetectType(%q) error: %v", path, err)
	}
	if typ != TypeCargo {
		t.Errorf("DetectType(cargo file) = %q, want %q", typ, TypeCargo)
	}
}

// ---- TestDetectType_Go: go-module inherit detects go ----

func TestDetectType_Go(t *testing.T) {
	ebuild := `
EAPI=8
inherit go-module
SRC_URI="mirror://github/owner/pkg/pkg-1.0.tar.gz"
`
	info := &EbuildInfo{
		SRC_URI:  ebuild,
		Inherit:  []string{"go-module"},
	}
	typ, err := DetectTypeFromInfo(info)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo() error: %v", err)
	}
	if typ != TypeGo {
		t.Errorf("DetectTypeFromInfo(go ebuild) = %q, want %q", typ, TypeGo)
	}

	path := setupTestEbuild(t, ebuild)
	typ, err = DetectType(path)
	if err != nil {
		t.Fatalf("DetectType(%q) error: %v", path, err)
	}
	if typ != TypeGo {
		t.Errorf("DetectType(go file) = %q, want %q", typ, TypeGo)
	}
}

// ---- TestDetectType_RpiKernel: linux-info + _p in PV detects rpi-kernel ----

func TestDetectType_RpiKernel(t *testing.T) {
	ebuild := `
EAPI=8
inherit linux-info
PV="6.1_p20240101"
SRC_URI="mirror://kernel/pub/linux/kernel/v6.x/linux-6.1.tar.xz"
`
	info := &EbuildInfo{
		PVVal:   "6.1_p20240101",
		SRC_URI: ebuild,
		Inherit: []string{"linux-info"},
	}
	typ, err := DetectTypeFromInfo(info)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo() error: %v", err)
	}
	if typ != TypeRpiKernel {
		t.Errorf("DetectTypeFromInfo(rpi-kernel ebuild) = %q, want %q", typ, TypeRpiKernel)
	}

	path := setupTestEbuild(t, ebuild)
	typ, err = DetectType(path)
	if err != nil {
		t.Fatalf("DetectType(%q) error: %v", path, err)
	}
	if typ != TypeRpiKernel {
		t.Errorf("DetectType(rpi-kernel file) = %q, want %q", typ, TypeRpiKernel)
	}
}

// ---- TestDetectType_Generic: everything else is generic ----

func TestDetectType_Generic(t *testing.T) {
	ebuild := `
EAPI=8
SRC_URI="https://example.com/pkg-1.0.tar.gz"
DESCRIPTION="A generic package"
`
	info := &EbuildInfo{SRC_URI: ebuild}
	typ, err := DetectTypeFromInfo(info)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo() error: %v", err)
	}
	if typ != TypeGeneric {
		t.Errorf("DetectTypeFromInfo(generic ebuild) = %q, want %q", typ, TypeGeneric)
	}

	path := setupTestEbuild(t, ebuild)
	typ, err = DetectType(path)
	if err != nil {
		t.Fatalf("DetectType(%q) error: %v", path, err)
	}
	if typ != TypeGeneric {
		t.Errorf("DetectType(generic file) = %q, want %q", typ, TypeGeneric)
	}
}

// ---- TestDetectType_Force: force overrides auto-detection ----

func TestDetectType_Force(t *testing.T) {
	ebuild := `
EAPI=8
SRC_URI="mirror://npm/express/express-4.18.0.tgz"
CRATES="crates.io/foo/foo-1.0"
`
	// Force go type on an npm ebuild
	typ, err := DetectTypeForce(ebuild, TypeGo)
	if err != nil {
		t.Fatalf("DetectTypeForce() error: %v", err)
	}
	if typ != TypeGo {
		t.Errorf("DetectTypeForce(npm, go) = %q, want %q", typ, TypeGo)
	}

	// Test via file
	path := setupTestEbuild(t, ebuild)
	typ, err = DetectTypeForce(path, TypeGeneric)
	if err != nil {
		t.Fatalf("DetectTypeForce() error: %v", err)
	}
	if typ != TypeGeneric {
		t.Errorf("DetectTypeForce(file, generic) = %q, want %q", typ, TypeGeneric)
	}
}

// ---- TestDetectType_Force_Nil: nil force defaults to auto ----

func TestDetectType_ForceNil(t *testing.T) {
	info := &EbuildInfo{SRC_URI: "https://example.com/pkg-1.0.tar.gz"}
	typ, err := DetectTypeFromInfoForce(info, "")
	if err != nil {
		t.Fatalf("DetectTypeFromInfoForce() error: %v", err)
	}
	if typ != TypeGeneric {
		t.Errorf("DetectTypeFromInfoForce(info, \"\") = %q, want %q", typ, TypeGeneric)
	}
}

// ---- TestDetectTypeFromInfo_Nil: nil info returns generic ----

func TestDetectTypeFromInfo_Nil(t *testing.T) {
	typ, err := DetectTypeFromInfo(nil)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo(nil) error: %v", err)
	}
	if typ != TypeGeneric {
		t.Errorf("DetectTypeFromInfo(nil) = %q, want %q", typ, TypeGeneric)
	}
}

// ---- TestDetectType_NonExistentFile: returns error for missing file ----

func TestDetectType_NonExistentFile(t *testing.T) {
	_, err := DetectType("/nonexistent/cat/pkg-1.0.ebuild")
	if err == nil {
		t.Fatal("DetectType(non-existent) should return error")
	}
}

// ---- TestDetectType_GoModuleInheritLine: go-module in Inherit slice ----

func TestDetectType_GoModuleInheritLine(t *testing.T) {
	info := &EbuildInfo{
		SRC_URI:  "mirror://github/owner/pkg/pkg-1.0.tar.gz",
		Inherit:  []string{"go-module", "nodejs"},
	}
	typ, err := DetectTypeFromInfo(info)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo() error: %v", err)
	}
	if typ != TypeGo {
		t.Errorf("DetectTypeFromInfo(go-module inherit) = %q, want %q", typ, TypeGo)
	}
}

// ---- TestDetectType_InheritFromInfo: go-module in Inherit slice ----

func TestDetectType_InheritFromInfo(t *testing.T) {
	info := &EbuildInfo{
		SRC_URI:  "https://example.com/pkg.tar.gz",
		Inherit:  []string{"go-module", "python-r1"},
	}
	typ, err := DetectTypeFromInfo(info)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo() error: %v", err)
	}
	if typ != TypeGo {
		t.Errorf("= %q, want %q", typ, TypeGo)
	}
}

// ---- TestDetectType_Priority: npm takes precedence over other checks ----

func TestDetectType_Priority(t *testing.T) {
	// npm with CRATES= should still detect as npm (SRC_URI check first)
	ebuild := `
EAPI=8
CRATES="crates.io/foo/foo-1.0"
SRC_URI="mirror://npm/pkg/pkg-1.0.tgz"
`
	info := &EbuildInfo{SRC_URI: ebuild}
	typ, err := DetectTypeFromInfo(info)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo() error: %v", err)
	}
	// npm is checked first (mirror://npm/)
	if typ != TypeNPM {
		t.Errorf("= %q, want %q (npm takes precedence over cargo)", typ, TypeNPM)
	}
}

// ---- TestDetectType_NpmInInherit: go-module in INHERITED line ----

func TestDetectType_NpmInInherit(t *testing.T) {
	// When INHERITED contains go-module, it should be detected as go
	ebuild := `
EAPI=8
INHERITED="go-module cargo"
SRC_URI="https://example.com/pkg-1.0.tar.gz"
`
	info := &EbuildInfo{
		SRC_URI:  ebuild,
		Inherit:  []string{"go-module", "cargo"},
	}
	typ, err := DetectTypeFromInfo(info)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo() error: %v", err)
	}
	if typ != TypeGo {
		t.Errorf("= %q, want %q", typ, TypeGo)
	}
}

// ---- TestDetectType_RpiNoLinuxInfo: linux-info not inherited ----

func TestDetectType_RpiNoLinuxInfo(t *testing.T) {
	// PV has _p but no linux-info inherit → should be generic
	ebuild := `
EAPI=8
SRC_URI="https://example.com/pkg.tar.gz"
`
	info := &EbuildInfo{
		PVVal:   "6.1_p20240101",
		SRC_URI: ebuild,
		Inherit: []string{"base-utils"}, // not linux-info
	}
	typ, err := DetectTypeFromInfo(info)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo() error: %v", err)
	}
	// Without linux-info, _p doesn't trigger rpi-kernel detection
	if typ != TypeGeneric {
		t.Errorf("= %q, want %q (no linux-info inherit)", typ, TypeGeneric)
	}
}

// ---- TestDetectType_RpiNoP: linux-info but no _p in PV ----

func TestDetectType_RpiNoP(t *testing.T) {
	// linux-info but PV has no _p → should be generic
	ebuild := `
EAPI=8
SRC_URI="https://example.com/pkg.tar.gz"
`
	info := &EbuildInfo{
		PVVal:   "6.1.0",
		SRC_URI: ebuild,
		Inherit: []string{"linux-info"},
	}
	typ, err := DetectTypeFromInfo(info)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo() error: %v", err)
	}
	if typ != TypeGeneric {
		t.Errorf("= %q, want %q (no _p in PV)", typ, TypeGeneric)
	}
}

// ---- TestDetectType_MultiInherit: multiple eclasses ----

func TestDetectType_MultiInherit(t *testing.T) {
	ebuild := `
EAPI=8
inherit go-module cargo npm
SRC_URI="https://example.com/pkg.tar.gz"
`
	info := &EbuildInfo{
		SRC_URI:  ebuild,
		Inherit:  []string{"go-module", "cargo", "npm"},
	}
	typ, err := DetectTypeFromInfo(info)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo() error: %v", err)
	}
	// go-module triggers go detection
	if typ != TypeGo {
		t.Errorf("= %q, want %q", typ, TypeGo)
	}
}
