# Upstream Command (T10–T11)

**Why:** Provider-heavy command; tests full domain model.
**Precondition:** T4, T5, T6, T7, T8, T8b, T9
**Note:** T10 uses `httpclient.NewClientWithTransport(rl, transport)` from T8b to inject `httptest.Server` for PyPI/NPM/GitLab/Codeberg provider tests.

### Status: 🔴 **STUB** — 4 stub files, 0 tests, 0 CLI subcommand

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
