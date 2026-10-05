# 27b — `larry test --emerge`: podman argv, mounts and image existence

**Why:** Even with a correct image, `internal/test/container.go` could not work. The
emerge command line contains a stray bare `"="` argument and targets a host **file
path** instead of an atom. The overlay is mounted where Portage will never look, there
is no binpkgs mount despite `PKGDIR=/binpkgs` being exported, `--pretend` is never
passed to `emerge`, the image build runs *before* the pretend check, image existence is
tracked by a sentinel file that goes stale forever, and `cfg` is dereferenced eight
lines before its nil guard.

**Precondition:** 12 (`internal/atom`), 13a (`13a-PLAN-CLI-HARNESS.md` — config + the
`cmd/larry` test harness),
26 (test-command wiring — it adds `RunWithEnv` to `CmdRunner`, which `realRunner` in
this file must implement).

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent on this machine, this
plan's text is authoritative — do not guess at the script's behaviour.**
Functions: `ensure_test_image` (L819–831), `do_test_container` (L833–874).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. In particular: the known-red baseline is
> exactly `TestExtractVar/multi_line` in `internal/prepare` plus one `go vet` finding at
> `internal/git/git.go:318`. Do not chase either.

**Sibling plan:** `plan/27a-PLAN-TEST-CONTAINERFILE.md` rewrites
`Containerfile.larry-test`. 27a touches no Go; this plan touches no Containerfile. They
are independent, but the Tier 3 end-to-end check needs both.

---

## Precondition check — run these first

```bash
go doc ./internal/atom Atom
go doc ./internal/atom ParseAtom
go doc ./internal/atom ParsedEbuild
go doc ./internal/config ResolveCacheDir
grep -n 'RunWithEnv' internal/test/local.go internal/test/container.go
grep -n 'func execCmd\|func fixtureOverlay' cmd/larry/testhelp_test.go
```

Expected (plan 12): `ParseAtom(overlay, atom string) (*Atom, error)` returning
`Atom{Category, PN, Version, PkgDir}`, and `ParsedEbuild{Path, Category, PN, PVR, PV,
Revision, P, PF, Live}`.

Expected (plan 13a): `config.ResolveCacheDir`, defaulting to `$XDG_CACHE_HOME/larry`.

Expected (plan 26): `CmdRunner` declares `RunWithEnv(env []string, name string, args
...string) error` and **both** `runner` and `realRunner` implement it.

**If `cmd/larry/testhelp_test.go` with `execCmd` and `fixtureOverlay` does not exist,
plan 13a has not landed. STOP and report — do not hand-roll a second `cmd/larry`
harness.**

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D2 | `internal/test/container.go:135` | `args = append(args, "emerge", "--verbose", "--autounmask=y", "=", ebuildPath)` — a stray bare `"="` argv element, a host **file path** instead of an atom, and missing `-1` and `--autounmask-write=y`. Script (L836): `emerge -1 --verbose --autounmask=y --autounmask-write=y $atom` |
| D3 | `internal/test/container.go:135-140` | `--pretend` is never added to the emerge args; the printed command at `:137` is the same args, so copy-pasting it performs a **real** merge. Script adds `--pretend` to `emerge_args` (L837-839) |
| D4 | `internal/test/container.go:229-242` | `buildImage` runs **before** the pretend check (which lives inside `runContainerEmerge` at `:136`) and before `checkPodmanAvailable` at `:239`. Script skips `ensure_test_image` when pretending (L843-845) |
| D5 | `internal/test/container.go:84-103` | image existence is a sentinel **file** at `<cacheDir>/<imageName>`. Once written, deleting the image means every run fails and never rebuilds. Script uses `podman image exists` (L820) |
| D6 | `internal/test/container.go:94` | build context is `"."` (the CWD); the script builds from `$OVERLAY_DIR` (L825) |
| D7 | `internal/test/container.go:50-60` | mounts `<overlay>:/overlay` (no `:ro`, wrong target) and `<distdir>:/distfiles`; **no binpkgs mount at all**, yet `PKGDIR=/binpkgs` is exported at `:131`. Script mounts `$OVERLAY_DIR:/var/db/repos/<repo>:ro`, `$LARRY_CACHE/distfiles:/var/cache/distfiles`, `$LARRY_CACHE/binpkgs:/var/cache/binpkgs`, and `mkdir -p`s both cache dirs first (L834) |
| D8 | `internal/test/container.go:218` vs `:230` | `cfg.Overlay` is dereferenced at `:218`, **twelve lines before** the `if cfg == nil` guard at `:230`. The second `if containerfile == ""` at `:225` is unreachable (`:219-223` always assigns a non-empty path) |
| D9 | `cmd/larry/test.go:114` | image name hardcoded `"larry-test"`; `ContainerConfig.ImageName` exists (`container.go:16`) but no flag feeds it |
| D10 | `internal/test/container.go:63-70` | `--device=/dev/dri/card0 --device=/dev/dri/renderD128`; the script passes `--device /dev/dri` — the whole directory (L847-849) |
| D11 | `internal/test/container.go:177-183` | `checkPodmanAvailable(runner CmdRunner)` **ignores its runner** and calls `exec.Command("podman","--version")` directly. Three of this plan's tests would shell out to real podman because of it |

