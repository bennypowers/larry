package version

import (
	"testing"
)

// ---- TestVersion_Parse_ShortSuffixes: parses shorthand suffix forms ----

func TestVersion_Parse_ShortSuffixes(t *testing.T) {
	tests := []struct {
		input   string
		wantFmt string
	}{
		{"1.0_a1", "1.0_alpha1"},
		{"1.0_a2", "1.0_alpha2"},
		{"1.0_b1", "1.0_beta1"},
		{"1.0_b2", "1.0_beta2"},
		{"1.0_pre1", "1.0_rc1"},
		{"1.0_pre2", "1.0_rc2"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.input, err)
			}
			got := v.Format()
			if got != tt.wantFmt {
				t.Errorf("Format() = %q, want %q", got, tt.wantFmt)
			}
		})
	}
}

// ---- TestVersion_Parse_EquivalentSuffixes: shorthand and longform compare equal ----

func TestVersion_Parse_EquivalentSuffixes(t *testing.T) {
	// Shorthand and longform should parse to equal versions
	equivalentPairs := []struct {
		shorthand string
		longform  string
	}{
		{"1.0_a1", "1.0_alpha1"},
		{"1.0_a2", "1.0_alpha2"},
		{"1.0_b1", "1.0_beta1"},
		{"1.0_b12", "1.0_beta12"},
		{"1.0_pre1", "1.0_rc1"},
		{"1.0_pre99", "1.0_rc99"},
	}

	for _, tt := range equivalentPairs {
		t.Run(tt.shorthand, func(t *testing.T) {
			shortV, err := Parse(tt.shorthand)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.shorthand, err)
			}
			longV, err := Parse(tt.longform)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.longform, err)
			}

			// Round-trip shorthand: parse → format → parse → should equal original longform
			formatted := shortV.Format()
			reParsed, err := Parse(formatted)
			if err != nil {
				t.Fatalf("Parse(%q) round-trip error: %v", formatted, err)
			}
			if got := reParsed.Compare(longV); got != 0 {
				t.Errorf("Round-trip of %q (%q) != %q: got %d", tt.shorthand, formatted, tt.longform, got)
			}
		})
	}
}

// ---- TestVersion_Format_NormalizesShortSuffixes ----

func TestVersion_Format_NormalizesShortSuffixes(t *testing.T) {
	// Format() should always expand shorthand to longform
	tests := []struct {
		input    string
		wantLong string
	}{
		{"1.0_a1", "1.0_alpha1"},
		{"1.0_a1-r1", "1.0_alpha1-r1"},
		{"1.0_b1", "1.0_beta1"},
		{"1.0_pre1", "1.0_rc1"},
		{"1.0_pre1-r1", "1.0_rc1-r1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			got := v.Format()
			if got != tt.wantLong {
				t.Errorf("Format(%q) = %q, want %q", tt.input, got, tt.wantLong)
			}
		})
	}
}

// ---- TestVersion_Parse_EdgeSuffixes ----

func TestVersion_Parse_EdgeSuffixes(t *testing.T) {
	tests := []struct {
		input   string
		wantFmt string
	}{
		{"1.0_a0", "1.0_alpha0"},
		{"1.0_b0", "1.0_beta0"},
		{"1.0_pre0", "1.0_rc0"},
		{"9999_a1", "9999_alpha1", /* live version with suffix */},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.input, err)
			}
			got := v.Format()
			if got != tt.wantFmt {
				t.Errorf("Format() = %q, want %q", got, tt.wantFmt)
			}
		})
	}
}

// ---- TestVersion_Compare_ShortVsLong ----

func TestVersion_Compare_ShortVsLong(t *testing.T) {
	// Shorthand and longform should compare equal
	tests := []struct {
		short string
		long  string
		want  int
	}{
		{"1.0_a1", "1.0_alpha1", 0},
		{"1.0_b1", "1.0_beta1", 0},
		{"1.0_pre1", "1.0_rc1", 0},
		{"1.0_a1", "1.0_beta1", -1}, // a1 < beta1 (alpha < beta)
		{"1.0_b1", "1.0_rc1", -1},   // beta1 < rc1
		{"1.0_pre1", "1.0", -1},     // rc1 < release
	}

	for _, tt := range tests {
		t.Run(tt.short, func(t *testing.T) {
			sv, _ := Parse(tt.short)
			lv, _ := Parse(tt.long)
			if got := sv.Compare(lv); got != tt.want {
				t.Errorf("Compare(%q, %q) = %d, want %d", tt.short, tt.long, got, tt.want)
			}
		})
	}
}
