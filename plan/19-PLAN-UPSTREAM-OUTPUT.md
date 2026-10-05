# 19 — `upstream`: Source column, hyperlinks & rendering

**Why:** The table computes a correct source URL and then throws it away in favour of a
buggy re-derivation. GitLab links point at `gitlab.com` regardless of host; SourceHut
links never render because the label and the matcher disagree on capitalisation; and
OSC-8 escapes are emitted even when the output is a pipe.

**Precondition:** 15 (remote-id `Host`/`Path`), 16 (discovery), 17a (`StatusCheckTag`),
17b (GitHub tiers), 18 (`UpstreamResult.Depth` and the Package-cell indent).

**Ordering: plan 18 runs BEFORE this plan.** Plan 18 owns `UpstreamResult.Depth` and the
**Package** cell indent. This plan owns the **Source** cell, the hyperlink gate,
`RenderTable`'s signature and the golden file. **The golden file committed here must
contain a depth-1 row**, which only exists once plan 18 has landed. Do not reorder.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent on this machine, this
plan's text is authoritative — do not guess at the script's behaviour.**
Functions: `colorize_upstream` (L1514–1528), the `printf` in `check_package` (L1499),
`do_upstream`'s `column -t` pipeline (L1583–1588).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. Known-red baseline: exactly
> `TestExtractVar/multi_line` in `internal/prepare`, plus one `go vet` finding at
> `internal/git/git.go:318`. Do not chase either.

**Line numbers below are as measured at HEAD on 2026-10-05.** Plans 16, 17a and 18 all
edit `internal/upstream/output.go` and `internal/upstream/upstream.go` before this one,
so the numbers will have moved. **Locate by symbol name, not by line.**

---

## Precondition check — run these first

```bash
go doc ./internal/remoteid RemoteId
go doc ./internal/upstream Status
go doc ./internal/upstream UpstreamResult
go doc ./internal/upstream RunWithTargets
grep -n 'Depth' internal/upstream/output.go
grep -n 'Path: *path\|Path: *"/"' internal/upstream/upstream.go
```

Expected:

- (plan 15) `remoteid.RemoteId` has `Host string` and `Path string`, and **`Path`
  carries NO leading slash.** At HEAD, `internal/upstream/upstream.go:816` and `:830`
  write `Path: "/" + path`, so a naive `https://<Host>/<Path>` would double-slash.
  The second `grep` above must show `Path: path` (or equivalent), **not**
  `Path: "/" + path`. If it still shows `"/" + path`, **STOP** — plan 15 has not landed.
- (plan 16) `go doc ./internal/upstream Status` lists `StatusHasRelease`.
- (plan 17a) `go doc ./internal/upstream Status` lists `StatusCheckTag`.
- (plan 18) `UpstreamResult` has a `Depth int` field and the first `grep` finds the
  `strings.Repeat("  ", r.Depth)` indent in the Package cell.

**If any of these is missing, STOP and report.** The prerequisite plan has not landed.
Do not implement the missing piece yourself.

---

## Defects being fixed

| ID | Location | Defect | Verified |
|----|----------|--------|----------|
| D1 | `internal/upstream/output.go:82-83` | `"https://gitlab.com/" + sourceLabel[7:]` while the label is `GitLab:gitlab.gnome.org/GNOME/geary` | renders `https://gitlab.com/gitlab.gnome.org/GNOME/geary` |
| D2 | `internal/upstream/upstream.go:884-888` vs `output.go:70,90` | `resolveSourceLabel` emits `"Sourcehut:"` (lines 886 and 888); `SourceGlyph` and `SourceURL` test for `"SourceHut:"` | no glyph, no hyperlink, raw label |
| D3 | `internal/upstream/output.go:105-130` | `SourceCell` prefers the `r.SourceLabel` branch (`:116`) and re-derives the URL with the buggy `SourceURL()` (`:118`); the correct `r.Source` from `resolveSourceURL` (`upstream.go:895-912`) is only reached when the label is empty (`:127`) | — |
| D4 | `internal/upstream/output.go:153-158` | `renderCellHyperlink` emits OSC-8 unconditionally, including when stdout is not a TTY | piping shows raw `]8;;…` |
| D5 | `cmd/larry/upstream.go:119` | `RenderTable`'s error return is discarded — and `RenderTable` can never report one anyway (`output.go:213-214` is `fmt.Fprint(w, …); return nil`) | — |

