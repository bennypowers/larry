# 33 — `upstream`: regex `tag_match`, `pv_match`, and `version_template` in `.larry.yaml`

**Why:** Upstream repos like `raspberrypi/linux` release tags formatted as `stable_20260911`, while Gentoo ebuilds carry a compound `PV` like `6.18.46_p20260824`. Standard version comparison fails because `20260911` evaluates as greater than major version `6`. By allowing `.larry.yaml` to extract named capture groups from the ebuild `PV` and upstream tag, `larry` can assemble canonical version strings (e.g. `${BASE}_p${DATE}` $\rightarrow$ `6.18.46_p20260911`) using a completely generic template mechanism without hardcoding package-specific strategies in Go.

**Precondition:** Plan 32 (`.larry.yaml` shared `larryconfig` and `ResolveOptions`).

## Design

### Config schema (`.larry.yaml`)

```yaml
upstream:
  # 1. Regex with named capture groups run against ebuild PV (e.g. "6.18.46_p20260824")
  pv_match: '^(?P<BASE>\d+\.\d+\.\d+)(?:_p\d+)?$'

  # 2. Regex with named capture groups run against upstream tag (e.g. "stable_20260911")
  #    Acts as both a tag filter (ignores tags that don't match) and a capture source.
  tag_match: '^stable_(?P<DATE>\d{8})$'

  # 3. Expansion template combining variables from PV and tag captures
  version_template: '${BASE}_p${DATE}'

```

### Variable Capture & Template Expansion Lifecycle

1. **`PV` Capture (`resolveOne` execution time):**
When `resolveOne` loads `.larry.yaml`, `UpstreamConfig.Compile(eb.PV)` runs `pv_match` against the ebuild's `PV`.


* Submatch captures (e.g. `BASE="6.18.46"`) are stored in `ResolveOptions.PVCaptures`.

2. **`Tag` Matching & Expansion (`GitHubProvider` resolution time):**
When testing candidate tags in `tryParseVersion`:
* If `TagMatchRe` is set, non-matching tags are rejected immediately.


* Submatch captures from `tag_match` (e.g. `DATE="20260911"`) are extracted.
* `PVCaptures` and `TagCaptures` are merged.
* If `VersionTemplate` is set, `ExpandTemplate("${BASE}_p${DATE}", mergedVars)` produces `6.18.46_p20260911`.
* The resulting string is parsed into a `version.Version` for comparison.

## Implementation Steps

### Step 1 — Extend `larryconfig` (`internal/larryconfig/config.go`)

Add fields to `UpstreamConfig`, `ResolveOptions`, and implement helper functions for named subexp capture and template expansion:

```go
package larryconfig

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

type UpstreamConfig struct {
	IgnoreTags      []string `yaml:"ignore_tags,omitempty"`
	StripPrefixes   []string `yaml:"strip_prefixes,omitempty"`
	Strategy        string   `yaml:"strategy,omitempty"`
	PVMatch         string   `yaml:"pv_match,omitempty"`
	TagMatch        string   `yaml:"tag_match,omitempty"`
	VersionTemplate string   `yaml:"version_template,omitempty"`
}

type ResolveOptions struct {
	IgnoreTagRe     []*regexp.Regexp
	StripPrefixes   []string
	TagMatchRe      *regexp.Regexp
	VersionTemplate string
	PVCaptures      map[string]string
}

// ExtractCaptures extracts named capture groups from s using re.
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
		if i != 0 && name != "" {
			captures[name] = matches[i]
		}
	}
	return captures
}

// ExpandTemplate replaces ${VAR} or$VAR references using vars map.
func ExpandTemplate(template string, vars map[string]string) string {
	return os.Expand(template, func(key string) string {
		return vars[key]
	})
}

// Compile compiles config regexes and extracts PV captures for a given ebuild PV.
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

```

### Step 2 — Update Tag Match & Template Resolution in `GitHubProvider` (`internal/providers/github.go`)

Update `p.tryParseVersion` to evaluate `TagMatchRe`, perform capture extraction, and apply `VersionTemplate`:

```go
func (p *GitHubProvider) tryParseVersion(tag string) (*version.Version, error) {
	candidateStr := tag

	if p.ResolveOptions != nil {
		// 1. Tag Match Check & Capture Extraction
		if p.ResolveOptions.TagMatchRe != nil {
			if !p.ResolveOptions.TagMatchRe.MatchString(tag) {
				return nil, fmt.Errorf("tag %q did not match tag_match pattern", tag)
			}
			tagCaps := larryconfig.ExtractCaptures(p.ResolveOptions.TagMatchRe, tag)

			// Merge PVCaptures and TagCaptures
			allVars := make(map[string]string)
			for k, v := range p.ResolveOptions.PVCaptures {
				allVars[k] = v
			}
			for k, v := range tagCaps {
				allVars[k] = v
			}

			// Render version template if provided
			if p.ResolveOptions.VersionTemplate != "" {
				candidateStr = larryconfig.ExpandTemplate(p.ResolveOptions.VersionTemplate, allVars)
			}
		}
	}

	// 2. Standard Prefix Stripping & Ignore Filter
	stripped := p.stripCustomPrefixes(candidateStr)
	if p.isIgnored(tag, stripped) {
		return nil, fmt.Errorf("tag %q matches ignore pattern", tag)
	}

	if stripped == "" || !versionStartsWithDigit(stripped) {
		return nil, fmt.Errorf("tag %q does not look like a version", candidateStr)
	}
	norm := version.Normalize(stripped)
	return version.Parse(norm)
}

```

