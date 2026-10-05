# Prepare Command (T13–T19)

> **Phase 1 (historical).** The ✅ marks below mean "code and tests exist",
> not "behaviour matches the reference script". A 2026-10-05 audit found defects
> in this area. See `plan/PLAN.md` for the corrected status and `20`–`25` (prepare parity plans).

**Why:** Most complex command; 6 type-specific pipelines (generic, npm, cargo, go, rpi-kernel, vendored). Broke into sub-tasks.
**TUI stream isolation (all T13–T19):** Subprocess stdout/stderr piped via `StdoutPipe()`/`StderrPipe()` when `useTUI` is true. Never inherit `os.Stdout`/`os.Stderr`.
**Rollback (all T13–T19):** Ebuild backed up `.bak` before mutations, restored on failure.
**Dry-run (all T13–T19):** `$XDG_CACHE_HOME/larry/dry-run/`. No external mutations.

### Status: ✅ **T13 COMPLETE** — 6 files, 35 tests, 1 CLI subcommand
✅ **T14 COMPLETE** — 2 files, 38 tests
✅ **T15 COMPLETE** — 2 files, 24 tests
✅ **T16 COMPLETE** — 2 files, 23 tests
✅ **Escape hatch** — 2 files, 10 tests (custom `files/prepare.sh` per-package)
✅ **T18 COMPLETE** — 2 files, 24 tests (vendored pipeline)

> T17 (rpi-kernel pipeline) removed — replaced by escape hatch. Users put
> `files/prepare.sh` in the package directory for custom logic.

> `vendored` type detection implemented (from MIXED_DEPS.md) — 6th pipeline.
> Detection: SRC_URI contains `releases/download/${PN}/`. Supports npm/cargo/go sub-deps.

---

### T13 — `larry prepare` — type detection + generic + variable resolution

**Precondition:** T9
**Note:** Type detection owned by T9 (`internal/ebuild/detect.go`). T13 tests CLI `--type` flag override, generic pipeline, and utility functions.

**Tests:**
- ✅ `TestDetectType_Force` — `--type` flag overrides auto-detection (6 subtests)
- ✅ `TestDetectTypeFromInfo` — auto-detect npm/cargo/go/vendored/generic/nil (6 subtests)
- ✅ `TestParseEbuildInfo` — extracts PN/PV/P/SRC_URI/HOMEPAGE/Inherit from ebuild
- ✅ `TestParseEbuildInfo_Revision` — strips -rN suffix from PN
- ✅ `TestResolveVariables` — `${PN}`/`${PV}`/`${P}` substitution (6 subtests)
- ✅ `TestParsePVRaw` — parses PV + revision (7 subtests: simple, -rN, _p, live, etc.)
- ✅ `TestIsLiveVersion` — 9999 detection
- ✅ `TestIsRevisionBump` — revision bump detection
- ✅ `TestCollectEbuilds` — finds .ebuild files, ignores .bak and non-ebuild
- ✅ `TestCollectEbuilds_NoEbuilds` — empty dir returns empty
- ✅ `TestCollectEbuilds_NonExistent` — error on missing dir
- ✅ `TestBackup_Rollback` — .bak backup/restore round-trip
- ✅ `TestBackupDir` — backup all files in directory
- ✅ `TestBackupNonExistent` — error on missing file
- ✅ `TestManifestArgs` — builds manifest args with optional --distdir
- ✅ `TestVerifyDistfiles` — checks distfile existence
- ✅ `TestExtractDistfilesFromSRCURI` — parses SRC_URI into distfile list
- ✅ `TestFindManifestRunner` — finds pkgdev binary in PATH
- ✅ `TestFindManifestRunner_NotFound` — error when not in PATH
- ✅ `TestGetEnv` — env var reading
- ✅ `TestTuiStreamIsolation` — StdoutPipe captures output without inheriting
- ✅ `TestPrepare_DryRun_SkipsManifest` — dry-run skips manifest, no .bak
- ✅ `TestPrepareFromFile` — parses and prepares single ebuild
- ✅ `TestPrepareFromFile_NonExistent` — error on missing file
- ✅ `TestPrepareAll_NoEbuilds` — empty dir succeeds
- ✅ `TestPrepare_Generic_RealManifest` — integration test (skips if pkgdev missing)

