# 26 — `larry test`: local mode

**Why:** `--compile`, `--merge` and `--phase` are parsed into variables that are never
read; the runner is always handed `nil` phases and falls back to `clean`. Separately,
the sandbox-disabling environment the script sets up is in a dead code branch, so phases
run under the full Portage sandbox with no `DISTDIR` override. And `--merge` is defined
to include `qmerge`, which installs to the live system — the one thing the script's
`--merge` is documented not to do.

**Precondition:** 12 (`internal/atom`), 13a (`13a-PLAN-CLI-HARNESS.md` — the `cmd/larry`
test harness, persistent flags, `config.Load`, `ExitError`), 13b
(`13b-PLAN-CLI-OUTPUT.md` — the `*output.Logger` `Msg`/`Warn`/`Error` methods).

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent on this machine, this
plan's text is authoritative — do not guess at the script's behaviour.**
Functions: `do_test` (L715–778), `do_test_local` (L780–817), `msg`/`warn`/`err`
(L37–47).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. In particular: the known-red baseline is
> exactly `TestExtractVar/multi_line` in `internal/prepare` plus one `go vet` finding at
> `internal/git/git.go:318`. Do not chase either.

**Scope note — deliberately NOT split.** D9 (short flags) and D10 (bare `--<phase>`)
stay in this plan rather than moving to a `26b`, because the pre-parse pass that makes
`--<phase>` work is the same code that registers `-c`/`-m`/`-p`; splitting would
duplicate it. There is a hard checkpoint after step 5 — stop there and hand back if
budget is short.

---

## Precondition check — run these first

```bash
go doc ./internal/atom Atom
go doc ./internal/atom ParsedEbuild
go doc ./internal/atom ParseAtom
go doc ./internal/atom FindForAtom
go doc ./internal/config ResolveDistdir
go doc ./internal/output Logger
grep -n 'type ExitError' cmd/larry/*.go internal/*/*.go
grep -n 'func execCmd\|func fixtureOverlay' cmd/larry/testhelp_test.go
```

Expected (plan 12):

```go
type Atom struct {
    Category string
    PN       string
    Version  string // "" when the atom carried no version
    PkgDir   string // <overlay>/<category>/<pn>
}

type ParsedEbuild struct {
    Path     string // absolute path
    Category string
    PN       string
    PVR      string
    PV       string
    Revision int
    P        string
    PF       string
    Live     bool
}

func ParseAtom(overlay, atom string) (*Atom, error)
func FindForAtom(a *Atom) (*ParsedEbuild, error)
```

Expected (plan 13a): `config.ResolveDistdir` and
`type ExitError struct{ Code int; Err error }`.

Expected (plan 13b) — **methods on `*output.Logger`, not package-level functions:**

```go
func (l *Logger) Msg(format string, args ...any)    // ">>> ", stdout, quiet-suppressed
func (l *Logger) Warn(format string, args ...any)   // "*** ", stderr, quiet-suppressed
func (l *Logger) Error(format string, args ...any)  // "!!! ", stderr, NEVER suppressed
func (l *Logger) SetOut(w io.Writer)
func (l *Logger) SetErr(w io.Writer)
```

**How to reach the logger from a non-`cmd` package.** Plan 13b makes `Msg` / `Warn` /
`Error` **methods on `*output.Logger`** with injectable writers (`SetOut` / `SetErr`) —
there are no package-level `output.Msg` functions. If the package you are editing has
no logger handle, add a `Log *output.Logger` field to the config struct it already
receives and fall back to a default logger writing to `os.Stdout`/`os.Stderr` when it
is nil. **Do not add a package-level global.** Tests set `cfg.Log` to a logger whose
`SetOut`/`SetErr` point at `bytes.Buffer`s.

For this plan that means adding `Log *output.Logger` to the parameters `TestLocal`
already takes, or to a small `LocalConfig` struct you introduce — your call, but say
which in your report.

