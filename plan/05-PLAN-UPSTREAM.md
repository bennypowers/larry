# Upstream Command (T10–T11)

**Why:** Provider-heavy command; tests full domain model.
**Precondition:** T4, T5, T6, T7, T8, T8b, T9
**Note:** T10 uses `httpclient.NewClientWithTransport(rl, transport)` from T8b to inject `httptest.Server` for PyPI/NPM/GitLab/Codeberg provider tests.

### Status: ✅ **COMPLETE** — 33 tests, 4 files, CLI subcommand registered

---

### T10 — `larry upstream` — providers + CLI + cache + output

**Precondition:** T4, T5, T6, T7, T8, T8b, T9
**Tests:**
- ✅ `TestCheckUpstream_MockOutdated` — mock provider returns 2.0.0, current 1.0.0 → OUTDATED
- ✅ `TestCheckUpstream_MockUpToDate` — same version → UP-TO-DATE
- ✅ `TestCheckUpstream_MockAhead` — upstream 0.5.0, current 1.0.0 → AHEAD
- ✅ `TestCheckUpstream_PyPIOutdated` — real PyPI via httptest server
- ✅ `TestCheckUpstream_SkipAcctGroup` — acct-group/* skipped
- ✅ `TestCheckUpstream_SkipAcctUser` — acct-user/* skipped
- ✅ `TestCheckUpstream_LivePackage` — PV=9999 → LIVE
- ✅ `TestCheckUpstream_PrereleaseFiltered` — prerelease excluded without --prereleases
- ✅ `TestCheckUpstream_PrereleaseIncluded` — prerelease included with --prereleases
- ✅ `TestCheckUpstream_UnknownProvider` — no provider match → UNKNOWN
- ✅ `TestRun_PyPIOutdated` — full overlay walk with mock provider: requests=OUTDATED, flask=UP-TO-DATE, livepkg=LIVE
- ✅ `TestRun_EmptyOverlay` — no ebuilds → empty results
- ✅ `TestRunWithCategories_FilterByCategory` — only specified categories checked
- ✅ `TestCache_Miss`/`Cache_Hit`/`Cache_DiskPersistence`/`Cache_AtomicWrite`/`Cache_TTL6h` — 6h TTL, disk persistence
- ✅ `TestRenderTable_*` — colored table output for all statuses
- ✅ `TestDetectProviderFromSRCURI_*` — PyPI, NPM, GitHub detection from mirror URIs
- ✅ `TestResolveSourceURL_*` — source URL generation for PyPI/NPM

**Implementation:**
1. ✅ `cmd/larry/upstream.go` — cobra command with `--fresh`, `--verbose`, `--prereleases` flags
2. ✅ `internal/upstream/upstream.go` — full `CheckUpstream` and `RunWithCategories`:
   - Walk overlay dirs, detect ebuild types from SRC_URI
   - Resolve providers via registry
   - Fetch latest versions, compare
   - Prerelease filter, cache integration
   - Skip rules: `acct-group/*`, `acct-user/*`, live `-9999`
3. ✅ `internal/upstream/cache.go` — 6h TTL, `$XDG_CACHE_HOME/larry/version-check-cache/`, key format `cat/pkg/provider:name`, atomic file writes via temp+rename
4. ✅ `internal/upstream/output.go` — colored table: OUTDATED=red, UP-TO-DATE=green, AHEAD=blue, LIVE/UNKNOWN=dim, OSC 8 hyperlinks on source
5. ✅ `internal/providers/providerlist.go` — `AllProviders()` helper for registry initialization

**Acceptance:** ✅ All providers report status. Cache works with 6h TTL. Colors render. CLI flags work. 33 tests pass across 4 files.

---

### T10 — `larry upstream` — providers + CLI + cache + output

**Tests:**
- `TestUpstream_CheckGitHub_Outdated/UpToDate/Ahead` — overlay 1.0.0 vs upstream 2.0.0 → OUTDATED/UP-TO-DATE/AHEAD
- `TestUpstream_CheckPyPI_Outdated` / `CheckNPM_Outdated` / `CheckGitLab_Outdated` / `CheckCodeberg_Outdated` — via httptest (resolves T8b deferred HTTP provider tests)
- `TestUpstream_LivePackage` — 9999 → LIVE
- `TestUpstream_CacheHit` — 2nd call within 6h uses cache
- `TestUpstream_CacheMiss` — after 6h, fresh query
- `TestUpstream_FreshFlag` — `--fresh` bypasses cache
- `TestUpstream_PrereleaseFiltered` — 2.0.0-beta.1 → next stable → OUTDATED
- `TestUpstream_PrereleaseIncluded` — `--prereleases` → beta.1 → OUTDATED
- `TestUpstream_NoRemoteId` — SRC_URI scrape → URL pattern → provider fallback
- `TestUpstream_SkipAcct` — `acct-group/*` skipped
- `TestUpstream_UnknownProvider` — no match → UNKNOWN
- CLI tests: `larry upstream` (all), `cat/pkg` (single), multiple pkgs, `--fresh`, `--verbose`, `--prereleases`

**Implementation:**
1. `cmd/larry/upstream.go` — cobra command with flags
2. `internal/upstream/upstream.go` — walk overlay dirs, detect type, resolve provider, fetch version, compare. Prerelease filter, cache, rate limiting. Result: `{Package, Current, Latest, Status, Source}`
3. `internal/upstream/cache.go` — `$XDG_CACHE_HOME/larry/overlay-version-check-cache/`, key `cat/pkg/provider:name`, 6h TTL, atomic writes
4. `internal/upstream/output.go` — colored table: OUTDATED=red, UP-TO-DATE=green, AHEAD=blue, NEWER-TAG=yellow, LIVE/UNKNOWN=dim. OSC 8 hyperlinks on source.
5. Skip rules: `acct-group/*`, `acct-user/*`, live `-9999` unless `--verbose`

**Acceptance:** All providers report status. Cache works. Colors render. Flags work.

---

### T11 — `larry upstream` — dependency scanning (recursive, 3 levels)

**Precondition:** T10

**Tests:**
- ✅ `TestExtractAtoms_Simple` — strips `>=`, extracts `dev-lang/python`
- ✅ `TestExtractAtoms_Multi` — multiple atoms: `dev-lang/python`, `dev-java/icedtea-web`
- ✅ `TestExtractAtoms_UseFlag` — `python? ( dev-python/requests )` → `dev-python/requests`
- ✅ `TestExtractAtoms_Disjunction` — `|| ( foo bar )` → both atoms
- ✅ `TestExtractAtoms_BraceExpansion` — `app-arch/bzip2[bzip2?]` → `app-arch/bzip2`
- ✅ `TestExtractAtoms_Slot` — `>=dev-libs/glib-2:0` → `dev-libs/glib-2` (slot stripped, package suffix preserved)
- ✅ `TestExtractAtoms_VersionSpec` — `>=dev-lang/perl-5.32.0` → `dev-lang/perl`
- ✅ `TestExtractAtoms_Empty` — empty/whitespace → no atoms
- ✅ `TestScanDEPEND` / `ScanRDEPEND` — extracts from both DEPEND and RDEPEND
- ✅ `TestDepNode_RootNode` — `DepNode{Atom, Version, Status, Level, Children}`
- ✅ `TestRecursiveScan_3Levels` — A→B,C,D: 3 nodes at level 1
- ✅ `TestRecursiveScan_CircularRef` — A→B→A doesn't hang
- ✅ `TestRecursiveScan_NoCycleInfinite` — bidirectional cycle handled
- ✅ `TestMergeDepStatus_*` — Outdated/UpToDate/Ahead merging

**Implementation:**
1. ✅ `internal/upstream/dependencies.go` — `DepNode{Atom, Version, Status, Level int, Children []DepNode}`
2. ✅ Regex: `[a-z][a-z0-9_-]+/[a-z][a-z0-9_.-]*` after stripping operators/slots/conditionals
3. ✅ `stripVersion()` — strips `:slot` and version-like patterns (`-1.0`, `-9999`, `-100`) but preserves package suffixes (`glib-2`)
4. ✅ Recursive DFS with visited set prevents cycles
5. ✅ `ScanDependencies(ei, registry, allowPrereleases)` — public API with 3-level default depth
6. ✅ `MergeDepStatuses(parent, deps)` — merges child statuses into parent

**Acceptance:** ✅ All spec patterns parse. 3-level recursion. Cycle-safe. Version stripping distinguishes versions (`-3.9`) from package suffixes (`glib-2`).

**Precondition:** T10

**Tests:**
- `TestDepScanner_ExtractAtoms` — strips `>=`, `<=`, `:0`, `=`, slots, version specs
- `TestDepScanner_ExtractAtoms_UseFlag` — `python? ( dev-python/requests )` → atom
- `TestDepScanner_ExtractAtoms_Disjunction` — `|| ( foo bar )` → both
- `TestDepScanner_ExtractAtoms_Brace` — `app-arch/bzip2[bzip2?]` → `app-arch/bzip2`
- `TestDepScanner_ScanDEPEND` / `ScanRDEPEND` — extracts from DEPEND=/RDEPEND=
- `TestDepScanner_Recursive3Levels` — A→B→C→D stops at 3
- `TestDepScanner_CircularRef` — A→B→A does not loop
- `TestDepScanner_SkipInstalled` — installed deps skipped
- `TestDepScanner_ResultsMerged` — dep statuses merged into parent
- CLI: `--verbose` shows dep tree

**Implementation:**
1. `internal/upstream/dependencies.go` — `DepNode{Atom, Version, Status, Level int, Children []DepNode}`
2. Regex: `[a-z][a-z0-9_-]+/[a-z][a-z0-9_-]+` after stripping operators/slots/conditionals
3. Recursive DFS up to 3 levels, visited set prevents cycles
4. Merge into parent row (indented)

**Acceptance:** All spec patterns parse. 3-level recursion. Cycle-safe.
