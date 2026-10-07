# 32 — `upstream`: per-package `.larry.yaml` rules replace global tag filtering

**Why:** The global `IsPureDateTag` filter is over-broad — it discards tags matching `^\d+\.\d{8}$` and `^20\d{6}$` across ALL repos. Some packages genuinely use date-based versions (e.g. `20250410_rc1`). The correct fix is per-package rules in `.larry.yaml`: filter firmware date tags for `raspberrypi-kernel` only, with optional `strip_prefixes` for `stable_`.

**Precondition:** Plan 17b (tier 4, `fetchLatestTagByDate`).

**Status:** ✅ COMPLETE — all steps implemented, all tests green, golangci-lint clean.

## Defects

| ID   | Location                                            | Defect                                                               |
| ---- | --------------------------------------------------- | -------------------------------------------------------------------- |
| D1   | `internal/providers/github.go:tryParseVersion`      | Global `IsPureDateTag` filter discards valid CalVer across all repos |
| D2   | `internal/providers/github.go:fetchLatestTagByDate` | Same global filter in tier 4                                         |
| D3   | `internal/version/version.go:IsPureDateTag`         | Hardcoded firmware-date heuristic, not configurable                  |
| D4   | `internal/prepare/config.go`                        | `.larry.yaml` loading is prepare-only, not shared                    |

## Design

### Config schema (`.larry.yaml`)

```yaml
type_override: cargo
vendor_dirs: { mylib: ["src/lib"] }
cleanup_orphaned_assets: true

upstream:
  ignore_tags:          # Regex patterns for tags to skip (compiled at load time)
    - "^\d+\.\d{8}$"    # Raspberry Pi firmware dates: 1.20230405
  strip_prefixes:       # Prefixes to strip before version parsing (applied in order)
    - "stable_"
    - "oldstable_"
  strategy: semver      # Optional hint; not yet consumed (future: date-based, calver, etc.)

```

### `ignore_tags` safety & matching target

Regex patterns are compiled at config-load time. Evaluated against **both** the raw tag name (e.g. `v1.20230405`) and the stripped tag name (e.g. `1.20230405`) to ensure prefix removal doesn't bypass user regexes.

### `strip_prefixes` semantics

Applied in order. First match wins. Custom prefixes are stripped **before** standard digit checks. `stable_20260911` + `["stable_", "oldstable_"]` → `20260911`.

### Provider state lifecycle

`GitHubProvider` holds a `ResolveOptions` pointer. `resolveOne` **unconditionally resets** `ghProv.ResolveOptions = resOpts` on every package iteration (setting it to `nil` when a package lacks `.larry.yaml`), preventing state leak between packages during a multi-package walk.

## Implementation Steps

### Step 1 — Shared config package (`internal/larryconfig`)

Create `internal/larryconfig/` (new package):

```go
package larryconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Config represents the full .larry.yaml schema.
type Config struct {
	TypeOverride          string              `yaml:"type_override,omitempty"`
	VendorDirs            map[string][]string `yaml:"vendor_dirs,omitempty"`
	CleanupOrphanedAssets bool                `yaml:"cleanup_orphaned_assets,omitempty"`
	Upstream              *UpstreamConfig     `yaml:"upstream,omitempty"`
}

// UpstreamConfig holds per-package upstream resolution rules.
type UpstreamConfig struct {
	IgnoreTags    []string `yaml:"ignore_tags,omitempty"`
	StripPrefixes []string `yaml:"strip_prefixes,omitempty"`
	Strategy      string   `yaml:"strategy,omitempty"`
}

// ResolveOptions is the compiled, runtime form of UpstreamConfig.
type ResolveOptions struct {
	IgnoreTagRe   []*regexp.Regexp
	StripPrefixes []string
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

// Compile compiles regex patterns into ResolveOptions.
func (uc *UpstreamConfig) Compile() (*ResolveOptions, error) {
	if uc == nil {
		return nil, nil
	}
	opts := &ResolveOptions{
		StripPrefixes: uc.StripPrefixes,
	}
	for i, pat := range uc.IgnoreTags {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("ignore_tags[%d]: invalid regex %q: %w", i, pat, err)
		}
		opts.IgnoreTagRe = append(opts.IgnoreTagRe, re)
	}
	return opts, nil
}

```

