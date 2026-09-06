package providers

import (
	"testing"

	"bennypowers.dev/larry/internal/version"
)

// ---- TestPyPIProvider_IsPrerelease ----

func TestPyPIProvider_IsPrerelease(t *testing.T) {
	p := &PyPIProvider{}

	tests := []struct {
		input  string
		isPre  bool
	}{
		{"1.0.0", false},
		{"1.0.0-r1", false},
		{"1.0.0_p1", true},  // patch-level pre-release
		{"1.0_alpha1", true},
		{"1.0_beta1", true},
		{"1.0_rc1", true},
		{"1.0_pre1", true},
		{"2.0.0", false},
		{"9999", false}, // live version is not prerelease
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := version.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			if got := p.IsPrerelease(v); got != tt.isPre {
				t.Errorf("IsPrerelease(%q) = %v, want %v", tt.input, got, tt.isPre)
			}
		})
	}
}

// ---- TestNpmProvider_IsPrerelease ----

func TestNpmProvider_IsPrerelease(t *testing.T) {
	p := &NpmProvider{}

	tests := []struct {
		input  string
		isPre  bool
	}{
		{"1.0.0", false},
		{"1.0.0-r1", false},
		{"1.0.0_p1", true},
		{"1.0_alpha1", true},
		{"1.0_beta1", true},
		{"1.0_rc1", true},
		{"1.0_pre1", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := version.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			if got := p.IsPrerelease(v); got != tt.isPre {
				t.Errorf("IsPrerelease(%q) = %v, want %v", tt.input, got, tt.isPre)
			}
		})
	}
}

// ---- TestGitLabProvider_IsPrerelease ----

func TestGitLabProvider_IsPrerelease(t *testing.T) {
	p := &GitLabProvider{}

	tests := []struct {
		input  string
		isPre  bool
	}{
		{"1.0.0", false},
		{"1.0.0-r1", false},
		{"1.0.0_p1", true},
		{"1.0_alpha1", true},
		{"1.0_beta1", true},
		{"1.0_rc1", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := version.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			if got := p.IsPrerelease(v); got != tt.isPre {
				t.Errorf("IsPrerelease(%q) = %v, want %v", tt.input, got, tt.isPre)
			}
		})
	}
}

// ---- TestCodebergProvider_IsPrerelease ----

func TestCodebergProvider_IsPrerelease(t *testing.T) {
	p := &CodebergProvider{}

	tests := []struct {
		input  string
		isPre  bool
	}{
		{"1.0.0", false},
		{"1.0.0-r1", false},
		{"1.0.0_p1", true},
		{"1.0_alpha1", true},
		{"1.0_rc1", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := version.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			if got := p.IsPrerelease(v); got != tt.isPre {
				t.Errorf("IsPrerelease(%q) = %v, want %v", tt.input, got, tt.isPre)
			}
		})
	}
}

// ---- TestSourceHutProvider_IsPrerelease ----

func TestSourceHutProvider_IsPrerelease(t *testing.T) {
	p := &SourceHutProvider{}

	tests := []struct {
		input  string
		isPre  bool
	}{
		{"1.0.0", false},
		{"1.0.0-r1", false},
		{"1.0.0_p1", true},
		{"1.0_alpha1", true},
		{"1.0_rc1", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := version.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			if got := p.IsPrerelease(v); got != tt.isPre {
				t.Errorf("IsPrerelease(%q) = %v, want %v", tt.input, got, tt.isPre)
			}
		})
	}
}

// ---- TestGitHubProvider_IsPrerelease ----

func TestGitHubProvider_IsPrerelease(t *testing.T) {
	p := &GitHubProvider{}

	tests := []struct {
		input  string
		isPre  bool
	}{
		{"1.0.0", false},
		{"1.0.0-r1", false},
		{"1.0.0_p1", true},
		{"1.0_alpha1", true},
		{"1.0_beta1", true},
		{"1.0_rc1", true},
		{"2.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := version.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			if got := p.IsPrerelease(v); got != tt.isPre {
				t.Errorf("IsPrerelease(%q) = %v, want %v", tt.input, got, tt.isPre)
			}
		})
	}
}

// ---- TestProvider_IsPrerelease_EdgeCases ----

func TestProvider_IsPrerelease_EdgeCases(t *testing.T) {
	tests := []struct {
		input  string
		isPre  bool
	}{
		{"0.0.0", false},    // zero version is stable
		{"1.0", false},      // two-component stable
		{"9999", false},     // live version is stable
		{"1.0-alpha", false}, // invalid format (not parseable as Gentoo suffix)
	}

	p := &PyPIProvider{}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := version.Parse(tt.input)
			if err != nil {
				// Skip unparseable inputs
				t.Skipf("Parse(%q) error: %v (expected for invalid format)", tt.input, err)
			}
			if got := p.IsPrerelease(v); got != tt.isPre {
				t.Errorf("IsPrerelease(%q) = %v, want %v", tt.input, got, tt.isPre)
			}
		})
	}
}

// ---- TestProvider_IsPrerelease_RevisionNotPrerelease ----

func TestProvider_IsPrerelease_RevisionNotPrerelease(t *testing.T) {
	// Revisions (-rN) alone are NOT prereleases — they are post-release
	// BUT if there's ALSO a pre-release suffix, the version IS a prerelease
	tests := []struct {
		input   string
		isPre   bool
	}{
		{"1.0.0-r1", false},      // stable + revision → not prerelease
		{"1.0.0-r10", false},     // stable + revision → not prerelease
		{"1.0_alpha1-r1", true},  // prerelease suffix + revision → IS prerelease
		{"1.0_beta2-r3", true},   // prerelease suffix + revision → IS prerelease
		{"1.0_rc1-r1", true},     // prerelease suffix + revision → IS prerelease
	}

	p := &PyPIProvider{}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := version.Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			if got := p.IsPrerelease(v); got != tt.isPre {
				t.Errorf("IsPrerelease(%q) = %v, want %v", tt.input, got, tt.isPre)
			}
		})
	}
}