Note: column alignment *with* OSC-8 was verified correct by the audit; the misalignment
seen when piping is a consequence of D4, not a width-accounting bug. Fix D4 and
re-verify before touching width logic.

---

## Target behaviour

### 1. `SourceInfo` replaces the two string fields

```go
// SourceInfo is computed once, in upstream.go, and carried on the result row.
// output.go renders it and NEVER re-derives a URL from a label.
type SourceInfo struct {
    Label string // "GitHub:owner/repo", "PyPI:name", "npm:@scope/name",
                 // "GitLab:gitlab.gnome.org/GNOME/geary", "Codeberg:owner/repo",
                 // "SourceHut:git.sr.ht/~tsdh/nirius"
    URL   string // canonical browse URL, already host-correct
    Glyph string // Nerd Font glyph, "" when unknown
}
```

**Migration — this is a breaking field change; do it in one pass:**

1. **DELETE** `UpstreamResult.Source string` and `UpstreamResult.SourceLabel string`
   (`internal/upstream/output.go:141-142`).
2. **ADD** `Source SourceInfo` in their place.
3. Update every writer in `internal/upstream/upstream.go` (grep `SourceLabel:` and
   `result.Source =` — at HEAD that is lines 41, 86-87, 101, 121, 132-133, 154, 253,
   266, 283, 297, 312, 323, 336, 345, 349, 358, 366-367, 1025, 1038, 1081, 1110, 1113,
   1129-1130; plans 16/17a/18 will have consolidated most of these into `resolveOne`).
4. Update `internal/cli/upstream_tui.go:141` (`source := r.Source` →
   `source := r.Source.Label`) and `nonTTYTable` at `:213` (`r.Source` →
   `r.Source.Label`).
5. The **Deprecated** branch in `SourceCell` (`output.go:109-115`) keeps using
   `r.Target` and now reads `r.Source.Label`:
   `r.Source.Label + " → " + r.Target` when `r.Target != ""`, else `r.Source.Label`.
   **No glyph and no hyperlink on a deprecated row.**

### 2. Provider label constants — one definition, two consumers

Define them once, exported, in `internal/upstream/output.go`, and use them in **both**
`resolveSourceLabel` (the producer) and the glyph table (the consumer), so they cannot
drift apart again (**D2**):

```go
const (
    LabelGitHub    = "GitHub:"
    LabelGitLab    = "GitLab:"
    LabelCodeberg  = "Codeberg:"
    LabelSourceHut = "SourceHut:"   // NOTE the capital H — the producer at
                                    // upstream.go:886,888 currently writes "Sourcehut:"
    LabelPyPI      = "PyPI:"
    LabelNPM       = "npm:"
)
```

### 3. Canonical URLs

| Provider | URL |
|---|---|
| GitHub | `https://github.com/<Owner>/<Repo>` |
| GitLab | `fmt.Sprintf("https://%s/%s", rid.Host, rid.Path)` |
| Codeberg | `https://codeberg.org/<Owner>/<Repo>` |
| SourceHut | `fmt.Sprintf("https://%s/%s", rid.Host, rid.Path)` — with plan 15's `Host == "git.sr.ht"` this yields `https://git.sr.ht/~tsdh/nirius` |
| PyPI | `https://pypi.org/project/<Name>/` |
| npm | `https://www.npmjs.com/package/<Name>` |

Both GitLab and SourceHut use the **same** `Host`/`Path` join. After plan 15, `Path`
has no leading slash, so the explicit `/` in the format string is required and correct.
Do not write `"https://" + Host + Path`.

### 4. The hyperlink gate (**D4**)

The previous revision of this plan said "gate on the colour/TTY decision already made in
`internal/output/color.go`". **There is no such reachable decision.** Measured:

- `RenderTable` does its own inline detection at `internal/upstream/output.go:162-169`
  (`LARRY_COLOR` then `termenv.DefaultOutput().ColorProfile()`).
- `output.ColorHelper` is constructed at `cmd/larry/root.go:46` and
  `internal/cli/upstream_tui.go:35,37`, and **is never passed into `internal/upstream`**.
