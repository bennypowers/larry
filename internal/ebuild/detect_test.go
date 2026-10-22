package ebuild

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "ebuilds", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// buildInfo builds an *EbuildInfo from raw ebuild text for the FromInfo
// variant of the tests.
func buildInfo(raw string) *EbuildInfo {
	return &EbuildInfo{
		Raw:     raw,
		SRC_URI: extractVar(raw, "SRC_URI"),
		Inherit: ParseInherit(raw),
	}
}

// runDetect runs both DetectType (via a temp file) and DetectTypeFromInfo
// on the same raw content and returns the pair of results.
func runDetect(t *testing.T, raw string) (fromPath, fromInfo PackageType) {
	t.Helper()

	// Write to a temp file for DetectType.
	dir := t.TempDir()
	p := filepath.Join(dir, "test-ebuild")
	if err := os.WriteFile(p, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	pathType, err := DetectType(p)
	if err != nil {
		t.Fatalf("DetectType: %v", err)
	}

	infoType, err := DetectTypeFromInfo(buildInfo(raw))
	if err != nil {
		t.Fatalf("DetectTypeFromInfo: %v", err)
	}

	return pathType, infoType
}

// --- Individual detection tests ---

func TestDetectType_NpmWithVendoredDepsURL(t *testing.T) {
	// T1: npm ebuild that also contains releases/download/${PN}/ must be NPM.
	raw := fixture(t, "npm-with-deps-release.ebuild")
	pathType, infoType := runDetect(t, raw)
	if pathType != TypeNPM {
		t.Errorf("DetectType(NPM fixture) = %q, want %q", pathType, TypeNPM)
	}
	if infoType != TypeNPM {
		t.Errorf("DetectTypeFromInfo(NPM fixture) = %q, want %q", infoType, TypeNPM)
	}
}

func TestDetectType_NpmDirectRegistryURL(t *testing.T) {
	// Overlay ebuilds moved from mirror://npm/ to direct
	// registry.npmjs.org URLs; those must still detect as NPM.
	raw := fixture(t, "npm-direct-registry.ebuild")
	pathType, infoType := runDetect(t, raw)
	if pathType != TypeNPM {
		t.Errorf("DetectType(direct-registry fixture) = %q, want %q", pathType, TypeNPM)
	}
	if infoType != TypeNPM {
		t.Errorf("DetectTypeFromInfo(direct-registry fixture) = %q, want %q", infoType, TypeNPM)
	}
}

func TestDetectType_CratesLiteral(t *testing.T) {
	// T2: literal CRATES= at column 0 should detect cargo.
	raw := fixture(t, "cargo-literal-crates.ebuild")
	pathType, _ := runDetect(t, raw)
	if pathType != TypeCargo {
		t.Errorf("DetectType = %q, want %q", pathType, TypeCargo)
	}
}

func TestDetectType_CargoEclassNoCrates(t *testing.T) {
	raw := fixture(t, "cargo-no-crates.ebuild")
	pathType, infoType := runDetect(t, raw)
	if pathType != TypeCargo {
		t.Errorf("DetectType = %q, want %q", pathType, TypeCargo)
	}
	if infoType != TypeCargo {
		t.Errorf("DetectTypeFromInfo = %q, want %q", infoType, TypeCargo)
	}
}

func TestDetectType_GoModule(t *testing.T) {
	// T3: inherit go-module → go.
	raw := fixture(t, "go-module.ebuild")
	pathType, infoType := runDetect(t, raw)
	if pathType != TypeGo {
		t.Errorf("DetectType = %q, want %q", pathType, TypeGo)
	}
	if infoType != TypeGo {
		t.Errorf("DetectTypeFromInfo = %q, want %q", infoType, TypeGo)
	}
}

func TestDetectType_GoModuleContinuation(t *testing.T) {
	// T3: backslash-joined inherit go-module → go.
	raw := fixture(t, "go-module-continuation.ebuild")
	pathType, infoType := runDetect(t, raw)
	if pathType != TypeGo {
		t.Errorf("DetectType = %q, want %q", pathType, TypeGo)
	}
	if infoType != TypeGo {
		t.Errorf("DetectTypeFromInfo = %q, want %q", infoType, TypeGo)
	}
}

func TestDetectType_InheritedLineIsNotGo(t *testing.T) {
	// T3: INHERITED= alone is not go-module.
	raw := fixture(t, "inherited-line.ebuild")
	pathType, infoType := runDetect(t, raw)
	if pathType != TypeGeneric {
		t.Errorf("DetectType = %q, want %q", pathType, TypeGeneric)
	}
	if infoType != TypeGeneric {
		t.Errorf("DetectTypeFromInfo = %q, want %q", infoType, TypeGeneric)
	}
}

func TestDetectType_Vendored(t *testing.T) {
	raw := fixture(t, "vendored.ebuild")
	pathType, infoType := runDetect(t, raw)
	if pathType != TypeVendored {
		t.Errorf("DetectType = %q, want %q", pathType, TypeVendored)
	}
	if infoType != TypeVendored {
		t.Errorf("DetectTypeFromInfo = %q, want %q", infoType, TypeVendored)
	}
}

func TestDetectType_UpstreamReleaseTarballIsGeneric(t *testing.T) {
	// T7: cockpit has releases/download/${PV}/${P} where ${PN}/ is the
	// owner path segment, not the release tag. Should be generic.
	raw := fixture(t, "upstream-release.ebuild")
	pathType, infoType := runDetect(t, raw)
	if pathType != TypeGeneric {
		t.Errorf("DetectType = %q, want %q", pathType, TypeGeneric)
	}
	if infoType != TypeGeneric {
		t.Errorf("DetectTypeFromInfo = %q, want %q", infoType, TypeGeneric)
	}
}

func TestDetectType_Generic(t *testing.T) {
	raw := fixture(t, "generic.ebuild")
	pathType, infoType := runDetect(t, raw)
	if pathType != TypeGeneric {
		t.Errorf("DetectType = %q, want %q", pathType, TypeGeneric)
	}
	if infoType != TypeGeneric {
		t.Errorf("DetectTypeFromInfo = %q, want %q", infoType, TypeGeneric)
	}
}

// --- Agreement tests ---

func TestDetectType_AgreesWithInfoVariant(t *testing.T) {
	// T1, T5: both entry points must agree for every fixture.
	names := []string{
		"npm-with-deps-release.ebuild",
		"npm-direct-registry.ebuild",
		"cargo-literal-crates.ebuild",
		"cargo-no-crates.ebuild",
		"go-module.ebuild",
		"go-module-continuation.ebuild",
		"inherited-line.ebuild",
		"vendored.ebuild",
		"upstream-release.ebuild",
		"generic.ebuild",
	}

	for _, name := range names {
		raw := fixture(t, name)
		fromPath, fromInfo := runDetect(t, raw)
		if fromPath != fromInfo {
			t.Errorf("%s: DetectType=%q != DetectTypeFromInfo=%q",
				name, fromPath, fromInfo)
		}
	}
}

// --- Force tests ---

func TestDetectType_ForceWins(t *testing.T) {
	// Force overrides detection for both entry points.
	raw := fixture(t, "generic.ebuild")

	forceType, err := DetectTypeForce("", TypeNPM)
	if err != nil {
		t.Fatalf("DetectTypeForce: %v", err)
	}
	if forceType != TypeNPM {
		t.Errorf("DetectTypeForce(generic, NPM) = %q, want %q", forceType, TypeNPM)
	}

	forceInfoType, err := DetectTypeFromInfoForce(buildInfo(raw), TypeNPM)
	if err != nil {
		t.Fatalf("DetectTypeFromInfoForce: %v", err)
	}
	if forceInfoType != TypeNPM {
		t.Errorf("DetectTypeFromInfoForce(generic, NPM) = %q, want %q",
			forceInfoType, TypeNPM)
	}
}

// --- Nil info test ---

func TestDetectType_NilInfo(t *testing.T) {
	// info == nil → TypeGeneric, nil.
	t.Run("path", func(t *testing.T) {
		_, err := DetectType("/nonexistent/path.ebuild")
		if err == nil {
			t.Error("DetectType on nonexistent file should error")
		}
	})
	t.Run("info", func(t *testing.T) {
		got, err := DetectTypeFromInfo(nil)
		if err != nil {
			t.Fatalf("DetectTypeFromInfo(nil) error = %v", err)
		}
		if got != TypeGeneric {
			t.Errorf("DetectTypeFromInfo(nil) = %q, want %q", got, TypeGeneric)
		}
	})
}

// --- rpi-kernel removed tests (plan 25a) ---

func TestDetectType_RpiKernelEbuildIsGeneric(t *testing.T) {
	// A2: linux-info + _p version must NOT detect rpi-kernel; it is generic.
	info := &EbuildInfo{Inherit: []string{"linux-info"}, PVVal: "6.18.46_p20260824", Pkg: "raspberrypi-kernel"}
	got, err := DetectTypeFromInfo(info)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo error: %v", err)
	}
	if got != TypeGeneric {
		t.Errorf("DetectTypeFromInfo(rpi-kernel fixture) = %q, want %q", got, TypeGeneric)
	}
}

