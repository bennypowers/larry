# Bump Command Tasks (T12a–T12b)

**What this covers:** `larry bump` — version/revision bump logic.
**Why sixth → split:** Depends on provider (T6, for auto-version) and prepare (T13, to call it after bump). Since T13 is phase 7 and bump is phase 6, T12 is split into two: file creation (no prepare dependency) and prepare invocation (phase 7+).

---

### T12a — `larry bump` — create bumped ebuild file (no prepare dependency)

**Precondition:** T4, T9 complete

**Tests:**
- `TestBump_VersionBump` — latest ebuild `pkg-1.0.0.ebuild` → `pkg-2.0.0.ebuild`, content copied
- `TestBump_VersionBump_Variables` — `${PV}` and `${P}` references preserved, hardcoded strings untouched
- `TestBump_RevisionBump` — latest `pkg-1.0.0.ebuild` → `pkg-1.0.0-r1.ebuild`
- `TestBump_RevisionBump_Increment` — `pkg-1.0.0-r2.ebuild` exists → `pkg-1.0.0-r3.ebuild`
- `TestBump_LiveExcluded` — `-9999.ebuild` not considered as source
- `TestBump_NoEbuilds` — no ebuilds in dir → error
- `TestBump_RevisionNoUpstreamQuery` — `--revision` does not query upstream
- `TestBump_PrereleasesFlag` — `--prereleases` allows prerelease version resolution

**Implementation:**
1. `cmd/larry/bump.go` — cobra subcommand
2. `internal/bump/bump.go`:
   - Find latest non-live ebuild in `${cat}/${pkg}/`
   - Copy content to new file
   - Rename filename: `pkg-${PV_old}.ebuild` → `pkg-${PV_new}.ebuild`
   - For `--revision`: append `-r${N}` to filename, keep PV same
   - For version bump without explicit version: query upstream (uses provider from T6) — *the auto-version query logic is tested here; actual provider implementation is tested in T8b*
3. Helper: `findLatestEbuild(dir string) (path string, err error)` — sorted by PV descending
4. Helper: `incrementRevision(dir string) int` — find highest -rN, return N+1

**Acceptance:** Version bumps create correct ebuild files. Revision bumps work. Auto-version resolution logic is present (provider integration tested separately in T8b).

---

### T12b — `larry bump` — invoke prepare on new ebuild

**Precondition:** T12a, T13 complete (bump file creation + prepare pipeline exist)

**Tests:**
- `TestBump_PrepareCalled` — after bump, calls prepare pipeline on new ebuild with correct package type
- `TestBump_Prepare_Failure` — prepare failure → bump file removed, error returned
- `TestBump_Prepare_TypeMismatch` — bump detected as npm but prepare dispatches generic → error

**Implementation:**
1. After `internal/bump/bump.go` creates the new ebuild file:
   - Detect package type (calls `internal/ebuild.DetectType` from T9)
   - Delegate to `internal/prepare.Prepare(newEbuildPath, pkgType, dryRun, useTUI)`
2. On prepare failure: remove the newly created ebuild file, return error
3. CLI: `larry bump --auto` triggers bump+prepare in one command

**Acceptance:** Bumped ebuild is prepared automatically. Prepare failure cleans up the bump. Package type is correctly detected and dispatched.
