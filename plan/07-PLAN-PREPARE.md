# Prepare Command (T13–T18)

**Why:** Most complex command; 5 type-specific pipelines. Broke into sub-tasks.
**TUI stream isolation (all T13–T18):** Subprocess stdout/stderr piped via `StdoutPipe()`/`StderrPipe()` when `useTUI` is true. Never inherit `os.Stdout`/`os.Stderr`.
**Rollback (all T13–T18):** Ebuild backed up `.bak` before mutations, restored on failure.
**Dry-run (all T13–T18):** `$XDG_CACHE_HOME/larry/dry-run/`. No external mutations.

---

### T13 — `larry prepare` — type detection + generic + variable resolution

**Precondition:** T9
**Note:** Type detection owned by T9 (`internal/ebuild/detect.go`). T13 only tests CLI `--type` flag override and the generic pipeline.

**Tests:**
- `TestDetectType_Force` — `--type` flag overrides auto-detection
- `TestPrepare_Generic` — runs `pkgdev manifest`
- `TestPrepare_Generic_DistdirAvailable` — tarball in DISTDIR, runs manifest on file
- `TestPrepare_Generic_DryRun` — no manifest written, exit 0
- `TestPrepare_AllFlag` — runs manifest on all ebuilds
- `TestPrepare_MultipleFiles` — handles multiple paths
- `TestPrepare_TuiStreamIsolation` — long subprocesses don't corrupt bubbletea terminal

**Implementation:**
1. `cmd/larry/prepare.go` — `--type`, `--all`, `--dry-run`, `--stage` flags
2. `internal/prepare/prepare.go` — `Prepare(path, pkgType, dryRun, useTUI) error`. Dispatches to type pipelines.
3. `internal/prepare/manifest.go` — `pkgdev manifest` runner, accepts optional `distdir`, verifies distfiles present before running
4. `internal/prepare/rollback.go` — `.bak` backup/restore
5. Dry-run: local ops only, staging for artifacts

**Acceptance:** Generic pipeline works. Dry-run skips mutations. `.bak` rollback works. TUI clean.

---

### T14 — `larry prepare` — npm pipeline (distfiles, tarball, upload)

**Precondition:** T13, T8, T8b

**Tests:**
- `TestNpm_Pipeline_DownloadDistfiles` — resolves `mirror://npm/`, downloads from registry
- `TestNpm_Pipeline_ResolveVariables` — `${PN}`/`${PV}`/`${P}`/custom vars
- `TestNpm_Pipeline_NpmInstall` — runs `npm install` with tarball
- `TestNpm_Pipeline_CreateTarball` — packages npm cache → `${P}-deps.tar.xz`
- `TestNpm_Pipeline_CopyToDistdir` — copies tarball to DISTDIR
- `TestNpm_Pipeline_ReleaseExists/ReleaseNotExists` — `gh release view` → proceed/create
- `TestNpm_Pipeline_UploadToGithub` — uploads to `${PN}` release (stub `os/exec`)
- `TestNpm_Pipeline_AssetCollision_ChecksumMatch/Mismatch/New` — SHA-256 compare → skip/clobber/upload
- `TestNpm_Pipeline_ReleaseCreateFailure` — release create fails → error, rollback
- `TestNpm_Pipeline_RevisionBump_SkipUpload` — revision bump + existing tarball → skip upload
- `TestNpm_Pipeline_ManifestGeneration` — `pkgdev manifest` after distfile in DISTDIR
- `TestNpm_Pipeline_DistdirBeforeManifest` — verifies tarball in DISTDIR before manifest
- `TestNpm_Pipeline_DryRun` — all ops local, no upload/manifest
- `TestNpm_Pipeline_Failure_Rollback` — manifest fails → `.bak` restore

**Implementation:**
1. `internal/prepare/npm.go` — `PrepareNpm(info, cfg, dryRun, useTUI) error`
2. Steps: resolve SRC_URI → download distfiles → `npm install` → tar npm cache → copy to DISTDIR → `gh release view ${PN}` → create if missing → SHA-256 collision → `gh release upload` → `pkgdev manifest`

**Acceptance:** Full pipeline end-to-end. Asset collision handled. Release auto-created. DISTDIR before manifest. Rollback works.

---

### T15 — `larry prepare` — cargo pipeline (Cargo.lock, CRATES)

**Precondition:** T8, T8b, T9

**Tests:**
- `TestCargo_Pipeline_FindRepo` — SRC_URI/HOMEPAGE/remote-id → owner/repo or host/path
- `TestCargo_Pipeline_FindCargoLock_GitHubTree` — tree API finds `Cargo.lock`
- `TestCargo_Pipeline_FindCargoLock_FallbackPaths` — tree fails → common paths
- `TestCargo_Pipeline_DownloadCargoLock` — downloads from upstream tag
- `TestCargo_Pipeline_ParseCargoLock` — parses `[package]` entries
- `TestCargo_Pipeline_PycargoebuildAvailable` — delegates to `pycargoebuild`
- `TestCargo_Pipeline_NativeParsing` — native CRATES when no tool available
- `TestCargo_Pipeline_AllToolsUnavailable` — pycargoebuild + cargo-ebuild + native all fail → error
- `TestCargo_Pipeline_UpdateEbuild` — replaces `CRATES="..."` in ebuild
- `TestCargo_Pipeline_Rollback` — update fails → `.bak` restore
- `TestCargo_Pipeline_ManifestGeneration` — `pkgdev manifest` after CRATES update