**Implementation:**
1. ✅ `cmd/larry/prepare.go` — Cobra subcommand with `--type`, `--all`, `--dry-run`, `--distdir`, `--overlay` flags
2. ✅ `internal/prepare/prepare.go` — `Prepare(ei, cfg)`, `PrepareAll(dir, cfg)`, `PrepareFromFile(path, cfg)`, `parseEbuildInfo()`, `resolveVariables()`, `parsePVRaw()`, `isLiveVersion()`, `isRevisionBump()`, `collectEbuilds()`
3. ✅ `internal/prepare/manifest.go` — `runManifest(path, distdir)`, `findManifestRunner()`, `manifestArgs()`, mockable function variables
4. ✅ `internal/prepare/rollback.go` — `Backup()`, `Restore()`, `BackupDir()` (unchanged from stub)
5. ✅ Dry-run: `PrepareConfig.DryRun` skips all mutations (backup, manifest, distfiles)
6. ✅ Type dispatch: npm, cargo, go, rpi-kernel, vendored, generic pipelines
7. ✅ Pipeline stubs: `prepareNpm()`, `prepareCargo()`, `prepareGo()`, `prepareRpiKernel()`, `prepareVendored()` (deferred to T14-T18)

**Acceptance:** Generic pipeline works. Dry-run skips mutations. `.bak` rollback works. TUI stream isolation verified. CLI flags work. All 6 type dispatches functional.

---

### T14 — `larry prepare` — npm pipeline (distfiles, tarball, upload)

**Precondition:** T13, T8, T8b

**Tests:**
- ✅ `TestNpm_ResolveSRCURI` — converts `mirror://npm/` to registry URL (4 subtests: basic, vars, scoped, non-npm)
- ✅ `TestNpm_DownloadDistfile` — downloads from HTTP server, verifies content
- ✅ `TestNpm_DownloadDistfile_404` — error on HTTP 404
- ✅ `TestNpm_DownloadDistfile_NonHTTP` — error on non-HTTP URL
- ✅ `TestNpm_RunNpmInstall_Skipped` — dry-run skips npm install
- ✅ `TestNpm_RunNpmInstall_FindsNpm` — verifies npm is available
- ✅ `TestNpm_CreateTarball` — packages node_modules → tarball
- ✅ `TestNpm_CreateTarball_EmptyDir` — no error on empty dir
- ✅ `TestNpm_CopyToDistdir` — copies tarball to DISTDIR
- ✅ `TestNpm_CopyToDistdir_SourceMissing` — error on missing source
- ✅ `TestNpm_CheckReleaseExists` — returns true for existing release (StubRunner)
- ✅ `TestNpm_CheckRelease_NotExists` — returns false for missing release
- ✅ `TestNpm_UploadAsset` — uploads asset to release (StubRunner)
- ✅ `TestNpm_ComputeSHA256` — SHA-256 hash of file
- ✅ `TestNpm_ComputeSHA256_MissingFile` — returns empty for missing file
- ✅ `TestNpm_AssetCollisionCheck` — match→skip, mismatch→upload, none→upload (3 subcases)
- ✅ `TestNpm_EnsureRelease` — no-op for existing release
- ✅ `TestNpm_EnsureRelease_Create` — creates release for missing one (StubRunner)
- ✅ `TestNpm_PipelineRevisionBump_SkipUpload` — detects -rN, skips upload
- ✅ `TestNpm_ManifestGeneration_DistdirBefore` — verifies distfiles before manifest
- ✅ `TestNpm_DryRun_SkipsAllMutations` — no backup, no manifest, no upload
- ✅ `TestNpm_Pipeline_Failure_Rollback` — .bak restore on failure
- ✅ `TestNpm_ResolveVariable` — `${PN}`/`${PV}`/`${P}` substitution
- ✅ `TestNpm_PipelineIntegration_DryRun` — dry-run pipeline end-to-end
- ✅ `TestNpm_Pipeline_DownloadDistfiles` — full download + copy pipeline with httptest server
- ✅ `TestNpm_AssetCollisionAction` — enum values distinct
- ✅ `TestNpm_Pipeline_FullMock` — full mock pipeline in dry-run
- ✅ `TestNpm_Pipeline_RealSubprocess` — creates tarball with real tar binary
- ✅ `TestNpm_Pipeline_DistdirBeforeManifest` — distfile verification before manifest
- ✅ `TestNpm_Pipeline_DryRun_StagingDir` — staging dir management
- ✅ `TestNpm_ResolveSRCURI_VariousFormats` — multiple SRC_URI patterns (3 subtests)
- ✅ `TestNpm_Pipeline_MultipleTarballs` — handles 2+ tarballs
- ✅ `TestNpm_Pipeline_ReleaseCreateFailure` — rollback on create failure
- ✅ `TestNpm_ParseReleaseJSON` — parses gh release view JSON output
- ✅ `TestNpm_DownloadDistfile_ReadCloser` — io.ReadCloser simulation
- ✅ `TestNpm_ExtractDistfileName` — extracts `my-pkg-1.0.0.tgz` from npm URL

