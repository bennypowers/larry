# Prepare Command Tasks (T13–T18)

**What this covers:** `larry prepare` — all 5 type pipelines (generic, npm, cargo, go, rpi-kernel), dry-run/staging mode.
**Why seventh:** The most complex command; broken into type-specific sub-tasks for tractability.

---

### T13 — `larry prepare` — type detection + generic + variable resolution

**Precondition:** T9 complete

**Note on T9 duplication:** `DetectType` (npm/cargo/go/rpi-kernel/generic/force) is specified twice — identically in T9 (`internal/ebuild/detect.go`) and here. **T9 is the sole owner** of type detection. T13's tests below are **deprecated** for detection — they should be removed. T13 calls `internal/ebuild.DetectType` instead of re-implementing detection. The only detection test T13 needs is `TestDetectType_Force` (the CLI `--type` flag override), which lives in the prepare command layer.

**Tests (T13-specific, detection moved to T9):**
- `TestDetectType_Force` — `--type` flag overrides auto-detection (CLI-layer test)
- `TestPrepare_Generic` — runs `pkgdev manifest` directly
- `TestPrepare_Generic_DistdirAvailable` — copies tarball into DISTDIR, runs `pkgdev manifest` on file in DISTDIR
- `TestPrepare_Generic_DryRun` — no manifest written, exit 0
- `TestPrepare_AllFlag` — runs `pkgdev manifest` on all ebuilds
- `TestPrepare_MultipleFiles` — handles multiple ebuild paths
- `TestPrepare_TuiStreamIsolation` — long-running subprocesses (pkgdev manifest) do not corrupt bubbletea terminal

**Detection tests (moved to T9, removed here):**
- ~~`TestDetectType_Npm`~~ — belongs to `internal/ebuild/detect.go` (T9)
- ~~`TestDetectType_Cargo`~~ — belongs to T9
- ~~`TestDetectType_Go`~~ — belongs to T9
- ~~`TestDetectType_RpiKernel`~~ — belongs to T9
- ~~`TestDetectType_Generic`~~ — belongs to T9

**Implementation:**
1. `cmd/larry/prepare.go` — cobra subcommand with `--type`, `--all`, `--dry-run`, `--stage` flags
2. `internal/prepare/prepare.go`:
   - Entry point: `Prepare(ebuildPath string, pkgType PackageType, dryRun bool, useTUI bool) error`
   - Dispatches to type-specific pipeline functions
   - `pkgdev manifest` wrapper for generic/all modes
   - Transaction safety: ebuild backup `.bak` before mutations, restore on failure
3. `internal/prepare/manifest.go` — `pkgdev manifest` runner:
   - Runs `pkgdev manifest <ebuild-path>` in the ebuild's directory
   - Accepts an optional `distdir` argument to ensure DISTDIR is set correctly
   - Verifies distfiles referenced in SRC_URI are present in DISTDIR before running manifest
4. `internal/prepare/rollback.go` — `.bak` backup/restore logic
5. Dry-run mode: all local ops run, no external mutations, staging area for artifacts
6. **TUI stream isolation:** When `useTUI` is true, all subprocess `Stdout` and `Stderr` must be captured via `exec.Cmd.StdoutPipe()` / `exec.Cmd.StderrPipe()` — never inherit `os.Stdout`/`os.Stderr`. Read pipes into a buffer, display as post-execution log so bubbletea rendering is not corrupted.

**Acceptance:** All 5 types detected correctly. Generic prepare runs `pkgdev manifest`. Dry-run skips mutations. `.bak` rollback works. TUI mode does not corrupt terminal output.

---

### T14 — `larry prepare` — npm pipeline (distfiles, tarball, upload)

**Precondition:** T13, T8, T8b complete (prepare entry + gh wrapper + npm provider)

