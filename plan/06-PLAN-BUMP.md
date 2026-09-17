# Bump Command (T12a–T12b)

**Why:** Depends on provider (T6, auto-version) and prepare (T13). Split into file creation + prepare invocation.

### Status: ✅ **T12a COMPLETE** — 2 files, 17 tests, 1 CLI subcommand

---

### T12a — `larry bump` — create bumped ebuild (no prepare dependency)

**Precondition:** T4, T9

**Tests:**
- ✅ `TestFindLatestEbuild_SingleEbuild` — one ebuild found
- ✅ `TestFindLatestEbuild_MultipleEbuilds` — highest PV selected
- ✅ `TestFindLatestEbuild_NoEbuilds` — error when empty
- ✅ `TestFindLatestEbuild_LiveExcluded` — 9999 excluded
- ✅ `TestFindLatestEbuild_MixedLiveStable` — stable preferred over live
- ✅ `TestFindLatestEbuild_RevisionsOnly` — highest revision selected when PVs equal
- ✅ `TestIncrementRevision_None` — first revision returns 1
- ✅ `TestIncrementRevision_Increment` — r3 → 4
- ✅ `TestIncrementRevision_SingleRevision` — r2 → 3
- ✅ `TestBump_VersionBump` — `pkg-1.0.0.ebuild` → `pkg-2.0.0.ebuild`, content copied
- ✅ `TestBump_VersionBump_Variables` — `${P}` preserved, hardcoded strings untouched, PV= updated
- ✅ `TestBump_RevisionBump` — → `pkg-1.0.0-r1.ebuild`
- ✅ `TestBump_RevisionBump_Increment` — `pkg-1.0.0-r2.ebuild` → `pkg-1.0.0-r3.ebuild`
- ✅ `TestBump_LiveExcluded` — `-9999.ebuild` not source
- ✅ `TestBump_NoEbuilds` — no ebuilds → error
- ✅ `TestBumpConfig_Validation` — empty config rejected
- ✅ `TestBump_AutoVersionWithProvider` — full auto-version bump with mock provider

**Implementation:**
1. ✅ `cmd/larry/bump.go` — Cobra subcommand with `--revision`, `--version`, `--prereleases`, `--dry-run`, `--overlay`
2. ✅ `internal/bump/bump.go` — `Bump(cfg)`, `findLatestEbuild(dir)`, `incrementRevision(dir)`, `generateBumpedEbuild()`
3. ✅ `findLatestEbuild(dir) (path, err)` — sorted by PV descending, revision tiebreak, live excluded
4. ✅ `incrementRevision(dir) int` — regex `-r(\d+)$` on filenames
5. ✅ `generateBumpedEbuild(path, content, pn, oldPV, newPV)` — updates PV=, P=, SRC_URI; preserves `${PV}`, `${P}`
6. ✅ `providerTypeToRemoteId(pt)` — maps string to `remoteid.RemoteIdType`
7. ✅ `buildRemoteId(ei, providerType)` — constructs `*remoteid.RemoteId` for auto-version queries
8. ✅ `getUpstreamVersion(cfg, pn, content)` — full auto-version pipeline (parse → detect → resolve → fetch)

**Acceptance:** ✅ Version bumps create correct files with updated PV/P/SRC_URI. Revision bumps work with proper -rN naming. Auto-version queries upstream via provider registry. Dry-run mode available.

---

### T12b — `larry bump` — invoke prepare on new ebuild

**Precondition:** T12a, T13

**Tests:**
- `TestBump_PrepareCalled` — after bump, calls prepare with correct type
- `TestBump_Prepare_Failure` — prepare fails → bump file removed, error returned
- `TestBump_Prepare_TypeMismatch` — bump detected npm but prepare dispatches generic → error

**Implementation:**
1. After `internal/bump/bump.go` creates file: detect type via `internal/ebuild.DetectType`, call `internal/prepare.Prepare(newPath, pkgType, dryRun, useTUI)`
2. On prepare failure: remove newly created ebuild, return error
3. CLI: `larry bump --auto` triggers bump+prepare in one command

**Acceptance:** Bumped ebuild prepared automatically. Prepare failure cleans up.
