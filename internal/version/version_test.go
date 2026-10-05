package version

import (
	"slices"
	"strings"
	"testing"
)

// ---- TestVersion_Parse: parses supported formats ----

func TestVersion_Parse(t *testing.T) {
	tests := []struct {
		input    string
		wantFmt  string
		wantLive bool
	}{
		{"1.2.3", "1.2.3", false},
		{"1.2.3-r1", "1.2.3-r1", false},
		{"1.2.3_p1", "1.2.3_p1", false},
		{"1.0_alpha1", "1.0_alpha1", false},
		{"1.0_beta2", "1.0_beta2", false},
		{"1.0_rc3", "1.0_rc3", false},
		{"9999", "9999", true},
		{"1.2", "1.2", false},
		{"1.2.3.4", "1.2.3.4", false},
		{"2024.01.01_rc1", "2024.01.01_rc1", false},
		{"0.0.0", "0.0.0", false},
		{"10.20.30", "10.20.30", false},
		{"1.0.0-r1", "1.0.0-r1", false},
		{"1.0.0_p2", "1.0.0_p2", false},
		{"1.0_alpha1-r1", "1.0_alpha1-r1", false},
		{"9999-r1", "9999-r1", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.input, err)
			}
			if got := v.Format(); got != tt.wantFmt {
				t.Errorf("Format() = %q, want %q", got, tt.wantFmt)
			}
			if v.Live != tt.wantLive {
				t.Errorf("Live = %v, want %v", v.Live, tt.wantLive)
			}
		})
	}
}

// ---- TestVersion_Parse_Invalid: rejects malformed strings ----

func TestVersion_Parse_Invalid(t *testing.T) {
	invalid := []string{
		"",
		"abc",
		"nightly",
		"--version",
	}

	for _, input := range invalid {
		t.Run(input, func(t *testing.T) {
			_, err := Parse(input)
			if err == nil {
				t.Errorf("Parse(%q) should error", input)
			}
		})
	}
}

// ---- TestVersion_Compare: correct ordering ----

