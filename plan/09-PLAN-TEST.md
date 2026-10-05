# Test Command (T20–T21)

> **Phase 1 (historical).** The ✅ marks below mean "code and tests exist",
> not "behaviour matches the reference script". A 2026-10-05 audit found defects
> in this area. See `plan/PLAN.md` for the corrected status and `26-PLAN-TEST-LOCAL.md`, `27a-PLAN-TEST-CONTAINERFILE.md`, `27b-PLAN-TEST-CONTAINER-ARGS.md`.

**Why:** Self-contained; depends on ebuild helpers + output/color.
**TUI stream isolation:** Subprocess stdout/stderr piped via `StdoutPipe()`/`StderrPipe()` when `useTUI` is true.

### Status: ✅ **T20 + T21 COMPLETE** — 6 files, 40 tests

#### T20 — Local mode

**Tests:** 19 passing
- ✅ `TestResolveEbuild_ExistingFile` — direct ebuild path works
- ✅ `TestResolveEbuild_CatPkg` — cat/pkg resolves to latest ebuild
- ✅ `TestResolveEbuild_CatPkgNotFound` — missing package → error
- ✅ `TestValidatePhase_Valid` — all valid phases accepted
- ✅ `TestValidatePhase_Invalid` — invalid phases rejected (case-insensitive match for valid)
- ✅ `TestBuildPhaseList_Default` — default phase list: [clean]
- ✅ `TestBuildPhaseList_WithCompile` — compile flag: [clean, compile]
- ✅ `TestBuildPhaseList_WithMerge` — merge flag: [clean, fetch, unpack, compile, install, qmerge]
- ✅ `TestBuildPhaseList_CustomPhases` — custom phases override defaults
- ✅ `TestRunPhase_Success` — runs ebuild phase via runner
- ✅ `TestRunPhase_Fail` — runner error propagated with cmd context
- ✅ `TestRunPhase_Pretend` — pretend mode prints command, skips execution
- ✅ `TestLocal_RunClean` — default clean phase
- ✅ `TestLocal_RunMultiplePhases` — multiple phases in sequence
- ✅ `TestLocal_Pretend` — pretend mode: no commands executed
- ✅ `TestLocal_InvalidPhase` — invalid phase → error
- ✅ `TestLocal_EbuildNotFound` — missing ebuild → error
- ✅ `TestLocal_LivePackage` — live version (9999) handled correctly
- ✅ `TestSetNetworkFeatures_Disabled` — FEATURES=-network-sandbox injected
- ✅ `TestSetNetworkFeatures_Existing` — existing FEATURES replaced

#### T21 — Container mode

**Tests:** 20 passing
- ✅ `TestContainer_BuildImage_Cached` — cache dir exists → skip build
- ✅ `TestContainer_BuildImage_NotCached` — cache missing → podman build
- ✅ `TestContainer_BuildImage_ContainerfileMissing` — missing Containerfile → error
- ✅ `TestContainer_MountOverlay` — overlay mount flag generated
- ✅ `TestContainer_MountDistdir` — distfiles mount flag generated
- ✅ `TestContainer_GPUPassthrough` — GPU devices + video group flag
- ✅ `TestContainer_RunEmerge` — podman run with emerge
- ✅ `TestContainer_RunEmerge_Pretend` — pretend skips execution
- ✅ `TestContainer_ImageName` — default: larry-test
- ✅ `TestContainer_ImageName_Custom` — custom image name
- ✅ `TestContainer_CacheDir` — returns configured cache dir
- ✅ `TestContainer_PodmanNotFound` — podman check
- ✅ `TestContainer_GetEbuildDir` — extracts directory from path
- ✅ `TestContainer_GetEbuildDir_Dir` — path ending in dir
- ✅ `TestContainer_TuiStreamIsolation` — --tty=false in TUI mode
- ✅ `TestContainer_RunClean` — full container workflow
- ✅ `TestContainer_RunMultiplePhases` — multiple phases in container
- ✅ `TestContainer_Pretend` — pretend mode: no commands executed
- ✅ `TestContainer_EbuildNotFound` — missing ebuild → error
- ✅ `TestContainerfile_Exists` — Containerfile.larry-test exists and valid

**Implementation:**
1. ✅ `internal/test/local.go` — `TestLocal(ebuildPath, phases, runner, pretend, useTUI) error`
   - `CmdRunner` interface for testability
   - `NewCmdRunner()` — concrete exec.Command implementation
   - Phase validation and list builder
   - `setNetworkFeatures` — injects FEATURES=-network-sandbox
2. ✅ `internal/test/container.go` — `TestContainer(ebuildPath, cfg, runner, pretend, useTUI) error`
   - `ContainerConfig` struct (overlay, distdir, cacheDir, imageName, useTUI)
   - `buildImage` — builds from Containerfile, skips if cached
   - `getContainerMounts` — bind mounts for overlay, distfiles
   - `getGPUFlags` — /dev/kfd, /dev/dri, --group-add=video
   - `runContainerEmerge` — podman run with emerge --verbose --autounmask=y
   - `checkPodmanAvailable` — verifies podman is installed
   - `findContainerfile` — searches for Containerfile.larry-test
3. ✅ `internal/test/local_test.go` — 19 tests
4. ✅ `internal/test/container_test.go` — 20 tests
5. ✅ `cmd/larry/test.go` — CLI with `--compile`, `--merge`, `--pretend`, `--container`, `--distdir`, `--phase`, `--overlay` flags
6. ✅ `Containerfile.larry-test` — Ubuntu base with Portage deps, larry user, EPREFIX
7. Steps:
   - Accept `cat/pkg` or direct ebuild path
   - Resolve to latest ebuild in overlay (if cat/pkg)
   - Local mode: run ebuild phases with FEATURES=-network-sandbox
   - Container mode: build image → mount overlay/distfiles → podman run emerge
   - Pretend mode: print commands without executing

**Acceptance:** ✅ Local mode: all phases run. Pretend works. Invalid input errors gracefully. Container mode: image build, mounts, GPU passthrough. Containerfile created. TUI stream isolation supported.