**Tests:**
- `TestNpm_Pipeline_DownloadDistfiles` — resolves `mirror://npm/` URIs, downloads from npm registry
- `TestNpm_Pipeline_ResolveVariables` — expands `${PN}`, `${PV}`, `${P}`, custom vars in SRC_URI
- `TestNpm_Pipeline_NpmInstall` — runs `npm install` with downloaded tarball
- `TestNpm_Pipeline_CreateTarball` — packages npm cache as `${P}-deps.tar.xz`
- `TestNpm_Pipeline_CopyToDistdir` — copies tarball into DISTDIR (verified: file exists after copy)
- `TestNpm_Pipeline_ReleaseExists` — `gh release view ${PN} --json tagName` → exists → proceed to upload
- `TestNpm_Pipeline_ReleaseNotExists` — release missing → `gh release create ${PN}` then upload
- `TestNpm_Pipeline_UploadToGithub` — uploads to release tagged `${PN}` (stub `os/exec`)
- `TestNpm_Pipeline_AssetCollision_ChecksumMatch` — `gh release view --json assets` returns existing with same SHA-256 → skip upload
- `TestNpm_Pipeline_AssetCollision_ChecksumMismatch` — different SHA-256 → `gh release upload --clobber`
- `TestNpm_Pipeline_AssetCollision_New` — no existing asset → `gh release upload`
- `TestNpm_Pipeline_RevisionBump_SkipUpload` — revision bump with existing tarball → skip upload
- `TestNpm_Pipeline_ManifestGeneration` — runs `pkgdev manifest` after distfile is in DISTDIR
- `TestNpm_Pipeline_DistdirBeforeManifest` — verifies tarball is present in DISTDIR before invoking `pkgdev manifest`
- `TestNpm_Pipeline_DryRun` — all ops run locally, no upload, no manifest
- `TestNpm_Pipeline_Failure_Rollback` — manifest failure → ebuild restored from `.bak`
- `TestNpm_Pipeline_TuiNpmInstall` — `npm install` runs with piped stdout/stderr during TUI

**Implementation:**
1. `internal/prepare/npm.go`:
   ```go
   func PrepareNpm(info *ebuild.EbuildInfo, cfg config.Config, dryRun bool, useTUI bool) error
   ```
2. Steps:
   a. Resolve SRC_URI, download distfiles from npm registry URLs
   b. Expand variables using `portageq` + `ebuild info` (T9)
   c. `npm install` to resolve transitive deps
   d. Tar up npm cache → `${P}-deps.tar.xz`
   e. Copy tarball to `DISTDIR` (verified: file exists at expected path)
   f. **Release check:** Use `gh` CLI wrapper (T8):
   - `gh release view ${PN} --json tagName` — check if release exists
   - If missing: `gh release create ${PN} --title "${PN}"`
   g. **Upload with asset collision:** Use `gh` CLI wrapper (T8):
   - `gh release view ${PN} --json assets` — parse JSON, compute/compare SHA-256 locally
   - If checksum match → skip upload
   - If checksum mismatch → `gh release upload --clobber`
   - If no existing asset → `gh release upload`
   h. Run `pkgdev manifest` — distfile is now in DISTDIR, so digest computation succeeds
3. Asset collision: `gh release view --json assets` → parse JSON → SHA-256 comparison → decide `--clobber` or skip
4. Revision bump detection: compare base version tarball existence
5. Dry-run: all ops to staging area (`$XDG_CACHE_HOME/larry/dry-run/`)
6. **TUI stream isolation:** All subprocesses (`npm install`, `pkgdev manifest`) capture stdout/stderr via pipes when `useTUI` is true.

**Acceptance:** Full npm pipeline runs end-to-end (mocked distfiles). Asset collision handled via `gh` CLI. Release auto-created via `gh release create`. DISTDIR populated before manifest. Dry-run skips upload/manifest. Rollback works. TUI mode does not corrupt terminal output.

---

### T15 — `larry prepare` — cargo pipeline (Cargo.lock, CRATES)

**Precondition:** T8, T8b, T9 complete (go-git + gh wrapper + cargo provider + ebuild helpers)

