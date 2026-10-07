# 32 — `upstream`: per-package `.larry.yaml` rules replace global tag filtering

**Status:** Plan 32 has been replaced. See `32-PLAN-UPSTREAM-RPI-TIER4-FILTER.md` for the current implementation plan.

## Original problem

`raspberrypi-kernel` uses a `COMMIT=` hash in the ebuild, not a versioned tag.
Tier 4 (`fetchLatestTagByDate`) resolves the **most recently committed** tag on
the GitHub remote, regardless of whether it's version-like.

Result: the resolver returns a firmware date tag as "latest", which compared
against the ebuild PV produces a bogus `AHEAD` status.

## Tag landscape

| Repo | Tag format | Examples |
|---|---|---|
| `raspberrypi/firmware` | `1.YYYYMMDD` | `1.20260915`, `1.20230405` |
| `raspberrypi/linux` | `stable_YYYYMMDD` / `oldstable_YYYYMMDD` | `stable_20260911`, `oldstable_20260911` |
| Gentoo ebuilds | various, date-based versions carry suffixes | `20250410_rc1`, `6.18.46_p20260824` |

The firmware tags (`1.YYYYMMDD`) are **not kernel versions** — they're from a
different repo (`raspberrypi/firmware`). The kernel's own tags (`stable_YYYYMMDD`)
are meaningful.

## What was implemented

Added `IsPureDateTag()` that matches two patterns:

1. `^\d+\.\d{8}$` — `X.YYYYMMDD` (e.g. `1.20230405`)
2. `^20\d{6}$` — bare 8-digit date (e.g. `20260824`)

Applied in three places:
- `tryParseVersion` (tier 1/2) — skips firmware tags before parsing
- `fetchLatestTagByRemote` (tier 3) — skips in go-git tag list
- `fetchLatestTagByDate` (tier 4) — skips in GraphQL node loop

## Current regex vs real-world tags

| Tag | `^\d+\.\d{8}$` | `^20\d{6}$` | Outcome |
|---|---|---|---|
| `1.20260915` | ✅ skip | ❌ | Filtered (correct) |
| `1.20230405` | ✅ skip | ❌ | Filtered (correct) |
| `stable_20260911` | ❌ | ❌ | Passes → date-extracted → version ✅ |
| `oldstable_20260911` | ❌ | ❌ | Same ✅ |
| `20250410_rc1` | ❌ | ❌ | Passes → date-extracted → version ✅ |
| `6.18.46_p20260824` | ❌ | ❌ | Passes → parsed as version ✅ |

## Open questions

### Q1: Is `^\d+\.\d{8}$` exclusive to Raspberry Pi firmware?

This pattern (`1.YYYYMMDD`) is the firmware repo's convention. No other known
project uses this format. If true, this branch of the regex is safe to keep.

**Action:** Search GitHub for other repos using `N.YYYYMMDD` tag patterns.

### Q2: Is `^20\d{6}$` safe for bare 8-digit dates?

No Raspberry Pi project uses bare `YYYYMMDD` as a version. But some projects
outside this ecosystem might. This would silently filter a legitimate version
without error.

**Risk:** Lower than Q1 (bare dates are rare as versions), but non-zero.

**Action:** Decide: keep for paranoia, or drop to avoid silent drops.

### Q3: Should filtering be repo-specific?

Instead of a global filter, only apply to known problematic repos
(`raspberrypi/firmware`, `raspberrypi/linux`).

**Pros:** No risk of filtering legitimate tags from other projects.
**Cons:** More complex; requires remote-id context in the version parsing path;
harder to maintain as new problem repos appear.

**Action:** Worth considering if Q1 reveals this pattern exists elsewhere.

### Q4: What is the actual remote for `raspberrypi-kernel`?

The `raspberrypi-kernel` ebuild uses a `COMMIT=` hash. The upstream resolver
needs to know which GitHub repo to check. Is it `raspberrypi/linux` or
`raspberrypi/firmware`? If it's `linux`, why are firmware tags appearing?

**Hypothesis:** The ebuild's SRC_URI or a remote-id might reference both repos,
or the GraphQL query is pulling tags from a merged/linked source.

**Action:** Check the actual ebuild SRC_URI and the remote-id resolution for
`raspberrypi-kernel`.

### Q5: Should `stable_YYYYMMDD` tags be parsed differently?

They pass through the filter and get date-extracted to `20260911`, then parsed
as a version. This works, but `stable_` prefix stripping isn't explicit — it
relies on `stripTagPrefix` removing the prefix and `ExtractDate` finding the
date inside. This is fragile if tag naming conventions change.

**Action:** Consider explicit `stable_`/`oldstable_` prefix handling in
`stripTagPrefix`.

## Suggested refinement (conservative)

Drop the bare-date branch. Filter only `^\d+\.\d{8}$`:

```go
var firmwareDateRe = regexp.MustCompile(`^\d+\.\d{8}$`)
```

This handles the confirmed firmware contamination case while leaving bare date
tags (if they exist) to be handled by date extraction rather than outright
discarded.

## Files to check

- `raspberrypi-kernel` ebuild — SRC_URI, COMMIT variable, category/package
- `internal/providers/github.go` — remote-id resolution for this package
- `internal/ebuild/detect.go` — how `raspberrypi-kernel` is detected (type)