---

## Target behaviour

### Mount syntax changes form — state it, test it

Today `getContainerMounts` emits one `--mount=type=bind,source=…,target=…` element per
mount. **Switch to the script's `-v src:dst[:ro]` two-element form.** This is a
deliberate, visible change: `TestContainerArgs_Mounts` asserts the exact argv pairs, so
the two forms cannot be confused.

```go
// getContainerMounts returns podman argv elements, two per mount.
func getContainerMounts(cfg *ContainerConfig) []string
```

returns, in this order:

```
"-v", "<overlay>:/var/db/repos/<repo>:ro"
"-v", "<cacheDir>/distfiles:/var/cache/distfiles"
"-v", "<cacheDir>/binpkgs:/var/cache/binpkgs"
```

and `os.MkdirAll`s `<cacheDir>/distfiles` and `<cacheDir>/binpkgs` first (script L834).
`<repo>` is the repo name (see below). Drop the now-redundant `-e DISTDIR=/distfiles`
and `-e PKGDIR=/binpkgs` at `:130-131` — the image's `make.conf` sets both.

### Full run invocation

```
podman run --rm \
  --device /dev/kfd --device /dev/dri --group-add video \
  -v <overlay>:/var/db/repos/<repo>:ro \
  -v <cache>/distfiles:/var/cache/distfiles \
  -v <cache>/binpkgs:/var/cache/binpkgs \
  <image> emerge -1 --verbose --autounmask=y --autounmask-write=y [--pretend] <atom>
```

GPU flags become six separate elements: `--device`, `/dev/kfd`, `--device`, `/dev/dri`,
`--group-add`, `video` (**D10**).

The emerge target is the **atom**, never a host path: `category/package` when the user
gave a bare atom, `=category/package-PVR` when they gave a version or an ebuild path.
Derive it with `internal/atom`:

```go
pe, err := atom.ParseEbuildPath(ebuildPath)   // plan 12
target := "=" + pe.Category + "/" + pe.PF     // PF = pn-pv-rN, or pn-pv when rN == 0
```

### Repo name

Add a config key, resolved like the others:

```
repo name : --repo → $LARRY_REPO → config file `repo_name`
            → <overlay>/profiles/repo_name (first line, trimmed)
            → ERROR
```

as `config.ResolveRepoName(overlay string, ...)`. `fixtureOverlay` (plan 13a) already
writes `profiles/repo_name`, so the tests get it for free.

### Image existence and build order

`TestContainer` becomes, in this order:

1. `if cfg == nil { cfg = &ContainerConfig{} }` — **first statement in the function**
   (**D8**).
2. `cfg.UseTUI = useTUI`; resolve ebuild, atom, repo name, cache dir.
3. If `pretend`: print the command and return. **No `checkPodmanAvailable`, no
   `imageExists`, no `buildImage`.** (**D4**)