**Tests:**
- `TestCargo_Pipeline_FindRepo` — SRC_URI/HOMEPAGE/remote-id → owner/repo or host/path
- `TestCargo_Pipeline_FindCargoLock_GitHubTree` — tree API finds `Cargo.lock`
- `TestCargo_Pipeline_FindCargoLock_FallbackPaths` — tree API fails, tries common paths
- `TestCargo_Pipeline_DownloadCargoLock` — downloads from upstream tag
- `TestCargo_Pipeline_ParseCargoLock` — parses `[package]` entries
- `TestCargo_Pipeline_PycargoebuildAvailable` — delegates to `pycargoebuild` when present
- `TestCargo_Pipeline_NativeParsing` — native CRATES block generation when no external tool
- `TestCargo_Pipeline_UpdateEbuild` — replaces `CRATES="..."` variable in ebuild
- `TestCargo_Pipeline_Rollback` — update failure → ebuild restored from `.bak`
- `TestCargo_Pipeline_ManifestGeneration` — `pkgdev manifest` after CRATES update
- `TestCargo_Pipeline_TuiPycargoebuild` — `pycargoebuild` stdout/stderr piped during TUI

**Implementation:**
1. `internal/prepare/cargo.go`:
   ```go
   func PrepareCargo(info *ebuild.EbuildInfo, cfg config.Config, dryRun bool, useTUI bool) error
   ```
2. Steps:
   a. Resolve upstream repo from `<remote-id>` or SRC_URI/HOMEPAGE
   b. Download `Cargo.lock` (tree API → fallback paths)
   c. Try `pycargoebuild` / `cargo-ebuild` first; fall back to native parsing
   d. Update `CRATES="..."` in ebuild (`.bak` backup/restore)
   e. Run `pkgdev manifest`
3. Native CRATES generation: parse `Cargo.lock` `[package]` entries → extract registry crate URLs → format as `CRATES="..."`
4. Extensible search paths: configurable list, tree API preferred
5. **TUI stream isolation:** `pycargoebuild` and `pkgdev manifest` subprocesses capture stdout/stderr via pipes when `useTUI` is true.

**Acceptance:** Cargo pipeline finds and downloads Cargo.lock, generates CRATES block, updates ebuild, generates manifest. Rollback works. TUI mode does not corrupt terminal output.

---

### T16 — `larry prepare` — go pipeline (vendor, tarball)

**Precondition:** T8, T8b, T9 complete (go-git + gh wrapper + go provider + ebuild helpers)

**Tests:**
- `TestGo_Pipeline_FindRepo` — SRC_URI → GitHub owner/repo
- `TestGo_Pipeline_DownloadSource` — downloads `v${PV}.tar.gz` from GitHub
- `TestGo_Pipeline_ExtractSource` — extracts to temp dir
- `TestGo_Pipeline_GoModVendor` — runs `go mod vendor` in extracted source
- `TestGo_Pipeline_CreateTarball` — packages `vendor/` as `${P}-deps.tar.gz`
- `TestGo_Pipeline_CopyToDistdir` — copies to DISTDIR (verified: file exists after copy)
- `TestGo_Pipeline_ReleaseExists` — `gh release view ${PN} --json tagName` → exists → upload asset
- `TestGo_Pipeline_ReleaseNotExists` — release missing → `gh release create ${PN}` then upload
- `TestGo_Pipeline_UploadToGithub` — uploads to release `${PN}` (stub `os/exec`)
- `TestGo_Pipeline_ManifestGeneration` — runs `pkgdev manifest` after distfile in DISTDIR
- `TestGo_Pipeline_DistdirBeforeManifest` — verifies tarball present before manifest
- `TestGo_Pipeline_RevisionBump_Skip` — revision bump with existing tarball → skip
- `TestGo_Pipeline_DryRun` — all ops local, no upload
- `TestGo_Pipeline_TuiGoVendor` — `go mod vendor` stdout/stderr piped during TUI

**Implementation:**
1. `internal/prepare/go.go`:
   ```go
   func PrepareGo(info *ebuild.EbuildInfo, cfg config.Config, dryRun bool, useTUI bool) error
   ```