**Implementation:**
1. ✅ `internal/prepare/npm.go` — `prepareNpm(ei, cfg) error`
2. ✅ `internal/github/github.go` — added `Gh()` (exported), `Runner()` (getter) methods
3. Steps: resolve SRC_URI → download distfile → copy to DISTDIR → create npm tarball from node_modules → verify distfiles → check/create GH release → upload → `pkgdev manifest`

**Acceptance:** ✅ Full pipeline with mock chain. Asset collision handled (SHA-256). Release auto-created. DISTDIR verified before manifest. Rollback works. 38 tests pass.

---

### T15 — `larry prepare` — cargo pipeline (Cargo.lock, CRATES)

**Precondition:** T8, T8b, T9

**Tests:**
- ✅ `TestCargo_Pipeline_FindRepo` — SRC_URI/HOMEPAGE/remote-id → owner/repo (3 subtests: github_mirror, homepage_github, no_repo_info)
- ✅ `TestCargo_Pipeline_FindCargoLock_GitHubTree` — tree API finds Cargo.lock (StubRunner)
- ✅ `TestCargo_Pipeline_FindCargoLock_FallbackPaths` — tree fails → src/Cargo.lock fallback
- ✅ `TestCargo_Pipeline_FindCargoLock_Fail` — returns error when Cargo.lock missing
- ✅ `TestCargo_Pipeline_DownloadCargoLock` — downloads from HTTP server, verifies content
- ✅ `TestCargo_Pipeline_ParseCargoLock` — parses [[package]] entries (4 tests: basic, empty, deps, git)
- ✅ `TestCargo_Pipeline_PycargoebuildAvailable` — verifies pycargoebuild is found in PATH
- ✅ `TestCargo_Pipeline_PycargoebuildUpdateEbuild` — calls pycargoebuild --inplace (stubbed)
- ✅ `TestCargo_Pipeline_NativeParsing` — native CRATES generation from lock content
- ✅ `TestCargo_Pipeline_AllToolsUnavailable` — skips (pycargoebuild available on test host)
- ✅ `TestCargo_Pipeline_UpdateEbuild` — replaces CRATES="..." in ebuild with new crates
- ✅ `TestCargo_Pipeline_UpdateEbuild_Rollback` — .bak backup/restore round-trip
- ✅ `TestCargo_Pipeline_ManifestGeneration` — verifies distfiles before manifest
- ✅ `TestCargo_Pipeline_DryRun_SkipsAllMutations` — dry-run skips GitHub lookup and ebuild update
- ✅ `TestCargo_Pipeline_Failure_Rollback` — .bak backup/restore round-trip
- ✅ `TestCargo_Pipeline_RevisionBump_SkipUpload` — revision bump detection works
- ✅ `TestCargo_FindRepoVariousSrcUris` — various SRC_URI patterns (3 subtests: github_mirror, homepage_github, sourcehut)
- ✅ `TestCargo_ParseCargoLock_Dependencies` — parses crates with dependencies
- ✅ `TestCargo_ParseCargoLock_GitCrate` — handles git+source entries
- ✅ `TestCargo_FormatCRATES` — formats CRATES variable as `name@version`
- ✅ `TestCargo_Pipeline_FullMock` — full mock pipeline in dry-run mode
- ✅ `TestCargo_DownloadCargoLock_GitHubRelease` — downloads from GitHub raw URL
- ✅ `TestCargo_ParseCargoLockJSON` — parses gh api JSON response