**If `cmd/larry/testhelp_test.go` with `execCmd` and `fixtureOverlay` does not exist,
plan 13a has not landed. STOP and report — do not hand-roll a second `cmd/larry`
harness.** Half this plan's tests live in `cmd/larry` and depend on it.

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D1 | `cmd/larry/test.go:28-41, 125` | `testCompile`, `testMerge`, `testPhases` are registered and never read; `test.TestLocal(ebuildPath, nil, runner, testPretend, true)` hardcodes `nil` phases → `internal/test/local.go:199-201` falls back to `[]string{"clean"}`. **`larry test --compile foo` runs `ebuild <path> clean` and nothing else** |
| D2 | `internal/test/local.go:68` | `--merge` → `clean fetch unpack compile install **qmerge**`. The script stops at `install` (L729) and documents "NOT live system" (L739). `qmerge` merges into the live filesystem |
| D3 | `internal/test/local.go:65` | `--compile` → `clean compile` (no fetch/unpack); the script uses `clean fetch unpack compile` (L727). Compiling an unfetched, unextracted package cannot work |
| D4 | `internal/test/local.go:63-71` | default is `[]string{"clean"}`; the script's default is `clean fetch unpack` (L768-770) |
| D5 | `internal/test/local.go:145-168` | because `cmd/larry/test.go:124` always supplies a non-nil `runner`, execution takes the `runner.Run(...)` branch at `:152-157`, which sets **no `cmd.Env` at all**. The env-bearing branch at `:159-167` — which only ever sets `FEATURES=-network-sandbox`, missing the other four — is dead, as are `setNetworkFeatures` (`:128-138`) and `TestLocalConfig` (`:175-180`). `$DISTDIR` and `$PORTAGE_TMPDIR` are never created |
| D6 | `internal/test/local.go:37-49` | the valid-phase set is missing `test package preinst postinst prerm postrm nofetch` and adds four non-script entries (`postunpack digest manifest qmerge`) |
| D7 | `internal/test/local.go:211-215` | one `ebuild` process per phase; the script runs `ebuild $ebuild $_test_phases` once (L804) |
| D8 | `internal/test/local.go` + `cmd/larry/main.go:8-13` | the real exit code is swallowed; always `os.Exit(1)`. No `Package:`/`Ebuild:`/`Phases:` banner (script L785-787), no `Success:`/`Failed: … Exit code: N` summary (script L809-814) |
| D9 | `cmd/larry/test.go:28-33` | no `-c`/`-m`/`-p` short flags (script L725, L727, L729) |
| D10 | `cmd/larry/test.go` | no arbitrary `--<phase>` passthrough; cobra rejects `--prepare`, `--configure`, … (script L746-753) |
| D11 | `internal/test/local.go:81-89`, `cmd/larry/test.go:76` | relative ebuild paths are rejected: `resolveEbuild` only accepts a path starting with `/` |
| D12 | `cmd/larry/test.go:74-107` | the inline `os.ReadDir` + `ebuilds[len(ebuilds)-1]` ebuild picker: no version sort, no `-rN` handling, `foo-9.0` beats `foo-10.0`, `foo-9999` always wins. A versioned atom (`dev-util/javy-8.1.1`) is treated as a directory name and fails |

---

## Target behaviour

### Phase selection (script L724–771)

```
--compile / -c  -> clean fetch unpack compile
--merge   / -m  -> clean fetch unpack compile install      (NO qmerge)
--phase X       -> exactly the phases given, in order
--<phase>       -> appended to whatever base list is in effect
default         -> clean fetch unpack
```

**Precedence — settle it exactly like this, the script's `for arg in $argv` loop is the
reference:**

1. If `--phase` was given at least once, it **wins outright**: the phase list is
   exactly the `--phase` values in command-line order, and `--compile`/`--merge` are
   ignored entirely. (This is the one place where larry deliberately differs from the
   script, which has no `--phase`; the existing flag help at `cmd/larry/test.go:41`
   already promises "overrides --compile/--merge", so honour it.)
2. Otherwise the **base list** is: `--merge`'s list if `--merge` was given, else
   `--compile`'s list if `--compile` was given, else the default. **`--compile` and
   `--merge` together → `--merge` wins** (it is the superset; the script's last-wins
   loop would do the same for `-c -m`, and `--merge` is the safer reading of
   `-m -c`).
3. Bare `--<phase>` flags are then **appended** to that base list, in command-line
   order, with no deduplication.

