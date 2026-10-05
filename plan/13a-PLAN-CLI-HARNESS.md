# 13a — CLI harness, exit codes, persistent flags, config wiring

**Why:** `cmd/larry` has **0 test functions across 7 source files**. As a direct result:
the config system is never loaded, every global flag is registered non-persistently (so
`larry upstream --distdir /tmp` fails with "unknown flag"), the bare command prints a
debug string, `larry bump` panics on zero args, and every subprocess exit code is
flattened to 1. This plan builds the test harness first, then fixes the plumbing it
exposes.

**Precondition:** none — this is the start of the CLI branch. Plan **13b** depends on it.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent, this plan's text is
authoritative — do not guess at the script's behaviour.**
Functions: globals (L23–35), `print_usage` (L49–67), main dispatch (L1741–1859).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting.

---

## Precondition check (run first)

This plan has no plan-level precondition, but it edits specific symbols. Confirm they
exist with exactly these signatures before you write a line of code:

```bash
go doc bennypowers.dev/larry/internal/config Load
# expect: func Load() (*Config, error)        <-- NOTE: NO ARGUMENTS

go doc bennypowers.dev/larry/internal/config FilePath
# expect: var FilePath string

go doc bennypowers.dev/larry/internal/config Config.ResolveOverlay
# expect: func (c *Config) ResolveOverlay(override string) string

go doc bennypowers.dev/larry/internal/config ResolveOverlay
# expect: func ResolveOverlay(override string) string

go doc bennypowers.dev/larry/internal/git ResolveOverlayDir
# expect: func ResolveOverlayDir() (string, error)

go doc bennypowers.dev/larry/cmd/larry Execute
# expect: func Execute() error
```

```bash
go doc bennypowers.dev/larry/internal/config ResolveDistdir
go doc bennypowers.dev/larry/internal/config Config.ResolveCacheDir
# expect: BOTH to fail with "no symbol". These do not exist. You are CREATING them,
# not fixing them. If they DO exist, someone has already done part of this plan —
# STOP and report.
```

If any of the first six differs from what is printed above, the repo has moved out from
under this plan — **STOP and report**, do not improvise a different signature.

---

## Defects being fixed (13a)

| ID | Location | Defect | Verified |
|----|----------|--------|----------|
| D1 | `cmd/larry/root.go:61-80` | all ten global flags use `rootCmd.Flags()`, not `PersistentFlags()` | `larry upstream --distdir /tmp` → `Error: unknown flag: --distdir` |
| D2 | `cmd/larry/root.go:42` | `config.DefaultConfig()` is used; `config.Load()` has **zero** non-test callers | `LARRY_OVERLAY=/tmp/x larry test …` ignores the var |
| D3 | `cmd/larry/root.go:31` | `fmt.Println("larry: hello")` on the bare command | prints on every bare `larry` |
| D4 | `cmd/larry/root.go:30-33` | bare `larry` exits 0; script exits 1 | — |
| D5 | `internal/config/config.go:188-190` | package-level `ResolveOverlay` builds a fresh `DefaultConfig()` whose `Overlay` is always `""`, making the `c.Overlay` branch at `:173-175` unreachable from every caller | all 7 call sites use the package-level form |
| D6 | `internal/config/config.go:169-180` | overlay falls back to `/var/db/repos/gentoo` — a write-capable tool silently targeting the main tree | `:179` |
| D7 | three call sites | DISTDIR resolved three different ways: `cmd/larry/prepare.go:82-88` (→ `""`), `cmd/larry/bump.go:154-164` (→ `~/.cache/larry/distfiles`), `cmd/larry/test.go:113` (`Distdir: testDistdir` → unresolved, so no container mount) | — |
| D10 | `cmd/larry/main.go:8-13` | always `os.Exit(1)`; subprocess exit codes lost | — |
| D11 | `cmd/larry/bump.go:190` | `args[0]` under `cobra.MaximumNArgs(1)` (`bump.go:62`) → index-out-of-range panic on zero args | — |