4. `checkPodmanAvailable(runner)` — now actually using the runner (**D11**).
5. `imageExists(imageName, runner)` → `runner.Run("podman","image","exists",name)`;
   error means absent. If absent, `buildImage`. **Delete the sentinel-file logic at
   `container.go:86-103` entirely, including the `os.WriteFile(cacheFile, …)` at
   `:103`.** (**D5**)
6. `buildImage` builds with the overlay as the context and `--build-arg` values:
   `podman build -t <image> --build-arg REPO=<repo> -f <containerfile> <overlay>`
   (**D6**).
7. `runContainerEmerge`.

Fix `findContainerfile` usage at `:218-226`: compute the containerfile **after** the nil
guard, and delete the unreachable second `if containerfile == ""` at `:225` (**D8**).

### `checkPodmanAvailable` must use its runner

```go
func checkPodmanAvailable(runner CmdRunner) error {
    if err := runner.Run("podman", "--version"); err != nil {
        return fmt.Errorf("podman not found: install podman or use 'larry test' local mode instead")
    }
    return nil
}
```

Note the lower-case error string — `golangci-lint` already flags
`internal/test/container.go:81` and `:226` as `ST1005` (capitalised error strings);
lower-case any error string you touch here. Plan 29c owns the rest.

### `--image` flag

`cmd/larry/test.go` gains `--image` (default empty) feeding `ContainerConfig.ImageName`;
`getImageName` keeps its `"larry-test"` fallback (`container.go:21-26`). Delete the
hardcoded `ImageName: "larry-test"` at `cmd/larry/test.go:114` (**D9**).

---

## Tests (write first — every one must fail before you implement)

All of these use a **recording stub runner** — no podman ever runs:

```go
type recRunner struct {
    calls [][]string            // each: name followed by args
    fail  map[string]error      // keyed by strings.Join(args, " ")
}
func (r *recRunner) Run(name string, args ...string) error
func (r *recRunner) RunWithEnv(env []string, name string, args ...string) error
```

### `internal/test/container_test.go`

| Test | Exact input | Exact expected output |
|---|---|---|
| `TestContainerArgs_EmergeCommandLine` (**D2**) | overlay fixture with `dev-util/javy/javy-9.1.0.ebuild`, repo `bennypowers`, image `larry-test`, pretend=false, stub reports the image exists | the single recorded `podman` call's args end with exactly `["larry-test","emerge","-1","--verbose","--autounmask=y","--autounmask-write=y","=dev-util/javy-9.1.0"]`. Additionally assert: **no** element equals `"="`, and **no** element has the suffix `.ebuild` |
| `TestContainerArgs_PretendAddsFlag` (**D3**) | same, pretend=true, output captured | the printed command contains `--pretend` immediately before the atom, and the stub recorded **zero** calls |
| `TestContainerArgs_Mounts` (**D7**) | overlay `/o`, cacheDir `/c`, repo `bennypowers` | `getContainerMounts` returns exactly `[]string{"-v","/o:/var/db/repos/bennypowers:ro","-v","/c/distfiles:/var/cache/distfiles","-v","/c/binpkgs:/var/cache/binpkgs"}`. Compare with `reflect.DeepEqual` against that exact slice |
| `TestContainerArgs_NoMountTypeBindSyntax` (**D7**) | same | no returned element contains the substring `--mount=` |
| `TestContainerArgs_NoDistdirPkgdirEnv` (**D7**) | full run args | no element equals `DISTDIR=/distfiles` or `PKGDIR=/binpkgs`, and no element is `-e` |
| `TestContainer_CreatesCacheDirs` (**D7**) | `cacheDir = filepath.Join(t.TempDir(),"c")`, not pre-created | `<c>/distfiles` and `<c>/binpkgs` are directories afterwards |
| `TestContainer_GPUFlags` (**D10**) | — | `getGPUFlags` returns exactly `[]string{"--device","/dev/kfd","--device","/dev/dri","--group-add","video"}`; assert with `reflect.DeepEqual`. No element contains `card0` or `renderD128` |
| `TestContainer_PretendSkipsBuild` (**D4**) | pretend=true | the stub recorded zero calls — in particular no `podman build` and no `podman --version` |
| `TestContainer_UsesImageExists` (**D5**) | stub answers `podman image exists larry-test` with nil (present) | the recorded calls include `["podman","image","exists","larry-test"]` and **no** `podman build` |
| `TestContainer_RebuildsWhenImageAbsentDespiteStaleSentinel` (**D5**) | stub answers `podman image exists` with an error; **and** a stale sentinel file `<cacheDir>/larry-test` is pre-created with `os.WriteFile` | a `podman build` call **is** recorded. This is the exact regression the sentinel caused |
| `TestContainer_BuildContextIsOverlay` (**D6**) | image absent | the recorded build call's **last** element is the overlay path, and the call contains `--build-arg` followed by `REPO=bennypowers` |
| `TestContainer_NilConfigNoPanic` (**D8**) | `TestContainer(path, nil, stub, true, false)` | returns an error (not a panic); `recover()` must not fire. Run it with `-race` too |
| `TestContainer_PodmanCheckUsesRunner` (**D11**) | stub whose `Run` returns an error for `podman --version`; pretend=false | `TestContainer` returns an error mentioning `podman not found`, and the stub **recorded** the `["podman","--version"]` call. This proves the real `exec.Command` is gone |
| `TestContainer_AtomForBareCategoryPackage` (**D2**) | an ebuild `dev-util/javy/javy-9.1.0-r2.ebuild` | the emerge target is `=dev-util/javy-9.1.0-r2` (uses `pe.PF`) |

