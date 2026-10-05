# 29a — Hygiene: `go vet`, `ineffassign`, `unused`, dead code, repo cleanliness, Makefile

**Why:** Several of the bugs found in plans 12–28 were *already reported* by
`golangci-lint` as `unused` — nobody was reading the output. `make lint` cannot even run
(it guards on a binary called `govet` that does not exist). Two log files and a stray
ebuild sit untracked at the repo root. This plan takes the three *mechanical* linter
classes plus the housekeeping; `errcheck` is 29b and `staticcheck` is 29c.

**Precondition:** plans 12–28 (run after them; **re-measure your own baseline before
starting** — most of the dead code below should already be gone).

**Reference:** AGENTS.md "Before every push" rule in the user's global guidance.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
This plan does not change behaviour, so it cites no script function. **If that file is
absent on this machine, this plan's text is authoritative — do not guess.**

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting.

**Sibling plans:** `29b` (`errcheck` — **280** untruncated, not the 49 the default
output shows) and `29c` (`staticcheck` — **24** untruncated, not 18 — plus the red test
and a bounded, delete-only test-quality sweep). The three are independent and can run in any order;
each has its own hard gate.

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

## A grep gotcha on this machine — read before running any check below

`grep -r … .` from the repo root **silently skips `cmd/`** in this environment
(reproduced 2026-10-05: `grep -rln 'package main' --include='*.go' .` finds
`containerfile_test.go` and `internal/prepare/go_test.go` but **not** the seven
`cmd/larry/*.go` files). Every recursive grep in this plan therefore passes explicit
roots:

```bash
grep -rn '<pattern>' --include='*.go' ./cmd ./internal .
```

If you write `grep -r … .` and get zero hits, **do not conclude the symbol is gone.**
Re-run with `./cmd ./internal` before believing it.

---

## Measured baseline (2026-10-05, at HEAD before any Phase 2 work)

```
go vet ./...              1  internal/git/git.go:318:2: self-assignment of s
go test ./... -count=1    1 FAIL  internal/prepare  TestExtractVar/multi_line
golangci-lint run ./...   82 issues
                          49 errcheck      -> plan 29b
                          18 staticcheck   -> plan 29c
                          12 unused        -> THIS PLAN
                           2 ineffassign   -> THIS PLAN
                           1 govet         -> THIS PLAN
```

**Record your own numbers before you start.** If they differ from the above, the
behavioural plans changed them — that is expected. Use *your* measurement as the
denominator.

---

## Scope — three linter classes plus housekeeping

### Item 1 — `go vet` clean (1 finding)

`internal/git/git.go:318:2: self-assignment of s`. Read the surrounding lines; the fix
is either deleting the statement or completing an assignment that was meant to
transform `s`. **Gate:** `go vet ./...` produces no output.

### Item 2 — `ineffassign` clean (2 findings)

```
internal/cli/upstream_tui_test.go:147:5  ineffectual assignment to msg
internal/test/local_test.go:121:2        ineffectual assignment to phases
```

`local_test.go:120-129` is `TestBuildPhaseList_CustomPhases`: it calls
`buildPhaseList(false,false,false)`, throws the result away, reassigns
`phases = []string{"fetch","unpack"}`, and then asserts on its own literal. It tests
nothing. **Delete the function.** (Plan 26 also deletes it — if it is already gone,
say so and move on.)

`upstream_tui_test.go:147` — read the surrounding code before deleting; an ineffectual
assignment in a test usually means the test meant to assert on the reassigned value.

**Gate:** `golangci-lint run --enable-only=ineffassign ./...` → 0 issues.

### Item 3 — `unused` clean (12 findings)

Measured at HEAD:

```
cmd/larry/prepare.go:193:6              func printPrepareResult
cmd/larry/root.go:15:2                  var log
internal/ebuild/info.go:17:2            var inheritLineRe
internal/importcmd/import.go:589:6      func findOverlaysViaPortageq   -> plan 28a
internal/integration/integration_test.go:13:6  func fixtureRoot
internal/prepare/manifest.go:21:5       var manifestRunnerArgs         -> plan 21
internal/prepare/manifest.go:83:6       func runManifestCmd            -> plan 21
internal/prepare/vendored.go:182:6      func createNPMDepsTarball      -> plan 22b/25b
internal/prepare/vendored.go:220:6      func copyFile                  -> plan 25b
internal/prepare/vendored.go:228:6      func copyDir                   -> plan 25b
internal/providers/provider_test.go:15:6  func newTestClient
internal/upstream/upstream_test.go:25:2   field mockAllowPrerele
```

Delete, do not export-to-silence, do not add `var _ = …`. Most should already be gone
when you run; verify rather than assume.

**Gate:** `golangci-lint run --enable-only=unused ./...` → 0 issues.

### Item 4 — the "additional dead code" list (17 entries)

`unused` does **not** report an unexported function that a test file references, so
these do not show up as linter findings. They need a manual pass.

**Method — for each entry, run exactly this and paste the result into your report:**

```bash
grep -rn '<name>' --include='*.go' ./cmd ./internal .
```

**Done criterion for an entry, one of exactly two:**
- the only hits are its own definition (and its own test file) → **delete both**; or
- a Phase 2 plan gave it a production caller → **keep it, and name the plan in your
  report**.

Anything else (e.g. "it looks useful") is not a valid outcome.

Reference counts measured at HEAD, so you can see at a glance what changed:

| Symbol | Location at HEAD | Hits at HEAD | Expected owner |
|---|---|---|---|
| `PrepareConfig.RevisionBump` | `internal/prepare/prepare.go:27` | 13 (`RevisionBump`) | — |
| `parseEbuildInfo` | `internal/prepare/prepare.go:163` | 11 | — |
| `isLiveVersion` | `internal/prepare/prepare.go:238` | 17 | — |
| `isRevisionBump` | `internal/prepare/prepare.go:243` | 7 | — |
| `verifyDistfiles` | `internal/prepare/prepare.go:297` | 15 | — |
| `extractDistfilesFromSRCURI` | `internal/prepare/prepare.go:311` | 7 | — |
| `copyToDistdir` | `internal/prepare/go.go:308` | 5 | **plan 24 gives it a caller — keep** |
| `printPrepareResult` | `cmd/larry/prepare.go:193` | 2 (definition only) | — |
| `setNetworkFeatures` | `internal/test/local.go:128` | 5 | plan 26 deletes it |
| `TestLocalConfig` | `internal/test/local.go:175` | 2 | plan 26 deletes it |
| `CheckUpstream` | `internal/upstream/upstream.go:26` | 29 | plan 16 |
| `CheckUpstreamSimple` | `internal/upstream/upstream.go:139` | 2 | plan 16 |
| `Run` | `internal/upstream/upstream.go:185` | — (too common to grep by name; check callers in-editor) | plan 16 |
| `MergeDepStatuses` | `internal/upstream/dependencies.go:291` | 6 | plan 18 |
| `ParseLegacyUpstream` | `internal/remoteid/remoteid.go:148` | 2 | plan 15 |
| `autoDetectOverlay` | `internal/importcmd/import.go:454` | 12 | plan 28a deletes it |
| `autoDetectOverlayDirs` | `internal/importcmd/import.go:464` | 4 | plan 28a deletes it |

(`findOverlaysViaPortageq` is at `internal/importcmd/import.go:589` and is already in
the `unused` list above — an earlier draft of this plan attributed `:454` and `:589` to
the wrong functions. The three correct line numbers are `:454` `autoDetectOverlay`,
`:464` `autoDetectOverlayDirs`, `:589` `findOverlaysViaPortageq`.)

**Gate:** every row of the table has a one-line verdict in your report: `deleted`, or
`kept — caller added by plan N`.

### Item 5 — repo cleanliness

At HEAD the repo root holds, untracked and unignored:

```
gaze.log
pi.log
test-crate-0.1.0.ebuild
bump.test  cli.test  github.test     # already matched by *.test in .gitignore
larry                                # already in .gitignore
```

- Add `*.log` to `.gitignore` (the existing file has no log pattern) and delete
  `gaze.log` and `pi.log`.
- `test-crate-0.1.0.ebuild`: move it into `testdata/` if a test reads it
  (`grep -rn 'test-crate' --include='*.go' ./cmd ./internal .` first), otherwise delete
  it.
- Confirm `*.test` and `larry` stay ignored — they already are
  (`.gitignore` lines for `larry` and `*.test`).

**Gate — read this carefully, it replaces an earlier, wrong one.**
The old acceptance said `git status --porcelain # clean`. That is **unachievable** — by
this point you will have edited dozens of tracked files — and it invites you to commit,
which you must not do. The real gate is:

```bash
git status --porcelain
```

lists **only files you intentionally changed**. Specifically: no `gaze.log`, no
`pi.log`, no stray `*.test` binaries, no `test-crate-0.1.0.ebuild` at the root.
**Do NOT commit. Leave the working tree for the human.**

### Item 6 — Makefile

`Makefile:25` reads:

```make
@command -v govet >/dev/null 2>&1 || { echo "go vet not found"; exit 1; }
```

**There is no binary called `govet`** — `go vet` is a `go` subcommand — so `make lint`
exits 1 on every machine, always. The rewrite removes that line entirely:

```make
.PHONY: lint
lint:
	@command -v golangci-lint >/dev/null 2>&1 \
	  && golangci-lint run ./... \
	  || { echo "golangci-lint not found; falling back to go vet"; $(GO) vet ./...; }
```

**Gate:** `make lint` exits 0 and its verdict matches `golangci-lint run ./...`.

---

## Tests

This plan is mechanical: deletions, a `.gitignore` line and a Makefile rule. **It adds
no unit tests by design** — every item has a linter or shell gate instead, listed above.
Deleting a function means deleting its tests; say which in your report.

The one exception: if item 1's `go vet` fix at `internal/git/git.go:318` turns out to be
a real behavioural bug rather than a no-op statement, **stop** and record it as a
follow-up per "Out of scope" below — do not guess at the intended behaviour.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**. Everything here is Tier 1
> (automated) or Tier 2 (read-only). There is no Tier 3 in this plan.

**Tier 1 — run these. All four are required.**

```bash
go build ./...
go vet ./...                      # expect NO output at all — item 1's gate
go test ./... -count=1            # no NEW failures vs. your recorded baseline
golangci-lint run ./...           # per AGENTS.md, required before any push
```

**Tier 1 — the three hard per-linter gates:**

```bash
golangci-lint run --enable-only=govet       ./...    # 0 issues
golangci-lint run --enable-only=ineffassign ./...    # 0 issues
golangci-lint run --enable-only=unused      ./...    # 0 issues
```

Note the baseline's `TestExtractVar/multi_line` belongs to **plan 29c** in this split,
not here. If it is still red when you run, leave it.

**Tier 1 — Makefile:**

```bash
make lint; echo "exit=$?"      # expect exit=0
```

**Tier 2 — read-only, safe to run:**

```bash
git status --porcelain
# expect: only files you intentionally changed.
# expect NOT present: gaze.log, pi.log, *.test, test-crate-0.1.0.ebuild
# DO NOT COMMIT.

ls gaze.log pi.log test-crate-0.1.0.ebuild 2>&1
# expect: "No such file or directory" for all three

grep -n '\*.log' .gitignore
# expect: a match
```

## Out of scope

`errcheck` (plan 29b). `staticcheck`, `TestExtractVar/multi_line`, and the test-quality
sweep (plan 29c). **Behavioural change of any kind.** If a deletion or a lint fix
requires a behavioural decision, stop and record it as a follow-up in your report rather
than guessing. Do not commit anything.