func TestDetectType_LinuxInfoPreReleaseIsGeneric(t *testing.T) {
	// A2: linux-info + _pre version must NOT detect rpi-kernel; it is generic.
	info := &EbuildInfo{Inherit: []string{"linux-info"}, PVVal: "6.18.0_pre1"}
	got, err := DetectTypeFromInfo(info)
	if err != nil {
		t.Fatalf("DetectTypeFromInfo error: %v", err)
	}
	if got != TypeGeneric {
		t.Errorf("DetectTypeFromInfo(linux-info_pre) = %q, want %q", got, TypeGeneric)
	}
}

func TestParsePackageType_RejectsRpiKernel(t *testing.T) {
	// A3: rpi-kernel must be rejected; error must point to .larry/prepare.sh.
	_, err := ParsePackageType("rpi-kernel")
	if err == nil {
		t.Fatal("ParsePackageType(\"rpi-kernel\") expected error, got nil")
	}
	if !strings.Contains(err.Error(), ".larry/prepare.sh") {
		t.Errorf("error = %q, want substring \".larry/prepare.sh\"", err.Error())
	}
}

func TestParsePackageType_AcceptsTheFive(t *testing.T) {
	// A3: the five accepted types must still work.
	for _, pt := range []PackageType{TypeNPM, TypeCargo, TypeGo, TypeVendored, TypeGeneric} {
		got, err := ParsePackageType(string(pt))
		if err != nil {
			t.Errorf("ParsePackageType(%q) unexpected error: %v", pt, err)
			continue
		}
		if got != pt {
			t.Errorf("ParsePackageType(%q) = %q, want %q", pt, got, pt)
		}
	}
}

