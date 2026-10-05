# 13b — Output layer, `--quiet`/`--color`, GitHub-repo resolution

**Why:** Every subcommand writes with bare `fmt.Printf`, so `--quiet` and `--color` do
nothing, warnings and errors land on stdout, and none of the script's `>>>` / `***` /
`!!!` prefixes appear. Separately, `resolveGitHubRepo` text-parses `.git/config`,
**lowercases every line** (so `BennyPowers/Overlay` becomes `bennypowers/overlay` and
every `gh` call 404s), ignores `--remote`, and is never reached from the bump→prepare
path.

**Precondition:** **plan 13a** (`13a-PLAN-CLI-HARNESS.md`). 13b consumes the `execCmd`
harness, the persistent flag set, and the loaded `cfg`. Do not start without it.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent, this plan's text is
authoritative — do not guess at the script's behaviour.**
Functions: `msg` / `warn` / `err` (L37–47), globals (L23–35).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting.

---

## Precondition check (run first)

```bash
go doc bennypowers.dev/larry/cmd/larry ExitError
# expect: type ExitError struct { Code int; Err error }

grep -n 'func newRootCmd' cmd/larry/root.go
# expect: one hit — func newRootCmd() *cobra.Command

grep -n 'func execCmd\|func fixtureOverlay' cmd/larry/testhelp_test.go
# expect: both present

grep -n 'PersistentFlags()' cmd/larry/root.go
# expect: 11 hits (verbose, quiet, color, dry-run, cache-dir, distdir, jobs,
#         github-token, remote, config, overlay)

go doc bennypowers.dev/larry/internal/config Config.ResolveDistdir
# expect: func (c *Config) ResolveDistdir(override string) string
```

**If any of these is missing, plan 13a has not landed — STOP and report.** Do not
implement 13a's pieces yourself.

Also confirm the two symbols this plan builds on, which exist today:

```bash
go doc bennypowers.dev/larry/internal/git Repo.RemoteURL
# expect: func (r *Repo) RemoteURL(name string) (string, error)   <-- a METHOD on *Repo

go doc bennypowers.dev/larry/internal/git ParseRemoteURL
# expect: func ParseRemoteURL(url string) (owner, repo string, err error)  <-- package level
```

---

## Defects being fixed (13b)

| ID | Location | Defect | Verified |
|----|----------|--------|----------|
| D8 | `cmd/larry/prepare.go:201-222` | `resolveGitHubRepo` reads `<overlay>/.git/config` as text, applies `strings.ToLower` to **every line** (`:209`), hard-codes the first matching `url =` line so `--remote` is ignored, and is called from exactly one place (`prepare.go:104`). `cmd/larry/bump.go:172-178` builds a `prepare.PrepareConfig` that never sets `GitHubRepo`, so the bump→prepare path uploads to `""`. | — |
| D9 | `cmd/larry/{bump,import,test,prepare,upstream}.go` | informational output via bare `fmt.Printf`/`fmt.Println`; `--quiet` and `--color` inert; no `>>>`/`***`/`!!!` prefixes; warnings and errors not on stderr | — |
| D12 | `cmd/larry/upstream.go:21,35` | `upstreamRemote` is declared and bound to `--remote`, then **never read** | — |

Bare-`fmt` sites to replace (measured at HEAD):
`import.go:68,69,70,72,77,78,79,84` · `prepare.go:197` · `test.go:121,129` ·
`upstream.go:112,113,114,145`.

Already-correct sites to **leave alone**: `bump.go:181,211` use
`cmd.OutOrStdout()`; `bump.go:234,235,240,241,242,243,245` write to an `io.Writer`
parameter. `main.go:10` is the process's final error print and is exempt.

---

## Target behaviour

### `internal/output.Logger`

The type already has `Warn` (`logger.go:63`) and `Error` (`logger.go:73`), but they are
backed by `slog.NewTextHandler` (`logger.go:32-40`), which emits
`time=… level=WARN msg="…"` key=value lines, always to `os.Stderr`, with no prefixes
and **no injectable writer**. Rewrite, do not wrap.

| Method | Prefix | Stream | Suppressed by `--quiet`? |
|---|---|---|---|
| `Msg(format string, args ...any)` | `>>> ` | stdout | **yes** |
| `Warn(format string, args ...any)` | `*** ` | stderr | **yes** |
| `Error(format string, args ...any)` | `!!! ` | stderr | **NO — never** |
| `Info` / `Infof` | `>>> ` | stdout | yes (keep as thin aliases of `Msg`) |
| `Debug` / `Debugf` | `>>> ` | stderr | yes; additionally gated on `verbose >= 2` |