Delete or rewrite the existing tests whose subject you change:
`TestContainer_BuildImage_Cached`, `_BuildImage_NotCached` (`container_test.go:15, 40` —
both assert the sentinel file), `TestContainer_MountOverlay`, `_MountDistdir` (`:88`,
`:112` — both assert `--mount=type=bind`), `TestContainer_GPUPassthrough` (`:136` —
asserts `card0`), `TestContainer_PodmanNotFound` (`:268` — shells out to real podman),
`TestContainer_RunEmerge`, `_RunEmerge_Pretend` (`:171`, `:202` — assert the broken
argv). List every one you delete in your report.

### `cmd/larry/test_test.go` (plan 13a's harness)

| Test | Exact input | Exact expected output |
|---|---|---|
| `TestTestCmd_ImageNameFlag` (**D9**) | `execCmd(t,"test","--emerge","--pretend","--image","foo","dev-util/javy")` | the printed command contains ` foo emerge ` and does not contain `larry-test` |
| `TestTestCmd_EmergePretendBuildsNothing` (**D4**) | `--emerge --pretend` | the injected runner records zero calls |

---

## Implementation steps

Every step has a test above. Write the test red first.

1. Move the `if cfg == nil` guard to the first statement of `TestContainer`; compute the
   containerfile after it; delete the unreachable `if containerfile == ""` at `:225`.
   Covers: `TestContainer_NilConfigNoPanic`. (**D8**)
2. Make `checkPodmanAvailable` call `runner.Run("podman","--version")`.
   Covers: `TestContainer_PodmanCheckUsesRunner`. (**D11**)
3. Add `imageExists(name string, runner CmdRunner) bool` on
   `runner.Run("podman","image","exists",name)`. Delete the sentinel logic at
   `container.go:86-92` and `:99-103`.
   Covers: `TestContainer_UsesImageExists`,
   `_RebuildsWhenImageAbsentDespiteStaleSentinel`. (**D5**)
4. Reorder `TestContainer` per "Image existence and build order": pretend returns before
   podman is touched at all.
   Covers: `TestContainer_PretendSkipsBuild`, `TestTestCmd_EmergePretendBuildsNothing`.
   (**D4**)
5. `buildImage`: context = overlay, add `--build-arg REPO=<repo>`; add
   `config.ResolveRepoName`.
   Covers: `TestContainer_BuildContextIsOverlay`. (**D6**)
