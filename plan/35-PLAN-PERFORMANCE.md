# Plan 35 — Performance Optimisations

**Precondition:** Plans 12–34 complete. `go build ./...` clean, `go vet` clean, `golangci-lint` clean.

**Goal:** Reduce wall-clock time for `larry upstream` (the dominant user-facing operation) on a typical overlay of 500–2000 packages. Eliminate wasted I/O, parallelise network calls, and compile regexes once.

---

## P1 — Parallelise upstream version checks

**Severity:** 🔴 Primary bottleneck. `larry upstream` checks packages strictly sequentially — even with a rate limiter that advertises `GlobalConcurrency: 8`, the callers never fire goroutines.

**Current state:** `walkAndCheckTargets` iterates targets in a for-loop and calls `resolveOne` → `FetchResolution` / `FetchLatestVersion` synchronously. Each package waits for the previous one to finish.

**Fix:**
1. In `walkAndCheckTargets`, batch-process targets in groups of `maxConcurrency` (default 8, configurable).
2. Within each batch, fire `resolveOne` in goroutines and collect results via channels.
3. Share the `VersionCache` across goroutines (already has `sync.RWMutex`).
4. Respect `visited` map with a `sync.Mutex` to prevent duplicate dependency scans.
5. Preserve the current output order by sorting results before returning (or preserve original order via an index channel).

**Files:** `internal/upstream/upstream.go` — `walkAndCheckTargets`, `resolveOne`
**Tests:** Add a test that verifies parallel execution with a timing assertion (e.g. 16 targets with a 100ms fake provider must complete in < 200ms, not ~1600ms).

---

## P2 — Compile regexes once in hot paths

**Severity:** 🟡 Moderate. Regexes compiled inside hot loops waste CPU and cause unnecessary allocations.

### P2a — `internal/upstream/dependencies.go`

Four regexes are compiled fresh inside `extractAtoms` on every call:

```go
useFlagRe := regexp.MustCompile(`\w+\?\s*\(([^)]+)\)`)
disjunctionRe := regexp.MustCompile(`\|\|\s*\(([^)]+)\)`)
braceRe := regexp.MustCompile(`\[[^\]]*\]`)
slotRe := regexp.MustCompile(`:\S+`)
```

**Fix:** Move to package-level `var` declarations (already have `depAtomRe` and `depVerRe` at package level).

### P2b — `internal/upstream/upstream.go`

`versionLikeRe` is already package-level ✅.

`npmAssignRe` is already package-level ✅.

**Files:** `internal/upstream/dependencies.go`

**Tests:** No behavioural change; existing tests cover `extractAtoms`.

---

## P3 — Reduce ebuild file reads during upstream

**Severity:** 🟡 Medium. Each `resolveOne` call:

1. Reads the ebuild file once (`ParseEbuildInfo` → `os.ReadFile`)
2. Reads `metadata.xml` once (`ResolveRemoteId` → `remoteid.ResolveRemoteId` → `os.ReadFile`)

For overlays with 1000+ packages, that's 2000+ small file reads. On network-mounted overlays this is expensive.

**Fix:**
1. `discoverTargets` already reads each package directory. Add a small in-memory cache: read the ebuild content once, pass it to `ParseEbuildInfo` via a new `ParseEbuildInfoFromBytes` variant.
2. Cache `metadata.xml` reads keyed by package directory (already deduped by `remoteid.ResolveRemoteId` per ebuild path, but a global cache per `upstream` invocation would save repeated reads during dep scanning).

**Files:** `internal/upstream/upstream.go`, `internal/ebuild/ebuild.go`
**Tests:** Existing tests still pass; add a test with a mock file reader to verify single-read behaviour.

---

## P4 — Parallelise prepare --all manifest generation

**Severity:** 🟡 Moderate. `PrepareManifestAll` iterates cat/pkg directories sequentially, spawning `pkgdev manifest` for each one. These are independent.

**Current:**

```go
for _, cp := range dirs {
    // run pkgdev manifest for cp
}
```

**Fix:**
1. Fan-out `pkgdev manifest` calls into goroutines (bounded by a semaphore, default 8).
2. Collect errors, return aggregated error with count.
3. Preserve the existing sequential behaviour as an option (`--manifest-serial` flag) for debugging.

**Files:** `internal/prepare/prepare.go` — `PrepareManifestAll`
**Tests:** Existing tests; add a concurrency test with 20 fake manifests.

---

## P5 — Eliminate redundant variable map scanning

**Severity:** 🟢 Minor. `npmVarMap` and `BuildVariableMap` both scan the entire raw ebuild text with `regexp.FindAllStringSubmatch` on every call. In `resolveOne`, `npmVarMap` is called once per npm package; in `ParseEbuildInfo`, `npmAssignRe` is also called.

