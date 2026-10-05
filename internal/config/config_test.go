package config

import (
	"os"
	"path/filepath"
	"testing"

	"bennypowers.dev/larry/internal/git"
)

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()

	if c.Verbose != 0 {
		t.Errorf("default Verbose = %d, want 0", c.Verbose)
	}
	if c.Quiet {
		t.Error("default Quiet = true, want false")
	}
	if c.Color != "auto" {
		t.Errorf("default Color = %q, want %q", c.Color, "auto")
	}
	if c.DryRun {
		t.Error("default DryRun = true, want false")
	}
	if c.Remote != "origin" {
		t.Errorf("default Remote = %q, want %q", c.Remote, "origin")
	}
	if c.CacheDir == "" {
		t.Error("default CacheDir is empty")
	}
	if c.Jobs != 0 {
		t.Errorf("default Jobs = %d, want 0", c.Jobs)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	// Unset relevant env vars.
	for _, v := range []string{"LARRY_VERBOSE", "LARRY_COLOR", "LARRY_DRY_RUN",
		"LARRY_CACHE_DIR", "LARRY_DISTDIR", "LARRY_JOBS", "LARRY_REMOTE",
		"LARRY_OVERLAY", "LARRY_MAINTAINER_NAME", "LARRY_MAINTAINER_EMAIL",
		"PORTAGE_DISTDIR", "XDG_CONFIG_HOME"} {
		os.Unsetenv(v)
	}

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if c.Verbose != 0 {
		t.Errorf("default Verbose = %d, want 0", c.Verbose)
	}
	if c.Color != "auto" {
		t.Errorf("default Color = %q, want %q", c.Color, "auto")
	}
	if c.Remote != "origin" {
		t.Errorf("default Remote = %q, want %q", c.Remote, "origin")
	}
}

func TestLoadConfig_EnvOverride(t *testing.T) {
	os.Setenv("LARRY_VERBOSE", "3")
	os.Setenv("LARRY_COLOR", "always")
	os.Setenv("LARRY_DRY_RUN", "true")
	os.Setenv("LARRY_CACHE_DIR", "/tmp/test-cache")
	os.Setenv("LARRY_REMOTE", "upstream")
	defer func() {
		os.Unsetenv("LARRY_VERBOSE")
		os.Unsetenv("LARRY_COLOR")
		os.Unsetenv("LARRY_DRY_RUN")
		os.Unsetenv("LARRY_CACHE_DIR")
		os.Unsetenv("LARRY_REMOTE")
	}()

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if c.Verbose != 3 {
		t.Errorf("Verbose = %d, want 3", c.Verbose)
	}
	if c.Color != "always" {
		t.Errorf("Color = %q, want %q", c.Color, "always")
	}
	if c.CacheDir != "/tmp/test-cache" {
		t.Errorf("CacheDir = %q, want %q", c.CacheDir, "/tmp/test-cache")
	}
	if c.Remote != "upstream" {
		t.Errorf("Remote = %q, want %q", c.Remote, "upstream")
	}
}

func TestLoadConfig_FileOverride(t *testing.T) {
	// Create a temp config file.
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	configContent := `
verbose: 2
color: always
dry_run: true
remote: myremote
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Temporarily set FilePath.
	oldPath := FilePath
	FilePath = configPath
	defer func() { FilePath = oldPath }()

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if c.Verbose != 2 {
		t.Errorf("Verbose = %d, want 2", c.Verbose)
	}
	if c.Color != "always" {
		t.Errorf("Color = %q, want %q", c.Color, "always")
	}
	if c.Remote != "myremote" {
		t.Errorf("Remote = %q, want %q", c.Remote, "myremote")
	}
}

func TestLoadConfig_CacheDir(t *testing.T) {
	// Test XDG_CACHE_HOME.
	os.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")
	defer os.Unsetenv("XDG_CACHE_HOME")

	expected := filepath.Join("/tmp/xdg-cache", "larry")
	if got := defaultCacheDir(); got != expected {
		t.Errorf("defaultCacheDir() = %q, want %q", got, expected)
	}
}

func TestLoadConfig_DistdirOverride(t *testing.T) {
	os.Setenv("LARRY_DISTDIR", "/tmp/distfiles")
	defer os.Unsetenv("LARRY_DISTDIR")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if c.Distdir != "/tmp/distfiles" {
		t.Errorf("Distdir = %q, want %q", c.Distdir, "/tmp/distfiles")
	}
}

func TestLoadConfig_FilePathFlag(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "custom.yaml")
	configContent := `
color: never
quiet: true
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	oldPath := FilePath
	FilePath = configPath
	defer func() { FilePath = oldPath }()

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if c.Color != "never" {
		t.Errorf("Color = %q, want %q", c.Color, "never")
	}
	if !c.Quiet {
		t.Error("Quiet = false, want true")
	}
}

