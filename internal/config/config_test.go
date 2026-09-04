package config

import (
	"os"
	"path/filepath"
	"testing"
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
	// Unset relevant env vars
	os.Unsetenv("LARRY_VERBOSE")
	os.Unsetenv("LARRY_COLOR")
	os.Unsetenv("LARRY_DRY_RUN")
	os.Unsetenv("LARRY_CACHE_DIR")
	os.Unsetenv("LARRY_DISTDIR")
	os.Unsetenv("LARRY_JOBS")
	os.Unsetenv("LARRY_REMOTE")
	os.Unsetenv("LARRY_OVERLAY")
	os.Unsetenv("XDG_CONFIG_HOME")

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
	// Create a temp config file
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

	// Temporarily set FilePath
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
	// Test XDG_CACHE_HOME
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