So `larry test --compile --install foo` → `clean fetch unpack compile install`, and
`larry test --phase fetch --compile foo` → `fetch`.

### Valid phases — exactly the script's list (L724)

```
clean fetch unpack prepare configure compile install test package preinst postinst
prerm postrm nofetch
```

Fourteen entries. `postunpack`, `digest`, `manifest` and `qmerge` are **removed**.

### Execution (script L796–805)

Create `$DISTDIR` and `$PORTAGE_TMPDIR` with `os.MkdirAll`, then exactly **one**
invocation:

```
FEATURES="-sandbox -usersandbox -network-sandbox -ipc-sandbox -pid-sandbox"
PORTAGE_TMPDIR=<resolved portage tmpdir>
DISTDIR=<resolved distdir>
GENTOO_MIRRORS=
ebuild <path> <phase...>
```

The env must be applied on the **runner** path, not only on a direct `exec.Command`
path.

**`PORTAGE_TMPDIR` must be overridable.** The script hardcodes `/tmp/portage-test`
(L797). Hardcoding it here makes `TestTestLocal_CreatesDirs` do a real
`MkdirAll("/tmp/portage-test")` on the developer's machine and collide between parallel
`go test` runs. Add a config key, resolved exactly like the others:

```
portage tmpdir : --portage-tmpdir → $LARRY_PORTAGE_TMPDIR → config file
                 `portage_tmpdir` → /tmp/portage-test
```

as `config.ResolvePortageTmpdir(...)`, mirroring plan 13a's `ResolveDistdir`. Tests set
it to a `t.TempDir()` subpath.

### `CmdRunner` gains `RunWithEnv`

```go
type CmdRunner interface {
    Run(name string, args ...string) error
    RunWithEnv(env []string, name string, args ...string) error
}
```

**There are two implementations in package `test`, and widening the interface breaks
both:** `runner` (`internal/test/local.go:18-29`) and `realRunner`
(`internal/test/container.go:250-257`). `internal/test/container.go` is otherwise out of
scope for this plan — **add `RunWithEnv` to `realRunner` as a mechanical copy of its
`Run`, with `cmd.Env = env` inserted, and change nothing else in that file:**

```go
func (r *realRunner) RunWithEnv(env []string, name string, args ...string) error {
    cmd := exec.Command(name, args...)
    cmd.Env = env
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}
```

### Banner and summary — exact format (script L785–787, L791–792, L810–813)

Via `log.Msg` / `log.Error` (plan 13b owns the `>>> ` / `!!! ` prefixes; the strings
below are what you pass *after* the prefix):

```
Package: <category>/<pn>-<pvr>
Ebuild:  <absolute ebuild path>
Phases:  <phases joined by a single space>
```

(note the **two** spaces after `Ebuild:` and `Phases:`, one after `Package:` —
column-aligned, copied verbatim from the script), then a blank line.

On `--pretend`, after the banner, `log.Warn("Pretend mode: would run:")` then a plain
line `  ebuild <path> <phases…>` (two leading spaces) and return nil.

After the run, a blank line, then on success:

```
Success: <category>/<pn>-<pvr> (<phases joined by a single space>)
```

and on failure, two `log.Error` lines:

```
Failed: <category>/<pn>-<pvr> (<phases joined by a single space>)
Exit code: <N>
```

**Exit code** propagates by wrapping the runner's error in plan 13a's `ExitError`.

---

## Tests (write first — every one must fail before you implement)

### `internal/test/local_test.go`

