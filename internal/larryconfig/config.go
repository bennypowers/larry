package larryconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"go.yaml.in/yaml/v3"
)

// Config represents the full .larry.yaml schema.
type Config struct {
	TypeOverride          string            `yaml:"type_override,omitempty"`
	VendorDirs            map[string][]string `yaml:"vendor_dirs,omitempty"`
	CleanupOrphanedAssets bool              `yaml:"cleanup_orphaned_assets,omitempty"`
	Upstream              *UpstreamConfig   `yaml:"upstream,omitempty"`
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

// LoadConfig reads .larry.yaml from the given directory.
// Returns empty Config (no error) if file not found.
func LoadConfig(dir string) (*Config, error) {
	cfgPath := filepath.Join(dir, ".larry.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("read .larry.yaml: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse .larry.yaml: %w", err)
	}
	return &cfg, nil
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