**Implementation:**
1. ✅ `internal/prepare/cargo.go` — `PrepareCargo(ei, cfg) error`, `parseCargoLock()`, `formatCRATES()`, `updateCargoEbuildNative()`, `runPycargoebuild()`, `findCargoRepo()`, `findCargoLockAndDownload()`, `downloadCargoLock()`
2. Steps: resolve upstream (HOMEPAGE/SRC_URI → github.com/owner/repo) → GH tree API → download Cargo.lock → try pycargoebuild --inplace → fallback native parsing → update CRATES="..." → pkgdev manifest
3. Native CRATES: [[package]] entries → `name@version` format

**Acceptance:** Finds/downloads Cargo.lock, generates CRATES, updates ebuild, manifest. Rollback works. Dry-run skips all mutations. 24 tests pass.

---

### T16 — `larry prepare` — go pipeline (vendor, tarball)

**Precondition:** T8, T8b, T9

**Tests:**
- ✅ `TestGo_Pipeline_FindRepo` — SRC_URI/HOMEPAGE → GitHub owner/repo (3 subtests: github_mirror, homepage_github, no_repo_info)
- ✅ `TestGo_Pipeline_DownloadSource` — downloads tarball from HTTP server, verifies content
- ✅ `TestGo_Pipeline_DownloadSource_Non200` — returns error for 404
- ✅ `TestGo_Pipeline_ExtractSource` — extracts tarball to temp dir, verifies contents
- ✅ `TestGo_Pipeline_ExtractSource_NoExtract` — returns error for nonexistent tarball
- ✅ `TestGo_Pipeline_GoModVendor` — verifies go mod vendor called (stubbed runner)
- ✅ `TestGo_Pipeline_GoModVendor_NoGoMod` — handles missing go.mod gracefully
- ✅ `TestGo_Pipeline_CreateTarball` — packages `vendor/` → `${P}-deps.tar.gz` in DISTDIR
- ✅ `TestGo_Pipeline_CreateTarball_NoVendor` — returns error when vendor dir missing
- ✅ `TestGo_Pipeline_CopyToDistdir` — copies tarball to DISTDIR, verifies content
- ✅ `TestGo_Pipeline_ReleaseExists` — gh release view succeeds → returns true
- ✅ `TestGo_Pipeline_ReleaseNotExists` — gh release view fails → returns false
- ✅ `TestGo_Pipeline_ReleaseCreateFailure` — gh release create fails → error
- ✅ `TestGo_Pipeline_ReleaseCreateSuccess` — gh release create succeeds
- ✅ `TestGo_Pipeline_UploadToGithub` — gh release upload succeeds
- ✅ `TestGo_Pipeline_UploadToGithub_Fail` — gh release upload fails → error
- ✅ `TestGo_Pipeline_ManifestGeneration` — verifies distfiles before manifest
- ✅ `TestGo_Pipeline_DistdirBeforeManifest` — verifies tarball exists before manifest
- ✅ `TestGo_Pipeline_RevisionBump_Skip` — revision bump detection works
- ✅ `TestGo_Pipeline_DryRun` — dry-run skips all mutations, ebuild unchanged
- ✅ `TestGo_FindRepoVariousSrcUris` — various SRC_URI patterns (3 subtests)
- ✅ `TestGo_Pipeline_FullMock` — full mock pipeline in dry-run mode