| Test | Exact input | Exact expected output |
|---|---|---|
| `TestBuildPhaseList_Default` | no flags | `["clean","fetch","unpack"]` (**D4**) |
| `TestBuildPhaseList_Compile` | compile=true | `["clean","fetch","unpack","compile"]` (**D3**) |
| `TestBuildPhaseList_Merge` | merge=true | `["clean","fetch","unpack","compile","install"]`; additionally assert `slices.Contains(got,"qmerge") == false` (**D2**) |
| `TestBuildPhaseList_MergeBeatsCompile` | compile=true, merge=true | `["clean","fetch","unpack","compile","install"]` |
| `TestBuildPhaseList_ExplicitPhasesOverride` | compile=true, explicit=`["fetch","unpack"]` | `["fetch","unpack"]` |
| `TestBuildPhaseList_ExtraPhasesAppend` | compile=true, extra=`["install","test"]` | `["clean","fetch","unpack","compile","install","test"]` |
| `TestValidPhases_MatchesScript` | — | the key set of `validPhases` equals exactly the 14-element list above; assert both directions (no missing, no extra). (**D6**) |
| `TestTestLocal_SingleInvocation` | stub runner; phases `["clean","fetch","unpack"]` | exactly **one** recorded call; `name == "ebuild"`; `args == []string{<path>,"clean","fetch","unpack"}` (**D7**) |
| `TestTestLocal_SetsSandboxEnv` | same | the recorded env contains, as whole entries: `FEATURES=-sandbox -usersandbox -network-sandbox -ipc-sandbox -pid-sandbox`, `PORTAGE_TMPDIR=<the tmpdir passed in>`, `DISTDIR=<the distdir passed in>`, and `GENTOO_MIRRORS=` (empty value, entry present). **Regression test for D5.** |
| `TestTestLocal_CreatesDirs` | `distdir = filepath.Join(t.TempDir(),"d")`, `portageTmpdir = filepath.Join(t.TempDir(),"pt")` — **neither exists beforehand** | both are directories afterwards. **Never touch `/tmp/portage-test` from a test.** |
| `TestTestLocal_PropagatesExitCode` | stub runner returns an error wrapping exit status 7 | the returned error unwraps to an `*ExitError` with `Code == 7` (**D8**) |
| `TestTestLocal_RelativePath` | `os.Chdir(t.TempDir())`-free: pass `"foo/foo-1.0.ebuild"` relative to a fixture created under the test's working dir | the recorded `args[0]` is the `filepath.Abs` of that path (**D11**) |
| `TestTestLocal_Banner` | stub runner, pretend=false, phases `["clean","fetch"]`, package `dev-util/javy-9.1.0` | the captured `log.Msg` (stdout) stream contains these three lines in order and verbatim: `Package: dev-util/javy-9.1.0`, `Ebuild:  <abs path>`, `Phases:  clean fetch` (**D8**) |
| `TestTestLocal_SuccessSummary` | same, runner returns nil | the captured stream contains verbatim `Success: dev-util/javy-9.1.0 (clean fetch)` (**D8**) |
| `TestTestLocal_FailureSummary` | same, runner returns exit 7 | the captured **stderr** stream contains verbatim `Failed: dev-util/javy-9.1.0 (clean fetch)` and `Exit code: 7` (**D8**) |
| `TestTestLocal_PretendPrintsCommand` | pretend=true | the stub runner records **zero** calls; the captured output contains `Pretend mode: would run:` and the line `  ebuild <abs path> clean fetch` |

Delete `TestBuildPhaseList_CustomPhases` (`internal/test/local_test.go:120-129`) — it
calls `buildPhaseList`, throws the result away, reassigns `phases`, and asserts on its
own literal. That is the `ineffassign` finding at `local_test.go:121` and it tests
nothing.

### `cmd/larry/test_test.go` (uses plan 13a's `execCmd` + `fixtureOverlay`)