- `ColorHelper.Detect()` (`internal/output/color.go:30-46`) keys off `NO_COLOR` plus the
  termenv profile — **not a TTY check** — and no-ops entirely unless `mode == "auto"`.
- `SourceCell(r UpstreamResult) string` and
  `RenderTable(w io.Writer, results []UpstreamResult) error` have no parameter that
  could carry such a decision, which makes two of the named tests below unwritable as
  previously specified.

**Do NOT import `internal/output` into `internal/upstream`.** Instead:

1. Change the two signatures to take the decision explicitly:

   ```go
   // SourceCell builds the Source cell. hyperlinks==false means plain text only:
   // no OSC-8 escape is emitted. This function reads no environment.
   func SourceCell(r UpstreamResult, hyperlinks bool) string

   // RenderTable renders results. hyperlinks is passed straight through to
   // SourceCell; it does not affect the status-column colouring, which keeps its
   // own detection below.
   func RenderTable(w io.Writer, results []UpstreamResult, hyperlinks bool) error
   ```

2. `RenderTable` **keeps its existing colour-profile block at `output.go:162-169`
   unchanged**, and keeps using `colorProfile` for the status column only.
3. Add one exported helper next to them, reusing exactly that logic, so production
   callers have something to pass:

   ```go
   // HyperlinksEnabled reports whether OSC-8 hyperlinks should be emitted.
   // Same rule as RenderTable's colour detection (output.go:162-169).
   func HyperlinksEnabled() bool {
       switch os.Getenv("LARRY_COLOR") {
       case "always":
           return true
       case "never":
           return false
       }
       return termenv.DefaultOutput().ColorProfile() != termenv.Ascii
   }
   ```

4. Callers: `cmd/larry/upstream.go:119` becomes
   `err := upstream.RenderTable(&buf, results, upstream.HyperlinksEnabled())` with the
   error **checked** (**D5**); `internal/cli/upstream_tui.go` passes `false` from
   `nonTTYTable` (it builds its own plain text) and `upstream.HyperlinksEnabled()`
   anywhere it calls `SourceCell`.

> **`--color always` does not reach this command.** `rootCmd.Flags().StringVar(&cfg.Color, …)`
> at `cmd/larry/root.go:65` is **non-persistent**, so `larry upstream --color always`
> fails with "unknown flag". Plan 13a owns that fix. **Gate on `LARRY_COLOR` only here**
> and do not try to plumb `cfg.Color` through.

### 5. `RenderTable` must be able to fail (**D5**)

`internal/upstream/output.go:213-214` is currently:

```go
fmt.Fprint(w, t.Render())
return nil
```

`lipgloss`'s `Render()` returns a plain `string` and cannot fail, so the only possible
error source is the writer. Change it to:

```go
_, err := fmt.Fprint(w, t.Render())
return err
```

That makes `TestRenderTable_ErrorPropagates` writable with a failing writer.

### 6. Status colours

