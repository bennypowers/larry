# 29b — Hygiene: `errcheck`

**Why:** Unchecked error returns hide real bugs. A swallowed `os.WriteFile` means the
file silently was not written; a swallowed `cmd.Run` means the subprocess silently did
not run. Several Phase 1 defects were of exactly this shape.

**Precondition:** plans 12–28 (run after them; **re-measure your own baseline before
starting**).

**Reference:** AGENTS.md "Before every push" rule in the user's global guidance.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
This plan does not change behaviour, so it cites no script function. **If that file is
absent on this machine, this plan's text is authoritative — do not guess.**

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting.

**Sibling plans:** `29a` (`go vet`, `ineffassign`, `unused`, dead code, repo
cleanliness, Makefile) and `29c` (`staticcheck`, the red test, the test-quality sweep).
The three are independent.

---

## READ THIS FIRST — the real count is 280, not 49

A plain `golangci-lint run ./...` reports **49 errcheck**. That number is **truncated
output**, not a measurement: `golangci-lint`'s defaults are `max-issues-per-linter: 50`
and `max-same-issues: 3`, so repeated findings are collapsed and the per-linter total
is capped. (`plan/PLAN.md`'s "Current state" table carries both numbers; the 82-issue
figure is the truncated one.)

Measured 2026-10-05 at HEAD with truncation disabled:

```bash
golangci-lint run --max-issues-per-linter=0 --max-same-issues=0 ./...
```

```
319 issues:
* errcheck: 280        <-- not 49
* govet: 1
* ineffassign: 2
* staticcheck: 24      <-- not 18
* unused: 12
```

**Use the untruncated form for every command in this plan.** A run without those two
flags will tell you you are done when you are not.

Of the 280: **222 are in `_test.go` files and 58 are in production code.**

| Package | errcheck | Notes |
|---|---|---|
| `internal/prepare` | 126 | `go_test.go` 27, `custom_prepare_test.go` 27, `npm.go` 18, `cargo_test.go` 15, `vendored_test.go` 12, `prepare_test.go` 8, `npm_test.go` 6, `vendored.go` 4, `prepare.go` 4, `go.go` 3, `cargo.go` 2 |
| `internal/importcmd` | 33 | all in `import_test.go` |
| `internal/test` | 27 | `container_test.go` 18, `local_test.go` 9 |
| `internal/config` | 23 | all in `config_test.go` (`os.Setenv`/`os.Unsetenv`) |
| `internal/providers` | 15 | `provider_test.go` 8, `sourcehut.go` 2, `gitlab.go` 2, `pypi.go` 1, `npm.go` 1, `codeberg.go` 1 |
| `cmd/larry` | 12 | `bump.go` 10, `upstream.go` 1, `prepare.go` 1 |
| `internal/upstream` | 11 | `upstream_test.go` 7, `cache_test.go` 2, `output.go` 1, `cache.go` 1 |
| `internal/httpclient` | 11 | `client_test.go` 10, `client.go` 1 |
| `internal/integration` | 8 | `integration_test.go` |
| `internal/ebuild` | 6 | `portageq_test.go` 3, `portageq.go` 2, `info.go` 1 |
| `internal/ratelimit` | 3 | `retry.go` 2, `ratelimiter_test.go` 1 |
| `internal/git` | 3 | `git_test.go` |
| `internal/output` | 2 | `color_test.go` |

Those are HEAD numbers. Plans 12–28 will have moved them — **re-measure and record your
own table before you start.**

---

## Precondition check — run these first

```bash
git -C . status --porcelain | head -20     # expect: a working tree you did NOT create
go build ./...
go test ./... -count=1 2>&1 | tail -5
golangci-lint run --max-issues-per-linter=0 --max-same-issues=0 ./... 2>&1 | tail -8
grep -n 'func execCmd\|func fixtureOverlay' cmd/larry/testhelp_test.go
go doc ./internal/atom ParseEbuildPath
```

Expected:

