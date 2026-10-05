# 29c — Hygiene: `staticcheck`, the last red test, and a bounded test-quality sweep

**Why:** `staticcheck` is the linter class most likely to be reporting a real bug rather
than a style nit — `SA4000` (identical expressions either side of `!=`) and `SA4006`
(a computed value never used) are assertions that do not assert. One test has been red
since before Phase 2 started. And the Phase 1 failure mode was tests that exercise
helpers the production path never calls; four known instances need a bounded sweep.

**Precondition:** plans 12–28 (run after them; **re-measure your own baseline before
starting**). In particular plan 20a (`20a-PLAN-EBUILD-VARS.md`) and plan 22a
(`22a-PLAN-PREPARE-NPM-SRCURI.md`) own `extractVar`.

**Reference:** AGENTS.md "Before every push" rule in the user's global guidance.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
This plan does not change behaviour, so it cites no script function. **If that file is
absent on this machine, this plan's text is authoritative — do not guess.**

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting.

**Sibling plans:** `29a` (`go vet`, `ineffassign`, `unused`, dead code, repo
cleanliness, Makefile) and `29b` (`errcheck`). The three are independent.

---

## READ THIS FIRST — the real count is 24, not 18

A plain `golangci-lint run ./...` reports **18 staticcheck**. That is truncated output:
`golangci-lint`'s defaults are `max-issues-per-linter: 50` and `max-same-issues: 3`.
Measured 2026-10-05 at HEAD with truncation disabled:

```bash
golangci-lint run --max-issues-per-linter=0 --max-same-issues=0 ./...
# 319 issues: errcheck 280, staticcheck 24, unused 12, ineffassign 2, govet 1
```

**Use the untruncated form for every command in this plan.**

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

## Item 1 — `staticcheck` clean (24 findings at HEAD)

The complete list, measured at HEAD. Two of them are `(related information)` lines
attached to the `S1034` above them, so there are **22 distinct sites**.

| Code | Location | What to do |
|---|---|---|
| `SA4000` | `internal/ebuild/portageq_test.go:374:5` | **identical expressions on both sides of `!=`** — the assertion compares a value with itself and can never fail. Read the test, work out what it meant to compare, fix it. If the intent is unrecoverable, delete the assertion and say so. **This one is a real bug, not a nit.** |
| `SA4006` | `internal/ebuild/portageq_test.go:232:5`, `:266:5` | `this value of ok is never used` — the test computes `ok` and then does not assert on it. Add the missing `if !ok { t.Fatalf(...) }`. |
| `SA4006` | `internal/prepare/vendored_test.go:296:5` | same |
| `SA1012` | `internal/output/logger.go:51:22, 58:24, 68:22, 78:24, 90:24` | `do not pass a nil Context` → `context.TODO()` |
| `SA1012` | `internal/httpclient/client.go:129:26, 146:29` | same |
| `S1034` | `internal/cli/spinner.go:119:9` (+ related at `:121:13`, `:123:6`) | `switch msg := msg.(type)` instead of repeating the type assertion |
| `QF1012` | `internal/cli/upstream_tui.go:207:2, 212:3` | `fmt.Fprintf(w, …)` instead of `w.WriteString(fmt.Sprintf(…))` |
| `QF1012` | `internal/importcmd/import.go:408:3, 417:3` | same. **Plan 28b rewrites `writeMetadata` entirely — if these are already gone, say so.** |
| `QF1012` | `internal/prepare/cargo.go:284:3` | same |
| `QF1004` | `internal/upstream/upstream.go:1180:11, 1186:11, 1191:10` | `strings.ReplaceAll(s, old, new)` instead of `strings.Replace(s, old, new, -1)` |

`SA1012`, `S1034`, `QF1012` and `QF1004` are mechanical and safe. `SA4000` and the three
`SA4006`s are **assertions that do not assert** — treat each as a potential bug and read
the surrounding test before changing it.

Do **not** silence anything with `//nolint`, and do **not** add or weaken
`.golangci.yml`.

**Gate:**
```bash
golangci-lint run --enable-only=staticcheck --max-issues-per-linter=0 --max-same-issues=0 ./...
# expect: 0 issues
```

---

## Item 2 — the last red test: `TestExtractVar/multi_line`

Measured failure at HEAD:

```
--- FAIL: TestExtractVar/multi_line
    npm_test.go:243: extractVar("SRC_URI=\"\n\tmirror://npm/pkg/pkg-1.0.0.tgz\n\"", "SRC_URI")
                     = "", want "\n\tmirror://npm/pkg/pkg-1.0.0.tgz\n"
```

- The test is `TestExtractVar` at `internal/prepare/npm_test.go:211`; the failing case
  is the table entry named `multi_line` at `npm_test.go:226-230`; the assertion line is
  `npm_test.go:243`.
- The subject is `extractVar` at `internal/prepare/npm.go:446`. There are **three**
  functions with that name: `internal/prepare/npm.go:446`,
  `internal/ebuild/info.go:201`, `internal/upstream/upstream.go:491`. Plan 20a collapses
  the rival SRC_URI expanders; this one may or may not survive.

