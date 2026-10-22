package larryconfig

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// ---- TestLoadConfig_NotFound — empty config, no error ----

func TestLoadConfig_NotFound(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := LoadConfig(tmp)
	if err != nil {
		t.Fatalf("LoadConfig(%q): unexpected error: %v", tmp, err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}
	if cfg.Upstream != nil {
		t.Error("Upstream should be nil when file not found")
	}
}

// ---- TestLoadConfig_InvalidYAML — parse error ----

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmp := t.TempDir()
	larryDir := filepath.Join(tmp, ".larry")
	if err := os.MkdirAll(larryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(larryDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{{{{invalid yaml"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	_, err := LoadConfig(tmp)
	if err == nil {
		t.Fatal("LoadConfig should error on invalid YAML")
	}
}

// ---- TestLoadConfig_Valid — YAML tags mapped properly ----

func TestLoadConfig_Valid(t *testing.T) {
	tmp := t.TempDir()
	larryDir := filepath.Join(tmp, ".larry")
	if err := os.MkdirAll(larryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(larryDir, "config.yaml")
	yaml := `
type_override: cargo
vendor_dirs:
  mylib:
    - src/lib
cleanup_orphaned_assets: true
upstream:
  ignore_tags:
    - "^\\d+\\.\\d{8}$"
  strip_prefixes:
    - "stable_"
    - "oldstable_"
  strategy: semver
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cfg, err := LoadConfig(tmp)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.TypeOverride != "cargo" {
		t.Errorf("TypeOverride = %q, want cargo", cfg.TypeOverride)
	}

	if !cfg.CleanupOrphanedAssets {
		t.Error("CleanupOrphanedAssets should be true")
	}

	if len(cfg.VendorDirs) != 1 || len(cfg.VendorDirs["mylib"]) != 1 || cfg.VendorDirs["mylib"][0] != "src/lib" {
		t.Errorf("VendorDirs = %v, want {mylib: [src/lib]}", cfg.VendorDirs)
	}

	if cfg.Upstream == nil {
		t.Fatal("Upstream should not be nil")
	}
	if len(cfg.Upstream.IgnoreTags) != 1 || cfg.Upstream.IgnoreTags[0] != `^\d+\.\d{8}$` {
		t.Errorf("IgnoreTags = %v, want [^\\d+\\.\\d{8}$]", cfg.Upstream.IgnoreTags)
	}
	if len(cfg.Upstream.StripPrefixes) != 2 {
		t.Errorf("StripPrefixes len = %d, want 2", len(cfg.Upstream.StripPrefixes))
	} else {
		if cfg.Upstream.StripPrefixes[0] != "stable_" {
			t.Errorf("StripPrefixes[0] = %q, want stable_", cfg.Upstream.StripPrefixes[0])
		}
		if cfg.Upstream.StripPrefixes[1] != "oldstable_" {
			t.Errorf("StripPrefixes[1] = %q, want oldstable_", cfg.Upstream.StripPrefixes[1])
		}
	}
	if cfg.Upstream.Strategy != "semver" {
		t.Errorf("Strategy = %q, want semver", cfg.Upstream.Strategy)
	}
}

// ---- TestLoadConfig_EmptyUpstream — upstream block present but empty ----

func TestLoadConfig_EmptyUpstream(t *testing.T) {
	tmp := t.TempDir()
	larryDir := filepath.Join(tmp, ".larry")
	if err := os.MkdirAll(larryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(larryDir, "config.yaml")
	yaml := `
upstream:
  ignore_tags: []
  strip_prefixes: []
  strategy: ""
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cfg, err := LoadConfig(tmp)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Upstream == nil {
		t.Fatal("Upstream should not be nil when block is present")
	}
	if cfg.Upstream.Strategy != "" {
		t.Errorf("Strategy = %q, want empty", cfg.Upstream.Strategy)
	}
}

// ---- TestCompile_ValidAndInvalid — regex compilation & error reporting ----

func TestCompile_Valid(t *testing.T) {
	uc := &UpstreamConfig{
		IgnoreTags:    []string{`^\d+\.\d{8}$`, `^stable_`},
		StripPrefixes: []string{"stable_", "oldstable_"},
	}

	opts, err := uc.Compile("1.2.3_p1")
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}

	if len(opts.IgnoreTagRe) != 2 {
		t.Errorf("IgnoreTagRe len = %d, want 2", len(opts.IgnoreTagRe))
	}
	if len(opts.StripPrefixes) != 2 {
		t.Errorf("StripPrefixes = %v, want [stable_ oldstable_]", opts.StripPrefixes)
	}

	// Verify regexes match correctly
	if !opts.IgnoreTagRe[0].MatchString("1.20230405") {
		t.Error("First regex should match 1.20230405")
	}
	if !opts.IgnoreTagRe[1].MatchString("stable_1.0") {
		t.Error("Second regex should match stable_1.0")
	}
}

func TestCompile_Strategy(t *testing.T) {
	tests := []struct {
		strategy string
		wantErr  bool
	}{
		{"", false},
		{"releases", false},
		{"semver", false},
		{"tags", false},
		{"bogus", true},
	}
	for _, tt := range tests {
		t.Run(tt.strategy, func(t *testing.T) {
			uc := &UpstreamConfig{Strategy: tt.strategy}
			opts, err := uc.Compile("1.0")
			if tt.wantErr {
				if err == nil {
					t.Fatal("Compile should error on invalid strategy")
				}
				return
			}
			if err != nil {
				t.Fatalf("Compile(): %v", err)
			}
			if opts.Strategy != tt.strategy {
				t.Errorf("Strategy = %q, want %q", opts.Strategy, tt.strategy)
			}
		})
	}
}

func TestCompile_InvalidRegex(t *testing.T) {
	uc := &UpstreamConfig{
		IgnoreTags: []string{`[invalid(regex`},
	}

	_, err := uc.Compile("anything")
	if err == nil {
		t.Fatal("Compile should error on invalid regex")
	}
}

func TestCompile_NilUpstream(t *testing.T) {
	var uc *UpstreamConfig
	opts, err := uc.Compile("anything")
	if err != nil {
		t.Fatalf("Compile on nil should not error: %v", err)
	}
	if opts != nil {
		t.Error("Compile on nil Upstream should return nil options")
	}
}

// ---- Plan 33: PV match, tag match, version template ----

func TestCompile_PVAndTagMatch(t *testing.T) {
	uc := &UpstreamConfig{
		PVMatch:         `^(?P<BASE>\d+\.\d+\.\d+)(?:_p\d+)?$`,
		TagMatch:        `^stable_(?P<DATE>\d{8})$`,
		VersionTemplate: `${BASE}_p${DATE}`,
	}

	opts, err := uc.Compile("6.18.46_p20260824")
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}

	// PV captures
	base, ok := opts.PVCaptures["BASE"]
	if !ok {
		t.Fatal("PVCaptures should contain BASE")
	}
	if base != "6.18.46" {
		t.Errorf("PVCaptures[BASE] = %q, want 6.18.46", base)
	}

	// TagMatchRe compiled
	if opts.TagMatchRe == nil {
		t.Fatal("TagMatchRe should be compiled")
	}
	if !opts.TagMatchRe.MatchString("stable_20260911") {
		t.Error("TagMatchRe should match stable_20260911")
	}
	if opts.TagMatchRe.MatchString("oldstable_20260911") {
		t.Error("TagMatchRe should not match oldstable_20260911")
	}

	// VersionTemplate passed through
	if opts.VersionTemplate != "${BASE}_p${DATE}" {
		t.Errorf("VersionTemplate = %q, want ${BASE}_p${DATE}", opts.VersionTemplate)
	}
}

func TestCompile_PVMatchMismatch(t *testing.T) {
	uc := &UpstreamConfig{
		PVMatch: `^(?P<BASE>\d+\.\d+\.\d+)(?:_p\d+)?$`,
	}

	_, err := uc.Compile("invalid_pv")
	if err == nil {
		t.Fatal("Compile should error when PV does not match pv_match")
	}
}

func TestCompile_PVCaptures_NoSuffix(t *testing.T) {
	uc := &UpstreamConfig{
		PVMatch: `^(?P<BASE>\d+\.\d+\.\d+)(?:_p\d+)?$`,
	}

	opts, err := uc.Compile("6.18.46")
	if err != nil {
		t.Fatalf("Compile(): %v", err)
	}

	if opts.PVCaptures["BASE"] != "6.18.46" {
		t.Errorf("PVCaptures[BASE] = %q, want 6.18.46", opts.PVCaptures["BASE"])
	}
}

func TestCompile_InvalidPVMatch(t *testing.T) {
	uc := &UpstreamConfig{
		PVMatch: `[invalid(regex`,
	}

	_, err := uc.Compile("anything")
	if err == nil {
		t.Fatal("Compile should error on invalid pv_match regex")
	}
}

func TestCompile_InvalidTagMatch(t *testing.T) {
	uc := &UpstreamConfig{
		TagMatch: `[invalid(regex`,
	}

	_, err := uc.Compile("anything")
	if err == nil {
		t.Fatal("Compile should error on invalid tag_match regex")
	}
}

func TestExtractCaptures(t *testing.T) {
	re := regexp.MustCompile(`^(?P<BASE>\d+\.\d+\.\d+)(?:_p(?P<PATCH>\d+))?$`)

	// With _p suffix
	caps := ExtractCaptures(re, "6.18.46_p2")
	if caps["BASE"] != "6.18.46" {
		t.Errorf("captures[BASE] = %q, want 6.18.46", caps["BASE"])
	}
	if caps["PATCH"] != "2" {
		t.Errorf("captures[PATCH] = %q, want 2", caps["PATCH"])
	}

	// Without _p suffix — PATCH should not be set
	caps2 := ExtractCaptures(re, "6.18.46")
	if caps2["BASE"] != "6.18.46" {
		t.Errorf("captures2[BASE] = %q, want 6.18.46", caps2["BASE"])
	}
	if _, ok := caps2["PATCH"]; ok {
		t.Error("captures2 should not contain PATCH")
	}

	// nil re → empty map
	caps3 := ExtractCaptures(nil, "anything")
	if len(caps3) != 0 {
		t.Errorf("ExtractCaptures(nil, ...) = %v, want empty", caps3)
	}

	// No match → empty map
	caps4 := ExtractCaptures(re, "not-a-version")
	if len(caps4) != 0 {
		t.Errorf("ExtractCaptures(no match) = %v, want empty", caps4)
	}
}

func TestExpandTemplate(t *testing.T) {
	vars := map[string]string{
		"BASE":  "6.18.46",
		"DATE":  "20260911",
		"PATCH": "2",
	}

	tests := []struct {
		tmpl string
		want string
	}{
		{"${BASE}_p${DATE}", "6.18.46_p20260911"},
		{"${DATE}", "20260911"},
		{"$BASE", "6.18.46"},
		{"no vars here", "no vars here"},
		{"${MISSING}", ""},
		{"${BASE}-${DATE}-${PATCH}", "6.18.46-20260911-2"},
	}

	for _, tt := range tests {
		t.Run(tt.tmpl, func(t *testing.T) {
			got := ExpandTemplate(tt.tmpl, vars)
			if got != tt.want {
				t.Errorf("ExpandTemplate(%q) = %q, want %q", tt.tmpl, got, tt.want)
			}
		})
	}
}

func TestCompile_Check(t *testing.T) {
	yes := false
	tests := []struct {
		name  string
		check *bool
		want  bool
	}{
		{"default", nil, true},
		{"enabled", boolPtr(true), true},
		{"disabled", &yes, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := (&UpstreamConfig{Check: tt.check}).Compile("1.0")
			if err != nil {
				t.Fatalf("Compile(): %v", err)
			}
			if opts.Check != tt.want {
				t.Errorf("Check = %v, want %v", opts.Check, tt.want)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }
