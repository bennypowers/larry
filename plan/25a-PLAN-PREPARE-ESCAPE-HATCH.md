# 25a — `prepare`: the rpi-kernel hole and the `files/prepare.sh` escape hatch

**Why:** `TypeRpiKernel` is detected and then silently handled as generic — the package
reports success having regenerated a Manifest against a stale `COMMIT=`. The
`files/prepare.sh` escape hatch that was supposed to replace it skips the Manifest
entirely, never backs the ebuild up, and prints a 3-line sandbox warning on every run
including `--quiet` (it currently floods `go test ./...` output 8 times).

**Precondition:** 12 (`internal/atom`), 13a (`13a-PLAN-CLI-HARNESS.md` — the `cmd/larry`
test harness), 13b (`13b-PLAN-CLI-OUTPUT.md` — the `*output.Logger` `Msg`/`Warn`/`Error`
methods and `--quiet`), 20b (`20b-PLAN-EBUILD-DETECT.md` — `ebuild.ParsePackageType`),
21 (manifest with `catPkg`).

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent on this machine, this
plan's text is authoritative — do not guess at the script's behaviour.**
Functions: `prepare_rpi_kernel` (L501–543), `prepare_generic` (L545–549),
`do_prepare_one` (L553–579).
Prior decision: `plan/07-PLAN-PREPARE.md:195-205` removed the hardcoded rpi-kernel
pipeline in favour of a custom-script escape hatch.

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. In particular: the known-red baseline is
> exactly `TestExtractVar/multi_line` in `internal/prepare` plus one `go vet` finding at
> `internal/git/git.go:318`. Do not chase either.

**Sibling plan:** the vendored pipeline moved to `plan/25b-PLAN-PREPARE-VENDORED.md`.
The two share nothing; they can run in either order.

---

## Precondition check — run these first

```bash
go doc ./internal/atom ParseEbuildPath
go doc ./internal/ebuild ParsePackageType
go doc ./internal/output Logger
grep -n 'func runManifest' internal/prepare/manifest.go
grep -n 'func execCmd\|func fixtureOverlay' cmd/larry/testhelp_test.go
```

Expected:

```go
// plan 12
func ParseEbuildPath(path string) (*ParsedEbuild, error)   // .Category, .PN, .PV, .PVR

// plan 20b
func ParsePackageType(string) (PackageType, error)

// plan 13b — METHODS on *output.Logger, not package functions.
func (l *Logger) Msg(format string, args ...any)    // ">>> ", stdout, quiet-suppressed
func (l *Logger) Warn(format string, args ...any)   // "*** ", stderr, quiet-suppressed
func (l *Logger) Error(format string, args ...any)  // "!!! ", stderr, NEVER suppressed
func (l *Logger) SetOut(w io.Writer)
func (l *Logger) SetErr(w io.Writer)

// plan 21
func runManifest(overlay, catPkg, distdir string) error    // catPkg, NOT a file path
```

`grep -rn 'ParsePackageType' --include='*.go' ./cmd ./internal` returns **zero hits at
HEAD** — that symbol is created by plan 20b. If it is still absent, **STOP and report**; do not write
it yourself.

If `cmd/larry/testhelp_test.go` does not exist, plan 13a has not landed. **STOP and
report — do not hand-roll a second `cmd/larry` harness.**

---

## Part A — close the rpi-kernel hole

### Current state (verified at HEAD)

| ID | Location | Defect |
|----|----------|--------|
| A1 | `internal/prepare/prepare.go:73-84` | the dispatch `switch pkgType` has no `case ebuild.TypeRpiKernel`; it falls into `default:` → `prepareGeneric` (`prepare.go:117-137`), which backs the ebuild up and runs the manifest. The Manifest is regenerated against the **old** `COMMIT=` and `larry prepare` reports success |
| A2 | `internal/ebuild/detect.go:129-140` | detection *is* reachable but useless: it fires when `info.Inherit` contains `linux-info` **and** `strings.Contains(info.PV(), "_p")`. `info.PV()` reads `EbuildInfo.PVVal` (`internal/ebuild/ebuild.go:41-47`), i.e. the version parsed from the *filename*, not a literal `PV=` line. Its only consumer is the dispatch, which ignores it (A1). The same condition also misfires on any `linux-info` package with a `_pre`/`_patch` version |
| A3 | `cmd/larry/prepare.go:29-30, 55-63, 110-112` | `--type` is cast straight to `ebuild.PackageType` with no validation (`cmd/larry/prepare.go:111`), so `--type rpi-kernel` is silently accepted and falls through to `prepareGeneric`; the flag help at `:30` and the `Type detection (auto)` block in `Long` at `:55-63` list only `npm, cargo, go, vendored, generic` |

### Decision — already made, do not re-open

Honour the plan-07 decision: **do not** reintroduce a hardcoded Raspberry Pi pipeline.
Make the hole loud instead of silent.

1. Delete the rpi-kernel detection branch at `internal/ebuild/detect.go:129-140` and the
   `TypeRpiKernel` constant at `internal/ebuild/ebuild.go:10` (**A2**). Deleting the
   constant is the point: it makes every remaining reference a compile error, so nothing
   can quietly depend on it again.
2. Exclude `rpi-kernel` from `ebuild.ParsePackageType`'s accepted set (plan 20b owns the
   function; this plan owns the set), so `--type rpi-kernel` is a clear error pointing at
   `files/prepare.sh` (**A3**).
3. Ship the script's logic as a reference custom script at
   `docs/examples/rpi-kernel-prepare.sh`, referenced from the `prepare` long help and
   from `docs/commands.md`. Faithful port of script L501–543:
   extract `_p(\d{8})` from PV → `fw_tag="1.$fw_date"` → fetch
   `https://raw.githubusercontent.com/raspberrypi/firmware/$fw_tag/extra/git_hash` →
   validate `^[0-9a-f]{40}$` → require a `^COMMIT=` line → rewrite it.
4. The rollback the script performs on manifest failure (L536–542) becomes a *framework*
   guarantee, not the script's job — see Part B step 3. The example script therefore
   does **not** reimplement rollback.

If a future maintainer prefers a built-in pipeline instead, that is a separate plan;
this one must not leave the silent-success behaviour in place either way.

---

## Part B — `files/prepare.sh` escape hatch

| ID | Location | Defect |
|----|----------|--------|
| B1 | dispatch at `internal/prepare/prepare.go:39-41`; body at `internal/prepare/prepare.go:339-368` | `runCustomPrepare` returns **without generating a Manifest**. Every built-in pipeline ends with one; the script always produces one (`generate_manifest`, script L177) |
| B2 | `internal/prepare/prepare.go:339-368` | no ebuild backup around the custom script, so a script that rewrites the ebuild and then fails leaves it mutated. `Backup`/`Restore` already exist at `internal/prepare/rollback.go:9` and `:18` and are used by `prepareGeneric` (`prepare.go:124` and `:132`) — just not here |
| B3 | `internal/prepare/prepare.go:344-347` | a bare `fmt.Fprint(os.Stderr, "\nWARNING: custom prepare.sh runs with partial sandboxing. …")` printed on every run, including `--quiet`. **This string appears 8 times in today's `go test ./...` output.** It is in `internal/prepare`, not in `cmd/larry/prepare.go` as previously documented |

**Target for `runCustomPrepare`:**

```
if cfg.DryRun            -> return nil immediately, execute nothing, back nothing up
log.Warn(<sandbox warning>)      // one call, suppressed by --quiet
Backup(ei.Path)
run the script (cmd.Dir = filepath.Dir(ei.Path), cmd.Env = sandboxEnv(ei, cfg))
  on failure -> Restore(ei.Path); return the error
pe, _ := atom.ParseEbuildPath(ei.Path)
runManifest(cfg.Overlay, pe.Category+"/"+pe.PN, cfg.Distdir)
  on failure -> Restore(ei.Path); return the error
os.Remove(ei.Path + ".bak")
return nil
```