**"Correct" is defined precisely:** given the input string
`SRC_URI="\n\tmirror://npm/pkg/pkg-1.0.0.tgz\n"` (a `SRC_URI=` assignment whose opening
quote is followed immediately by a newline, one tab-indented URI, a newline, then the
closing quote) and the variable name `SRC_URI`, the function must return the **entire
quoted body including both newlines and the tab**:
`"\n\tmirror://npm/pkg/pkg-1.0.0.tgz\n"`. It currently returns `""` because it only
looks at a single line.

**Two outcomes, pick the one that matches reality when you run:**

1. **`internal/prepare/extractVar` still exists** → make it return the full multi-line
   value. Scan from the `=` for an opening `"` and return everything up to the matching
   unescaped closing `"`, newlines included. Keep every other case in the table green
   (single-line, quoted, unquoted, `not_found` → `""`).
2. **Plan 20a deleted it** → delete `TestExtractVar` from `npm_test.go` along with it,
   and then **verify an equivalent case exists in `internal/ebuild/srcuri_test.go`**:
   ```bash
   grep -n 'multi.line\|multi_line' internal/ebuild/srcuri_test.go
   ```
   If there is no multi-line SRC_URI case there, **add one** with exactly the input and
   expected output above, against whichever function plan 20a kept. Do not let the
   coverage disappear along with the function.

**Gate:** `go test ./... -count=1` passes with **zero** failures — no known-red baseline
remains after this plan.

---

## Item 3 — bounded test-quality sweep (delete-only)

The Phase 1 failure mode: tests that assert on a helper the production path never calls.
They pass while the feature is broken. `PLAN.md` lists confirmed instances.

**This item is DELETE-ONLY and bounded to four named files.** An earlier draft said
"either wire the helper up or delete the test", which contradicts this plan's own
"Out of scope: behavioural change" — wiring a helper up *is* a behavioural change.
It does not happen here.

Scope, exactly these four:

| File | Suspect symbol | Expectation |
|---|---|---|
| `internal/test/local_test.go` | `buildPhaseList` (`internal/test/local.go:63`) | plan 26 gives it a production caller — expect **keep** |
| `internal/prepare/prepare_test.go:442-468` | `manifestArgs` (`internal/prepare/prepare.go:292`) and its two wrappers `manifestRunnerArgs` (`manifest.go:21`) and `GetManifestArgs` (`manifest.go:102`) | plan 21 rewrote the manifest path; at HEAD the only non-test callers are those two wrappers, which are themselves uncalled — **check** |
| `containerfile_test.go` | `TestContainerfile_Exists` | plan 27a replaced the file wholesale — expect **nothing to do** |
| `internal/upstream/dependencies_test.go` | `ScanDependencies`, `MergeDepStatuses` (`internal/upstream/dependencies.go:291`) | plan 18 wires dependency scanning — **check** |

**Procedure for each suspect symbol:**

```bash
grep -rn '<symbol>' --include='*.go' ./cmd ./internal .
```

- **Zero non-test hits** → the function is test-only. Delete the function **and** the
  tests whose only subject it is. Record the deletion.
- **At least one non-test hit** → keep everything. Record which plan added the caller.
- **Ambiguous** (e.g. the only caller is itself on a dead branch) → **keep it, record it
  as a follow-up candidate, and move on.** Do not wire it up.

**Gate:** a table in your report with one row per suspect symbol and one of the three
verdicts above. No other file is in scope for this sweep — do not go hunting.

---

## Tests

Items 1 and 3 write no new tests (one is a linter sweep, the other is delete-only).
Item 2 either fixes the existing `TestExtractVar/multi_line` or relocates an equivalent
case into `internal/ebuild/srcuri_test.go` with the exact input/output given above.

Safety rail for item 1: making an `SA4006` assertion actually assert can turn a
silently-passing test red. **If that happens you found a real bug** — stop, record it in
your report, and do not delete the assertion to make it green.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**. Everything here is Tier 1
> (automated) or Tier 2 (read-only). There is no Tier 3 in this plan.

**Tier 1 — run these. All four are required.**

```bash
go build ./...
go vet ./...                      # expect only what plan 29a has not yet fixed
go test ./... -count=1            # expect: ALL OK — this plan removes the last red test
golangci-lint run ./...           # per AGENTS.md, required before any push
```

**Tier 1 — the hard gates for this plan:**

```bash
golangci-lint run --enable-only=staticcheck --max-issues-per-linter=0 --max-same-issues=0 ./...
# expect: 0 issues

go test ./internal/prepare/ ./internal/ebuild/ -run 'ExtractVar|SrcUri|SRCURI' -count=1 -v
# expect: green, and a multi-line SRC_URI case is present somewhere in the output
```

**Tier 2 — read-only, safe to run:**

```bash
grep -rn 'nolint' --include='*.go' ./cmd ./internal .
# expect: no new entries vs. your baseline

git diff -- .golangci.yml .golangci.yaml
# expect: EMPTY. Weakening the lint config is not a fix.

git status --porcelain
# expect: only files you intentionally changed. DO NOT COMMIT.
```

## Out of scope

`go vet`, `ineffassign`, `unused`, dead code, `.gitignore`, the Makefile (plan 29a).
`errcheck` (plan 29b). **Behavioural change** — including wiring an unused helper into
the production path, which is explicitly forbidden by item 3. If a `staticcheck` fix
requires a behavioural decision, stop and record it as a follow-up rather than guessing.
Do not commit anything.