Rules:
- **Keep the name `Error`.** Do **not** add a method named `Err`; a single error-level
  method avoids two rival spellings across the codebase.
- `Msg` is new. `Warn` and `Error` keep their names and change their bodies.
- Replace `log/slog` entirely with `fmt.Fprintf` against two injectable
  `io.Writer` fields:
  ```go
  type Logger struct {
      verbose int
      quiet   bool
      color   *ColorHelper
      out     io.Writer // default os.Stdout
      errw    io.Writer // default os.Stderr
  }
  func (l *Logger) SetOut(w io.Writer)
  func (l *Logger) SetErr(w io.Writer)
  ```
  (Remove the `info`/`debug`/`error`/`warn` `*slog.Logger` fields and the `output`
  field.) Every line ends with exactly one `\n`.
- Colour: when `l.color != nil && l.color.Enabled()`, colour the **message body only**,
  not the prefix — `Msg` blue, `Warn` yellow, `Error` red. When colour is disabled the
  output contains no `\x1b[` byte at all.
- `cmd/larry` must call `log.SetOut(cmd.OutOrStdout())` and
  `log.SetErr(cmd.ErrOrStderr())` in `PersistentPreRunE` so `execCmd` can capture.

### `config.ResolveGitHubRepo`

```go
// ResolveGitHubRepo returns "owner/repo" for the overlay's GitHub remote.
// Order: c.GitHubRepo (config key `github_repo` / $LARRY_GITHUB_REPO)
//        -> git remote `remote` on `overlay`.
func (c *Config) ResolveGitHubRepo(overlay, remote string) (string, error)
```

Correct flow — note `RemoteURL` is a **method**, `ParseRemoteURL` is **package level**:

```go
repo, err := git.Open(overlay)          // *git.Repo
url,  err := repo.RemoteURL(remote)     // METHOD on *Repo
owner, name, err := git.ParseRemoteURL(url)   // package-level
return owner + "/" + name, nil
```

When `remote == ""`, use `c.Remote` (default `origin`). Case is preserved end to end —
`git.ParseRemoteURL` captures the path verbatim.

---

## Tests (write first — every one must fail before you implement)

### `internal/output/logger_test.go` (rewrite)

The two existing tests construct `Logger` literals with `slog` fields
(`logger_test.go:38-44` and `:71-78`) and will not compile after the rewrite. Rewrite
both. **`TestLogger_QuietMode` (`logger_test.go:63`) currently asserts that
`log.Error` IS suppressed in quiet mode — that assertion must be FLIPPED.**

- `TestMsgWarnErrorPrefixes` — a `Logger` with `out` and `errw` set to separate
  `bytes.Buffer`s, colour `never`, quiet `false`:
  `log.Msg("hello")` → `out` == `">>> hello\n"`, `errw` empty.
  `log.Warn("careful")` → `errw` == `"*** careful\n"`, `out` unchanged.
  `log.Error("boom")` → `errw` == `"!!! boom\n"`, `out` unchanged.
- `TestWarnErrorGoToStderr` — after all three calls, `out` contains neither `***` nor
  `!!!`.
- `TestQuietSuppressesMsgAndWarnNotError` — quiet `true`:
  `log.Msg("a"); log.Warn("b"); log.Error("c")` → `out` == `""`,
  `errw` == `"!!! c\n"` exactly.
- `TestLoggerFormatting` — `log.Msg("x=%d", 7)` → `">>> x=7\n"`.
- `TestLogger_VerboseLevels` (rewritten) — `Debug` writes to `errw` only when
  `verbose >= 2` and `quiet == false`. Table: `(v=0,q=false)` → empty;
  `(v=2,q=false)` → non-empty; `(v=2,q=true)` → empty.
- `TestColorNeverProducesNoAnsi` — colour helper built with `NewColorHelper("never")`:
  after `Msg`/`Warn`/`Error`, neither buffer contains the substring `"\x1b["`.
  With `NewColorHelper("always")`, **both** buffers contain `"\x1b["`.
  (Renamed from the old plan's `TestColorNeverStripsANSI`, which read as if the logger
  should strip ANSI the caller supplied. It must not *produce* ANSI; it does not strip.)

### `cmd/larry/output_test.go` (new)

These must drive a **real subcommand** through `execCmd`. The defect is that the
subcommands bypass the logger, so a test against `internal/output` in isolation passes
while the feature is broken.

