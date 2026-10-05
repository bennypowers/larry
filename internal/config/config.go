package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"bennypowers.dev/larry/internal/git"

	"github.com/spf13/viper"
)

// FilePath is the path to the config file, set via the --config flag.
var FilePath string

// Config holds all configuration for larry.
type Config struct {
	Verbose       int
	Quiet         bool
	CacheDir      string
	Distdir       string
	Color         string
	DryRun        bool
	Jobs          int
	GitHubToken   string
	Remote        string
	Overlay       string
	MaintainerName  string
	MaintainerEmail string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Verbose:       0,
		Quiet:         false,
		Color:         "auto",
		DryRun:        false,
		Jobs:          0,
		Remote:        "origin",
		CacheDir:      defaultCacheDir(),
		Distdir:       "",
		Overlay:       "",
		GitHubToken:   "",
		MaintainerName:  "",
		MaintainerEmail: "",
	}
}

// Load reads configuration from file, environment, flags, and defaults.
// Precedence: flags > env vars > config file > defaults.
func Load() (*Config, error) {
	c := DefaultConfig()

	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	if FilePath != "" {
		dir := filepath.Dir(FilePath)
		v.AddConfigPath(dir)
		v.SetConfigFile(FilePath)
	} else {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			v.AddConfigPath(filepath.Join(xdg, "larry"))
		}
		if u, err := user.Current(); err == nil && u.HomeDir != "" {
			v.AddConfigPath(filepath.Join(u.HomeDir, ".config", "larry"))
		}
		v.AddConfigPath(".")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	// Apply config file values (file > defaults).
	if err := applyConfig(v, c); err != nil {
		return nil, err
	}

	// Apply environment overrides (env > file).
	applyEnvOverrides(c)

	return c, nil
}

func applyConfig(v *viper.Viper, c *Config) error {
	if v.Get("verbose") != nil {
		c.Verbose = v.GetInt("verbose")
	}
	if v.Get("color") != nil {
		c.Color = v.GetString("color")
	}
	if v.Get("dry_run") != nil {
		c.DryRun = v.GetBool("dry_run")
	}
	if v.Get("cache_dir") != nil {
		c.CacheDir = v.GetString("cache_dir")
	}
	if v.Get("distdir") != nil {
		c.Distdir = v.GetString("distdir")
	}
	if v.Get("jobs") != nil {
		c.Jobs = v.GetInt("jobs")
	}
	if v.Get("github_token") != nil {
		c.GitHubToken = v.GetString("github_token")
	}
	if v.Get("remote") != nil {
		c.Remote = v.GetString("remote")
	}
	if v.Get("overlay") != nil {
		c.Overlay = v.GetString("overlay")
	}
	if v.Get("quiet") != nil {
		c.Quiet = v.GetBool("quiet")
	}
	if v.Get("maintainer.name") != nil {
		c.MaintainerName = v.GetString("maintainer.name")
	}
	if v.Get("maintainer.email") != nil {
		c.MaintainerEmail = v.GetString("maintainer.email")
	}
	return nil
}

func applyEnvOverrides(c *Config) {
	if v := os.Getenv("LARRY_VERBOSE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Verbose = n
		}
	}
	if v := os.Getenv("LARRY_COLOR"); v != "" {
		c.Color = v
	}
	if v := os.Getenv("LARRY_DRY_RUN"); v != "" {
		c.DryRun = v == "true" || v == "1"
	}
	if v := os.Getenv("LARRY_CACHE_DIR"); v != "" {
		c.CacheDir = v
	}
	if v := os.Getenv("LARRY_DISTDIR"); v != "" {
		c.Distdir = v
	}
	if v := os.Getenv("LARRY_JOBS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Jobs = n
		}
	}
	if v := os.Getenv("LARRY_GITHUB_TOKEN"); v != "" {
		c.GitHubToken = v
	}
	if v := os.Getenv("LARRY_REMOTE"); v != "" {
		c.Remote = v
	}
	if v := os.Getenv("LARRY_OVERLAY"); v != "" {
		c.Overlay = v
	}
	if v := os.Getenv("LARRY_QUIET"); v != "" {
		c.Quiet = v == "true" || v == "1"
	}
	if v := os.Getenv("LARRY_MAINTAINER_NAME"); v != "" {
		c.MaintainerName = v
	}
	if v := os.Getenv("LARRY_MAINTAINER_EMAIL"); v != "" {
		c.MaintainerEmail = v
	}
}

// ResolveOverlay determines the overlay directory using the following priority:
//
//	1. override — explicit value from --overlay flag
//	2. cfg.Overlay — from config file or $LARRY_OVERLAY env var
//	3. git rev-parse --show-toplevel — current working directory's git root
//
// Returns an error when no overlay can be resolved. The user must supply one
// via --overlay, $LARRY_OVERLAY, the overlay config key, or by running
// inside a git repo that is the overlay.
func (c *Config) ResolveOverlay(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if c.Overlay != "" {
		return c.Overlay, nil
	}
	if gitDir, err := git.ResolveOverlayDir(); err == nil && gitDir != "" {
		return gitDir, nil
	}
	return "", fmt.Errorf("no overlay directory found; set it via --overlay, $LARRY_OVERLAY, or the overlay config key (or run inside a git repo)")
}

// ResolveDistdir returns the distdir path using the following priority:
//
//	1. override — explicit value from --distdir flag
//	2. $LARRY_DISTDIR
//	3. cfg.Distdir — from config file
//	4. $PORTAGE_DISTDIR
//	5. <cacheDir>/distfiles
//
// This function never returns an empty string.
func (c *Config) ResolveDistdir(override string) string {
	if override != "" {
		return override
	}
	if v := os.Getenv("LARRY_DISTDIR"); v != "" {
		return v
	}
	if c.Distdir != "" {
		return c.Distdir
	}
	if v := os.Getenv("PORTAGE_DISTDIR"); v != "" {
		return v
	}
	return filepath.Join(c.ResolveCacheDir(""), "distfiles")
}

// ResolveCacheDir returns the cache directory using the following priority:
//
//	1. override — explicit value from --cache-dir flag
//	2. $LARRY_CACHE_DIR
//	3. cfg.CacheDir — from config file
//	4. defaultCacheDir()
func (c *Config) ResolveCacheDir(override string) string {
	if override != "" {
		return override
	}
	if v := os.Getenv("LARRY_CACHE_DIR"); v != "" {
		return v
	}
	if c.CacheDir != "" {
		return c.CacheDir
	}
	return defaultCacheDir()
}

func defaultCacheDir() string {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "larry")
	}
	u, err := user.Current()
	if err != nil {
		return filepath.Join(".cache", "larry")
	}
	return filepath.Join(u.HomeDir, ".cache", "larry")
}
