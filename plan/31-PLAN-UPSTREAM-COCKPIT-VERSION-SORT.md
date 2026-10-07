# 31 — `upstream`: sort GitHub releases by version, not creation date

**Why:** `cockpit-project/cockpit` reports latest `310.10` (creation date leader)
instead of `368` (actual highest version). Plan 17b tier 1 (`/releases/latest`)
and tier 2 (`gh release list`) both sort by **creation date**, not version.
The reconcile step (`github.go:105-112`) compares `relV` vs `tagV` but both
tiers often return the same wrong release. Fix tier 2 to sort by version.

**Precondition:** 17b (GitHub provider, `FetchResolution`, tier 1/2 cascade).

**Reference script:** script does NOT sort by version — it takes `.[0].tagName`
from creation-date order. Plan 17b inherits this script bug. We deliberately diverge.

---

## Defects

| ID | Location | Defect |
|----|----------|--------|
| D1 | `github/github.go:121-140` | `LatestReleaseTag` returns `.[0].tagName` from creation-date sort |

---

## Target behaviour

**`LatestReleaseTag`** (`github/github.go:121-140`): instead of returning
`.[0].tagName`, fetch all 100 release tags and return the highest version.

New implementation:

```go
func (c *Client) LatestReleaseTag(owner, repo string, allowPrereleases bool) (string, error) {
    args := []string{"release", "list", "-R", owner + "/" + repo, "--limit", "100", "--json", "tagName"}
    if !allowPrereleases {
        args = append(args, "--exclude-pre-releases")
    }
    output, err := c.gh(args...)
    if err != nil {
        return "", fmt.Errorf("gh release list: %w", err)
    }

    var releases []struct {
        TagName string `json:"tagName"`
    }
    if err := json.Unmarshal(output, &releases); err != nil {
        return "", fmt.Errorf("parse release list: %w", err)
    }

    // Sort by version number, return the highest.
    // This deliberately diverges from the script which takes .[0] (creation date).
    var best string
    var bestV *version.Version
    for _, r := range releases {
        tag := strings.TrimSpace(r.TagName)
        if tag == "" {
            continue
        }
        stripped := stripTagPrefix(tag)
        if stripped == "" || !versionStartsWithDigit(stripped) {
            continue
        }
        v, err := version.Parse(version.Normalize(stripped))
        if err != nil {
            continue
        }
        if bestV == nil || v.Compare(bestV) > 0 {
            bestV = v
            best = tag
        }
    }

    if best == "" {
        return "", fmt.Errorf("no version-like releases found for %s/%s", owner, repo)
    }
    return best, nil
}
```

Add `import "encoding/json"` and `import "bennypowers.dev/larry/internal/version"`
(if not already imported) to `github/github.go`. Also import
`bennypowers.dev/larry/internal/providers` for `stripTagPrefix` — or duplicate the
logic inline.

Actually, `stripTagPrefix` and `versionStartsWithDigit` are in
`internal/providers/github.go`. Two options:
- A) Export them from `internal/providers` (cleanest)
- B) Duplicate in `internal/github/github.go` (simpler, no cross-package export)

Option A: add to `internal/providers/versionutil.go`:

```go
// StripTagPrefix strips common tag prefixes (exported for use by internal/github).
func StripTagPrefix(tag string) string { return stripTagPrefix(tag) }

// VersionStartsWithDigit checks if a string starts with a digit.
func VersionStartsWithDigit(s string) bool { return versionStartsWithDigit(s) }
```

---

## Tests

### `internal/github/github_test.go` — append

- `TestLatestReleaseTag_SortsByVersion` — stub returns 3 releases:
  `310.10`, `368`, `356.4`. Expect `"368"` (highest version, not `310.10`).
- `TestLatestReleaseTag_EmptyReleases` — stub returns `[]`. Expect error.
- `TestLatestReleaseTag_NoVersionLike` — stub returns `["nightly"]`. Expect error.

### `internal/providers/github_test.go` — update

- `TestGitHub_Tier2_ExcludesPrereleases` — now tests that version sorting
  works with `--exclude-pre-releases`. Update expectations if needed.

---

## Implementation steps

1. Export `stripTagPrefix` and `versionStartsWithDigit` via
   `internal/providers/versionutil.go` (wrappers).
2. Rewrite `LatestReleaseTag` in `internal/github/github.go` as specified above.
3. Add tests per section above.
4. Run full suite.

---

## Acceptance

### Tier 1

```bash
go build ./...
go vet ./...
go test ./internal/github/... ./internal/providers/... ./internal/upstream/... -count=1
```

Grep assertions (must print nothing):
```bash
grep -n '\.\[0\]\.tagName' internal/github/github.go
```

### Tier 2

```bash
larry upstream --fresh --overlay ~/Developer/gentoo-overlay -v | grep cockpit
# expect: Latest >= 367 (or UP-TO-DATE), NOT 310.10
```

### Tier 3 — DO NOT RUN

Nothing in this plan is Tier 3.