- `TestQuietSuppressesSubcommandOutput` — run a subcommand that produces
  informational output (`upstream` against a `fixtureOverlay` with one ebuild, or
  `import --dry-run`) twice: without `--quiet`, stdout is non-empty; with `--quiet`,
  stdout is exactly `""`. In both runs an induced error still reaches stderr with the
  `!!! ` prefix. (**D9**)
- `TestColorNeverProducesNoAnsi_Subcommand` — same subcommand with
  `--color never`: neither stdout nor stderr contains `"\x1b["`. With
  `--color always`: at least one of them does. (**D9**)
- `TestNoBareFmtPrintfInCommands` — a source-level guard. Parse every `*.go` in
  `cmd/larry` (skip `*_test.go`, skip `main.go` and `exit.go`) with
  `go/parser` + `go/ast` and **fail** on:
  1. any call whose function is the selector `fmt.Print`, `fmt.Printf`, or
     `fmt.Println`;
  2. any call to `fmt.Fprint`, `fmt.Fprintf`, or `fmt.Fprintln` whose **first
     argument** is the selector expression `os.Stdout` or `os.Stderr`.

  **Explicitly allowed** (must not fail): `fmt.Fprint*` whose first argument is
  anything else — in particular `cmd.OutOrStdout()` / `cmd.ErrOrStderr()` (already
  present at `bump.go:181`) and a plain `io.Writer` identifier (`bump.go:234-245`).
  Report the offending `token.Position` so failures are actionable.

### `internal/config/config_test.go` (extend)

- `TestResolveGitHubRepo_PreservesCase` — temp repo via `git.PlainInit`, remote
  `origin` = `git@github.com:BennyPowers/Overlay.git`;
  `(&Config{Remote: "origin"}).ResolveGitHubRepo(dir, "")` → `"BennyPowers/Overlay"`,
  nil. (**D8**)
- `TestResolveGitHubRepo_HTTPSUrl` — remote `origin` =
  `https://github.com/BennyPowers/Overlay.git` → `"BennyPowers/Overlay"`.
- `TestResolveGitHubRepo_HonoursRemoteName` — two remotes, `origin` =
  `git@github.com:a/one.git` and `upstream` = `git@github.com:b/two.git`.
  `ResolveGitHubRepo(dir, "upstream")` → `"b/two"`;
  `ResolveGitHubRepo(dir, "")` with `Remote: "origin"` → `"a/one"`. (**D12**)
- `TestResolveGitHubRepo_UsesGoGit` — after `git.PlainInit` + `CreateRemote`, delete
  nothing and make no assumption about `.git/config` text: the function must work
  purely through go-git. Assert success. (**D8**)
- `TestResolveGitHubRepo_ConfigKeyWins` —
  `(&Config{GitHubRepo: "Explicit/Repo"}).ResolveGitHubRepo(dir, "origin")` →
  `"Explicit/Repo"` even though the remote says otherwise.
- `TestResolveGitHubRepo_NoRemoteIsError` — a repo with no remotes returns a non-nil
  error and an empty string.
- `TestResolveGitHubRepo_NonGitHubRemoteIsError` — remote
  `https://gitlab.com/a/b.git` → non-nil error (`git.ParseRemoteURL` rejects
  non-GitHub URLs).

### `cmd/larry/root_test.go` (extend — 13a created this file)

- `TestRemoteFlagReachesGitHubRepoResolution` — `fixtureOverlay` with two remotes
  (`origin` → `git@github.com:a/one.git`, `upstream` → `git@github.com:b/two.git`);
  running `prepare --dry-run --remote upstream --overlay <fixture> <ebuild>` resolves
  `b/two`, while the same run without `--remote` resolves `a/one`. Assert via the
  `PrepareConfig.GitHubRepo` the command builds (expose it through a package-level
  test hook, or assert on the `--dry-run` output line that names the repo). Guards the
  flag against going write-only again. (**D12**)

---

## Implementation steps

1. **Rewrite `internal/output/logger.go`.** Drop the `log/slog` import and the four
   `*slog.Logger` fields; add `out`/`errw` `io.Writer` fields defaulting to
   `os.Stdout`/`os.Stderr`, plus `SetOut`/`SetErr`. Implement `Msg`, `Warn`, `Error`,
   `Info`, `Infof`, `Debug`, `Debugf` per the table in "Target behaviour".
   `NewLogger(verbose int, quiet bool, color *ColorHelper) (*Logger, error)` keeps its
   signature (`cmd/larry/root.go:83` is the only caller).
2. **Rewrite `internal/output/logger_test.go`** — the two existing tests reference the
   deleted slog fields. Flip the `Error`-is-suppressed assertion in
   `TestLogger_QuietMode`.