Keep the existing `sandboxEnv` (`prepare.go:372`) and the TUI stream-isolation branch
(`prepare.go:358-361`) exactly as they are.

The warning text is a single literal, defined once as a package constant so the test can
reference it rather than re-typing it:

```go
const customPrepareSandboxWarning = "custom prepare.sh runs with partial sandboxing. " +
    "Network access is NOT blocked and file writes are only policy-restricted. " +
    "For full sandboxing, use larry test --emerge or run inside a container."
```

(Note: no leading `\n`, no trailing `\n` — `Logger.Warn` owns the `*** ` prefix and the
newline.)

**How to reach the logger from a non-`cmd` package.** Plan 13b makes `Msg` / `Warn` /
`Error` **methods on `*output.Logger`** with injectable writers (`SetOut` / `SetErr`) —
there are no package-level `output.Msg` functions. If the package you are editing has
no logger handle, add a `Log *output.Logger` field to the config struct it already
receives and fall back to a default logger writing to `os.Stdout`/`os.Stderr` when it
is nil. **Do not add a package-level global.** Tests set `cfg.Log` to a logger whose
`SetOut`/`SetErr` point at `bytes.Buffer`s.

---

## Tests (write first — every one must fail before you implement)

### `internal/ebuild/detect_test.go`

- **`TestDetectType_RpiKernelEbuildIsGeneric`** (**A2**)
  Input: `&EbuildInfo{Inherit: []string{"linux-info"}, PVVal: "6.18.46_p20260824", Pkg: "raspberrypi-kernel"}`.
  Expected: `DetectTypeFromInfo` returns `TypeGeneric, nil`.
- **`TestDetectType_LinuxInfoPreReleaseIsGeneric`** (**A2**)
  Input: `&EbuildInfo{Inherit: []string{"linux-info"}, PVVal: "6.18.0_pre1"}`.
  Expected: `TypeGeneric, nil`.
- **`TestParsePackageType_RejectsRpiKernel`** (**A3**)
  Input: `ParsePackageType("rpi-kernel")`.
  Expected: `err != nil`; `err.Error()` contains the substring `files/prepare.sh`.
- **`TestParsePackageType_AcceptsTheFive`**
  Input: each of `npm`, `cargo`, `go`, `vendored`, `generic`.
  Expected: `err == nil` and the matching `PackageType` constant.

### `internal/prepare/custom_prepare_test.go`

Shared fixture for all five: `t.TempDir()/overlay/dev-util/foo/foo-1.0.0.ebuild`
containing exactly `EAPI=8\nCOMMIT="aaaa"\n`, plus
`<pkgdir>/files/prepare.sh` (mode `0o755`), plus a `runManifest` stub installed via the
plan-21 seam that records `(overlay, catPkg, distdir)` and returns a configurable error.

- **`TestCustomPrepare_RunsManifest`** (**B1**)
  Script body: `#!/bin/sh\nexit 0\n`. Manifest stub returns nil.
  Expected: `runCustomPrepare` returns nil; the stub was called exactly once with
  `catPkg == "dev-util/foo"` (**not** a path ending in `.ebuild`).
- **`TestCustomPrepare_RestoresOnScriptFailure`** (**B2**)
  Script body: `#!/bin/sh\nsed -i 's/aaaa/bbbb/' foo-1.0.0.ebuild\nexit 1\n`.
  Expected: error non-nil; `os.ReadFile(ebuildPath)` is byte-identical to
  `EAPI=8\nCOMMIT="aaaa"\n`; the manifest stub was **not** called; no `.bak` file
  remains in the package dir.