**Fix:**
1. In `ParseEbuildInfo`, compute the variable map once and reuse it for DEPEND/RDEPEND/BDEPEND expansion AND for `npmVarMap`-style lookups.
2. Add a `varMap` field to `EbuildInfo` to cache the result.
3. `npmVarMap` should read from `EbuildInfo.VarMap` instead of rescanning.

**Files:** `internal/ebuild/ebuild.go`, `internal/upstream/upstream.go`

---

## P6 — Optimise `isPureAssignment`

**Severity:** 🟢 Minor. `isPureAssignment` scans character-by-character to find the first `=`:

```go
for i, c := range entry {
    if c == '=' { ... }
}
```

**Fix:** Replace with `idx := strings.Index(entry, "=")`.

**Files:** `internal/ebuild/srcuri.go`

---

## P7 — Pre-allocate slices in hot paths

**Severity:** 🟢 Minor. Several hot paths use `append` without capacity hints.

**Locations:**
- `discoverTargets`: `targets` slice pre-allocated to estimated overlay size (e.g. read `categories` length).
- `extractAtoms`: `atoms` slice pre-allocated to `len(matches)`.
- `collectCatPkgs`: pre-allocate with `len(entries)` capacity hint.
- `ScanDependencies`: `results` slice pre-allocated.

**Files:** As listed above

---

## P8 — Share HTTP client across providers

**Severity:** 🟡 Medium. Each `GitHubProvider` creates its own `*github.Client` (which spawns `gh` as a subprocess). For HTTP-based providers (PyPI, NPM, GitLab, Codeberg, SourceHut), each creates a new `http.Client`. With 1000 packages and 6 providers, that's potentially 6000 separate HTTP clients if not cached.

**Current:** `NewGitHubProvider` creates a new client every time; providers are created fresh per `RunWithTargets` call.

**Fix:**
1. Create ONE `http.Client` (via `httpclient.Client`) and pass it to all providers.
2. The rate limiter's `perHost` ticker map already shares tickers across calls — but a new `http.Client` means new connection pools.
3. Share the rate limiter across all providers.

**Files:** `cmd/larry/upstream.go`, `internal/provider/registry.go`, `internal/providers/*.go`

---

## P9 — Batch metadata.xml reads

**Severity:** 🟢 Minor. Multiple packages in the same category may share the same `metadata.xml` in the overlay root. Each `ResolveRemoteId` call re-parses it.

**Fix:** Cache metadata.xml content by directory path within a single `upstream` invocation.

**Files:** `internal/remoteid/metadata.go` — add a package-level `sync.Map` cache keyed by directory path.

---

## P10 — `discoverTargets` incremental sort

**Severity:** 🟢 Minor. `discoverTargets` builds a slice then calls `sort.Slice` on it at the end. The overlay categories and packages are already in a somewhat-ordered state from `os.ReadDir` on most filesystems.

**Fix:** Minor optimisation — pre-allocate targets to `estimatedCount` and use a simpler comparison. Already package-level, no algorithmic change.

**Files:** `internal/upstream/upstream.go`

---

## Verification tiers

### Tier 1 — automated, required

```bash
go build ./...
go vet ./...
go test ./... -count=1
golangci-lint run ./...
```

### Tier 2 — read-only manual

```bash
# Time the upstream check on the reference overlay
time larry upstream ~/Developer/gentoo-overlay
# Compare against baseline (record before any changes)
```

### Tier 3 — MUTATING (do not run — report only)

No Tier 3 steps for this plan. All changes are internal to the Go code and covered by tests.

---

## Suggested execution order

1. **P2** (regex compile) — trivial, zero risk, standalone
2. **P6** (isPureAssignment) — trivial, zero risk
3. **P3** (reduce ebuild reads) — moderate, needs test coverage
4. **P1** (parallelise upstream) — highest impact, most complex
5. **P5** (variable map cache) — needs P3 first
6. **P4** (parallelise prepare) — separate from upstream, can be done anytime
7. **P7, P8, P9, P10** — polish, can be done in any order

---

## Expected impact

| Change | Estimated improvement |
|--------|----------------------|
| P1 (parallelise upstream) | 5–10× on large overlays (1000+ pkgs) |
| P8 (share HTTP client) | 10–30% on network-heavy checks |
| P2 (regex compile once) | ~5% on dependency scanning |
| P3 (reduce file reads) | 5–15% on network mounts |
| P4 (parallelise prepare) | 3–5× for `--all` manifest |
| P5, P6, P7, P9, P10 | < 5% each, additive |

**Total expected:** 5–10× faster `larry upstream` on large overlays.