### Step 2 — Refactor `prepare` to use shared config

* Delete `internal/prepare/config.go`.
* Update callers in `internal/prepare/` to use `larryconfig.LoadConfig(filepath.Dir(ebuildPath))`.

### Step 3 — Add `ResolveOptions` and Helpers to `GitHubProvider`

In `internal/providers/github.go`:

```go
type ResolveOptions struct {
	IgnoreTagRe   []*regexp.Regexp
	StripPrefixes []string
}

type GitHubProvider struct {
	RateLimiter    *ratelimit.RateLimiter
	Client         *github.Client
	TagLister      func(url string) ([]string, error)
	ResolveOptions *ResolveOptions
}

// isIgnored checks if raw or stripped tag matches any ignore_tags pattern.
func (p *GitHubProvider) isIgnored(raw, stripped string) bool {
	if p.ResolveOptions == nil {
		return false
	}
	for _, re := range p.ResolveOptions.IgnoreTagRe {
		if re.MatchString(raw) || re.MatchString(stripped) {
			return true
		}
	}
	return false
}

// stripCustomPrefixes applies standard prefix stripping followed by strip_prefixes.
func (p *GitHubProvider) stripCustomPrefixes(tag string) string {
	stripped := stripTagPrefix(tag)
	if p.ResolveOptions != nil {
		for _, pfx := range p.ResolveOptions.StripPrefixes {
			if strings.HasPrefix(stripped, pfx) {
				return strings.TrimPrefix(stripped, pfx)
			}
		}
	}
	return stripped
}

```

### Step 4 — Convert `tryParseVersion` and Update Tier 3 (`github.go`)

Convert `tryParseVersion` to a method on `GitHubProvider`:

```go
func (p *GitHubProvider) tryParseVersion(tag string) (*version.Version, error) {
	stripped := p.stripCustomPrefixes(tag)

	if p.isIgnored(tag, stripped) {
		return nil, fmt.Errorf("tag %q matches ignore pattern", tag)
	}

	if stripped == "" || !versionStartsWithDigit(stripped) {
		return nil, fmt.Errorf("tag %q does not look like a version", tag)
	}
	norm := version.Normalize(stripped)
	return version.Parse(norm)
}

```

Update `fetchLatestTagByRemote` (Tier 3) so custom prefixes and ignore rules are applied prior to `versionStartsWithDigit`:

```go
	var tags []string
	for _, tag := range rawTags {
		stripped := p.stripCustomPrefixes(tag)
		if p.isIgnored(tag, stripped) {
			continue
		}
		if stripped != "" && versionStartsWithDigit(stripped) {
			tags = append(tags, stripped)
		}
	}

```

### Step 5 — Update `fetchLatestTagByDate` in `github.go`

In the GraphQL node loop, use `p.isIgnored` with `prefixStripped`. Keep `version.ExtractDate` operating on `rawName`:

```go
	for _, node := range result.Data.Repository.Refs.Nodes {
		rawName := strings.TrimSpace(node.Name)
		if prereleaseTagRe.MatchString(rawName) {
			continue
		}

		prefixStripped := p.stripCustomPrefixes(rawName)
		if p.isIgnored(rawName, prefixStripped) {
			continue
		}

		// NOTE: ExtractDate operates on rawName (not prefixStripped)
		// to extract YYYYMMDD directly from tag names like firmware-20260824
		if extractedDate, ok := version.ExtractDate(rawName); ok {
			return rawName, extractedDate, nil
		}

		raw := node.Target.dateString()
		if len(raw) >= 10 {
			return rawName, strings.ReplaceAll(raw[:10], "-", ""), nil
		}
	}

```

