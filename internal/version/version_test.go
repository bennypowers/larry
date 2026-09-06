package version

import (
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
		{"2024.01.01_rc1", "2024.1.1_rc1", false},
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
		"1.2.3-beta",
		"1.2.x",
		"v1.2.3.invalid",
		"--version",
		"1.2.3.4.5.6", // max 5 components
		"..",            // empty components
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

		// Suffix ordering: alpha < beta < rc < patch < release < revision
		{"1.0_alpha1", "1.0_alpha2", -1},
		{"1.0_alpha1", "1.0_beta1", -1},
		{"1.0_beta1", "1.0_rc1", -1},
		{"1.0_rc1", "1.0_p1", -1},
		{"1.0_p1", "1.0", -1}, // pre-release < release
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
		{"^{1.2.3}", "1.2.3"},
		{"  1.2.3  ", "1.2.3"},

		// Should leave unchanged
		{"1.2.3", "1.2.3"},
		{"1.2.3_p1", "1.2.3_p1"},
		{"1.0_alpha1", "1.0_alpha1"},
		{"1.2.3-r1", "1.2.3-r1"},
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
	// alpha < beta < rc < patch < release < revision
	order := []string{
		"1.0_alpha1",
		"1.0_alpha2",
		"1.0_beta1",
		"1.0_beta2",
		"1.0_rc1",
		"1.0_rc2",
		"1.0_p1",
		"1.0_p2",
		"1.0",
		"1.0-r1",
		"1.0-r2",
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
	// In Gentoo, _pre/rc is a pre-release, _p is a patch-level pre-release
	// _rc1 should come before _p1 in Gentoo semantics
	a, _ := Parse("1.0_rc1")
	b, _ := Parse("1.0_p1")
	if got := a.Compare(b); got >= 0 {
		t.Errorf("rc1 vs p1: rc1 should be < p1, got %d", got)
	}
}