// --- New tests for plan 13a ---

func TestResolveOverlay_UsesConfigValue(t *testing.T) {
	c := &Config{Overlay: "/tmp/x"}
	got, err := c.ResolveOverlay("")
	if err != nil {
		t.Fatalf("ResolveOverlay(\"\") error = %v", err)
	}
	if got != "/tmp/x" {
		t.Errorf("ResolveOverlay(\"\") = %q, want %q", got, "/tmp/x")
	}
}

func TestResolveOverlay_OverrideWins(t *testing.T) {
	c := &Config{Overlay: "/tmp/x"}
	got, err := c.ResolveOverlay("/tmp/y")
	if err != nil {
		t.Fatalf("ResolveOverlay(\"/tmp/y\") error = %v", err)
	}
	if got != "/tmp/y" {
		t.Errorf("ResolveOverlay(\"/tmp/y\") = %q, want %q", got, "/tmp/y")
	}
}

func TestResolveOverlay_NoOverlayIsError(t *testing.T) {
	// Find a temp dir that is not inside a git repo.
	// This is tricky since /tmp might be inside a repo.
	// Use a sub-dir and check.
	dir, err := os.MkdirTemp("", "larry-no-overlay")
	if err != nil {
		t.Skipf("cannot create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// Save CWD and chdir to the temp dir.
	origCwd, err := os.Getwd()
	if err != nil {
		t.Skipf("cannot get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Skipf("cannot chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origCwd) }()

	// Verify we're not in a git repo.
	if _, err := git.ResolveOverlayDir(); err == nil {
		t.Skip("temp dir is inside a git repo; cannot test overlay-not-found error")
	}

	c := &Config{}
	got, err := c.ResolveOverlay("")
	if err == nil {
		t.Fatal("ResolveOverlay(\"\") returned nil error, want non-nil")
	}
	if got != "" {
		t.Errorf("ResolveOverlay(\"\") = %q, want empty string", got)
	}
	msg := err.Error()
	for _, want := range []string{"--overlay", "LARRY_OVERLAY", "overlay"} {
		if !containsStr(msg, want) {
			t.Errorf("error message does not mention %q: %s", want, msg)
		}
	}
}

// TestResolveOverlay_NeverReturnsPortageTree verifies that no code path
// returns /var/db/repos/gentoo. The error case above must not return that path.
func TestResolveOverlay_NeverReturnsPortageTree(t *testing.T) {
	// The only way to get /var/db/repos/gentoo is if an override is passed.
	// Since we control all call sites (they pass rootFlags.Overlay or ""),
	// and rootFlags.Overlay starts at "", this test just verifies the error
	// case doesn't return the old default.
	dir, err := os.MkdirTemp("", "larry-portage-test")
	if err != nil {
		t.Skipf("cannot create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	origCwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Skipf("cannot chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origCwd) }()

	if _, err := git.ResolveOverlayDir(); err == nil {
		t.Skip("temp dir is inside a git repo")
	}

	c := &Config{}
	got, err := c.ResolveOverlay("")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got == "/var/db/repos/gentoo" {
		t.Errorf("ResolveOverlay(\"\") returned the old fallback %q", got)
	}
}

func TestResolveDistdir_Precedence(t *testing.T) {
	tests := []struct {
		name   string
		override string
		larry  string
		cfg    string
		portage string
		xdg    string
		want   string
	}{
		{
			name:     "override wins",
			override: "/a",
			larry:    "/b",
			cfg:      "/c",
			portage:  "/d",
			xdg:      "/x",
			want:     "/a",
		},
		{
			name:  "larry distdir beats cfg",
			override: "",
			larry:    "/b",
			cfg:      "/c",
			portage:  "/d",
			xdg:      "/x",
			want:     "/b",
		},
		{
			name:  "cfg beats portage",
			override: "",
			larry:    "",
			cfg:      "/c",
			portage:  "/d",
			xdg:      "/x",
			want:     "/c",
		},
		{
			name:  "portage beats cache",
			override: "",
			larry:    "",
			cfg:      "",
			portage:  "/d",
			xdg:      "/x",
			want:     "/d",
		},
		{
			name:  "cache fallback",
			override: "",
			larry:    "",
			cfg:      "",
			portage:  "",
			xdg:      "/x",
			want:     "/x/larry/distfiles",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore env vars.
			oldLarry := os.Getenv("LARRY_DISTDIR")
			oldPortage := os.Getenv("PORTAGE_DISTDIR")
			oldXdg := os.Getenv("XDG_CACHE_HOME")
			defer func() {
				os.Setenv("LARRY_DISTDIR", oldLarry)
				os.Setenv("PORTAGE_DISTDIR", oldPortage)
				os.Setenv("XDG_CACHE_HOME", oldXdg)
			}()

			// Set env vars.
			os.Setenv("LARRY_DISTDIR", tt.larry)
			os.Setenv("PORTAGE_DISTDIR", tt.portage)
			os.Setenv("XDG_CACHE_HOME", tt.xdg)

			cfg := &Config{
				CacheDir: tt.xdg + "/larry",
				Distdir:  tt.cfg,
			}

			got := cfg.ResolveDistdir(tt.override)
			if got != tt.want {
				t.Errorf("ResolveDistdir(%q) = %q, want %q", tt.override, got, tt.want)
			}
		})
	}
}

func TestResolveDistdir_NeverEmpty(t *testing.T) {
	// With all env vars unset and cfg.Distdir == "", should get cache fallback.
	os.Unsetenv("LARRY_DISTDIR")
	os.Unsetenv("PORTAGE_DISTDIR")
	os.Unsetenv("XDG_CACHE_HOME")

	cfg := &Config{Distdir: ""}
	got := cfg.ResolveDistdir("")
	if got == "" {
		t.Fatal("ResolveDistdir(\"\") returned empty string")
	}
	if !containsStr(got, ".cache/larry/distfiles") && !containsStr(got, "larry/distfiles") {
		t.Errorf("ResolveDistdir(\"\") = %q, want path ending in larry/distfiles", got)
	}
}

func TestResolveCacheDir_Precedence(t *testing.T) {
	tests := []struct {
		name   string
		override string
		larry  string
		cfg    string
		xdg    string
		want   string
	}{
		{
			name:     "override wins",
			override: "/a",
			larry:    "/b",
			cfg:      "/c",
			xdg:      "/x",
			want:     "/a",
		},
		{
			name:  "larry cache beats cfg",
			override: "",
			larry:    "/b",
			cfg:      "/c",
			xdg:      "/x",
			want:     "/b",
		},
		{
			name:  "cfg beats default",
			override: "",
			larry:    "",
			cfg:      "/c",
			xdg:      "/x",
			want:     "/c",
		},
		{
			name:  "default uses xdg",
			override: "",
			larry:    "",
			cfg:      "",
			xdg:      "/x",
			want:     "/x/larry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldLarry := os.Getenv("LARRY_CACHE_DIR")
			oldXdg := os.Getenv("XDG_CACHE_HOME")
			defer func() {
				os.Setenv("LARRY_CACHE_DIR", oldLarry)
				os.Setenv("XDG_CACHE_HOME", oldXdg)
			}()

			os.Setenv("LARRY_CACHE_DIR", tt.larry)
			os.Setenv("XDG_CACHE_HOME", tt.xdg)

			cfg := &Config{CacheDir: tt.cfg}
			got := cfg.ResolveCacheDir(tt.override)
			if got != tt.want {
				t.Errorf("ResolveCacheDir(%q) = %q, want %q", tt.override, got, tt.want)
			}
		})
	}
}

