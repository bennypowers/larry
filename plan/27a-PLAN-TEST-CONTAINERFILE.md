# 27a — `larry test --emerge`: the Containerfile

**Why:** `Containerfile.larry-test` is `FROM docker.io/library/ubuntu:24.04` with apt
packages, a `larry` user, `EPREFIX=/larry` and `CMD ["emerge","--help"]`. **Portage is
never installed**, so container mode cannot work under any input. The one test guarding
the file passes only because the literal string `emerge` appears in a comment and in
`CMD`, and three of its four assertions actively describe the *wrong* image.

**Precondition:** none beyond a checkout. This plan touches **zero Go production
code** — only `Containerfile.larry-test` and `containerfile_test.go` at the repo root.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent on this machine, this
plan's text is authoritative — do not guess at the script's behaviour.**
Function: `ensure_test_image` (L819–831).

**Reference image:** the real, working image at `~/Developer/gentoo-overlay/Containerfile`
(45 lines). **If it is absent on this machine, the skeleton in this plan is
authoritative — do not guess.**

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. In particular: the known-red baseline is
> exactly `TestExtractVar/multi_line` in `internal/prepare` plus one `go vet` finding at
> `internal/git/git.go:318`. Do not chase either.

**Sibling plan:** `plan/27b-PLAN-TEST-CONTAINER-ARGS.md` fixes
`internal/test/container.go` (argv, mounts, image existence). The two are independent;
27a can land first and alone.

---

## Precondition check — run these first

```bash
ls -l Containerfile.larry-test
head -1 Containerfile.larry-test
grep -n 'func TestContainerfile_Exists' containerfile_test.go
ls -l ~/Developer/gentoo-overlay/Containerfile
```

Expected: `Containerfile.larry-test` exists and its first non-comment line is
`FROM docker.io/library/ubuntu:24.04`; `containerfile_test.go:9` declares
`TestContainerfile_Exists`. If either file is missing, **STOP and report** — this plan
has nothing to rewrite.

Note: `containerfile_test.go` sits at the **repo root** in `package main`, and it is the
only file in that package. Keep it there; `os.Stat("Containerfile.larry-test")` works
because `go test` runs with the package directory as CWD.

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D1 | `Containerfile.larry-test:7` | `FROM docker.io/library/ubuntu:24.04`, apt packages, `useradd larry`, `ENV EPREFIX=/larry`, `CMD ["emerge","--help"]`. **Portage is never installed** |
| D2 | `containerfile_test.go:28-30` | the only portage assertion is `strings.Contains(content,"portage") \|\| …"gentoo" \|\| …"emerge"` — satisfied today by the comment on line 2 and by `CMD ["emerge","--help"]` |
| D3 | `containerfile_test.go:32-40` | asserts `useradd`, a `larry` user and `EPREFIX`. All three describe the **Ubuntu** image and are **wrong** for a stage3 base — they would fail the correct Containerfile |

---

## Target: the Containerfile

Replace the whole of `Containerfile.larry-test` with a port of the reference image,
parameterised by `ARG` so it is not hardcoded to one workstation. **The `ARG` lines are
mandatory** — `TestContainerfile_HasRepoArg` below asserts them, and `$REPO` is
referenced three times.

```dockerfile
# larry test container image
# A clean-room Gentoo emerge environment with ROCm GPU passthrough.
#
# Build: podman build -t larry-test --build-arg REPO=<name> <overlay-dir>
#
# The overlay, distfiles and binpkgs are mounted at runtime, not baked in.

FROM docker.io/gentoo/stage3:amd64-openrc

# Overlay repo name, and GPU targets matching the host.
ARG REPO=bennypowers
ARG VIDEO_CARDS=amdgpu
ARG AMDGPU_TARGETS=gfx1201

# Accept ~amd64 for overlay packages and their deps
RUN echo 'ACCEPT_KEYWORDS="~amd64"' >> /etc/portage/make.conf

# GPU targets
RUN printf 'VIDEO_CARDS="%s"\n' "$VIDEO_CARDS" >> /etc/portage/make.conf \
 && printf 'AMDGPU_TARGETS="%s"\n' "$AMDGPU_TARGETS" >> /etc/portage/make.conf

# Parallel builds
RUN printf 'MAKEOPTS="-j%s -l%s"\n' "$(nproc)" "$(nproc)" >> /etc/portage/make.conf \
 && printf 'EMERGE_DEFAULT_OPTS="--jobs=4 --load-average=%s"\n' "$(nproc)" >> /etc/portage/make.conf

# Features: use binpkgs when available
RUN echo 'FEATURES="binpkg-multi-instance getbinpkg"' >> /etc/portage/make.conf

# Point DISTDIR and PKGDIR at the runtime mount points
RUN echo 'DISTDIR="/var/cache/distfiles"' >> /etc/portage/make.conf \
 && echo 'PKGDIR="/var/cache/binpkgs"'    >> /etc/portage/make.conf

# Overlay repo config (the overlay itself is mounted at runtime)
RUN mkdir -p /etc/portage/repos.conf \
 && printf '[%s]\nlocation = /var/db/repos/%s\n' "$REPO" "$REPO" \
    > "/etc/portage/repos.conf/$REPO.conf"

# Guru overlay — many of our packages depend on it
RUN printf '[guru]\nlocation = /var/db/repos/guru\nsync-type = git\nsync-uri = https://github.com/gentoo-mirror/guru.git\n' \
    > /etc/portage/repos.conf/guru.conf

# Sync the gentoo tree first (rsync, no git needed)
RUN emaint sync -r gentoo

# Binpkg trust, install git, then sync guru
RUN getuto && emerge -1q dev-vcs/git && emaint sync -r guru

WORKDIR /var/db/repos/$REPO
```