| Test | Exact input | Exact expected output |
|---|---|---|
| `TestTestCmd_CompileFlagReachesRunner` | `execCmd(t, "test", "--compile", "dev-util/javy")` against a fixture overlay holding `dev-util/javy/javy-9.1.0.ebuild` | the injected runner sees `ebuild <overlay>/dev-util/javy/javy-9.1.0.ebuild clean fetch unpack compile`. **Regression test for D1.** |
| `TestTestCmd_MergeFlagReachesRunner` | `--merge` | `… clean fetch unpack compile install`, and no `qmerge` argument |
| `TestTestCmd_PhaseFlagReachesRunner` | `--phase fetch --phase unpack` | `… fetch unpack` (exactly two phases) |
| `TestTestCmd_ShortFlags` | `-c`, then `-m`, then `-p fetch` in three sub-runs | identical to their long forms (**D9**) |
| `TestTestCmd_BarePhaseFlag` | `--prepare dev-util/javy` | accepted; runner sees `… clean fetch unpack prepare` (**D10**) |
| `TestTestCmd_UnknownFlagMessage` | `--bogus dev-util/javy` | returns a non-nil error whose `Error()` is exactly `Unknown option or phase: --bogus` (script L750), and the runner records **zero** calls (**D10**) |
| `TestTestCmd_VersionedAtom` | `test --pretend dev-util/javy-8.1.1` against a fixture overlay holding both `javy-8.1.1.ebuild` and `javy-9.1.0.ebuild` | resolves to `javy-8.1.1.ebuild`, not `javy-9.1.0.ebuild` (**D12**) |
| `TestTestCmd_PicksHighestVersion` | `test --pretend dev-util/javy` against a fixture holding `javy-9.0.ebuild`, `javy-10.0.ebuild` and `javy-9999.ebuild` | resolves to `javy-10.0.ebuild` (**D12**) |
| `TestTestCmd_Pretend` | `test --pretend --compile dev-util/javy` | the printed command is `  ebuild <path> clean fetch unpack compile`; the runner records zero calls |

---

## Implementation steps

Every step has a test above. Write the test red first.

1. **Fix the ebuild picker.** Replace `cmd/larry/test.go:74-107` **wholesale** with
   `internal/atom`:

   ```go
   var ebuildPath string
   if filepath.IsAbs(input) || strings.HasSuffix(input, ".ebuild") {
       ebuildPath, err = filepath.Abs(input)
   } else {
       a, err := atom.ParseAtom(overlayDir, input)
       if err != nil { return err }
       pe, err := atom.FindForAtom(a)
       if err != nil { return err }
       ebuildPath = pe.Path
   }
   ```

   Delete the inline `os.ReadDir` block and the `ebuilds[len(ebuilds)-1]` pick at
   `:106`. Do the same at `internal/test/local.go:92-121`
   (`resolveEbuildFromCatPkg`) — or delete that function if nothing calls it after this
   change, and say so in your report.
   Covers: `TestTestCmd_VersionedAtom`, `_PicksHighestVersion`. (**D12**)
2. **Phase lists and the valid set.** Rewrite `buildPhaseList` to
   `buildPhaseList(compile, merge bool, explicit, extra []string) []string` implementing
   the precedence rules above; rewrite `validPhases` to the 14-element list.
   Covers: all seven `TestBuildPhaseList_*` plus `TestValidPhases_MatchesScript`.
   (**D2, D3, D4, D6**)
3. **Pass the phases through.** `cmd/larry/test.go` computes the phase list and hands it
   to `TestLocal`. Delete the `nil` literal at `cmd/larry/test.go:125`.
   Covers: `TestTestCmd_CompileFlagReachesRunner`, `_MergeFlagReachesRunner`,
   `_PhaseFlagReachesRunner`. (**D1**)
4. **Env on the runner path.** Add `RunWithEnv` to `CmdRunner` and to **both**
   implementations (`runner` in `local.go`, `realRunner` in `container.go` — mechanical
   copy only). Build the five-variable env in one place in `TestLocal`,
   `os.MkdirAll` the distdir and the portage tmpdir, and add
   `config.ResolvePortageTmpdir`. Delete `setNetworkFeatures` (`local.go:128-138`) and
   `TestLocalConfig` (`local.go:175-180`) and their tests.
   Covers: `TestTestLocal_SetsSandboxEnv`, `_CreatesDirs`. (**D5**)
5. **One invocation.** Collapse the per-phase loop (`local.go:211-215`) into a single
   `RunWithEnv(env, "ebuild", append([]string{path}, phases...)...)`. Fold `runPhase`'s
   pretend branch up into `TestLocal`.
   Covers: `TestTestLocal_SingleInvocation`, `_PretendPrintsCommand`. (**D7**)

   **Checkpoint — `go test ./internal/test/ ./cmd/... -count=1` must be green here.**
   If budget is short, stop and hand back. Steps 6–8 are a clean second session.

6. **Banner, summary, exit code.** Emit the exact strings from "Target behaviour"; wrap
   the runner error in `ExitError`.
   Covers: `TestTestLocal_Banner`, `_SuccessSummary`, `_FailureSummary`,
   `_PropagatesExitCode`. (**D8**)