2. Steps:
   a. Resolve GitHub repo
   b. Download source tarball from `v${PV}` tag
   c. `go mod vendor` in temp dir
   d. Package `vendor/` → `${P}-deps.tar.gz`
   e. Copy to `DISTDIR` (verified: file exists)
   f. **Release check:** Use `gh` CLI wrapper (T8):
   - `gh release view ${PN} --json tagName` — check existence
   - If missing: `gh release create ${PN} --title "${PN}"`
   - `gh release view ${PN} --json assets` → SHA-256 compare → `--clobber` or skip
   - `gh release upload ${PN} ${tarball} --clobber`
   g. Run `pkgdev manifest` — distfile is in DISTDIR
3. Same revision bump skip logic as npm
4. Dry-run: staging area for tarball
5. **TUI stream isolation:** `go mod vendor` and `pkgdev manifest` capture stdout/stderr via pipes when `useTUI` is true.

**Acceptance:** Go pipeline vendors deps, creates tarball, uploads via `gh` CLI, generates manifest. Revision bump skips if tarball exists. Release auto-created via `gh release create`. TUI mode does not corrupt terminal output.

---

### T17 — `larry prepare` — rpi-kernel pipeline

**Precondition:** T8, T8b, T9 complete (go-git + gh wrapper + rpi provider + ebuild helpers)

**Tests:**
- `TestRpiKernel_ParseDate` — PV `6.1.0_p20230915` → date `2023-09-15`
- `TestRpiKernel_LookupCommit` — queries GitHub releases for `rpi-firmware` on date → commit hash
- `TestRpiKernel_UpdateCommit` — replaces `COMMIT="..."` in ebuild
- `TestRpiKernel_Rollback` — manifest failure → COMMIT restored from `.bak`
- `TestRpiKernel_DryRun` — no ebuild modification

**Implementation:**
1. `internal/prepare/rpi_kernel.go`:
   ```go
   func PrepareRpiKernel(info *ebuild.EbuildInfo, cfg config.Config, dryRun bool, useTUI bool) error
   ```
2. Steps:
   a. Extract firmware date from PV (`_pYYYYMMDD`)
   b. Query GitHub releases for `raspberrypi/firmware` on that date
   c. Extract commit hash
   d. Replace `COMMIT="..."` in ebuild
   e. Run `pkgdev manifest`
3. Rollback: restore `COMMIT=` value on failure
4. Dry-run: display what would change
5. **TUI stream isolation:** `pkgdev manifest` captures stdout/stderr via pipes when `useTUI` is true.

**Acceptance:** Kernel commit lookup works. Ebuild updated atomically. Rollback on failure.

---

### T18 — `larry prepare` — dry-run / staging mode

**Precondition:** T13-T17 complete (individual pipelines)

**Tests:**
- `TestDryRun_Npm` — npm pipeline runs locally, no upload, artifacts in staging dir
- `TestDryRun_Cargo` — cargo pipeline runs locally, no upload, CRATES diff displayed
- `TestDryRun_Go` — go pipeline runs locally, no upload, tarball in staging dir
- `TestDryRun_RpiKernel` — rpi-kernel shows COMMIT diff
- `TestDryRun_Generic` — shows what manifest would contain
- `TestDryRun_AllTypes` — dry-run for all 5 types succeeds with exit 0
- `TestDryRun_WouldFail` — if any operation would fail, exit 1
- `TestDryRun_DisplayDiff` — ebuild diff shown to user
- `TestDryRun_StagingCleanup` — staging dir cleaned up on exit

**Implementation:**
1. Add `--dry-run` / `--stage` flag to `larry prepare` (already in T13 CLI)
2. Each pipeline checks `cfg.DryRun` flag:
   - Download → staging dir (`$XDG_CACHE_HOME/larry/dry-run/`)
   - Upload → skip, display what would upload
   - Ebuild modify → compute diff, skip write
   - Manifest → display what would be written, skip write
   - API queries → still run (read-only, no mutation)
3. Exit 0 if all operations would succeed, exit 1 if any would fail
4. Cleanup staging dir on exit (or keep for inspection)

**Acceptance:** Dry-run for all 5 types runs fully locally. No external mutations. Exit codes correct. Diff displayed.