- **`TestCustomPrepare_RestoresOnManifestFailure`** (**B2**, replaces script L536–542)
  Script body: `#!/bin/sh\nsed -i 's/aaaa/bbbb/' foo-1.0.0.ebuild\nexit 0\n`.
  Manifest stub returns `errors.New("boom")`.
  Expected: error non-nil and wraps `boom`; the ebuild is byte-identical to
  `EAPI=8\nCOMMIT="aaaa"\n`; no `.bak` remains.
- **`TestCustomPrepare_DryRunDoesNotExecute`**
  Script body: `#!/bin/sh\necho ran > ran.txt\nexit 0\n`; `cfg.DryRun = true`.
  Expected: returns nil; `<pkgdir>/ran.txt` does not exist; no `.bak` exists; the
  manifest stub was not called.
- **`TestCustomPrepare_QuietSuppressesWarning`** (**B3**)
  Run twice with the logger's `SetOut`/`SetErr` pointed at `bytes.Buffer`s (plan 13b
  makes the writers injectable): once with quiet off, once with quiet on.
  Expected: quiet off → the buffer contains `customPrepareSandboxWarning` exactly once;
  quiet on → the buffer does **not** contain the substring `partial sandboxing`.

### `internal/prepare/prepare_test.go`

- **`TestPrepare_RpiKernelEbuildWarnsManifestOnly`** (**A1**)
  Input: a package dir with **no** `files/prepare.sh` and an ebuild
  `sys-kernel/rpi/rpi-6.18.46_p20260824.ebuild` containing
  `EAPI=8\ninherit linux-info\nCOMMIT="deadbeef"\n`. Manifest stub returns nil.
  Expected, all three:
  1. `Prepare` returns nil;
  2. the captured `Logger.Warn` (stderr) stream contains **exactly** the literal
     `this package type has no pipeline; only the Manifest was regenerated — add files/prepare.sh to do more`
     (define it as a package constant `genericManifestOnlyWarning` and assert against
     the constant);
  3. `os.ReadFile(ebuildPath)` is byte-identical to its original content — `COMMIT=`
     unchanged.

  This replaces the old, unwritable `…DoesNotReportSuccessSilently`. The observable is
  the warning plus the unchanged bytes; nothing else.
  Implementation: emit `genericManifestOnlyWarning` via `Logger.Warn` from `prepareGeneric`
  (`prepare.go:117`) on every call.

### `docs`

- **`TestExampleRpiScriptExists`** — `docs/examples/rpi-kernel-prepare.sh` exists, its
  mode has the owner-execute bit set, and its first line is exactly `#!/bin/sh` or
  `#!/usr/bin/env bash`.

---

## Implementation steps

Every step has a test above. Write the test red first.

1. Delete `internal/ebuild/detect.go:129-140` (the rpi branch) and the `TypeRpiKernel`
   constant at `internal/ebuild/ebuild.go:10`. Fix the resulting compile errors by
   deleting the references, not by reintroducing the constant.
   Covers: `TestDetectType_RpiKernelEbuildIsGeneric`, `_LinuxInfoPreReleaseIsGeneric`.
   (**A2**)
   *Verify with the test before deleting*: run `TestDetectType_RpiKernelEbuildIsGeneric`
   and confirm it is red (it returns `TypeRpiKernel` today) before you touch `detect.go`.
2. Remove `rpi-kernel` from `ebuild.ParsePackageType`'s accepted set and make its error
   name `files/prepare.sh`. Update the `cmd/larry/prepare.go:30` flag help and the
   `Type detection (auto)` block in `Long` (`:55-63`) to mention the escape hatch.
   Covers: `TestParsePackageType_RejectsRpiKernel`, `_AcceptsTheFive`. (**A3**)
3. Add `genericManifestOnlyWarning` and emit it from `prepareGeneric`
   (`internal/prepare/prepare.go:117`).
   Covers: `TestPrepare_RpiKernelEbuildWarnsManifestOnly`. (**A1**)
4. Write `docs/examples/rpi-kernel-prepare.sh` (port of script L501–543, **without**
   rollback — the framework owns that now). Reference it from
   `cmd/larry/prepare.go`'s `Long` and from `docs/commands.md`.
   Covers: `TestExampleRpiScriptExists`.
