package providers

import (
	"testing"
)

// ---- TestStripTagPrefix_PeeledTag: ^{...} stripping via Normalize (D6) ----

func TestStripTagPrefix_PeeledTag(t *testing.T) {
	tests := []struct {
		input  string
		want   string
	}{
		{"v1.0^{}", "1.0"},
		{"1.0^{abc}", "1.0"},
		{"v1.0", "1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripTagPrefix(tt.input)
			if got != tt.want {
				t.Errorf("stripTagPrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