**Implementation:**
1. `internal/prepare/cargo.go` — `PrepareCargo(info, cfg, dryRun, useTUI) error`
2. Steps: resolve upstream → download `Cargo.lock` (tree API → fallback) → try `pycargoebuild`/`cargo-ebuild` → fallback native parsing → update `CRATES="..."` → `pkgdev manifest`
3. Native CRATES: `[package]` entries → registry URLs → `CRATES="..."` format

**Acceptance:** Finds/downloads `Cargo.lock`, generates CRATES, updates ebuild, manifest. Rollback works.

---

### T16 — `larry prepare` — go pipeline (vendor, tarball)

**Precondition:** T8, T8b, T9

**Tests:**
- `TestGo_Pipeline_FindRepo` — SRC_URI → GitHub owner/repo
- `TestGo_Pipeline_DownloadSource` — `v${PV}.tar.gz` from GitHub
- `TestGo_Pipeline_ExtractSource` — extracts to temp dir
- `TestGo_Pipeline_GoModVendor` — `go mod vendor`
- `TestGo_Pipeline_CreateTarball` — packages `vendor/` → `${P}-deps.tar.gz`
- `TestGo_Pipeline_CopyToDistdir` — copies to DISTDIR
- `TestGo_Pipeline_ReleaseExists/ReleaseNotExists` — `gh release view` → proceed/create
- `TestGo_Pipeline_UploadToGithub` — uploads `${PN}` release
- `TestGo_Pipeline_ReleaseCreateFailure` — release create fails → error, rollback
- `TestGo_Pipeline_ManifestGeneration` — `pkgdev manifest` after distfile
- `TestGo_Pipeline_DistdirBeforeManifest` — verifies tarball before manifest
- `TestGo_Pipeline_RevisionBump_Skip` — revision bump + existing tarball → skip
- `TestGo_Pipeline_DryRun` — all local, no upload

**Implementation:**
1. `internal/prepare/go.go` — `PrepareGo(info, cfg, dryRun, useTUI) error`
2. Steps: resolve GitHub → download `v${PV}` tarball → `go mod vendor` → tar `vendor/` → copy to DISTDIR → `gh release view` → create if missing → SHA-256 collision → upload → `pkgdev manifest`
3. Same revision bump skip as npm

**Acceptance:** Vendors deps, tarball, uploads via `gh`, manifest. Revision bump skips. Release auto-created.

---

### T17 — `larry prepare` — rpi-kernel pipeline

**Precondition:** T8, T8b, T9

**Tests:**
- `TestRpiKernel_ParseDate` — `6.1.0_p20230915` → date `2023-09-15`
- `TestRpiKernel_LookupCommit` — GitHub releases for `raspberrypi/firmware` on date → commit hash
- `TestRpiKernel_LookupCommit_AmbiguousDate` — multiple releases same date → deterministic (first/newest? clarify)
- `TestRpiKernel_UpdateCommit` — replaces `COMMIT="..."` in ebuild
- `TestRpiKernel_Rollback` — manifest fails → COMMIT restored
- `TestRpiKernel_DryRun` — no ebuild modification

**Implementation:**
1. `internal/prepare/rpi_kernel.go` — `PrepareRpiKernel(info, cfg, dryRun, useTUI) error`
2. Steps: extract date from PV (`_pYYYYMMDD`) → query GitHub releases → extract commit → replace `COMMIT="..."` → `pkgdev manifest`

**Acceptance:** Commit lookup works. Atomic update. Rollback.

---

### T18 — `larry prepare` — dry-run / staging mode

**Precondition:** T13–T17

**Tests:**
- `TestDryRun_Npm/Cargo/Go/RpiKernel/Generic` — pipeline runs local, no upload, artifacts in staging dir
- `TestDryRun_AllTypes` — all 5 succeed with exit 0
- `TestDryRun_WouldFail` — any op would fail → exit 1
- `TestDryRun_DisplayDiff` — ebuild diff shown
- `TestDryRun_StagingCleanup` — staging dir cleaned on exit

**Implementation:**
1. `--dry-run`/`--stage` flag (from T13). Each pipeline checks `cfg.DryRun`:
   - Download → staging dir
   - Upload → skip, display what would upload
   - Ebuild modify → diff, skip write
   - Manifest → display what would be written, skip write
   - API queries → still run (read-only)
2. Exit 0 if all ops would succeed, exit 1 if any would fail
3. Cleanup staging dir on exit (or keep for inspection)

**Acceptance:** Dry-run all 5 types local. No external mutations. Correct exit codes. Diff shown.