**Implementation:**
1. ✅ `internal/prepare/go.go` — `PrepareGo(ei, cfg) error`, `findGoRepo()`, `downloadGoSource()`, `downloadGoSourceURL()`, `extractGoSource()`, `findGoModuleRoot()`, `runGoModVendor()`, `createGoVendorTarball()`, `copyToDistdir()`, `checkGoReleaseExists()`, `createGoRelease()`, `uploadGoRelease()`
2. Steps: resolve GitHub → download v${PV} tarball (GH API) → extract → find go.mod root → go mod vendor → tar vendor/ → copy to DISTDIR → gh release view → create if missing → upload asset → pkgdev manifest
3. P= variable parsing: `(?m)^P\s*=\s*"([^"]+)"` regex for multiline ebuild files

**Acceptance:** Vendors deps, creates tarball, uploads via `gh`, manifest. Revision bump skips. Release auto-created. 23 tests pass.

---

> ~~T17 — rpi-kernel pipeline~~ — **REMOVED**
>
> The hardcoded rpi-kernel pipeline (`internal/prepare/rpi_kernel.go`) was removed
> in favor of the escape hatch (per-package `files/prepare.sh`). This avoids needing
> to bump `app-portage/larry` every time a new commit-tracking package appears.
>
> The `TypeRpiKernel` detection type remains in `ebuild/detect.go` for informational
> purposes (detecting rpi-kernel ebuilds), but it now falls through to the generic
> pipeline or the custom script escape hatch.
>
> **Migration:** Users with rpi-kernel or similar packages should create
> `sys-kernel/raspberrypi-kernel/files/prepare.sh` with their custom logic.
> The escape hatch runs with `EBUILD`, `PN`, `PV`, `P`, `CATEGORY`, `DISTDIR`
> env vars set, and `Dir = filepath.Dir(ebuildPath)`.

---

### T18 — `larry prepare` — vendored pipeline (mixed deps in GitHub releases)

**Precondition:** T13, T8, T8b

> From MIXED_DEPS.md: Package type for projects that vend dependencies into GitHub releases as tarballs.
> Pattern: SRC_URI contains `releases/download/${PN}/${P}-npm-deps.tar.xz`.
> Supports npm, cargo, and go sub-deps. Multiple dep directories per package.

**Detection:**
- `releases/download/${PN}/` in SRC_URI → `TypeVendored`
- Manual override via `.larry.yaml` config

**Tests:** 24 passing
- ✅ `TestVendored_ParseDepSpec_Npm` — distfile name `-npm-deps.tar.xz` → npm
- ✅ `TestVendored_ParseDepSpec_Cargo` — distfile name `-cargo-deps.tar.xz` → cargo
- ✅ `TestVendored_ParseDepSpec_Go` — distfile name `-go-deps.tar.gz` → go
- ✅ `TestVendored_ParseDepSpec_Unknown` — unknown pattern → empty type
- ✅ `TestVendored_ParseDepSpec_DistName` — preserves original filename
- ✅ `TestVendored_DownloadSource` — downloads from HTTP server
- ✅ `TestVendored_DownloadSource_NotFound` — 404 → error
- ✅ `TestVendored_ExtractSource` — extracts tarball correctly
- ✅ `TestVendored_ExtractSource_InvalidTarball` — invalid → error
- ✅ `TestVendored_CreateTarball` — packages dir → tarball
- ✅ `TestVendored_CreateTarball_MissingDir` — missing dir → error
- ✅ `TestVendored_CopyToDistdir` — copies to distdir
- ✅ `TestVendored_CopyToDistdir_CreateDistdir` — creates distdir if missing
- ✅ `TestVendored_GhReleaseView_Exists` — release exists → true
- ✅ `TestVendored_GhReleaseView_NotExists` — not found → false
- ✅ `TestVendored_GhReleaseCreate` — creates release
- ✅ `TestVendored_GhReleaseCreate_Failure` — failure → error
- ✅ `TestVendored_GhUpload` — uploads asset
- ✅ `TestVendored_GhUpload_MissingFile` — missing file → error
- ✅ `TestVendored_SHA256Hash` — correct hash
- ✅ `TestVendored_DryRun` — skips all ops in dry-run
- ✅ `TestVendored_MultipleDeps` — handles npm+cargo+go
- ✅ `TestVendored_ExtractSRCURIForPN` — extracts distfile names by pkg
- ✅ `TestVendored_ParseDepSpecsFromURI` — full pipeline parse