Status labels and their colours stay as they are (the Go palette is a deliberate
improvement over the script's), with `HAS-RELEASE` (plan 16) and `CHECK-TAG` (plan 17a)
already added. This plan adds no new status.

---

## Tests (write first — every one must fail before you implement)

All in `internal/upstream/output_test.go` unless noted.

Helper used by several tests:

```go
type failWriter struct{}
func (failWriter) Write(p []byte) (int, error) { return 0, errors.New("boom") }
```

- `TestSourceURL_GitLabSelfHosted` (**D1**) — a `remoteid.RemoteId{Type: TypeGitLab,
  Host: "gitlab.gnome.org", Path: "GNOME/geary"}` produces
  `SourceInfo.URL == "https://gitlab.gnome.org/GNOME/geary"` and
  `SourceInfo.Label == "GitLab:gitlab.gnome.org/GNOME/geary"`.
- `TestSourceURL_GitLabDotCom` — `{Host: "gitlab.com", Path: "a/b"}` →
  `https://gitlab.com/a/b`. (Exactly two slashes after the scheme — assert
  `!strings.Contains(url, "//a/b")`.)
- `TestSourceLabel_SourceHutCasing` (**D2**) — `{Type: TypeSourceHut,
  Host: "git.sr.ht", Path: "~tsdh/nirius"}` →
  `Label == LabelSourceHut + "git.sr.ht/~tsdh/nirius"`,
  `URL == "https://git.sr.ht/~tsdh/nirius"`, `Glyph != ""`.
  **Assert against the `LabelSourceHut` constant, not a string literal**, so the
  producer and consumer cannot drift again.
- `TestSourceCell_UsesPrecomputedURL` (**D3**) — a row with
  `Source: SourceInfo{Label: "GitHub:a/b", URL: "https://example.test/x", Glyph: "G"}`
  and `hyperlinks == true` renders a cell containing `https://example.test/x` and
  **not** `https://github.com/a/b`.
- `TestSourceCell_NoHyperlinkWhenNotTTY` (**D4**) — same row,
  `SourceCell(r, false)` → the result contains no `"\033]8;;"`, and does contain the
  glyph and the short label.
- `TestSourceCell_HyperlinkWhenColorAlways` — `SourceCell(r, true)` → contains
  `"\033]8;;https://example.test/x"`.
- `TestSourceCell_Deprecated` — a row with `Status: StatusDeprecated`,
  `Source: SourceInfo{Label: "deprecated"}`, `Target: "dev-util/newname"`, and
  `hyperlinks == true` renders exactly `deprecated → dev-util/newname` and contains no
  `"\033]8;;"`.
- `TestSourceCell_DeprecatedNoTarget` — same but `Target: ""` → exactly `deprecated`.
- `TestHyperlinksEnabled` — table with `t.Setenv("LARRY_COLOR", …)`:
  `"always"` → `true`; `"never"` → `false`. (Do not assert the unset case; it depends
  on the test runner's terminal.)
- `TestRenderTable_ErrorPropagates` (**D5**) —
  `RenderTable(failWriter{}, []UpstreamResult{{Package: "a/b"}}, false)` returns a
  non-nil error whose message contains `boom`.
- `TestRenderTable_AllProviders` — the golden-file test. See below.

`cmd/larry/upstream_test.go` (**APPEND**; plan 16 created it and plan 18 already
appended):

- `TestUpstreamNoRawOSC8WhenColorNever` — `t.Setenv("LARRY_COLOR", "never")`,
  `execCmd(t, "upstream", "--overlay", ov, "-v")` → stdout contains no `"\033]8;;"`
  and no literal `"]8;;"`.

### The golden file

**Path:** `internal/upstream/testdata/table.golden`.
`go test`'s working directory is the package directory, so the test reads
`filepath.Join("testdata", "table.golden")`. **Not** `testdata/upstream/table.golden`,
and not a repo-root path.

```go
var update = flag.Bool("update", false, "update golden files")
```

Regenerate with `go test ./internal/upstream/ -run TestRenderTable_AllProviders -update`.

The committed golden is generated with **`hyperlinks = false`** and
**`t.Setenv("LARRY_COLOR", "never")`**, so it contains no escape sequences at all and is
diffable.

Fixture rows — one per provider plus one per interesting status, and **at least one
depth-1 row** (plan 18's `Depth` field; without it the golden does not prove the indent
survives this plan's changes):

| Package | Depth | Current | Latest | Status | Source.Label |
|---|---|---|---|---|---|
| `dev-util/alpha` | 0 | `1.0.0` | `1.1.0` | `StatusOutdated` | `GitHub:acme/alpha` |
| `dev-util/beta` | **1** | `2.0.0` | `2.0.0` | `StatusUpToDate` | `GitLab:gitlab.gnome.org/g/beta` |
| `dev-util/gamma` | 0 | `3.0.0` | `2.9.0` | `StatusAhead` | `Codeberg:acme/gamma` |
| `dev-util/delta` | 0 | `4.0.0` | `4.1.0` | `StatusNewerTag` | `SourceHut:git.sr.ht/~u/delta` |
| `dev-python/epsilon` | 0 | `5.0.0` | `5.0.0` | `StatusLive` | `PyPI:epsilon` |
| `dev-util/zeta` | 0 | `9999 (live)` | `6.0.0` | `StatusHasRelease` | `npm:@acme/zeta` |
| `dev-util/eta` | 0 | `7.0.0` | `nightly` | `StatusCheckTag` | `GitHub:acme/eta` |
| `dev-util/theta` | 0 | `8.0.0` | `` | `StatusDeprecated` | `deprecated` (Target `dev-util/iota`) |
| `dev-util/kappa` | 0 | `9.0.0` | `` | `StatusUnknown` | `` (Error `no provider match`) |

Fill each row's `Source.URL` and `Source.Glyph` from the canonical table in §3 so the
golden also pins the URL derivation.

---

## Implementation steps

Every step has a test above; write the test first and watch it fail.

1. Add the `Label*` constants and the `SourceInfo` type to
   `internal/upstream/output.go`. Rewrite `resolveSourceLabel`
   (`upstream.go:863-893`) and `resolveSourceURL` (`upstream.go:895-913`) to use the
   constants, fixing the `Sourcehut:`→`SourceHut:` casing at `:886` and `:888` and the
   `Host`/`Path` join. Add a `resolveSourceInfo(rid *remoteid.RemoteId) SourceInfo`
   that returns all three fields at once. (**D1, D2**)
2. Perform the `UpstreamResult` field migration in §1 — delete `Source string` and
   `SourceLabel string`, add `Source SourceInfo`, update every writer and the two
   `internal/cli/upstream_tui.go` readers.
3. Delete `SourceURL()` (`output.go:77-95`) and `SourceLabelShort()`
   (`output.go:97-103`) if the new `SourceInfo` makes them dead — `grep` first.
   Rewrite `SourceCell` to render `r.Source` directly, with the Deprecated branch as
   specified. (**D3**)
4. Add the `hyperlinks bool` parameter to `SourceCell` and `RenderTable`; add
   `HyperlinksEnabled()`. (**D4**)
5. Change `output.go:213` to `_, err := fmt.Fprint(w, t.Render()); return err`, and
   check the error at `cmd/larry/upstream.go:119`. (**D5**)
6. Add the golden-file test with the `-update` flag guard; generate and commit
   `internal/upstream/testdata/table.golden`.
7. Run the suites; fix fallout.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**. Read that section before running
> anything.

### Tier 1 — automated, required. Run these. This is the real gate.

```bash
go build ./...
go vet ./...                 # internal/git/git.go:318 is the known baseline finding
go test ./... -count=1       # no NEW failures vs. your recorded baseline
golangci-lint run ./...
```

Targeted:

```bash
go test ./internal/upstream/... ./internal/cli/... ./cmd/... -count=1
git status --short internal/upstream/testdata/table.golden   # committed, not ignored
```

Grep assertions (all must print nothing):

```bash
grep -rn 'func SourceURL\|SourceLabel' --include=*.go ./internal ./cmd
grep -rn '"Sourcehut:"' --include=*.go .
grep -rn 'https://gitlab.com/" *+' --include=*.go .
grep -rn 'internal/output' --include=*.go ./internal/upstream
```

And these must print something:

```bash
grep -n 'func HyperlinksEnabled' internal/upstream/output.go
grep -n 'hyperlinks bool' internal/upstream/output.go     # on BOTH SourceCell and RenderTable
grep -c 'Depth' internal/upstream/testdata/table.golden || true   # informational
```

### Tier 2 — read-only manual. Safe to run, but **MANUAL** — report results, do not
treat a failure here as blocking.

Needs `~/Developer/gentoo-overlay` and network. A failure may be rate-limiting rather
than a defect; re-run before concluding.

```bash
LARRY_COLOR=never larry upstream --overlay ~/Developer/gentoo-overlay -v | cat
# expect: no raw ]8;; sequences anywhere

LARRY_COLOR=always larry upstream --overlay ~/Developer/gentoo-overlay -v
# expect: clickable links in a terminal that supports OSC-8
```

- A GitLab row links to its real host, not `gitlab.com`.
- `gui-apps/nirius` renders a `SourceHut:` label with a glyph and a working link.
  (Confirm the package still exists in the overlay rather than assuming it.)

### Tier 3 — DO NOT RUN.

Nothing in this plan is Tier 3.

---

## Out of scope

Which version is reported (plans 16, 17a, 17b). `UpstreamResult.Depth` and the Package
cell indent (plan 18 — already landed; do not touch the Package cell). Making
`--color` a persistent flag (plan 13a).

**The script's header underline** (`sed -e '1{p;s/./─/g;}'`, L1585) **is intentionally
not reproduced. The lipgloss border supersedes it. Do not add one.** This is recorded
here so a future reader does not "restore" it; there is no work item.

Do not fix unrelated bugs you notice; report them.
