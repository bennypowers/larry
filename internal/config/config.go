package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/spf13/viper"
)

// FilePath is the path to the config file, set via the --config flag.
var FilePath string

// Config holds all configuration for larry.
type Config struct {
	Verbose     int
	Quiet       bool
	CacheDir    string
	Distdir     string
	Color       string
	DryRun      bool
	Jobs        int
	GitHubToken string
	Remote      string
	Overlay     string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Verbose:     0,
		Quiet:       false,
		Color:       "auto",
		DryRun:      false,
		Jobs:        0,
		Remote:      "origin",
		CacheDir:    defaultCacheDir(),
		Distdir:     "",
		Overlay:     "",
		GitHubToken: "",
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

	// Apply config file values (file > defaults)
	if err := applyConfig(v, c); err != nil {
		return nil, err
	}

	// Apply environment overrides (env > file)
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