// --- Legacy ParsePackageType tests (rpi-kernel removed) ---

func TestParsePackageType_Legacy(t *testing.T) {
	tests := []struct {
		input    string
		want     PackageType
		wantErr  bool
		errMatch string // substring expected in error message
	}{
		{"npm", TypeNPM, false, ""},
		{"cargo", TypeCargo, false, ""},
		{"go", TypeGo, false, ""},
		{"vendored", TypeVendored, false, ""},
		{"generic", TypeGeneric, false, ""},
		{"", TypeUnknown, false, ""},
		{"npmm", "", true, "unknown type \"npmm\": valid types are npm, cargo, go, vendored, generic"},
		{"NPM", "", true, "unknown type \"NPM\""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParsePackageType(tt.input)
			if (tt.wantErr) && err == nil {
				t.Errorf("ParsePackageType(%q) expected error, got nil", tt.input)
				return
			}
			if (!tt.wantErr) && err != nil {
				t.Errorf("ParsePackageType(%q) unexpected error: %v", tt.input, err)
				return
			}
			if tt.wantErr && err != nil {
				if tt.errMatch != "" && !strings.Contains(err.Error(), tt.errMatch) {
					t.Errorf("ParsePackageType(%q) error = %q, want substring %q",
						tt.input, err.Error(), tt.errMatch)
				}
				return
			}
			if got != tt.want {
				t.Errorf("ParsePackageType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