**The guru block is hardcoded, deliberately.** The earlier draft said "extra
`repos.conf` entries come from a config list" without naming a key, a default, a YAML
shape or a test. That is now **explicitly out of scope**: hardcode the guru block
exactly as the reference image does (`~/Developer/gentoo-overlay/Containerfile:35-37,
43`). If a second overlay is ever needed, that is a new plan with a named config key and
its own test.

Three `ARG`s only: `REPO`, `VIDEO_CARDS`, `AMDGPU_TARGETS`, with the defaults shown.
`buildImage` passes them via `--build-arg` (plan 27b owns that wiring; this plan only
has to make the defaults correct so the file builds standalone).

---

## Tests (write first — every one must fail before you implement)

`containerfile_test.go`. **DELETE `TestContainerfile_Exists`
(`containerfile_test.go:9-41`) in its entirety** — lines 32-40 assert `useradd`, the
`larry` user and `EPREFIX`, which are properties of the image being removed. Do not try
to salvage it. Replace it with the seven functions below.

Each reads the file once with a tiny helper:

```go
func readContainerfile(t *testing.T) string {
    t.Helper()
    b, err := os.ReadFile("Containerfile.larry-test")
    if err != nil {
        t.Fatalf("read Containerfile.larry-test: %v", err)
    }
    return string(b)
}
```

| Test | Exact assertion |
|---|---|
| `TestContainerfile_Present` | `os.Stat("Containerfile.larry-test")` returns no error and `Size() > 0`. Nothing else — this is the trivial existence check that replaces the old function's first half. |
| `TestContainerfile_UsesGentooStage3` (**D1**) | scan the content line by line; the **first** line whose `strings.Fields(line)[0] == "FROM"` must have `strings.Fields(line)[1] == "docker.io/gentoo/stage3:amd64-openrc"`. Do **not** use `strings.Contains` — a comment must not satisfy it. |
| `TestContainerfile_NotUbuntu` (**D2**) | the content does **not** contain any of `ubuntu`, `apt-get`, `useradd`, `EPREFIX` (case-insensitive on the first two). |
| `TestContainerfile_ConfiguresReposConf` | the content contains `/etc/portage/repos.conf` and the literal `location = /var/db/repos/`. |
| `TestContainerfile_SetsDistdirAndPkgdir` | the content contains both `DISTDIR="/var/cache/distfiles"` and `PKGDIR="/var/cache/binpkgs"`. |
| `TestContainerfile_SyncsPortageTree` | some line, after stripping a leading `RUN ` and surrounding whitespace, starts with `emaint sync -r gentoo`. |
| `TestContainerfile_HasRepoArg` | there is a line matching `^ARG REPO=` and lines matching `^ARG VIDEO_CARDS=` and `^ARG AMDGPU_TARGETS=`; each has a non-empty default after the `=`. |

Keep the assertions string-based and comment-blind: strip everything from the first `#`
on a line before matching, except in `TestContainerfile_NotUbuntu` (which must catch the
word anywhere, including comments).

---

## Implementation steps

1. Delete `TestContainerfile_Exists` from `containerfile_test.go` and write the seven
   tests above. Run them: `TestContainerfile_Present` passes, the other six fail.
2. Replace `Containerfile.larry-test` with the skeleton above.
3. Re-run — all seven green.
4. Hand back, naming the deleted test function.

That is the whole plan. It is small on purpose.

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

Also Tier 1 — the real proof, no podman and no network needed:

```bash
go test . -run 'TestContainerfile' -count=1 -v
# all seven green; TestContainerfile_UsesGentooStage3 fails if the base image
# ever regresses to a non-Gentoo one
```

**Tier 2 — read-only, safe to run:**

```bash
grep -in 'ubuntu\|apt-get\|useradd\|EPREFIX' Containerfile.larry-test
# expect: no output

diff <(grep -v '^#' Containerfile.larry-test | grep -v '^$') \
     <(grep -v '^#' ~/Developer/gentoo-overlay/Containerfile | grep -v '^$')
# expect: differences ONLY where an ARG replaced a hardcoded value
```

**Tier 3 — DO NOT RUN. Report verbatim for a human.**

```bash
# Tier 3 — DO NOT RUN. Report verbatim.
# Requires network, ~30min and multiple GB of disk. Downloads a gentoo stage3,
# syncs the portage tree, runs getuto and a real emerge.
podman rmi larry-test 2>/dev/null
podman build -t larry-test --build-arg REPO=bennypowers \
  -f ~/Projects/Developer/larry/Containerfile.larry-test ~/Developer/gentoo-overlay
podman run --rm larry-test emerge --info | head -20
```

**Tier 1 alternative to that Tier 3 block** — what you must actually run and report
instead: `TestContainerfile_UsesGentooStage3` plus, once plan 27b has landed,
`TestContainerArgs_EmergeCommandLine`. Between them they prove the base image is Gentoo
and the command line is well-formed, which is everything the build would have shown
short of network availability.

## Out of scope

All Go code, including `internal/test/container.go` (plan 27b). Making the image
overlay-agnostic beyond the three `ARG`s. A config-driven `repos.conf` list — the guru
block is hardcoded. Do not fix unrelated bugs you notice — report them.