3. **Wire the logger into the commands.** In `PersistentPreRunE` (added by 13a), after
   `cfg` is final, build the logger and call `log.SetOut(cmd.OutOrStdout())` /
   `log.SetErr(cmd.ErrOrStderr())`.
4. **Replace the bare `fmt` calls** at the sites listed in the defect table:
   informational lines → `log.Msg`, warnings → `log.Warn`, failures that are not
   returned as errors → `log.Error`. Do not touch `bump.go`'s `cmd.OutOrStdout()` /
   `io.Writer` calls, `main.go:10`, or `internal/logger` (a different package; plan 29
   owns unifying it).
5. **Add `ResolveGitHubRepo`.** Add a `GitHubRepo string` field to `config.Config`,
   read it in `applyConfig` from viper key `github_repo` and in `applyEnvOverrides`
   from `LARRY_GITHUB_REPO`. Implement
   `(*Config).ResolveGitHubRepo(overlay, remote string) (string, error)` with the
   `git.Open` → `repo.RemoteURL` → `git.ParseRemoteURL` flow.
   `internal/config` already imports `internal/git` (`config.go:10`), so there is no
   new dependency and no cycle.
6. **Delete `cmd/larry/resolveGitHubRepo`** (`prepare.go:201-222`) and repoint
   `prepare.go:104-105` at `cfg.ResolveGitHubRepo(overlay, cfg.Remote)`.
7. **Wire the bump→prepare path.** `cmd/larry/bump.go:172-178` builds a
   `prepare.PrepareConfig` with no `GitHubRepo`; set it from
   `cfg.ResolveGitHubRepo(overlayRoot, cfg.Remote)`. A failure here is not fatal for
   non-upload package types — log it with `log.Warn` and continue with an empty value.
8. **Read `--remote` (D12).** `upstreamRemote` (`upstream.go:21`) and its flag
   declaration (`upstream.go:35`) are deleted by 13a; the value now lives on the root
   persistent flag and reaches `ResolveGitHubRepo` through `cfg.Remote`.
9. Run the suites; fix fallout. Hand back listing every function and test you deleted.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**.

**Tier 1 — automated, run these. The plan is not done until all are green.**
```bash
go build ./...
go vet ./...
go test ./cmd/... ./internal/output/... ./internal/config/... -count=1
go test ./... -count=1        # no NEW failures vs. your recorded baseline
golangci-lint run ./...
```
**Known-red baseline at HEAD (do not chase):**
`TestExtractVar/multi_line` in `internal/prepare` (plan 20/22 owns it) and one
`go vet` finding at `internal/git/git.go:318` (self-assignment; plan 29 owns it).
If your measured baseline differs, record it and continue.

**Tier 1 — source-level greps (run these; both must print nothing):**
```bash
grep -rn 'fmt\.Print\|fmt\.Fprint.*os\.Std' cmd/larry/ --include='*.go' \
  | grep -v '_test.go' | grep -v 'cmd/larry/main.go'
# expect: no output

grep -n 'func resolveGitHubRepo' cmd/larry/prepare.go
# expect: no output
```

**Tier 2 — read-only manual. Needs `~/Developer/gentoo-overlay`. Safe to run; skip and
say so if the overlay is absent.**
```bash
larry upstream --overlay ~/Developer/gentoo-overlay --quiet
# expect: no informational output on stdout

larry upstream --overlay ~/Developer/gentoo-overlay --color never | cat -v | grep '\^\['
# expect: no output (no ANSI escapes when piped with --color never)
```

**Tier 3 — MUTATING. DO NOT RUN. Report these verbatim for the human.**
```bash
# `larry prepare` on an npm or vendored package UPLOADS A GITHUB RELEASE ASSET
# to a real repo and writes into the reference overlay. It is the only end-to-end
# proof that ResolveGitHubRepo preserves case.
larry prepare --overlay ~/Developer/gentoo-overlay <category/package>
# expect: the gh upload targets BennyPowers/<Overlay> with original casing, not
# an all-lowercase repo that 404s.

larry bump --overlay ~/Developer/gentoo-overlay <category/package>
# expect: the bump->prepare path now carries a non-empty GitHubRepo.
```

---

## Out of scope

- Everything in plan 13a (flags, `config.Load()`, `ExitError`, `Resolve*`).
- `internal/logger` (the separate `[larry]`-prefixed package used by
  `cmd/larry/bump.go` and `internal/prepare/*`). Unifying it with `internal/output`
  is plan 29's call.
- Per-command behaviour fixes (plans 21–28). Only the output plumbing changes here.

If you find an unrelated bug, write it in your final report; do not fix it.