func TestLoad_MaintainerFromFileAndEnv(t *testing.T) {
	// Write a temp config file with maintainer info.
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	configContent := `
maintainer:
  name: File Person
  email: file@example.com
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	oldPath := FilePath
	FilePath = configPath
	defer func() { FilePath = oldPath }()

	// Test 1: file values.
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.MaintainerName != "File Person" {
		t.Errorf("MaintainerName = %q, want %q", c.MaintainerName, "File Person")
	}
	if c.MaintainerEmail != "file@example.com" {
		t.Errorf("MaintainerEmail = %q, want %q", c.MaintainerEmail, "file@example.com")
	}

	// Test 2: env overrides file.
	os.Setenv("LARRY_MAINTAINER_NAME", "Env Person")
	os.Setenv("LARRY_MAINTAINER_EMAIL", "env@example.com")
	defer func() {
		os.Unsetenv("LARRY_MAINTAINER_NAME")
		os.Unsetenv("LARRY_MAINTAINER_EMAIL")
	}()

	c, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.MaintainerName != "Env Person" {
		t.Errorf("after env override: MaintainerName = %q, want %q", c.MaintainerName, "Env Person")
	}
	if c.MaintainerEmail != "env@example.com" {
		t.Errorf("after env override: MaintainerEmail = %q, want %q", c.MaintainerEmail, "env@example.com")
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || findSubstr(s, substr)))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