- `go build ./...` is clean. **If it is not, a Phase 2 plan left the tree broken —
  STOP and report; do not start a lint sweep on a non-compiling tree.**
- `cmd/larry/testhelp_test.go` exists with `execCmd` and `fixtureOverlay` (plan 13a) and
  `internal/atom` exists (plan 12). Their absence means plans 12–28 have not all landed.
  **This plan is defined to run last — STOP and report rather than running it early.**
- The `golangci-lint` tail prints the per-linter counts. **Write those numbers down.
  They are your denominator; the HEAD numbers below are only a reference point.**

---

## A grep gotcha on this machine

`grep -r … .` from the repo root **silently skips `cmd/`** in this environment
(reproduced 2026-10-05). Always pass explicit roots:
`grep -rn '<pattern>' --include='*.go' ./cmd ./internal .`

---

## The rule — three categories, no fourth

For each finding, classify it into exactly one of these and apply the stated fix.

### 1. The error matters → handle it

Production code where failure changes the outcome. `os.WriteFile`, `os.MkdirAll`,
`cmd.Run`, `os.Rename`, `json.Encode` into a response — if it fails, the caller has
been lied to. Return it, wrapped:

```go
if err := os.WriteFile(p, b, 0o644); err != nil {
    return fmt.Errorf("write %s: %w", p, err)
}
```

**`internal/ebuild/portageq.go:*` and `internal/ebuild/info.go:*` `cmd.Run` findings are
in this category** — a `portageq` that did not run produces empty variables, which is
exactly the class of defect plan 20 was written for. Do not `_ =` them.

### 2. The error is genuinely uninteresting → `_ =` with a reason

A deferred `Close` on a read-only file, an `os.Remove` of a best-effort temp file, a
`fmt.Fprintf` to a terminal. Write the discard explicitly **with a comment saying
why**:

```go
// Best-effort cleanup; the temp dir is removed by the OS on reboot regardless.
_ = os.RemoveAll(tmp)
```

A bare `_ =` with no comment is not acceptable. A blanket `//nolint:errcheck` is not
acceptable. A `.golangci.yml` exclusion is not acceptable — **do not add or weaken a
lint config to make the number go down.**

**`defer f.Close()` on a file opened for *writing* is category 1, not 2** — a failed
`Close` can mean unflushed data. Use:

```go
defer func() {
    if cerr := f.Close(); cerr != nil && err == nil {
        err = fmt.Errorf("close %s: %w", p, cerr)
    }
}()
```

(with a named return `err`). There are several of these in `internal/prepare/npm.go`
(`dst.Close`, `out.Close`, `f.Close`, `gr.Close`, `src.Close`).

### 3. Test code → prefer the `t`-aware form

222 of the 280 are in tests, and almost all are one of three shapes. Use these
mechanical rewrites; they are faster and better than `_ =`:

| Found | Replace with |
|---|---|
| `os.Setenv(k, v)` / `os.Unsetenv(k)` in a test | `t.Setenv(k, v)` — it also restores the old value automatically. This alone clears all 23 in `internal/config/config_test.go` |
| `os.MkdirAll(...)`, `os.WriteFile(...)`, `os.Chtimes(...)` in a test | wrap: `if err := …; err != nil { t.Fatalf("…: %v", err) }` |
| `w.Write(...)` / `enc.Encode(...)` in an `httptest` handler | `_, _ = w.Write(…)` with a comment `// httptest writer; a write error cannot occur here.` |
| `defer resp.Body.Close()` in a test | `defer func() { _ = resp.Body.Close() }()` |
| `os.RemoveAll(dir)` in a test | replace the whole temp-dir dance with `t.TempDir()`, which cleans up for you |

Converting `os.Setenv` → `t.Setenv` and ad-hoc temp dirs → `t.TempDir()` is **not** a
behavioural change to production code and is explicitly in scope here. It also removes
cross-test env leakage, which plan 13's harness requires anyway.

---

## Tests

This plan writes **no new tests** — it is an error-handling sweep over existing code,
gated by the linter. Three safety rails instead:

1. `go test ./... -count=1` must not gain a failure. If making an error *checked* turns
   a silently-passing test red, **you found a real bug**: stop, record it in your
   report, and do not paper over it with `_ =`.
2. Run the suite with `-race` once at the end — the `t.Setenv` conversions change
   parallelism rules (`t.Setenv` forbids `t.Parallel()` in the same test; if a test
   calls both, drop the `t.Parallel()` and say so).
3. Diff-review your own changes for any `_ =` without an adjacent comment:
   ```bash
   git diff -U0 | grep -n '^\+.*_ = ' 
   ```
   every hit must have a comment on the line above.

---

## Suggested order (keeps the number falling fast)

1. `internal/config/config_test.go` — 23, all `os.Setenv`/`os.Unsetenv` → `t.Setenv`.
2. `internal/importcmd/import_test.go` — 33, mostly `os.MkdirAll`/`os.WriteFile`.
3. `internal/prepare/*_test.go` — 95 across five files, same shapes.
4. `internal/test/*_test.go` — 27.
5. `internal/httpclient/client_test.go`, `internal/providers/provider_test.go`,
   `internal/upstream/*_test.go`, `internal/git/git_test.go`,
   `internal/integration/integration_test.go`, `internal/output/color_test.go`,
   `internal/ebuild/portageq_test.go`, `internal/ratelimit/ratelimiter_test.go` — the
   remaining ~45 test findings.
6. **Production code last, and slowest** — 58 findings, the ones that can hide bugs:
   `internal/prepare/npm.go` 18, `cmd/larry/bump.go` 10, `internal/prepare/vendored.go`
   4, `internal/prepare/prepare.go` 4, `internal/prepare/go.go` 3,
   `internal/prepare/cargo.go` 2, `internal/ebuild/portageq.go` 2,
   `internal/ratelimit/retry.go` 2, `internal/providers/{sourcehut,gitlab}.go` 2 each,
   `internal/providers/{pypi,npm,codeberg}.go` 1 each,
   `internal/upstream/{output,cache}.go` 1 each, `internal/httpclient/client.go` 1,
   `internal/ebuild/info.go` 1, `cmd/larry/{upstream,prepare}.go` 1 each.

**Checkpoint — after step 5, re-run the gate and record the remaining count.** If budget
is short, stop there and hand back with the number; step 6 is a clean second session and
is the half that needs care.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**. Everything here is Tier 1
> (automated) or Tier 2 (read-only). There is no Tier 3 in this plan.

**Tier 1 — run these. All four are required.**

```bash
go build ./...
go vet ./...                      # expect only what plan 29a has not yet fixed
go test ./... -count=1            # no NEW failures vs. your recorded baseline
golangci-lint run ./...           # per AGENTS.md, required before any push
```

**Tier 1 — the hard gate for this plan:**

```bash
golangci-lint run --enable-only=errcheck --max-issues-per-linter=0 --max-same-issues=0 ./...
# expect: 0 issues
```

That exact command, with both `--max-*` flags, is the pass/fail line. Report the
before-number and the after-number.

```bash
go test ./... -count=1 -race
# expect: no new failures; note any test you had to de-parallelise for t.Setenv
```

**Tier 2 — read-only, safe to run:**

```bash
git diff --stat
# expect: only the files listed in your measured table

git diff -U0 | grep '^\+.*_ = '
# every hit must be preceded by an explanatory comment in the diff

git diff -- .golangci.yml .golangci.yaml
# expect: EMPTY. Weakening the lint config is not a fix.
```

Do **not** commit. Leave the working tree for the human.

## Out of scope

`go vet`, `ineffassign`, `unused`, dead code, `.gitignore`, the Makefile (plan 29a).
`staticcheck`, `TestExtractVar/multi_line`, the test-quality sweep (plan 29c).
**Behavioural change** beyond what checking an error necessarily implies. If handling an
error properly requires deciding what the code *should* do on failure and the answer is
not obvious, stop and record it as a follow-up rather than guessing.
