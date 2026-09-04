# Test Command Tasks (T20–T21)

**What this covers:** `larry test` — local mode (phase runner) and container mode (podman, GPU passthrough).
**Why ninth:** Self-contained; depends on ebuild helpers and output/color for display.

---

### T20 — `larry test` — local mode (phase runner)

**Precondition:** T3, T9 complete

**Tests:**
- `TestLocal_RunClean` — runs `ebuild <path> clean`
- `TestLocal_RunFetch` — runs `ebuild <path> fetch` with `-network-sandbox`
- `TestLocal_RunUnpack` — runs `ebuild <path> unpack`
- `TestLocal_RunCompile` — runs `ebuild <path> compile`
- `TestLocal_RunInstall` — runs `ebuild <path> install`
- `TestLocal_RunMultiplePhases` — runs `clean fetch unpack compile` in sequence
- `TestLocal_Pretend` — `--pretend` shows command without executing
- `TestLocal_InvalidPhase` — unknown phase → error
- `TestLocal_EbuildNotFound` — no ebuild for cat/pkg → error
- `TestLocal_LivePackage` — resolves latest non-live for 9999 package

**Implementation:**
1. `cmd/larry/test.go` — cobra subcommand with `--compile`, `--merge`, `--pretend`, `--emerge`, `--<phase>` flags
2. `internal/test/local.go`:
   ```go
   func TestLocal(ebuildPath string, phases []string, pretend bool, useTUI bool) error
   ```
3. Steps:
   a. Resolve ebuild path from cat/pkg (or cat/pkg-version)
   b. Disable network sandbox (set `FEATURES="-network-sandbox"`)
   c. Run `ebuild <path> <phases...>` in `/tmp/portage-test/` work dir
   d. `--pretend`: print command, skip execution
4. Phase validation: reject invalid phase names
5. **TUI stream isolation:** When `useTUI` is true, capture subprocess `Stdout` and `Stderr` via `exec.Cmd.StdoutPipe()` and `exec.Cmd.StderrPipe()` — do not inherit `os.Stdout`/`os.Stderr`. Read pipes into a buffer, display non-interactively (e.g., append to result log) so bubbletea rendering is not corrupted.

**Acceptance:** All phases run correctly. Pretend mode shows commands. Invalid input errors gracefully. TUI mode does not corrupt terminal output.

---

### T21 — `larry test` — container mode (podman, GPU passthrough)

**Precondition:** T20 complete

**Tests:**
- `TestContainer_BuildImage` — builds from overlay Containerfile if not cached
- `TestContainer_MountOverlay` — mounts overlay, DISTDIR, binpkgs into container
- `TestContainer_GPUPassthrough` — passes `/dev/kfd`, `/dev/dri`, `--group-add video`

**Note on GPU device paths:** `/dev/kfd`, `/dev/dri`, `/dev/dri/renderD128` are hardcoded. These are not pulled from `Config`. Scope T21's acceptance criteria to "this specific ROCm workstation" rather than implying general container-test portability. For portability, these should be moved into `Config` (future enhancement).
- `TestContainer_RunEmerge` — runs `emerge --verbose --autounmask=y` in container
- `TestContainer_Pretend` — `--pretend` shows container command without running
- `TestContainer_ImageName` — uses `larry-test` image
- `TestContainer_CacheDir` — uses `$XDG_CACHE_HOME/larry/binpkgs`
- `TestContainer_NotAvailable` — podman not found → error
- `TestContainer_ContainerfileMissing` — no Containerfile found → error with helpful message (tests the Containerfile creation task)

**Implementation:**
1. `internal/test/container.go`:
   ```go
   func TestContainer(ebuildPath string, cfg config.Config, pretend bool, useTUI bool) error
   ```
2. Steps:
   a. Build Podman image from overlay Containerfile (if `$XDG_CACHE_HOME/larry/binpkgs/larry-test` not cached)
   b. Create container with mounts:
      - Overlay → `/overlay`
      - DISTDIR → `/distfiles`
      - binpkgs → `/binpkgs`
      - `/dev/kfd`, `/dev/dri` for GPU
      - `--group-add video`
   c. Run `emerge --verbose --autounmask=y` inside container

**Note on Containerfile:** The overlay Containerfile that T21 assumes for building the `larry-test` image is not created by T1 (scaffold) or any other task. Add a task to create `Containerfile.larry-test` (or similar) — minimal Gentoo environment with Portage, podman client, GPU drivers. This is a missing precondition.
   d. `--pretend`: print podman command, skip execution
3. Image: `larry-test` (tagged from overlay Containerfile)
4. **TUI stream isolation:** When `useTUI` is true, capture `podman` subprocess output via pipes (same pattern as T20). Display captured output after the container exits, not during.

**Acceptance:** Container mode builds image, mounts correctly, passes GPU devices. Pretend mode shows podman command. TUI mode does not corrupt terminal output.

