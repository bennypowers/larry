# 32 — `upstream`: filter non-version tags from GitHub tier 4 date-based resolution

**Why:** `raspberrypi-kernel` shows latest `1.20230405` (a firmware date tag from
`raspberrypi/linux`). Tier 4 (`fetchLatestTagByDate`, github.go:248–307) returns
any tag whose `committedDate` matches, including firmware date tags like
`1.20230405` that are NOT kernel versions. The ebuild uses a `COMMIT=` variable
pointing to a specific commit hash, not a versioned tag. Comparing `6.18.46_p20260824`
against `1.20230405` produces `AHEAD` with a meaningless "latest" value.

**Precondition:** 17b (tier 4, `fetchLatestTagByDate`).

**Reference script:** script's `fetch_github_version` (L1132–1220) has the same
tier 4 path but the script also checks the tag against `is_version_like` (L1128–1130).
We inherit this gap.

---

## Defects

| ID | Location | Defect |
|----|----------|--------|
| D1 | `providers/github.go:290-305` | tier 4 returns firmware date tags (`1.20230405`) as version-like results |

---

## Target behaviour

Add a filter to tier 4: after extracting the tag name from the GraphQL response,
skip tags that match the Raspberry Pi firmware date pattern
`^\d+\.\d{8}$` (e.g., `1.20230405`, `1.20260519`). These are firmware release
markers, not package versions.

Add to `internal/version/version.go`:

```go
// IsPureDateTag returns true if s looks like a firmware date tag (series.date
// or pure date), not a meaningful version. Used to filter tier 4 results.
var firmwareDateRe = regexp.MustCompile(`^\d+\.\d{8}$|^20\d{7}$`)

func IsPureDateTag(s string) bool {
    return firmwareDateRe.MatchString(s)
}
```

In `fetchLatestTagByDate` (github.go), in the node loop, add after the
`prereleaseTagRe` skip and before `version.ExtractDate`:

```go
// Skip firmware date tags (e.g. 1.20230405) — not meaningful versions.
stripped := stripTagPrefix(rawName)
if version.IsPureDateTag(stripped) {
    continue
}
```

Also add the same check in `tryParseVersion` (github.go:207–215) for tier 1/2:

```go
// Skip firmware date tags — they are not package versions.
if version.IsPureDateTag(stripped) {
    return nil, fmt.Errorf("tag %q is a firmware date, not a version", tag)
}
```

---

## Tests

### `internal/version/version_test.go` — append

- `TestIsPureDateTag` — table:
  - `1.20230405` → `true`
  - `1.20260519` → `true`
  - `20260824` → `true`
  - `v1.2.3` → `false`
  - `1.2.3` → `false`
  - `firmware` → `false`
  - `pios/1` → `false`

### `internal/providers/github_test.go` — append

- `TestGitHub_Tier4_SkipsFirmwareDates` — tier 4 stub returns two nodes:
  `1.20230405` (committedDate `2023-04-05`) and `v6.1.75` (committedDate `2024-01-01`).
  Expect `Tag == "v6.1.75"`, `ByDate == true` — firmware date is skipped.
- `TestGitHub_Tier4_AllFirmwareDates` — all nodes are firmware date tags.
  Expect error (no version-like tags).
- `TestTryParseVersion_SkipsFirmwareDates` — `tryParseVersion("1.20230405")`
  returns error containing "firmware date".

---

## Implementation steps

1. Add `IsPureDateTag` + `firmwareDateRe` to `internal/version/version.go`.
2. Add `version.IsPureDateTag(stripped)` skip to `tryParseVersion` in
   `internal/providers/github.go`.
3. Add `version.IsPureDateTag(stripped)` skip in `fetchLatestTagByDate` node loop
   in `internal/providers/github.go` (before the `version.ExtractDate` call).
4. Run new tests.
5. Run full suite: `go test ./internal/version/... ./internal/providers/... -count=1`.

---

## Acceptance

### Tier 1

```bash
go build ./...
go vet ./...
go test ./internal/version/... ./internal/providers/... ./internal/upstream/... -count=1
```

### Tier 2

```bash
larry upstream --fresh --overlay ~/Developer/gentoo-overlay -v | grep raspberrypi-kernel
# expect: Status is CHECK-TAG or UP-TO-DATE or AHEAD with a meaningful latest,
# NOT Latest == 1.20230405
```

### Tier 3 — DO NOT RUN

Nothing in this plan is Tier 3.
