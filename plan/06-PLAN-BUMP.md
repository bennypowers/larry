# Bump Command (T12a–T12b)

**Why:** Depends on provider (T6, auto-version) and prepare (T13). Split into file creation + prepare invocation.

---

### T12a — `larry bump` — create bumped ebuild (no prepare dependency)

**Precondition:** T4, T9

**Tests:**
- `TestBump_VersionBump` — `pkg-1.0.0.ebuild` → `pkg-2.0.0.ebuild`, content copied
- `TestBump_VersionBump_Variables` — `${PV}`/`${P}` preserved, hardcoded strings untouched
- `TestBump_RevisionBump` — → `pkg-1.0.0-r1.ebuild`
- `TestBump_RevisionBump_Increment` — `pkg-1.0.0-r2.ebuild` → `pkg-1.0.0-r3.ebuild`
- `TestBump_LiveExcluded` — `-9999.ebuild` not source
- `TestBump_NoEbuilds` — no ebuilds → error
- `TestBump_RevisionNoUpstreamQuery` — `--revision` skips upstream
- `TestBump_PrereleasesFlag` — `--prereleases` allows prerelease resolution

**Implementation:**
1. `cmd/larry/bump.go` — cobra subcommand
2. `internal/bump/bump.go` — find latest non-live ebuild, copy content, rename filename, `--revision` appends `-rN`, version bump queries upstream (auto-version logic tested here)
3. `findLatestEbuild(dir) (path, err)` — sorted by PV descending
4. `incrementRevision(dir) int` — find highest -rN, return N+1

**Acceptance:** Version bumps create correct files. Revision bumps work. Auto-version logic present (provider impl tested T8b/T10).

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