Also fixed here because it is the same edit as D1: `--overlay` is declared
**per-subcommand** at `bump.go:76`, `test.go:34`, `upstream.go:37`, `prepare.go:37`
(`import.go` has none at all), and `--remote` is declared **twice** —
`root.go:77` and `upstream.go:35`.

---

## Target behaviour

**Resolution order, implemented once in `internal/config`:**

```
overlay : --overlay flag → $LARRY_OVERLAY → config file `overlay`
          → git toplevel of CWD → ERROR
distdir : --distdir → $LARRY_DISTDIR → config file `distdir`
          → $PORTAGE_DISTDIR → <cacheDir>/distfiles
cache   : --cache-dir → $LARRY_CACHE_DIR → config file `cache_dir`
          → defaultCacheDir()
```

No command takes an overlay as a positional argument. `bump` and `upstream` take an
optional `<category/package>`; `test` and `import` take a required `<category/package>`.
Do not add a positional overlay path.

`<cacheDir>` is `config.defaultCacheDir()` (`config.go:192-201`), which is
`$XDG_CACHE_HOME/larry` when `XDG_CACHE_HOME` is set and
`$HOME/.cache/larry` otherwise. **So with `XDG_CACHE_HOME` unset the distdir chain ends
at `~/.cache/larry/distfiles`.** `ResolveDistdir` must never return `""` (D7).

Overlay resolution **returns an error** instead of defaulting to `/var/db/repos/gentoo`
(D6). The error text must name all three ways to set it: `--overlay`, `LARRY_OVERLAY`,
and the `overlay` config key.

### Precedence mechanics (follow this exactly)

`config.Load()` applies *file* then *env*, so env already beats file. Flags must beat
both, but the current code binds flags directly into the `*Config` that `Load()` would
later overwrite. The fix:

1. Bind every persistent flag to a **separate** `rootFlags` struct, not to `cfg`.
2. In `rootCmd.PersistentPreRunE`, call `cfg, err = config.Load()` — **with no
   arguments**; it reads the package var `config.FilePath`, which `--config` is already
   bound to at `root.go:79`.
3. Then copy each flag value into `cfg` **only if** `cmd.Flags().Changed(<name>)`.
   This is safe inside `PersistentPreRunE`: cobra's `ParseFlags` has already run
   `mergePersistentFlags`, and the merged `*pflag.Flag` is the same pointer the root
   owns, so `Changed` is visible from the subcommand.

Result: flag > env > config file > default.

---

## Tests (write first — every one must fail before you implement)

### Step 0 deliverable: the `cmd/larry` test harness

Plans 13b, 16, 21, 26 and 28 all prescribe `cmd/larry/*_test.go` files. Build the
harness here, before anything else.

`cmd/larry/testhelp_test.go`:

```go
// execCmd builds a FRESH root command tree (it must not reuse package-level
// cobra state), runs it with args, and returns stdout, stderr and the error.
// Implementation note: extract the current body of Execute() into
//   func newRootCmd() *cobra.Command
// so each call gets its own commands and its own flag values. execCmd then does
//   root := newRootCmd()
//   root.SetOut(&outBuf); root.SetErr(&errBuf); root.SetArgs(args)
//   err := root.Execute()
func execCmd(t *testing.T, args ...string) (stdout, stderr string, err error)

// fixtureOverlay creates a temp overlay containing profiles/repo_name, an
// initialised git repo with the named remotes, and the supplied ebuilds
// (key = "category/pkg/pkg-1.0.ebuild", value = file content).
// remotes maps remote name -> URL. Returns the overlay path.
func fixtureOverlay(t *testing.T, remotes map[string]string, ebuilds map[string]string) string
```

Requirements that make the harness reusable:
- **No `os.Exit` anywhere in the command path.** `main.go` owns the exit; commands
  return errors. (This is D10 — the harness cannot exist without it.)
- Cobra output goes through `SetOut`/`SetErr`.
- `t.Setenv` for every `LARRY_*` var; no global mutation that survives the test.
- `fixtureOverlay` uses `go-git` (`git.PlainInit` + `repo.CreateRemote`) per AGENTS.md —
  **no `git` CLI subprocess**.