func TestVersion_Compare(t *testing.T) {
	tests := []struct {
		a, b string
		want int // -1 = a < b, 0 = a == b, 1 = a > b
	}{
		// Basic version comparison
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.2.3", "1.2.4", -1},
		{"1.2.3", "1.3.0", -1},

		// Two-component versions
		{"1.0", "1.1", -1},
		{"1.0", "2.0", -1},
		{"1.0", "1.0.0", 0}, // 1.0 == 1.0.0 (trailing zeros)

		// Suffix ordering: alpha < beta < pre < rc < patch < release < revision
		{"1.0_alpha1", "1.0_alpha2", -1},
		{"1.0_alpha1", "1.0_beta1", -1},
		{"1.0_beta1", "1.0_pre1", -1},
		{"1.0_pre1", "1.0_rc1", -1},
		{"1.0_rc1", "1.0_p1", -1},
		{"1.0_p1", "1.0", 1}, // _p is a post-release, > release
		{"1.0", "1.0-r1", -1}, // release < revision

		// Same suffix type, different number
		{"1.0_alpha1", "1.0_alpha1", 0},
		{"1.0_beta2", "1.0_beta1", 1},
		{"1.0_rc3", "1.0_rc2", 1},

		// Live version (9999) beats any finite version
		{"9999", "1.0.0", 1},
		{"1.0.0", "9999", -1},
		{"9999", "0.0.1", 1},
		{"9999", "9998", 1},
		{"9999", "9999", 0},

		// Live with revision
		{"9999-r1", "9999", 1},
		{"9999", "9998-r1", 1},
		{"9999-r1", "1.0.0", 1},

		// Four-component versions
		{"1.2.3.4", "1.2.3.5", -1},
		{"1.2.3.4", "1.2.3", 1}, // more components = greater (when extras > 0)
		{"1.2.3.4", "1.2.3.4.0", 0}, // trailing zero doesn't change value
		{"1.2.3.4", "1.2.3.4.1", -1},

		// Date-style versions
		{"2024.1.1_rc1", "2024.1.1", -1},
		{"2024.1.1", "2024.1.2", -1},
		{"2024.1.1", "2023.12.31", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			a, errA := Parse(tt.a)
			if errA != nil {
				t.Fatalf("Parse(%q) error: %v", tt.a, errA)
			}
			b, errB := Parse(tt.b)
			if errB != nil {
				t.Fatalf("Parse(%q) error: %v", tt.b, errB)
			}
			if got := a.Compare(b); got != tt.want {
				t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// ---- TestVersion_Normalize: strips known prefixes ----

func TestVersion_Normalize(t *testing.T) {
	tests := []struct {
		input  string
		want   string
	}{
		{"v1.2.3", "1.2.3"},
		{"V1.2.3", "1.2.3"},
		{"bun-1.2.3", "1.2.3"},
		{"rocm-1.2.3", "1.2.3"},
		{"release-1.2.3", "1.2.3"},
		{"  1.2.3  ", "1.2.3"},

		// Should leave unchanged
		{"1.2.3", "1.2.3"},
		{"1.2.3_p1", "1.2.3_p1"},
		{"1.0_alpha1", "1.0_alpha1"},
		{"1.2.3-r1", "1.2.3-r1"},

		// rel- is NO LONGER stripped (D7)
		{"rel-3.0", "rel-3.0"},

		// vulkan- should NOT be stripped (D5)
		{"vulkan-1.0", "vulkan-1.0"},
		{"version2", "version2"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := Normalize(tt.input); got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---- TestNormalize_PeeledTag: ^{...} stripped ANYWHERE in string (D6) ----

func TestNormalize_PeeledTag(t *testing.T) {
	tests := []struct {
		input  string
		want   string
	}{
		{"v1.0^{}", "1.0"},
		{"1.0^{abc}", "1.0"},
		{"1.0^{}-r1", "1.0-r1"},
		{"^{1.2.3}", ""}, // stripped anywhere — entire string is gone
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := Normalize(tt.input); got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---- TestNormalize_KnownPrefixes: only bun-/rocm-/release- (D7) ----

func TestNormalize_KnownPrefixes(t *testing.T) {
	tests := []struct {
		input  string
		want   string
	}{
		{"bun-1.2", "1.2"},
		{"rocm-6.1", "6.1"},
		{"release-3.0", "3.0"},
		{"rel-3.0", "rel-3.0"}, // deleted from prefix list
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := Normalize(tt.input); got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---- TestNormalize_VPrefix: v/V only when followed by digit (D5) ----

func TestNormalize_VPrefix(t *testing.T) {
	tests := []struct {
		input  string
		want   string
	}{
		{"v1.0", "1.0"},
		{"V1.0", "1.0"},
		{"v.1.0", "1.0"},
		{"V.2.1", "2.1"},
		{"vulkan-1.0", "vulkan-1.0"},
		{"version2", "version2"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := Normalize(tt.input); got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---- TestVersion_RoundTrip: parse → format → parse → equal ----

func TestVersion_RoundTrip(t *testing.T) {
	inputs := []string{
		"1.2.3",
		"1.2.3-r1",
		"1.0_alpha1",
		"1.0_beta2",
		"1.0_rc3",
		"1.2.3_p1",
		"9999",
		"1.2.3.4",
		"2024.01.01_rc1",
		"1.0_alpha1-r1",
		"1.2.3_p2-r1",
		"0.0.0",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			v1, err := Parse(input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", input, err)
			}
			formatted := v1.Format()
			v2, err := Parse(formatted)
			if err != nil {
				t.Fatalf("Parse(%q) round-trip error: %v", formatted, err)
			}
			if got := v1.Compare(v2); got != 0 {
				t.Errorf("Round-trip of %q: %v vs %v differ: %d", input, v1, v2, got)
			}
		})
	}
}

// ---- TestVersion_CompareSuffixes: detailed suffix ordering ----

func TestVersion_CompareSuffixes(t *testing.T) {
	// Verify the complete Gentoo suffix ordering
	// alpha < beta < pre < rc < release < revision < patch
	order := []string{
		"1.0_alpha1", "1.0_alpha2",
		"1.0_beta1",  "1.0_beta2",
		"1.0_pre1",
		"1.0_rc1",    "1.0_rc2",
		"1.0",
		"1.0-r1",     "1.0-r2",
		"1.0_p1",     "1.0_p1-r1", "1.0_p2",
		"1.0.1",
		"9999",
	}

	for i := 0; i < len(order)-1; i++ {
		for j := i + 1; j < len(order); j++ {
			a, _ := Parse(order[i])
			b, _ := Parse(order[j])
			if got := a.Compare(b); got >= 0 {
				t.Errorf("Compare(%q, %q) = %d, want < 0", order[i], order[j], got)
			}
		}
	}
}

// ---- TestVersion_Compare_PatchVsRc ----

func TestVersion_Compare_PatchVsRc(t *testing.T) {
	// In Gentoo, _rc is a pre-release, _p is a post-release
	// _rc1 should come before _p1 in Gentoo semantics
	a, _ := Parse("1.0_rc1")
	b, _ := Parse("1.0_p1")
	if got := a.Compare(b); got >= 0 {
		t.Errorf("rc1 vs p1: rc1 should be < p1, got %d", got)
	}
}

// =====================================================================
// NEW TESTS — plan 14
// =====================================================================

// TestCompare_AuthoritativeChain — for the 15-element order slice, assert Compare(order[i], order[j]) < 0 for every i < j
func TestCompare_AuthoritativeChain(t *testing.T) {
	order := []string{
		"1.0_alpha1", "1.0_alpha2",
		"1.0_beta1",  "1.0_beta2",
		"1.0_pre1",
		"1.0_rc1",    "1.0_rc2",
		"1.0",
		"1.0-r1",     "1.0-r2",
		"1.0_p1",     "1.0_p1-r1", "1.0_p2",
		"1.0.1",
		"9999",
	}
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			got := Compare(order[i], order[j])
			if got >= 0 {
				t.Errorf("Compare(%q, %q) = %d, want < 0 (chain violation)", order[i], order[j], got)
			}
		}
	}
}

// TestCompare_PatchAboveRelease — 1.0_p1 > 1.0 (D1)
func TestCompare_PatchAboveRelease(t *testing.T) {
	if got := Compare("1.0", "1.0_p1"); got != -1 {
		t.Errorf("Compare(%q, %q) = %d, want -1", "1.0", "1.0_p1", got)
	}
}

// TestCompare_PatchAboveRevision — 1.0_p1 > 1.0-r1 (D1)
func TestCompare_PatchAboveRevision(t *testing.T) {
	if got := Compare("1.0_p1", "1.0-r1"); got != 1 {
		t.Errorf("Compare(%q, %q) = %d, want 1", "1.0_p1", "1.0-r1", got)
	}
}

// TestCompare_PreBelowRc — 1.0_pre1 < 1.0_rc1 (D2)
func TestCompare_PreBelowRc(t *testing.T) {
	if got := Compare("1.0_pre1", "1.0_rc1"); got != -1 {
		t.Errorf("Compare(%q, %q) = %d, want -1", "1.0_pre1", "1.0_rc1", got)
	}
}

// TestIsPrerelease — table-driven (D3)
func TestIsPrerelease(t *testing.T) {
	tests := []struct {
		input  string
		isPre  bool
	}{
		{"1.0_alpha1", true},
		{"1.0_beta1", true},
		{"1.0_pre1", true},
		{"1.0_rc1", true},
		{"1.0_rc1-r1", true},
		{"1.0", false},
		{"1.0_p1", false},
		{"1.0_p2", false},
		{"1.0-r1", false},
		{"9999", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			if got := v.IsPrerelease(); got != tt.isPre {
				t.Errorf("IsPrerelease(%q) = %v, want %v", tt.input, got, tt.isPre)
			}
		})
	}
}

// TestParse_Lenient — all these return nil error (D4)
func TestParse_Lenient(t *testing.T) {
	shouldParse := []string{
		"1.2.3a",
		"1.0.0-beta.1",
		"2.0.0-rc1",
		"1.2.3.4.5.6",
		"20250410",
		"6.18.46_p20260824",
		"1.0_pre",
		"367",
		"2026.40.0",
	}
	for _, s := range shouldParse {
		t.Run(s, func(t *testing.T) {
			_, err := Parse(s)
			if err != nil {
				t.Errorf("Parse(%q) unexpected error: %v", s, err)
			}
		})
	}

	shouldReject := []string{"nightly", "abc", ""}
	for _, s := range shouldReject {
		t.Run(s, func(t *testing.T) {
			_, err := Parse(s)
			if err == nil {
				t.Errorf("Parse(%q) should error", s)
			}
		})
	}
}

// TestParse_ComponentsAndSegments — the worked table (D4)
func TestParse_ComponentsAndSegments(t *testing.T) {
	tests := []struct {
		input      string
		wantRev    uint64
		wantSType  SuffixType
		wantSNum   uint32
		wantComps  []uint64
		wantLive   bool
		wantFmt    string
	}{
		{"1.2.3", 0, 0, 0, []uint64{1, 2, 3}, false, "1.2.3"},
		{"1.2.3a", 0, 0, 0, []uint64{1, 2}, false, "1.2.3a"},
		{"1.0.0-beta.1", 0, Beta, 1, []uint64{1, 0, 0}, false, "1.0.0_beta1"},
		{"2.0.0-rc1", 0, Rc, 1, []uint64{2, 0, 0}, false, "2.0.0_rc1"},
		{"1.2.3.4.5.6", 0, 0, 0, []uint64{1, 2, 3, 4, 5, 6}, false, "1.2.3.4.5.6"},
		{"6.18.46_p20260824", 0, Patch, 20260824, []uint64{6, 18, 46}, false, "6.18.46_p20260824"},
		{"1.0_pre", 0, Pre, 0, []uint64{1, 0}, false, "1.0_pre0"},
		{"9999-r1", 1, 0, 0, []uint64{9999}, true, "9999-r1"},
		{"20250410", 0, 0, 0, []uint64{20250410}, false, "20250410"},
		{"2026.40.0", 0, 0, 0, []uint64{2026, 40, 0}, false, "2026.40.0"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			if v.Revision != tt.wantRev {
				t.Errorf("Revision = %d, want %d", v.Revision, tt.wantRev)
			}
			if tt.wantSType == 0 {
				if v.Suffix != nil {
					t.Errorf("Suffix = %+v, want nil", v.Suffix)
				}
			} else {
				if v.Suffix == nil {
					t.Fatalf("Suffix = nil, want Type=%d Num=%d", tt.wantSType, tt.wantSNum)
				}
				if v.Suffix.Type != tt.wantSType {
					t.Errorf("Suffix.Type = %d, want %d", v.Suffix.Type, tt.wantSType)
				}
				if v.Suffix.Number != tt.wantSNum {
					t.Errorf("Suffix.Number = %d, want %d", v.Suffix.Number, tt.wantSNum)
				}
			}
			if len(v.Components) != len(tt.wantComps) ||
				(len(v.Components) > 0 && v.Components[0] != tt.wantComps[0]) {
				// Full comparison
				if !slices.Equal(v.Components, tt.wantComps) {
					t.Errorf("Components = %v, want %v", v.Components, tt.wantComps)
				}
			}
			if v.Live != tt.wantLive {
				t.Errorf("Live = %v, want %v", v.Live, tt.wantLive)
			}
			if got := v.Format(); got != tt.wantFmt {
				t.Errorf("Format() = %q, want %q", got, tt.wantFmt)
			}
		})
	}
}

// TestCompare_LenientForms — non-standard suffix forms compare correctly (D4)
func TestCompare_LenientForms(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.2.3a", "1.2.3", 1},
		{"2.0.0-rc1", "2.0.0", -1},
		{"1.2.3.4.5.6", "1.2.3.4.5", 1},
		{"1.0.0-beta.1", "1.0.0", -1},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got := Compare(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestCompareFunc_TotalOrder — total order guarantee (D7)
func TestCompareFunc_TotalOrder(t *testing.T) {
	fixture := []string{"1.0", "1.0_p1", "9999", "nightly", "abc", "1.2.3a", ""}

	// Reflexivity: Compare(x, x) == 0
	for _, x := range fixture {
		if got := Compare(x, x); got != 0 {
			t.Errorf("Compare(%q, %q) = %d, want 0", x, x, got)
		}
	}

	// Antisymmetry: Compare(x, y) == -Compare(y, x)
	for i := 0; i < len(fixture); i++ {
		for j := i + 1; j < len(fixture); j++ {
			a := Compare(fixture[i], fixture[j])
			b := Compare(fixture[j], fixture[i])
			if a != -b {
				t.Errorf("Compare(%q, %q) = %d, but Compare(%q, %q) = %d; not antisymmetric",
					fixture[i], fixture[j], a, fixture[j], fixture[i], b)
			}
		}
	}

	// Parseable > unparseable
	parseable := map[string]bool{"1.0": true, "1.0_p1": true, "9999": true, "1.2.3a": true}
	unparseable := map[string]bool{"nightly": true, "abc": true, "": true}
	for p := range parseable {
		for u := range unparseable {
			if got := Compare(p, u); got <= 0 {
				t.Errorf("Compare(%q, %q) = %d, want > 0 (parseable > unparseable)", p, u, got)
			}
		}
	}

	// Both unparseable → strings.Compare
	if got := Compare("abc", "nightly"); got != strings.Compare("abc", "nightly") {
		t.Errorf("Compare(%q, %q) = %d, want %d (strings.Compare fallback)", "abc", "nightly", got, strings.Compare("abc", "nightly"))
	}
}

// TestLess_SortsSlice — Less works with slices.SortFunc
func TestLess_SortsSlice(t *testing.T) {
	s := []string{"1.0_p1", "9999", "1.0", "1.0_rc1", "1.0-r1"}
	slices.SortFunc(s, Less)
	want := []string{"1.0_rc1", "1.0", "1.0-r1", "1.0_p1", "9999"}
	if !slices.Equal(s, want) {
		t.Errorf("Sorted = %v, want %v", s, want)
	}
}

// TestCompare_RealWorldRegressions — vercmp block from the plan
func TestCompare_RealWorldRegressions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"367", "310.10", 1},
		{"6.18.46_p20260824", "1.20230405", 1},
		{"20250410", "20260519", -1},
		{"9.0", "10.0", -1},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got := Compare(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