### Step 3 — Update Tier 3 & Tier 4 Filtering in `GitHubProvider`

1. **Tier 3 (`fetchLatestTagByRemote`):**
Filter out tags that fail `TagMatchRe` before sorting:
```go
var tags []string
for _, tag := range rawTags {
    if p.ResolveOptions != nil && p.ResolveOptions.TagMatchRe != nil {
        if !p.ResolveOptions.TagMatchRe.MatchString(tag) {
            continue
        }
    }
    // ... existing prefix stripping and ignore filtering ...
}

```



2. **Tier 4 (`fetchLatestTagByDate`):**
Skip nodes that fail `TagMatchRe`:
```go
for _, node := range result.Data.Repository.Refs.Nodes {
    rawName := strings.TrimSpace(node.Name)
    if prereleaseTagRe.MatchString(rawName) {
        continue
    }
    if p.ResolveOptions != nil && p.ResolveOptions.TagMatchRe != nil {
        if !p.ResolveOptions.TagMatchRe.MatchString(rawName) {
            continue
        }
    }
    // ... existing prefixStripped and date extraction logic ...
}

```



### Step 4 — Wire `eb.PV` into `resolveOne` (`internal/upstream/upstream.go`)

Pass the ebuild `PV` into `cfg.Upstream.Compile(eb.PV)`:

```go
	var resOpts *providers.ResolveOptions
	if eb.Path != "" {
		if cfg, err := larryconfig.LoadConfig(filepath.Dir(eb.Path)); err == nil && cfg.Upstream != nil {
			if opts, err := cfg.Upstream.Compile(eb.PV); err == nil {
				resOpts = opts
			}
		}
	}

	if ghProv, ok := p.(*providers.GitHubProvider); ok {
		ghProv.ResolveOptions = resOpts
	}

```

---

## Step 5 — Unit Tests

#### `internal/larryconfig/config_test.go`

* `TestCompile_PVAndTagMatch`: Verifies `pv_match` extracts captures from `6.18.46_p20260824` into `PVCaptures["BASE"] == "6.18.46"`.
* `TestExpandTemplate`: Verifies `ExpandTemplate("${BASE}_p${DATE}", map[string]string{"BASE":"6.18.46", "DATE":"20260911"})` returns `6.18.46_p20260911`.
* `TestCompile_PVMatchMismatch`: Verifies `Compile("invalid_pv")` returns an error when `pv_match` fails to match.

#### `internal/providers/github_test.go`

* `TestGitHub_TryParseVersion_TemplateExpansion`:
Sets `PVCaptures = map[string]string{"BASE": "6.18.46"}`.
Sets `TagMatchRe = regexp.MustCompile("^stable_(?P<DATE>\\d{8})$")`.
Sets `VersionTemplate = "${BASE}_p${DATE}"`.
Calls `tryParseVersion("stable_20260911")` and asserts returned version string equals `6.18.46_p20260911`.


* `TestGitHub_Tier3_TagMatchFilter`:
Verifies `fetchLatestTagByRemote` ignores tags like `oldstable_20260911` when `TagMatchRe` is set to `^stable_`.

## Acceptance Criteria

### Tier 1

```bash
go build ./...
go vet ./...
go test ./internal/larryconfig/... ./internal/version/... ./internal/providers/... ./internal/upstream/... ./internal/prepare/... -count=1

```

### Tier 2

Place the following configuration in `sys-kernel/raspberrypi-kernel/.larry.yaml`:

```yaml
upstream:
  pv_match: '^(?P<BASE>\d+\.\d+\.\d+)(?:_p\d+)?$'
  tag_match: '^stable_(?P<DATE>\d{8})$'
  version_template: '${BASE}_p${DATE}'

```

Run `larry upstream`:

```bash
larry upstream --fresh --overlay ~/Developer/gentoo-overlay -v | grep raspberrypi-kernel

```

**Expected Output:**
`sys-kernel/raspberrypi-kernel` reports:

* Current: `6.18.46_p20260824`
* Latest: `6.18.46_p20260911` (or latest `stable_` date tag)
* Status: `OUTDATED` (or `UP-TO-DATE`)
