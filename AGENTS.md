# `larry` — Agent Development Guide

Project structure, workflow, and hard constraints for coding agents.

## Reference implementation

**This Go binary is larry.** The original fish script
(`gentoo-overlay/scripts/larry`, ~1860 lines) has been deleted — the Go
implementation fully replaced it, and there is no second implementation to
diverge from. Behavioural decisions live here and in the plan files.

`~/Developer/gentoo-overlay/` is the reference overlay — use it for manual verification.
Its `Containerfile` (gentoo/stage3) is the reference test image.

## Prerequisites (workstation-verified)

`ebuild`, `pkgdev`, `gh`, `podman`, `npm`, `go`, `git`, `pycargoebuild`, `golangci-lint`,
`staticcheck`, GPU devices (`/dev/kfd`, `/dev/dri/card0`, `/dev/dri/renderD128`).

## Project structure

```
plan/PLAN.md    — Master index, current status, task list.  READ FIRST.
plan/SPEC.md    — Full specification
plan/01..11-*   — Phase 1 build-out (historical; statuses corrected in PLAN.md)
plan/12..29-*   — Phase 2 parity & correctness (active work)
README.md       — End-user docs
AGENTS.md       — This file
```

One plan file = one agent session. Execute in numbered order; respect `Precondition:`.
Once a plan item is complete, tested, and verified, mark it done in plan/PLAN.md

## Build & test

```bash
make test                     # go test ./...
make build                    # compile
make lint                     # go vet + staticcheck
golangci-lint run ./...       # full lint — must be clean before any push
go test ./... -count=1        # no cached results before a push
```

## TDD discipline

Every change starts with a **failing** test. Each plan file names the tests to write.
Golden fixtures live in `testdata/`. Never implement before the red test exists.

## Hard rules

### Parse ebuild filenames with the shared helper, never ad hoc
`-r` revision suffixes are anchored: `-r\d+$`. `strings.LastIndex(name, "-r")` corrupts
every package whose name contains `-r` (`kornia-rs`, `python-requests`, `foo-runtime`).
PN comes from the **package directory name**, not from splitting the filename on hyphens.
One parser, in `internal/atom`. Do not add a fourth copy.

### One variable expander
Ebuild `${VAR}` expansion is fixpoint iteration over **every** `^[A-Z_]+="..."`
assignment in the file, plus `${PN}/${PV}/${P}`. Hardcoded var maps (`${MY_NPM_NAME}`,
etc.) silently leak literal `${...}` into URLs. Use `internal/ebuild.ExpandVariables`.

### One type detector
`internal/ebuild.DetectType*` is the sole owner of package-type detection. Detection
order is npm → cargo → go → vendored → generic. Two detectors that
disagree on the same file is a bug, not a feature.

### Single DISTDIR through the whole call chain
`$XDG_CACHE_HOME/larry/distfiles` (fallback `~/.cache/larry/distfiles`). Resolve once in
`internal/config`, pass as a parameter. Never hardcode `/var/cache/distfiles` or
`/usr/portage/distfiles`; never let it default to `""`.

### Manifest pipeline order
fetch **all** distfiles for **all** ebuilds in the package → verify → `rm -f Manifest` →
`pkgdev manifest -d $DISTDIR <category/package>` from the overlay root → verify.
Target `category/package`, never a single ebuild path: `rm Manifest` plus a single-file
target destroys sibling versions' entries.

### Prefer `pkgdev` over `ebuild digest`
`pkgdev manifest` handles eclass expansion (`${CARGO_CRATE_URIS}`) and category-level
operations that `ebuild digest` misses. If you change the runner, change the arg builder
in the same commit.

### `mirror://npm/` is invisible to Portage
pkgdev cannot resolve it. Pre-populate DISTDIR by hand before manifesting.

### Replicate external invocations exactly
Argument order, working directory, and input form (extracted dir vs. `.tgz` passed
directly) change behaviour in ways unit tests cannot catch. Replay the exact
external command when in doubt.

### Cache format compatibility
npm v11 writes `_cacache` with `index-v5`/`content-v2`. A mismatched format fails
silently at build time. Verify by running the consumer against the extracted tarball.

### Verify with a real build
Unit tests prove logic; only Portage proves the pipeline. After touching manifest or deps
tarballs: `ebuild <path> digest` (no empty entries), then the install phase (offline
`npm --cache` / cargo fetch must succeed).

### Reuse what exists
`internal/version` is the Gentoo version comparator. `internal/git` wraps go-git.
`internal/ebuild/srcuri.go` expands variables. Do not hand-roll parallel implementations
in `internal/importcmd` or `cmd/larry`.

### CLI invariants
Global flags go on `rootCmd.PersistentFlags()`. `config.Load()` must actually run.
Subprocess exit codes propagate to `os.Exit`. No subcommand prints with bare `fmt.Printf`
when `--quiet`/`--color` are supposed to apply.

## External tool delegation policy

**Delegate:** `ebuild`, `pkgdev manifest`, `podman`, `npm`, `go`, `pycargoebuild`,
`portageq`, `gh` (releases/uploads — auth via `GITHUB_TOKEN`).

**Native (no subprocess):** `go-git` (remotes, tags, commits — *never* text-parse
`.git/config`), `net/http`, `encoding/json`, stdlib regex/strings, `archive/tar` +
`compress/gzip`, `os.MkdirTemp`, `filepath.WalkDir`, `internal/version` (not `sort -V`).

## Key Go dependencies

| Package                              | Purpose                  |
| ------------------------------------ | ------------------------ |
| `github.com/spf13/cobra`             | CLI framework            |
| `github.com/spf13/viper`             | Configuration            |
| `github.com/charmbracelet/bubbletea` | TUI                      |
| `github.com/charmbracelet/lipgloss`  | TUI styling              |
| `github.com/muesli/termenv`          | Terminal color detection |
| `github.com/go-git/go-git/v5`        | Native git operations    |
