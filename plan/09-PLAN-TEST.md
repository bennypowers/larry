# Test Command (T20–T21)

**Why:** Self-contained; depends on ebuild helpers + output/color.
**TUI stream isolation:** Subprocess stdout/stderr piped via `StdoutPipe()`/`StderrPipe()` when `useTUI` is true.

---

### T20 — `larry test` — local mode (phase runner)

**Precondition:** T3, T9

**Tests:**
- `TestLocal_RunClean` / `Fetch` / `Unpack` / `Compile` / `Install` — runs `ebuild <path> <phase>`
- `TestLocal_RunMultiplePhases` — `clean fetch unpack compile` in sequence
- `TestLocal_Pretend` — `--pretend` shows command without executing
- `TestLocal_InvalidPhase` — unknown phase → error
- `TestLocal_EbuildNotFound` — no ebuild for cat/pkg → error
- `TestLocal_LivePackage` — resolves latest non-live for 9999 package
- `TestLocal_TuiStreamIsolation` — subprocess output captured, not inherited

**Implementation:**
1. `cmd/larry/test.go` — `--compile`, `--merge`, `--pretend`, `--emerge`, `--<phase>` flags
2. `internal/test/local.go` — `TestLocal(ebuildPath, phases []string, pretend, useTUI) error`
3. Steps: resolve ebuild from cat/pkg → set `FEATURES="-network-sandbox"` → run `ebuild <path> <phases>` in `/tmp/portage-test/` work dir
4. `--pretend`: print command, skip execution
5. Phase validation: reject invalid names

**Acceptance:** All phases run. Pretend works. Invalid input errors gracefully.

---

### T21 — `larry test` — container mode (podman, GPU passthrough)

**Precondition:** T20

**Tests:**
- `TestContainer_BuildImage` — builds from `Containerfile.larry-test` if not cached
- `TestContainer_MountOverlay` — mounts overlay, DISTDIR, binpkgs into container
- `TestContainer_GPUPassthrough` — passes `/dev/kfd`, `/dev/dri`, `--group-add video`
- `TestContainer_RunEmerge` — runs `emerge --verbose --autounmask=y` in container
- `TestContainer_Pretend` — `--pretend` shows podman command without running
- `TestContainer_ImageName` — uses `larry-test` image
- `TestContainer_CacheDir` — uses `$XDG_CACHE_HOME/larry/binpkgs`
- `TestContainer_NotAvailable` — podman not found → error
- `TestContainer_ContainerfileMissing` — no Containerfile → error with helpful message
- `TestContainer_TuiStreamIsolation` — podman output piped, not inherited

**Implementation:**
1. **Prerequisite:** Create `Containerfile.larry-test` — minimal Gentoo with Portage, podman client, GPU drivers. (Not created by earlier tasks — this is the only task that creates it.)
2. `internal/test/container.go` — `TestContainer(ebuildPath, cfg, pretend, useTUI) error`
3. Steps: build image from `Containerfile.larry-test` (skip if `$XDG_CACHE_HOME/larry/binpkgs/larry-test` cached) → create container with mounts (overlay→`/overlay`, DISTDIR→`/distfiles`, binpkgs→`/binpkgs`, GPU devices, `--group-add video`) → run `emerge --verbose --autounmask=y`
4. `--pretend`: print podman command, skip execution
5. **Note:** GPU device paths hardcoded for ROCm workstation. Not general-purpose. Future enhancement: move to Config.

**Acceptance:** Builds image, mounts correctly, GPU passthrough works. Pretend mode. Containerfile created and found.
