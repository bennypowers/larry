package larryconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"go.yaml.in/yaml/v3"
)

// ── Directory layout ────────────────────────────────────────────────────────
//
// All larry config lives under a single package-local directory:
//
//	<overlay>/<cat>/<pkg>/
//	  .larry/
//	    config.yaml   ← package config (type_override, vendor_dirs, cleanup, upstream)
//	    prepare.sh    ← custom prepare script (optional)
//
// This keeps larry-only files out of the ebuild's files/ so they never ship
// to end users, and concentrates every FS access for larry config in one
// package.

const (
	// Dir is the larry config directory name (relative to the package dir).
	Dir = ".larry"

	// ConfigFile is the config filename inside .larry/.
	ConfigFile = "config.yaml"

	// PrepareScript is the custom prepare script name inside .larry/.
	PrepareScript = "prepare.sh"
)

// Config represents the full .larry/config.yaml schema.
type Config struct {
	TypeOverride          string            `yaml:"type_override,omitempty"`
	VendorDirs            map[string][]string `yaml:"vendor_dirs,omitempty"`
	CleanupOrphanedAssets bool              `yaml:"cleanup_orphaned_assets,omitempty"`
	Upstream              *UpstreamConfig   `yaml:"upstream,omitempty"`
	// CustomPrepareScript is an optional override for the relative path
	// (from the package dir) of the custom prepare script.  Empty means
	// use the default <pkg>/.larry/prepare.sh.
	CustomPrepareScript string `yaml:"custom_prepare_script,omitempty"`
}

// UpstreamConfig holds per-package upstream resolution rules.
type UpstreamConfig struct {
	IgnoreTags      []string `yaml:"ignore_tags,omitempty"`
	StripPrefixes   []string `yaml:"strip_prefixes,omitempty"`
	Strategy        string   `yaml:"strategy,omitempty"`
	PVMatch         string   `yaml:"pv_match,omitempty"`
	TagMatch        string   `yaml:"tag_match,omitempty"`
	VersionTemplate string   `yaml:"version_template,omitempty"`
}

// ResolveOptions is the compiled, runtime form of UpstreamConfig.
type ResolveOptions struct {
	IgnoreTagRe     []*regexp.Regexp
	StripPrefixes   []string
	TagMatchRe      *regexp.Regexp
	VersionTemplate string
	PVCaptures      map[string]string
}

// PackageDir returns the absolute path to the .larry/ directory for the
// given package directory.
func PackageDir(pkgDir string) string {
	return filepath.Join(pkgDir, Dir)
}

// ConfigPath returns the absolute path to .larry/config.yaml.
func ConfigPath(pkgDir string) string {
	return filepath.Join(PackageDir(pkgDir), ConfigFile)
}

// PrepareScriptPath returns the absolute path to .larry/prepare.sh for the
// given package directory.
func PrepareScriptPath(pkgDir string) string {
	return filepath.Join(PackageDir(pkgDir), PrepareScript)
}

// HasConfig checks whether a .larry/config.yaml exists for the given
// package directory.
func HasConfig(pkgDir string) (bool, error) {
	_, err := os.Stat(ConfigPath(pkgDir))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat config: %w", err)
}

// LoadConfig reads .larry/config.yaml from the given package directory.
// Returns an empty Config (no error) if the file does not exist.
func LoadConfig(pkgDir string) (*Config, error) {
	cfgPath := ConfigPath(pkgDir)
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", ConfigFile, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ConfigFile, err)
	}
	return &cfg, nil
}

// HasPrepareScript checks whether a .larry/prepare.sh exists for the given
// package directory.
func HasPrepareScript(pkgDir string) (bool, error) {
	_, err := os.Stat(PrepareScriptPath(pkgDir))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat prepare script: %w", err)
}

// ResolvePrepareScript returns the absolute path to the custom prepare
// script for the given package directory and config.  Uses cfg's
// CustomPrepareScript field if non-empty; otherwise falls back to the
// default .larry/prepare.sh.
func ResolvePrepareScript(pkgDir string, cfg *Config) string {
	script := cfg.CustomPrepareScript
	if script == "" {
		// Default: <pkg>/.larry/prepare.sh
		return PrepareScriptPath(pkgDir)
	}
	// If the override is relative, resolve it under the package dir.
	if !filepath.IsAbs(script) {
		return filepath.Join(pkgDir, script)
	}
	return script
}

// ReadPrepareScript reads and returns the contents of the custom prepare
// script.  Use HasPrepareScript first to check existence.
func ReadPrepareScript(pkgDir string) ([]byte, error) {
	return os.ReadFile(PrepareScriptPath(pkgDir))
}

// ExtractCaptures extracts named capture groups from s using re.
// Only non-empty captures are returned — optional groups that didn't match
// are excluded so callers can distinguish "set to empty" from "not set".
func ExtractCaptures(re *regexp.Regexp, s string) map[string]string {
	captures := make(map[string]string)
	if re == nil {
		return captures
	}
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return captures
	}
	for i, name := range re.SubexpNames() {
		if i != 0 && name != "" && matches[i] != "" {
			captures[name] = matches[i]
		}
	}
	return captures
}

// ExpandTemplate replaces ${VAR} or $VAR references using vars map.
func ExpandTemplate(template string, vars map[string]string) string {
	return os.Expand(template, func(key string) string {
		return vars[key]
	})
}

// Compile compiles regex patterns into ResolveOptions and extracts PV captures.
func (uc *UpstreamConfig) Compile(pv string) (*ResolveOptions, error) {
	if uc == nil {
		return nil, nil
	}
	opts := &ResolveOptions{
		StripPrefixes:   uc.StripPrefixes,
		VersionTemplate: uc.VersionTemplate,
		PVCaptures:      make(map[string]string),
	}

	// 1. Compile ignore_tags
	for i, pat := range uc.IgnoreTags {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("ignore_tags[%d]: invalid regex %q: %w", i, pat, err)
		}
		opts.IgnoreTagRe = append(opts.IgnoreTagRe, re)
	}

	// 2. Compile pv_match and extract captures from current PV
	if uc.PVMatch != "" {
		pvRe, err := regexp.Compile(uc.PVMatch)
		if err != nil {
			return nil, fmt.Errorf("pv_match: invalid regex %q: %w", uc.PVMatch, err)
		}
		if !pvRe.MatchString(pv) {
			return nil, fmt.Errorf("ebuild PV %q did not match pv_match pattern %q", pv, uc.PVMatch)
		}
		opts.PVCaptures = ExtractCaptures(pvRe, pv)
	}

	// 3. Compile tag_match
	if uc.TagMatch != "" {
		tagRe, err := regexp.Compile(uc.TagMatch)
		if err != nil {
			return nil, fmt.Errorf("tag_match: invalid regex %q: %w", uc.TagMatch, err)
		}
		opts.TagMatchRe = tagRe
	}

	return opts, nil
}