7. **Short flags and bare `--<phase>`.** (**D9, D10**)
   Register `-c`/`-m`/`-p` with `BoolVarP`/`StringArrayVarP`.

   For the bare phase flags, **do not rely on `FParseErrWhitelist`** — it only tells
   cobra to tolerate unknown flags, it does not hand you their names or values. Instead
   do a pre-parse pass. Put it in `testCmd`'s `Args` function (it runs before `RunE` and
   receives the already-split args) or, more reliably, in `PersistentPreRunE`/`PreRunE`
   over a copy of `os.Args`:

   ```go
   // splitPhaseFlags removes every "--<name>" token whose <name> is in validPhases,
   // returning them in order plus the remaining args for cobra.
   func splitPhaseFlags(args []string) (extraPhases, rest []string)
   ```

   Rules:
   - a token is a candidate only if it starts with exactly `--` and has no `=`;
   - if the suffix is in `validPhases`, append it to `extraPhases` and drop it;
   - otherwise keep it in `rest`;
   - after cobra has parsed `rest`, if any argument that still begins with `--` was not
     consumed, return `fmt.Errorf("Unknown option or phase: %s", tok)` for the **first**
     such token. (`ST1005` says error strings should not be capitalised; the script's
     wording is a user-facing message, so emit it via `log.Error` and return a plain
     `ExitError{Code: 1}` — that keeps both the linter and the wording happy.)

   Wire `extraPhases` into `buildPhaseList`'s `extra` parameter.
   Covers: `TestTestCmd_ShortFlags`, `_BarePhaseFlag`, `_UnknownFlagMessage`.
8. **Relative paths.** `filepath.Abs` in `resolveEbuild` (`local.go:81-89`).
   Covers: `TestTestLocal_RelativePath`. (**D11**)
9. Hand back listing every function and test you deleted.

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

Also Tier 1 — the real proof, with a stub runner and no builds:

```bash
go test ./internal/test/ ./cmd/larry/ -count=1 -v
```

`TestTestCmd_CompileFlagReachesRunner` and `TestTestLocal_SetsSandboxEnv` prove the two
headline defects. The `qmerge` regression is provable by assertion, not by building
anything.

**Tier 2 — read-only, safe to run:**

```bash
grep -n 'qmerge' internal/test/local.go
# expect: no output at all — it is neither a valid phase nor part of --merge

grep -n 'ebuilds\[len(ebuilds)-1\]' cmd/larry/test.go internal/test/local.go
# expect: no output

grep -rn '/tmp/portage-test' --include='*_test.go' ./cmd ./internal
# expect: no output — tests must use t.TempDir()
```

```bash
# Tier 2: --pretend only prints; it fetches, unpacks and builds nothing.
cd ~/Developer/gentoo-overlay
larry test --pretend --compile dev-util/javy
# expect: "  ebuild <path> clean fetch unpack compile"
larry test --pretend --merge dev-util/javy
# expect: "  ... clean fetch unpack compile install"   (NO qmerge)
larry test --pretend --compile --install dev-util/javy
# expect: "  ... clean fetch unpack compile install"
larry test --pretend --phase fetch --compile dev-util/javy
# expect: "  ... fetch"                                 (--phase wins outright)
larry test --bogus dev-util/javy
# expect: "Unknown option or phase: --bogus", exit 1, nothing run
larry test --pretend dev-util/javy-8.1.1
# expect: resolves the 8.1.1 ebuild, not the newest
```

**Tier 3 — DO NOT RUN. Report verbatim for a human.**

```bash
# Tier 3 — DO NOT RUN. Report verbatim.
# These compile real packages and write into the live Portage tmpdir.
larry test --compile dev-util/javy        # actually fetches, unpacks and compiles
echo $status                              # must be ebuild's own exit code, not 1
larry test --merge dev-util/javy          # must NOT install to the live system:
                                          # verify with `qlist -I dev-util/javy` after
larry test --prepare dev-util/javy        # bare phase flag accepted
```

## Out of scope

Container mode (plans 27a/27b) — the only permitted change to `internal/test/container.go` is
adding `RunWithEnv` to `realRunner`. Do not fix unrelated bugs you notice — report them.
