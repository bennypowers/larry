package config

import (
	"testing"
)

func TestConfig_FlagOverride(t *testing.T) {
	c := DefaultConfig()

	// Flags should override defaults
	c.Verbose = 3
	c.Color = "always"
	c.DryRun = true
	c.CacheDir = "/tmp/flag-cache"
	c.Distdir = "/tmp/flag-distdir"
	c.Jobs = 4
	c.Remote = "custom-remote"
	c.Overlay = "/path/to/overlay"
	c.GitHubToken = "ghp_test123"

	if c.Verbose != 3 {
		t.Errorf("Verbose = %d, want 3", c.Verbose)
	}
	if c.Color != "always" {
		t.Errorf("Color = %q, want %q", c.Color, "always")
	}
	if !c.DryRun {
		t.Error("DryRun = false, want true")
	}
	if c.CacheDir != "/tmp/flag-cache" {
		t.Errorf("CacheDir = %q, want %q", c.CacheDir, "/tmp/flag-cache")
	}
	if c.Distdir != "/tmp/flag-distdir" {
		t.Errorf("Distdir = %q, want %q", c.Distdir, "/tmp/flag-distdir")
	}
	if c.Jobs != 4 {
		t.Errorf("Jobs = %d, want 4", c.Jobs)
	}
	if c.Remote != "custom-remote" {
		t.Errorf("Remote = %q, want %q", c.Remote, "custom-remote")
	}
	if c.Overlay != "/path/to/overlay" {
		t.Errorf("Overlay = %q, want %q", c.Overlay, "/path/to/overlay")
	}
	if c.GitHubToken != "ghp_test123" {
		t.Errorf("GitHubToken = %q, want %q", c.GitHubToken, "ghp_test123")
	}
}
