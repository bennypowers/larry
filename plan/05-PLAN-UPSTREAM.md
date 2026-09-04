# Upstream Command Tasks (T10–T11)

**What this covers:** `larry upstream` — provider-based version checking, CLI, caching, dependency scanning.
**Why fifth:** The most provider-heavy command; good testbed for the full domain model.

---

### T10 — `larry upstream` — providers + CLI + cache + output

**Precondition:** T4, T5, T6, T7, T8, T8b, T9 complete

**Tests:**
- `TestUpstream_CheckGitHub_Outdated` — overlay has 1.0.0, upstream has 2.0.0 → `OUTDATED`
- `TestUpstream_CheckGitHub_UpToDate` — both 2.0.0 → `UP-TO-DATE`
- `TestUpstream_CheckGitHub_Ahead` — overlay has 2.0.0, upstream has 1.0.0 → `AHEAD`
- `TestUpstream_CheckPyPI_Outdated` → `OUTDATED`
- `TestUpstream_CheckNPM_Outdated` → `OUTDATED`
- `TestUpstream_CheckGitLab_Outdated` → `OUTDATED`
- `TestUpstream_CheckCodeberg_Outdated` → `OUTDATED`
- `TestUpstream_LivePackage` — 9999 → `LIVE`
- `TestUpstream_CacheHit` — second call within 6h uses cache
- `TestUpstream_CacheMiss` — after 6h, fresh query made
- `TestUpstream_FreshFlag` — `--fresh` bypasses cache
- `TestUpstream_PrereleaseFiltered` — latest is `2.0.0-beta.1`, default → next stable → `OUTDATED`
- `TestUpstream_PrereleaseIncluded` — `--prereleases` → `2.0.0-beta.1` → `OUTDATED`
- `TestUpstream_NoRemoteId` — falls back to SRC_URI scraping: extract project URL from SRC_URI/HOMEPAGE, match against known patterns (github.com → GitHubProvider, registry.npmjs.org → NpmProvider, etc.)
- `TestUpstream_SkipAcct` — `acct-group/foo` skipped
- `TestUpstream_UnknownProvider` — no matching provider → `UNKNOWN`

**CLI tests (via cobra):**
- `larry upstream` — scans all packages
- `larry upstream cat/pkg` — checks single package
- `larry upstream cat/pkg2` — checks multiple
- `larry upstream --fresh` — bypasses cache
- `larry upstream --verbose` — includes UP-TO-DATE
- `larry upstream --prereleases` — includes prereleases

**Implementation:**
1. `cmd/larry/upstream.go` — cobra command with flags
2. `internal/upstream/upstream.go` — core logic:
   - Iterate all ebuilds in overlay (walk category dirs)
   - For each: detect type, resolve provider, fetch version, compare
   - Apply prerelease filter, cache, rate limiting
   - Build result rows: `{Package, Current, Latest, Status, Source}`
3. `internal/upstream/cache.go` — version cache in `$XDG_CACHE_HOME/larry/overlay-version-check-cache/`
   - Key: `cat/pkg/provider:name` → `{Version, Timestamp}`
   - TTL: 6 hours
   - Atomic writes (write to temp, rename)
4. `internal/upstream/output.go` — table renderer with colors:
   - OUTDATED=red, UP-TO-DATE=green, AHEAD=blue, NEWER-TAG=yellow, LIVE/UNKNOWN=dim
   - OSC 8 hyperlinks on source column
5. Skip rules: `acct-group/*`, `acct-user/*`, live (`-9999`) unless `--verbose`

**Acceptance:** All provider types report correct status. Cache works. Colors render. `--verbose`, `--fresh`, `--prereleases` flags work.

---

### T11 — `larry upstream` — dependency scanning (recursive, 3 levels)

**Precondition:** T10 complete

**Tests:**
- `TestDepScanner_ExtractAtoms` — strips `>=`, `<=`, `:0`, `=`, slots, version specs
- `TestDepScanner_ExtractAtoms_UseFlag` — `python? ( dev-python/requests )` → `dev-python/requests`
- `TestDepScanner_ExtractAtoms_Disjunction` — `|| ( dev-libs/foo dev-libs/bar )` → both
- `TestDepScanner_ExtractAtoms_Brace` — `app-arch/bzip2[bzip2?]` → `app-arch/bzip2`
- `TestDepScanner_ScanDEPEND` — extracts from DEPEND= line
- `TestDepScanner_ScanRDEPEND` — extracts from RDEPEND= line
- `TestDepScanner_Recursive3Levels` — A→B→C→D stops at level 3
- `TestDepScanner_CircularRef` — A→B→A does not loop
- `TestDepScanner_SkipInstalled` — already-installed deps skipped
- `TestDepScanner_ResultsMerged` — dependency statuses merged into parent output

**Implementation:**
1. `internal/upstream/dependencies.go`:
   ```go
   type DepNode struct {
       Atom     string
       Version  string
       Status   Status
       Level    int
       Children []DepNode
   }
   ```
2. Dependency extraction: `[a-z][a-z0-9_-]+/[a-z][a-z0-9_-]+` after stripping operators/slots/conditionals
3. Recursive DFS up to 3 levels with visited set to prevent cycles
4. Merge dependency results into parent row output (indented)
5. `--verbose` shows dependency tree

**Acceptance:** All dependency patterns from SPEC parse correctly. Recursive scanning stops at 3 levels. Cycles handled.