5. Restructure `runCustomPrepare` (`internal/prepare/prepare.go:339-368`) to
   backup → run → manifest → restore-on-any-failure, per the pseudocode above.
   Covers: `TestCustomPrepare_RunsManifest`, `_RestoresOnScriptFailure`,
   `_RestoresOnManifestFailure`, `_DryRunDoesNotExecute`. (**B1, B2**)
6. Replace the bare `fmt.Fprint(os.Stderr, …)` at `prepare.go:344-347` with a single
   `log.Warn(customPrepareSandboxWarning)` (see the logger-handle note above).
   Covers: `TestCustomPrepare_QuietSuppressesWarning`. (**B3**)
7. Hand back listing every symbol and test you deleted.

**Checkpoint — after step 3, `go test ./internal/ebuild/ ./internal/prepare/ -count=1`
must be no worse than baseline.** If budget is running short, stop there and hand back;
steps 4–6 (Part B) are a clean second session.

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

Also Tier 1:

```bash
go test ./internal/prepare/ ./internal/ebuild/ -count=1

# The sandbox warning no longer floods the suite (it printed 8 times at HEAD):
go test ./internal/prepare/ -count=1 2>&1 | grep -c 'partial sandboxing'
# expect: 0
```

**Tier 2 — read-only, safe to run:**

```bash
grep -rn 'TypeRpiKernel\|rpi-kernel' --include='*.go' internal/ cmd/
# expect: no output (docs/ and plan/ may still mention it)

grep -n 'fmt.Fprint(os.Stderr' internal/prepare/prepare.go
# expect: no output

larry prepare --type rpi-kernel ~/Developer/gentoo-overlay/dev-util/javy/javy-9.1.0.ebuild
# expect: exit non-zero, message names files/prepare.sh, NOTHING written
```

**Tier 3 — DO NOT RUN. Report verbatim for a human.**

```bash
# Tier 3 — DO NOT RUN. Report verbatim.
# Runs a real prepare pipeline against ~/Developer/gentoo-overlay and rewrites COMMIT=.
cd ~/Developer/gentoo-overlay
cp ~/Projects/Developer/larry/docs/examples/rpi-kernel-prepare.sh \
   sys-kernel/raspberrypi-kernel/files/prepare.sh
chmod +x sys-kernel/raspberrypi-kernel/files/prepare.sh
larry prepare sys-kernel/raspberrypi-kernel/<latest>.ebuild
#   expect: COMMIT= updated to the firmware's git_hash AND a fresh Manifest
git -C ~/Developer/gentoo-overlay checkout sys-kernel   # restore
git -C ~/Developer/gentoo-overlay clean -fd sys-kernel  # restore
```

```bash
# Tier 3 — DO NOT RUN. Report verbatim.
# Proves the rollback guarantee by deliberately breaking the manifest step.
cd ~/Developer/gentoo-overlay
printf '#!/bin/sh\nsed -i s/COMMIT=.*/COMMIT=\\"bad\\"/ *.ebuild\nexit 0\n' \
  > sys-kernel/raspberrypi-kernel/files/prepare.sh
chmod +x sys-kernel/raspberrypi-kernel/files/prepare.sh
larry prepare --distdir /nonexistent sys-kernel/raspberrypi-kernel/<latest>.ebuild
#   expect: exits non-zero AND `git diff` shows NO change to the ebuild
git -C ~/Developer/gentoo-overlay checkout sys-kernel
git -C ~/Developer/gentoo-overlay clean -fd sys-kernel
```

## Out of scope

The vendored pipeline (plan 25b). npm/cargo/go pipelines (plans 22a/22b, 23a/23b, 24). Manifest
mechanics (plan 21). `sandboxEnv`'s policy (it is adequate; AGENTS.md's "run inside a
container" caveat stands). Do not fix unrelated bugs you notice — report them.