**Implementation:**
1. ✅ `internal/prepare/vendored.go` — `PrepareVendored(ei, cfg) error` + helpers
2. Data model: `DepType` (npm/cargo/go), `DepSpec{Type, SourcePath, DistName, TarballContent}`
3. Steps:
   a. Parse SRC_URI → distfile names for `${PN}` → DepSpecs
   b. Download source tarball (first non-releases/download/ URL)
   c. Extract to temp dir
   d. For each DepSpec: runVendorCommand → createVendorTarball → copyToDistdir → gh upload
   e. Run `pkgdev manifest`
   f. Cleanup temp dir (defer RemoveAll)
4. GitHub Release Behavior:
   - Tag = `${PN}` (package name, not versioned)
   - Upload with `--clobber`
   - Create release if tag doesn't exist

**Notes:**
- `runVendorCommand` handles npm (`npm ci --ignore-scripts` + `.npmrc`), cargo (`cargo vendor vendor/`), go (`go mod vendor`)
- Compression auto-detected from extension: `.tar.gz`, `.tar.xz`, `.tar.zst`, `.tar`
- Config override (`.larry.yaml`) deferred — not implemented
- Asset collision detection (SHA-256 compare) deferred — uses `--clobber`
- Rollback via temp dir cleanup (defer RemoveAll)

**Acceptance:** Downloads source, vendors deps, creates tarballs, uploads to GH release, manifest. Multiple deps supported. Dry-run supported. 24 tests pass.

---

### T19 — `larry prepare` — dry-run / staging mode

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

**Acceptance:** Dry-run all 6 types local. No external mutations. Correct exit codes. Diff shown.

---

### Escape hatch — per-package `files/prepare.sh`

**Design:** If `files/prepare.sh` exists in the ebuild's directory, execute it instead of any built-in pipeline. No config DSL — just drop a script in the right place.

**Detection:** `filepath.Join(filepath.Dir(ei.Path), "files", "prepare.sh")`

**Environment:** Script receives:
- `EBUILD` — path to the ebuild
- `PN` — package name
- `PV` — package version
- `P` — `${PN}-${PV}`
- `CATEGORY` — ebuild category
- `DISTDIR` — distfiles directory
- `LARRY_PREPARE=1` — marker env var
- Script runs with `Dir = filepath.Dir(ebuildPath)`

**Behavior:**
- Dry-run: script path is detected but script is NOT executed
- Non-zero exit: error propagated with "custom prepare script failed" message
- No backup/rollback: the script owns its own mutations
- Takes precedence over all built-in pipelines

**Tests:**
- ✅ `TestPrepare_CustomScript_Detected` — script found and executed
- ✅ `TestPrepare_CustomScript_Skipped` — no script → falls through to built-in
- ✅ `TestPrepare_CustomScript_DryRun` — script NOT executed in dry-run
- ✅ `TestPrepare_CustomScript_Failure` — non-zero exit → error propagated
- ✅ `TestPrepare_CustomScript_EnvVars` — all env vars passed correctly
- ✅ `TestPrepare_GenericNoCustomScript` — no files/ dir → generic pipeline
- ✅ `TestRunCustomPrepare_DryRun` — dry-run skips execution
- ✅ `TestRunCustomPrepare_Success` — script succeeds → nil
- ✅ `TestRunCustomPrepare_Failure` — script fails → error with message
- ✅ `TestRunCustomPrepare_ScriptNotFound` — missing script → error
- ✅ `TestRunCustomPrepare_UsesCmdDir` — script runs in ebuild directory

**Implementation:**
1. `internal/prepare/prepare.go` — check in `Prepare()` before dispatch, `runCustomPrepare()` helper
2. `internal/prepare/custom_prepare_test.go` — 10 tests

**Files:** `prepare.go`, `custom_prepare_test.go`