### `cmd/larry/exit_test.go` (new)

- `TestExitError_Error` — `(&ExitError{Code: 7, Err: errors.New("boom")}).Error()`
  returns a string containing `boom`; `errors.Unwrap` of it returns the inner error.
- `TestExitCodeFor_ExitError` — `exitCodeFor(&ExitError{Code: 7})` == `7`.
- `TestExitCodeFor_PlainError` — `exitCodeFor(errors.New("x"))` == `1`.
- `TestExitCodeFor_Nil` — `exitCodeFor(nil)` == `0`.
- `TestExitCodeFor_WrappedExitError` —
  `exitCodeFor(fmt.Errorf("prepare failed: %w", &ExitError{Code: 3}))` == `3`
  (so `errors.As`, not a type assertion).

### `cmd/larry/root_test.go` (new)

- `TestPersistentFlagsRegisteredOnRoot` — table over the exact names
  `verbose, quiet, color, dry-run, cache-dir, distdir, jobs, github-token, remote,
  config, overlay`; assert `rootCmd.PersistentFlags().Lookup(name) != nil` and
  `rootCmd.Flags().Lookup(name) == nil`. (**D1**)
- `TestPersistentFlagsVisibleOnSubcommands` — table over every subcommand
  (`bump, import, prepare, test, upstream`) × the same flag names. For each subcommand
  `c`, **call `c.InheritedFlags()` first**, then assert
  `c.Flags().Lookup(name) != nil`.
  **Why: cobra v1.10.2's `(*Command).Flags()` does NOT merge parent persistent flags on
  its own** (`command.go:1688-1698` just lazily allocates the set). `InheritedFlags()`
  (`command.go:1744`) calls `mergePersistentFlags()` (`command.go:1898-1902`), which is
  what actually adds the parents' pflags. Asserting `c.Flags().Lookup(...)` without that
  call passes vacuously today and would not catch the bug.
- `TestNoDuplicateOverlayFlag` — for each subcommand, `c.LocalFlags().Lookup("overlay")`
  is nil, i.e. the per-subcommand declarations at `bump.go:76`, `test.go:34`,
  `upstream.go:37`, `prepare.go:37` are gone. Same assertion for `distdir` and for
  `remote` (`upstream.go:35`).
- `TestGlobalFlagAcceptedEndToEnd` — the real proof of D1:
  `execCmd(t, "upstream", "--distdir", "/tmp", "--overlay", fixtureOverlay(...))`
  returns an error that does **not** contain `unknown flag`. (A nil error is also fine.)
- `TestConfigLoadIsCalled` — write `overlay: <fixture>` into a temp
  `config.yaml`, pass `--config <that file>`, run a subcommand with no `--overlay` and
  no `LARRY_OVERLAY`, and assert the command acted on the fixture path (e.g. the
  resolved overlay appears in output, or the command fails naming that path rather than
  `/var/db/repos/gentoo`). Asserting that `config.Load` *exists* is not enough — the
  defect is that nothing calls it. (**D2**)
- `TestEnvOverrideReachesCommand` — `t.Setenv("LARRY_OVERLAY", fixture)`; the command
  uses it. (**D2**)
- `TestFlagBeatsEnvBeatsFile` — config file says `/tmp/from-file`, `LARRY_OVERLAY` says
  `/tmp/from-env`, `--overlay /tmp/from-flag`. Assert the command resolves
  `/tmp/from-flag`. Then drop the flag → `/tmp/from-env`. Then unset the env →
  `/tmp/from-file`.
- `TestRootNoArgsExitsNonZero` — `execCmd(t)` returns a non-nil error whose
  `exitCodeFor` is `1`. (**D4**)
- `TestRootDoesNotPrintHello` — `execCmd(t)`'s stdout+stderr contain no `hello`.
  (**D3**)
- `TestBumpNoArgsNoPanic` — `execCmd(t, "bump")` returns an error and does not panic.
  (**D11**)

### `internal/config/config_test.go` (extend)