6. Rewrite `getContainerMounts` to the three `-v` pairs with `os.MkdirAll`; drop the
   `-e DISTDIR=` / `-e PKGDIR=` pair at `:130-131`.
   Covers: `TestContainerArgs_Mounts`, `_NoMountTypeBindSyntax`,
   `_NoDistdirPkgdirEnv`, `TestContainer_CreatesCacheDirs`. (**D7**)
7. Rewrite `getGPUFlags` to the six-element form.
   Covers: `TestContainer_GPUFlags`. (**D10**)
8. Rebuild the emerge argv from the atom; add `--pretend` to the emerge args when
   pretending, so the printed command is exactly what would run.
   Covers: `TestContainerArgs_EmergeCommandLine`, `_PretendAddsFlag`,
   `TestContainer_AtomForBareCategoryPackage`. (**D2, D3**)
9. Add `--image` to `cmd/larry/test.go`; delete the hardcoded `ImageName: "larry-test"`
   at `:114`.
   Covers: `TestTestCmd_ImageNameFlag`. (**D9**)
10. Hand back listing every function and test you deleted.

**Checkpoint — after step 6, `go test ./internal/test/ -count=1` must be green.** If
budget is short, stop there and hand back; steps 7–9 are a clean second session.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**.

**Tier 1 — run these. All four are required.**

```bash
go build ./...
go vet ./...                      # expect ONLY internal/git/git.go:318 (baseline)
go test ./... -count=1            # no NEW failures vs. your recorded baseline
golangci-lint run ./...           # per AGENTS.md, required before any push
```

Also Tier 1 — the real proof. The whole argv/mount/GPU surface is testable without
podman: build the args, compare, never execute.

```bash
go test ./internal/test/ ./cmd/larry/ -count=1 -race -v
```

`TestContainerArgs_EmergeCommandLine` is a golden-argv assertion and is the proof of the
malformed command line.

**Tier 2 — read-only, safe to run:**

```bash
grep -n 'exec.Command("podman"' internal/test/container.go
# expect: no output outside realRunner

grep -n -- '--mount=type=bind\|card0\|renderD128\|"="' internal/test/container.go
# expect: no output

grep -n 'cacheFile\|os.WriteFile' internal/test/container.go
# expect: no output
```

```bash
# Tier 2: --pretend executes nothing and (after this plan) builds no image.
larry test --emerge --pretend dev-util/javy
# expect, verbatim shape:
#   podman run --rm --device /dev/kfd --device /dev/dri --group-add video \
#     -v <overlay>:/var/db/repos/<repo>:ro \
#     -v <cache>/distfiles:/var/cache/distfiles \
#     -v <cache>/binpkgs:/var/cache/binpkgs \
#     larry-test emerge -1 --verbose --autounmask=y --autounmask-write=y \
#     --pretend =dev-util/javy-9.1.0
# and NO image build, NO `podman --version`
```

**Tier 3 — DO NOT RUN. Requires GPU devices (`/dev/kfd`, `/dev/dri`), network, ~30min.**

```bash
# Tier 3 — DO NOT RUN. Report verbatim.
# Destroys a podman image, pulls a multi-GB stage3, runs emaint sync, getuto,
# emerge -1q dev-vcs/git, then a real emerge needing /dev/kfd + /dev/dri.
podman rmi larry-test 2>/dev/null
larry test --emerge dev-util/javy
```

**Tier 1 alternative to that block** — what you must run and report instead:
`TestContainerArgs_EmergeCommandLine` (this plan) and `TestContainerfile_UsesGentooStage3`
(plan 27a). Together they prove the command line is well-formed and the base image is
Gentoo, which is everything the real run would show short of network and GPU
availability.

## Out of scope

The Containerfile itself (plan 27a). Local mode (plan 26). The remaining `ST1005` and
`errcheck` findings in this file (plan 29b/29c) — fix only error strings you are already
rewriting. Do not fix unrelated bugs you notice — report them.