### Step 6 — Wire config loading in `resolveOne` (`internal/upstream/upstream.go`)

Unconditionally set `ghProv.ResolveOptions` to clear previous package state:

```go
	var resOpts *providers.ResolveOptions
	if eb.Path != "" {
		if cfg, err := larryconfig.LoadConfig(filepath.Dir(eb.Path)); err == nil && cfg.Upstream != nil {
			if opts, err := cfg.Upstream.Compile(); err == nil {
				resOpts = opts
			}
		}
	}

	// UNCONDITIONAL SET: Always reset ResolveOptions (even if resOpts is nil)
	// to prevent state leaking from previously resolved packages.
	if ghProv, ok := p.(*providers.GitHubProvider); ok {
		ghProv.ResolveOptions = resOpts
	}

```

### Step 7 — Remove global `IsPureDateTag` calls

IsPureDateTag may be left over from previous attempts to fix this issue
If so,

* Delete `IsPureDateTag` and `firmwareDateRe` from `internal/version/version.go`.
* Remove `TestIsPureDateTag` from `internal/version/version_test.go`.
* Clean up outdated tests in `internal/providers/github_test.go` that relied on `IsPureDateTag`.

### Step 8 — Tests

#### `internal/larryconfig/config_test.go` (new)

* `TestLoadConfig_NotFound` — empty config, no error
* `TestLoadConfig_InvalidYAML` — parse error
* `TestLoadConfig_Valid` — YAML tags mapped properly (`ignore_tags`, `strip_prefixes`)
* `TestCompile_ValidAndInvalid` — regex compilation & error reporting

#### `internal/prepare/prepare_test.go`

* Run `go test ./internal/prepare/... -count=1` — verify all existing `prepare` tests pass after deleting `internal/prepare/config.go` and switching to `larryconfig`.

#### `internal/providers/github_test.go`

* `TestGitHub_TryParseVersion_IgnoreTags` — verifies matching against raw and stripped names
* `TestGitHub_Tier3_StripPrefixes` — custom `strip_prefixes` allowed in `ls-remote` list
* `TestGitHub_Tier4_IgnoreTags` — firmware date skipped in GraphQL
* `TestGitHub_ProviderOptionsReset` — concrete design below:

```go
func TestGitHub_ProviderOptionsReset(t *testing.T) {
	p := &GitHubProvider{}

	// 1. Package A with ignore rule (e.g. ignore firmware dates)
	p.ResolveOptions = &ResolveOptions{
		IgnoreTagRe: []*regexp.Regexp{regexp.MustCompile(`^\d+\.\d{8}$`)},
	}
	if _, err := p.tryParseVersion("1.20230405"); err == nil {
		t.Fatal("Package A: expected 1.20230405 to be ignored")
	}

	// 2. Clear options (mimicking resolveOne resetting options for Package B with no .larry.yaml)
	p.ResolveOptions = nil

	// 3. Package B with no ignore rule
	v, err := p.tryParseVersion("1.20230405")
	if err != nil {
		t.Fatalf("Package B: expected 1.20230405 to parse successfully when options are reset, got: %v", err)
	}
	if v.String() != "1.20230405" {
		t.Errorf("Package B: parsed version = %q, want 1.20230405", v.String())
	}
}

```

## Acceptance

### Tier 1

```bash
go build ./...
go vet ./...
go test ./internal/larryconfig/... ./internal/version/... ./internal/providers/... ./internal/upstream/... ./internal/prepare/... -count=1

```

### Tier 2

```bash
# Add .larry.yaml to sys-kernel/raspberrypi-kernel and test resolution:
larry upstream --fresh --overlay ~/Developer/gentoo-overlay -v | grep raspberrypi-kernel

# Verify a standard package without .larry.yaml resolves clean with no leaked state:
larry upstream --fresh --overlay ~/Developer/gentoo-overlay -v | head -20

```