- `TestResolveOverlay_UsesConfigValue` — `(&Config{Overlay: "/tmp/x"}).ResolveOverlay("")`
  returns `/tmp/x`, nil. (**D5**)
- `TestResolveOverlay_OverrideWins` —
  `(&Config{Overlay: "/tmp/x"}).ResolveOverlay("/tmp/y")` returns `/tmp/y`, nil.
- `TestResolveOverlay_NoOverlayIsError` — `t.Chdir(t.TempDir())` (a dir with no git
  repo above it is not guaranteed on every machine; if the temp dir turns out to be
  inside a repo, `t.Skip`), `(&Config{}).ResolveOverlay("")` returns a non-nil error
  whose message contains all three of `--overlay`, `LARRY_OVERLAY`, and `overlay`, and
  an empty string. (**D6**)
- `TestResolveOverlay_NeverReturnsPortageTree` — no code path returns
  `/var/db/repos/gentoo`; grep-equivalent assertion: the error case above must not
  return that path. (**D6**)
- `TestResolveDistdir_Precedence` — table, each row clearing the others with
  `t.Setenv(..., "")`:

  | override | `LARRY_DISTDIR` | `cfg.Distdir` | `PORTAGE_DISTDIR` | `XDG_CACHE_HOME` | want |
  |---|---|---|---|---|---|
  | `/a` | `/b` | `/c` | `/d` | `/x` | `/a` |
  | `""` | `/b` | `/c` | `/d` | `/x` | `/b` |
  | `""` | `""` | `/c` | `/d` | `/x` | `/c` |
  | `""` | `""` | `""` | `/d` | `/x` | `/d` |
  | `""` | `""` | `""` | `""` | `/x` | `/x/larry/distfiles` |

  Note rows 2 and 3 cannot both be exercised through `Load()` (env overwrites
  `cfg.Distdir` there); drive `ResolveDistdir` directly with a hand-built `*Config`.
  (**D7**)
- `TestResolveDistdir_NeverEmpty` — with `LARRY_DISTDIR`, `PORTAGE_DISTDIR` and
  `XDG_CACHE_HOME` all unset and `cfg.Distdir == ""`, the result is non-empty and ends
  in `/.cache/larry/distfiles`. (**D7**)
- `TestResolveCacheDir_Precedence` — override > `LARRY_CACHE_DIR` > `cfg.CacheDir` >
  `defaultCacheDir()`.
- `TestLoad_MaintainerFromFileAndEnv` — **this is the test the old plan was missing.**
  Write a temp `config.yaml` containing:
  ```yaml
  maintainer:
    name: File Person
    email: file@example.com
  ```
  Set `config.FilePath` to it (`t.Cleanup` to restore), call `Load()`, and assert
  `cfg.MaintainerName == "File Person"` and `cfg.MaintainerEmail == "file@example.com"`.
  Then `t.Setenv("LARRY_MAINTAINER_NAME", "Env Person")` and
  `t.Setenv("LARRY_MAINTAINER_EMAIL", "env@example.com")`, call `Load()` again, and
  assert both now read `Env Person` / `env@example.com`.

### `internal/config/config_distdir_test.go` is not needed

Keep all of the above in the existing `internal/config/config_test.go`.

---

## Implementation steps

> Ordering note: the harness depends on `ExitError` and on `newRootCmd()`, so those come
> **first**. (The previous revision of this plan called the harness the "first
> deliverable" but numbered `ExitError` step 7 — that ordering does not compile.)

**Step 0 — `ExitError` + `main.go` (D10).**
In a new `cmd/larry/exit.go`:
```go
type ExitError struct {
    Code int
    Err  error
}
func (e *ExitError) Error() string { ... }   // include e.Err's text
func (e *ExitError) Unwrap() error { return e.Err }

// exitCodeFor maps an error to a process exit code.
// nil -> 0; *ExitError (found with errors.As, so wrapping works) -> its Code;
// anything else -> 1.
func exitCodeFor(err error) int
```
Rewrite `main.go` to:
```go
func main() {
    err := Execute()
    if err != nil {
        fmt.Fprintf(os.Stderr, "larry: %v\n", err)
    }
    os.Exit(exitCodeFor(err))
}
```
No other file in `cmd/larry` may call `os.Exit`.

**Step 1 — `newRootCmd()` + persistent flags (D1).**
Extract the body of `Execute()` (`root.go:38-89`) into
`func newRootCmd() *cobra.Command`, which builds the root, registers subcommands, and
declares **all** global flags on `rootCmd.PersistentFlags()`. `Execute()` becomes
`return newRootCmd().Execute()`.
- Flag set: `verbose` (use `CountVarP` — `bump.go:82` already uses `CountVarP` while
  `root.go:61` uses `IntVarP`; settle on `CountVarP`), `quiet`, `color`, `dry-run`,
  `cache-dir`, `distdir`, `jobs`, `github-token`, `remote`, `config`, and **add
  `overlay`**, which does not exist on root today.
- **Delete** the per-subcommand `--overlay` declarations at `bump.go:76-77`,
  `test.go:34-35`, `upstream.go:37-38`, `prepare.go:37-38`; the `--distdir`
  declarations at `bump.go:78-79`, `test.go:36`, `prepare.go:35-36`; the duplicate
  `--remote` at `upstream.go:35`; and the duplicate `--verbose` at `bump.go:82` and
  `upstream.go:33`. Repoint their package vars at the new persistent-flag struct.
- Because the subcommands' `init()` functions run at package load against the package
  global `rootCmd`, `newRootCmd()` must either rebuild the whole tree or reset flag
  values. Simplest correct approach: keep the `var rootCmd` global for `init()`
  registration, and have `newRootCmd()` re-declare the persistent flags onto a fresh
  `pflag.FlagSet` each call — i.e. move flag declaration out of `init()` and into
  `newRootCmd()`, and have `newRootCmd()` reset the backing `rootFlags` struct to its
  zero/default values before declaring. Verify with
  `TestFlagBeatsEnvBeatsFile`, which runs `execCmd` three times in one process.

**Step 2 — harness (`cmd/larry/testhelp_test.go`).** Write `execCmd` and
`fixtureOverlay` per the spec above. **Checkpoint: `go test ./cmd/... -count=1` must
compile and the harness's own smoke test must pass before you continue.** If budget is
short, stop here and hand back; steps 3–7 are a clean second session.

**Step 3 — `config.Load()` wiring (D2).**
Add `PersistentPreRunE` to the root:
```go
PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
    loaded, err := config.Load()          // NO ARGUMENTS. Reads config.FilePath.
    if err != nil { return err }
    applyFlagOverrides(cmd, loaded)       // only for cmd.Flags().Changed(name)
    cfg = loaded
    // re-init color + logger from the final cfg
    ...
    return nil
},
```
Delete `cfg = config.DefaultConfig()` from `Execute()` (`root.go:42`).
`config.DefaultConfig()` stays in `internal/config` — it is what `Load()` starts from.

**Step 4 — Resolve\* in `internal/config` (D5, D6, D7).**
- Change `(*Config).ResolveOverlay(override string) string` →
  `(*Config).ResolveOverlay(override string) (string, error)`. Delete the
  `/var/db/repos/gentoo` fallback at `config.go:179`; return an error instead.
- **Delete** the package-level `ResolveOverlay` (`config.go:188-190`) and its doc
  comment (`config.go:182-187`). Update all seven call sites to go through the loaded
  `cfg`: `cmd/larry/test.go:72`, `cmd/larry/import.go:56`, `cmd/larry/import.go:57`,
  `cmd/larry/prepare.go:91`, `cmd/larry/bump.go:96`, `cmd/larry/bump.go:99`,
  `cmd/larry/upstream.go:76`.
- **Create** (they do not exist):
  ```go
  func (c *Config) ResolveDistdir(override string) string
  func (c *Config) ResolveCacheDir(override string) string
  ```
  implementing the chains in "Target behaviour". `ResolveDistdir` ends at
  `filepath.Join(c.ResolveCacheDir(""), "distfiles")` so the `XDG_CACHE_HOME`-unset
  fallback is `~/.cache/larry/distfiles` via the existing `defaultCacheDir()`
  (`config.go:192`).
- Replace the three rival DISTDIR chains: `cmd/larry/prepare.go:82-88`,
  `cmd/larry/bump.go:154-164`, and `cmd/larry/test.go:113` (which currently passes the
  raw `testDistdir`). All three call `cfg.ResolveDistdir(<flag value>)`.

**Step 5 — maintainer config.**
Add **flat** fields to `config.Config` (plan 28 consumes them as flat fields, not as a
nested struct):
```go
MaintainerName  string
MaintainerEmail string
```
Read them in `applyConfig` from the viper keys `maintainer.name` and `maintainer.email`
(viper flattens nested YAML with `.`), and in `applyEnvOverrides` from
`LARRY_MAINTAINER_NAME` / `LARRY_MAINTAINER_EMAIL`. Covered by
`TestLoad_MaintainerFromFileAndEnv`.

**Step 6 — bare root (D3, D4).**
Delete `fmt.Println("larry: hello")` (`root.go:31`). The root's `RunE` prints usage
(`cmd.Help()`) and then returns `&ExitError{Code: 1, Err: errors.New("no subcommand")}`.
Set `SilenceUsage: true` and `SilenceErrors: true` on the root so `main.go` is the only
thing that prints the error.

**Step 7 — `bump` zero-arg guard (D11).**
`cmd/larry/bump.go:190` dereferences `args[0]` unconditionally under
`cobra.MaximumNArgs(1)` (`bump.go:62`). Guard it: build the commit message from
`result.PackageName` when `len(args) == 0`, or return a clear error. Either is
acceptable; the test only requires "error, no panic".

**Step 8 — run the suites, fix fallout, hand back.** List every flag declaration,
function and test you deleted.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**.

**Tier 1 — automated, run these. The plan is not done until all are green.**
```bash
go build ./...
go vet ./...
go test ./cmd/... ./internal/config/... -count=1
go test ./... -count=1        # no NEW failures vs. your recorded baseline
golangci-lint run ./...
```
**Known-red baseline at HEAD (do not chase):**
`TestExtractVar/multi_line` in `internal/prepare` (plan 20/22 owns it) and one
`go vet` finding at `internal/git/git.go:318` (self-assignment; plan 29 owns it).
If your measured baseline differs, record it and continue.

**Tier 1 — source-level greps (run these; both must print nothing):**
```bash
grep -rn 'config.DefaultConfig()' cmd/
# expect: no output

grep -n 'rootCmd.Flags()' cmd/larry/root.go
# expect: no output
```

**Tier 2 — read-only manual. Needs `~/Developer/gentoo-overlay`. Safe to run; skip and
say so if the overlay is absent.**
```bash
larry upstream --distdir /tmp --overlay ~/Developer/gentoo-overlay
# expect: runs; no "unknown flag"

larry
# expect: usage text, no "hello", exit status 1 (check with `echo $status` in fish)

cd /tmp && LARRY_OVERLAY=~/Developer/gentoo-overlay larry upstream
# expect: scans the reference overlay

cd /tmp && larry upstream
# expect: an error naming --overlay / LARRY_OVERLAY / the overlay config key.
# It must NOT scan /var/db/repos/gentoo.
```

**Tier 3 — MUTATING. DO NOT RUN. Report these verbatim for the human.**
```bash
# Proves exit-code propagation end to end: requires a real `ebuild` phase against
# the reference overlay, which writes into it.
larry test dev-util/javy ; echo "exit=$status"
# expect: the failing phase's exit code, not a flat 1.
```

---

## Out of scope

- `internal/output` rewrite, replacing bare `fmt.Printf` in the subcommands,
  `ResolveGitHubRepo`, and the `--remote` flag → **plan 13b**.
- Per-command behaviour fixes (plans 21–28). Only the plumbing changes here.
- The `go vet` self-assignment at `internal/git/git.go:318` (plan 29).

If you find an unrelated bug, write it in your final report; do not fix it.
