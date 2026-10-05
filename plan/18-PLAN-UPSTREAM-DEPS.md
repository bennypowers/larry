# 18 — `upstream`: recursive in-overlay dependency scanning

**Why:** The script walks each package's `DEPEND`/`RDEPEND`, and for every dependency
that also lives in the overlay it recurses and prints an indented row. Go has a
`dependencies.go` that is never called — and would not work if it were, because it
fabricates the dependency's version as `1.0.0`.

**Precondition:** 12 (`internal/atom`), 13a (`cmd/larry` test harness), 16 (discovery
must be correct first).

**Ordering: plan 18 runs BEFORE plan 19.** This plan owns `UpstreamResult.Depth` and the
indent applied to the **Package** cell. Plan 19 owns the **Source** cell, the hyperlink
gate and the golden file, and its golden must contain a depth-1 row produced by this
plan. Do not reorder them.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent on this machine, this
plan's text is authoritative — do not guess at the script's behaviour.**
Functions: `parse_upstream_dependencies` (L1315–1317), `is_in_overlay` (L1324–1329),
the recursion tail of `check_package` (L1504–1511), `CHECKED_PACKAGES`
(L28, L1391–1394), the indent at L1403 and the `printf` at L1499.

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. Known-red baseline: exactly
> `TestExtractVar/multi_line` in `internal/prepare`, plus one `go vet` finding at
> `internal/git/git.go:318`. Do not chase either.

**Line numbers below are as measured at HEAD on 2026-10-05, before plan 16 lands.**
Plan 16 rewrites `findEbuilds` and `walkAndCheckCategories`, so line numbers in
`internal/upstream/upstream.go` will have moved. **Locate by symbol name, not by line.**
`internal/upstream/dependencies.go` is untouched by plan 16; its line numbers are exact.

---

## Precondition check — run these first

```bash
go doc ./internal/atom Atom
go doc ./internal/atom ParsedEbuild
go doc ./internal/atom ParseAtom
go doc ./internal/atom ListEbuilds
go doc ./internal/atom FindLatest
go doc ./internal/atom ErrNoEbuilds
go doc ./internal/upstream RunWithTargets
go doc ./internal/upstream Status
grep -n 'func execCmd\|func fixtureOverlay' cmd/larry/testhelp_test.go
ls cmd/larry/upstream_test.go
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
    Category string // "dev-util"
    PN       string // "fish-lsp" — from the PARENT DIRECTORY NAME, never the filename
    PVR      string // "1.1.3-r2"
    PV       string // "1.1.3"
    Revision int    // 2 (0 when absent)
    P        string // "fish-lsp-1.1.3"
    PF       string // "fish-lsp-1.1.3-r2"
    Live     bool   // PV == "9999"
}

func ParseAtom(overlay, atom string) (*Atom, error)
func ListEbuilds(dir string) ([]*ParsedEbuild, error)
func FindLatest(dir string) (*ParsedEbuild, error)
var ErrNoEbuilds = errors.New("no ebuilds found")
```

Expected (plan 16): `func RunWithTargets(...)` exists (the old name was
`RunWithCategories`), and `Status` lists `StatusHasRelease`.

Expected (plan 13a): `cmd/larry/testhelp_test.go` declares

```go
func execCmd(t *testing.T, args ...string) (stdout, stderr string, err error)
func fixtureOverlay(t *testing.T, remotes map[string]string, ebuilds map[string]string) string
```

Expected (plan 16): `cmd/larry/upstream_test.go` **already exists**. Plan 16 CREATES
that file; **this plan APPENDS to it.** Do not recreate it and do not clobber plan 16's
tests.

**If any of these is missing, STOP and report.** The prerequisite plan has not landed.
Do not implement the missing piece yourself.

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D1 | `cmd/larry/upstream.go`, `internal/upstream/upstream.go` | `ScanDependencies` has **no non-test callers** |
| D2 | `internal/upstream/upstream.go` `ParseEbuildInfo` | never populates `DEPEND`/`RDEPEND`, so `extractAtoms(ei.DEPEND)` always receives `""` |
| D3 | `internal/upstream/dependencies.go:222-236` | `resolveDepEbuild` **fabricates** an ebuild with hardcoded `PVVal: "1.0.0"` and empty deps instead of reading the real one from the overlay |
| D4 | `internal/upstream/dependencies.go:257-267` | `buildRemoteIdForProvider` builds a RemoteId from the package *name* only (`Name: ei.Pkg`), ignoring SRC_URI → every GitHub/GitLab dependency fails |
| D5 | `internal/upstream/dependencies.go:121-124` | `depVisited` is computed and then unused; the recursive call at `:125` goes to `scanDependencies`, which creates a **fresh** `visited` map at `:50-52` → dedup does not propagate |
| D6 | `internal/upstream/dependencies.go:31,56,120,125` | `scanDependencies` takes `maxDepth` while `buildDepNode` tracks `level`, and the recursive call at `:125` resets level to 1 — depth limiting is broken |
| D7 | — | no `is_in_overlay` filter; the script only recurses into packages present in the overlay (L1507) |
| D8 | `internal/upstream/dependencies.go:289-304` | `MergeDepStatuses` has no production callers |
| D9 | `internal/upstream/dependencies.go:162` | the atom regex `([a-z][a-z0-9_-]+/[a-z][a-z0-9_.-]*)` cannot match `dev-libs/libstdc++` (no `+` in the class) |

---

## Target behaviour

### Step 0 — extract `resolveOne` (DO THIS FIRST, with NO behaviour change)

The previous revision of this plan said "reuse the top-level per-package resolution
function". **No such function exists.** The logic is inlined in
`walkAndCheckCategories` (`internal/upstream/upstream.go:968-1143`, ~175 lines of
provider lookup, cache, error rows and `buildResult`).

Extract it verbatim into:

```go
// resolveOne turns one package's ebuild into exactly one UpstreamResult row.
// It is the single source of truth for provider lookup, caching, error rows and
// status derivation, used by both the top-level walk and the dependency scanner.
func resolveOne(
    overlayRoot string,
    eb *atom.ParsedEbuild,
    registry *provider.ProviderRegistry,
    allowPrereleases bool,
    cache *VersionCache,
    depth int,
) UpstreamResult
```

Rules for the extraction:

- **Behaviour must not change.** Every `continue`-with-an-error-row in the current body
  becomes `return UpstreamResult{… Status: StatusUnknown, Error: …, Depth: depth}`.
- Set `Depth: depth` on every returned row, including the error rows.
- If plan 16 introduced a `PackageTarget` with a `LiveOnly` flag, **leave the live-only
  branch in the caller** and pass `target.Ebuild` into `resolveOne`. `resolveOne` only
  handles versioned ebuilds. Say so in its doc comment.
- The deprecated-package branch stays in the caller too — it is keyed off the
  `deprecated` map, which the dependency scanner does not consult.
- **The existing `TestRunWithCategories_*` tests (whatever plan 16 left of them) and
  every `cmd/larry/upstream_test.go` test from plan 16 must stay green across this
  extraction, with no edits.** That is the proof the refactor was behaviour-preserving.
  Run them before and after step 0 and say so in your report.

### `ParseEbuildInfo` populates the DEPEND family

The script greps `^\s*(R|B)?DEPEND=` (L1316). Fill `DEPEND`, `RDEPEND` **and**
`BDEPEND` using the existing multi-line `extractVar` in
`internal/upstream/upstream.go`. (**D2**)

> The multi-line `extractVar` intentionally finds **more** atoms than the script's
> single-line `grep`: the script only ever sees the first physical line of a
> multi-line `RDEPEND="..."`, so it misses most real dependencies. **This is a
> deliberate improvement. Do NOT narrow `extractVar` to match the script.** Any test
> you write must expect the full multi-line content.

### Atom extraction (**D9** + the version-stripping fix)

The script's regex is `[a-z0-9-]+/[a-z0-9_-]+` (L1316). **Applied to
`>=dev-libs/foo-1.2.3:0=[bar]` it yields `dev-libs/foo-1`, not `dev-libs/foo`** — the
class has no `.`, so the match stops at the first dot. The script genuinely produces
`dev-libs/foo-1`. The previous revision of this plan claimed the script's regex yields
`dev-libs/foo`; that was wrong.

Fix, in two parts, and **deliberately improve on the script**:

1. Widen the class so real package names survive:

   ```go
   // depAtomRe is the script's `[a-z0-9-]+/[a-z0-9_-]+` (L1316) widened with
   // '+', '.' and uppercase-free name chars, so that dev-libs/libstdc++ and
   // dev-python/zope.interface are matched instead of truncated.
   var depAtomRe = regexp.MustCompile(`[a-z0-9-]+/[a-z0-9_+.-]+`)
   ```

2. Post-process every match through an **anchored** version stripper. Repair the
   existing `stripVersion` helper at `internal/upstream/dependencies.go:186-220` in
   place — keep its name and its leading slot-strip, delete the hand-rolled backwards
   loop, and use:

   ```go
   // depVerRe matches a trailing "-<version>" on a dep atom: a hyphen, then a
   // digit, then anything that is not a path separator, to end of string.
   var depVerRe = regexp.MustCompile(`-[0-9][^/]*$`)
   ```

   `stripVersion(">=dev-libs/foo-1.2.3:0")` → slot strip → `>=dev-libs/foo-1.2.3` →
   `dev-libs/foo` (after the existing `TrimLeft(…, "<>=")` at `:169`).

   Known over-strip: a package whose name genuinely ends in `-<digits>`
   (e.g. `media-fonts/font-adobe-100dpi`) becomes `media-fonts/font-adobe`. That atom
   will not exist as an overlay directory, so the in-overlay filter drops it and
   nothing is printed. **Accept this; do not add a heuristic.** Note it in a comment.

Output is deduplicated and sorted (script's `sort -u`).

### Recursion, depth and dedup

**Depth wording — state it once, apply it once:**

> The root package is **depth 0**. A package at **depth < 3 recurses** into its
> in-overlay dependencies. A package at **depth 3 is printed but does not recurse**.
> **The maximum printed depth is 3.** (Script L1504: `if test $depth -lt 3`.)

- Recurse only into atoms that exist as a directory under the overlay —
  `atom.ParseAtom(overlayRoot, a)` succeeding is the test (**D7**).
- A **single** `visited map[string]bool` is threaded through the entire walk, created
  once at the top level — the script's global `CHECKED_PACKAGES`. A package already
  emitted at any depth is never emitted again (**D5**). Key it on `category/pn`.
- One depth mechanism, not two: carry `depth int` downwards and compare against the
  constant `maxDepDepth = 3`. Delete `maxDepth` and `level` (**D6**).

### `DepNode` is DELETED

Delete the `DepNode` struct (`dependencies.go:15-22`), `ScanDependencies`'s current
signature (`:27-29`), `scanDependencies` (`:31-61`), `buildDepNode` (`:63-129`),
`resolveDepEbuild` (`:222-236`), `buildRemoteIdForProvider` (`:257-267`) and
`MergeDepStatuses` (`:289-304`, **D8**). Also delete `compareVersions` (`:238-255`) and
`providerTypeToRemoteId` (`:269-287`) **iff** nothing else references them after the
rewrite (`grep` before deleting).

**There is no tree.** The scanner returns a flat `[]UpstreamResult` in **pre-order**
with `Depth` set:

```go
// ScanDependencies returns a flat, pre-order slice of rows for every in-overlay
// dependency of eb, recursively. The parent's own row is NOT included. Depth is set
// on every row: the parent is parentDepth, its direct dependencies are parentDepth+1.
// visited is shared across the whole walk and is mutated; a package already present
// in it is skipped entirely (the script's CHECKED_PACKAGES).
func ScanDependencies(
    overlayRoot string,
    eb *atom.ParsedEbuild,
    registry *provider.ProviderRegistry,
    allowPrereleases bool,
    cache *VersionCache,
    parentDepth int,
    visited map[string]bool,
) []UpstreamResult
```

Each child row is produced by `resolveOne(overlayRoot, childEbuild, registry,
allowPrereleases, cache, parentDepth+1)` — **the same function the top level uses**, so
the dep's real current version and real SRC_URI-derived provider are used
(**D3, D4**). The child's ebuild comes from `atom.FindLatest(a.PkgDir)` with the same
`ErrNoEbuilds`→live fallback plan 16 specified.

### `UpstreamResult.Depth` and the Package cell — THIS PLAN OWNS BOTH

1. Add `Depth int` to `UpstreamResult` (`internal/upstream/output.go:136-145`).
2. In `RenderTable`'s row loop (`internal/upstream/output.go:208-211`), build the
   Package cell as `strings.Repeat("  ", r.Depth) + r.Package` — `2 * depth` leading
   spaces, matching the script's `string repeat -n (math $depth \* 2) " "` (L1403) and
   `printf '%s%s\t…' "$indent" "$atom"` (L1499).
3. Do the same in `internal/cli/upstream_tui.go`'s `nonTTYTable` (`:201-215`) and in the
   TUI row at `:141`.

Plan 19 will change `RenderTable`'s **signature** and its **Source** cell. It must not
touch the Package cell. Leave a comment saying so.

### Call site

In the top-level walk, after appending the parent's row:

```go
row := resolveOne(overlayRoot, target.Ebuild, registry, allowPrereleases, cache, 0)
results = append(results, row)
results = append(results, ScanDependencies(overlayRoot, target.Ebuild,
    registry, allowPrereleases, cache, 0, visited)...)
```

`visited` is created once in `RunWithTargets` and passed down; seed it with each
top-level package as it is emitted so a dependency that is also a top-level package is
printed exactly once (**D5**, script L1391–1394). (**D1**)

---

## Tests (write first — every one must fail before you implement)

### `internal/upstream/dependencies_test.go` — REWRITE

The existing 18 test functions pass against the fabricated `1.0.0` path and the
`DepNode` tree. **All of them go.** Delete `TestDepNode_RootNode`,
`TestRecursiveScan_3Levels`, `TestRecursiveScan_CircularRef`,
`TestRecursiveScan_NoCycleInfinite`, `TestScanDEPEND`, `TestScanRDEPEND`, and the four
`TestMergeDepStatus_*`. Keep and extend the `TestExtractAtoms_*` group. **List every
deletion in your final report.**

`TestExtractAtoms` — one table, exact inputs and outputs:

| Input | Expected (sorted, deduped) |
|---|---|
| `>=dev-libs/foo-1.2.3:0=[bar]` | `["dev-libs/foo"]` |
| `\|\| ( a-cat/b c-cat/d )` | `["a-cat/b", "c-cat/d"]` |
| `python? ( dev-python/x )` | `["dev-python/x"]` |
| `dev-libs/foo dev-libs/foo` | `["dev-libs/foo"]` |
| `dev-libs/libstdc++` | `["dev-libs/libstdc++"]` (**D9**) |
| `dev-python/zope.interface` | `["dev-python/zope.interface"]` (**D9**) |
| `app-text/docbook-xml-dtd` | `["app-text/docbook-xml-dtd"]` |
| `>=dev-lang/python-exec-2.4.6` | `["dev-lang/python-exec"]` |
| `sys-libs/db-5.3:=` | `["sys-libs/db"]` |
| `""` | `nil` |

- `TestParseEbuildInfo_PopulatesDepends` (**D2**) — an ebuild whose body is

  ```
  EAPI=8
  DESCRIPTION="Test"
  SLOT="0"
  DEPEND="dev-libs/foo"
  RDEPEND="${DEPEND}
      dev-libs/bar
      dev-libs/baz"
  BDEPEND="dev-util/qux"
  ```

  Expect `ei.DEPEND == "dev-libs/foo"`, `ei.BDEPEND == "dev-util/qux"`, and `ei.RDEPEND`
  containing **all three** of `foo`-reference, `dev-libs/bar`, `dev-libs/baz` (the
  multi-line value, not just the first line).

Fixture-overlay convention for the rest of this group: a `t.TempDir()` overlay with
`profiles/repo_name`, package dirs created with `os.MkdirAll`, and a fake provider
registered for whichever `remoteid` type the fixture's SRC_URI implies.

- `TestScanDependencies_ReadsRealEbuild` (**D3**) — overlay with
  `app-misc/root/root-1.0.0.ebuild` (`RDEPEND="dev-libs/dep"`) and
  `dev-libs/dep/dep-2.5.0.ebuild`. Expect exactly one returned row:
  `Package == "dev-libs/dep"`, `Current == "2.5.0"` — **not `1.0.0`** — and
  `Depth == 1`.
- `TestScanDependencies_UsesDepSrcUri` (**D4**) — same shape, but
  `dev-libs/dep/dep-2.5.0.ebuild` has
  `SRC_URI="https://github.com/acme/widget/archive/v${PV}.tar.gz"`.
  Register a **recording fake provider** for `remoteid.TypeGitHub`:

  ```go
  type recordingProvider struct{ got []remoteid.RemoteId }
  func (p *recordingProvider) FetchLatestVersion(rid remoteid.RemoteId, _ bool) (*version.Version, error) {
      p.got = append(p.got, rid)
      return version.Parse("3.0.0")
  }
  // Type() -> remoteid.TypeGitHub, Name() -> "rec", Metadata() -> provider.ProviderMetadata{Name:"rec"}
  ```

  **Assertion:** `p.got` contains exactly one entry and it equals
  `remoteid.RemoteId{Type: remoteid.TypeGitHub, Owner: "acme", Repo: "widget"}`
  — i.e. derived from the dependency's own SRC_URI. It must **not** be
  `remoteid.RemoteId{Type: remoteid.TypeGitHub, Name: "dep"}`, which is what
  `buildRemoteIdForProvider` (`dependencies.go:257-267`) produces today.
- `TestScanDependencies_SkipsOutOfOverlay` (**D7**) — `app-misc/root` depends on
  `sys-libs/glibc`, which has no directory in the fixture overlay. Expect zero returned
  rows.
- `TestScanDependencies_DepthLimit` (**D6**) — chain
  `a/a-1.0 → b/b-1.0 → c/c-1.0 → d/d-1.0 → e/e-1.0`, each in its own category
  (`c1/a`, `c2/b`, `c3/c`, `c4/d`, `c5/e`), each `RDEPEND`ing on the next. Call
  `ScanDependencies` with `parentDepth == 0` rooted at `c1/a`. Expect exactly three
  rows, in this order: `c2/b` (Depth 1), `c3/c` (Depth 2), `c4/d` (Depth 3). `c5/e`
  is **absent**.
- `TestScanDependencies_PreOrder` — `root → {x, y}`, `x → x1`. Expect the row order
  `x` (1), `x1` (2), `y` (1). Pins pre-order, not breadth-first.
- `TestScanDependencies_GlobalDedup` (**D5**) — diamond: `root → {x, y}`, and both
  `x` and `y` depend on `z`. Expect `z` to appear **exactly once** in the returned
  slice.
- `TestScanDependencies_Cycle` — `a → b → a`. Terminates, and `a` does not appear in
  the returned rows at all (it is seeded into `visited` by the caller; seed it in the
  test too).

### `internal/upstream/output_test.go`

- `TestRenderTable_Indentation` — three rows with `Package` `p/a`/`p/b`/`p/c` and
  `Depth` 0/1/2. Render with `t.Setenv("LARRY_COLOR", "never")`. The output contains
  the substrings `"p/a"`, `"  p/b"` and `"    p/c"`. (Assert on the two-space and
  four-space prefixes specifically, not on full-line equality — the lipgloss border
  adds padding.)

### `cmd/larry/upstream_test.go` — **APPEND to plan 16's file**

- `TestUpstreamIncludesDependencyRows` (**D1**) — a fixture overlay where
  `app-misc/root-1.0.0` RDEPENDs on `dev-libs/dep-2.5.0`, both present.
  `t.Setenv("LARRY_COLOR", "never")`, then
  `execCmd(t, "upstream", "--overlay", ov, "-v")`.
  stdout contains a line matching `^\s*\|\s+dev-libs/dep\b` **indented two spaces
  further than** the `app-misc/root` line, and `dev-libs/dep` appears exactly once.

---

## Implementation steps

Every step has a test above; write the test first and watch it fail.

0. **Extract `resolveOne`** per "Step 0". No behaviour change. Checkpoint:
   `go test ./internal/upstream/... ./cmd/... -count=1` must be exactly as green as
   before you started. **If budget is running short, stop here and hand back** —
   steps 1–8 are a clean second session.
1. Extend `ParseEbuildInfo` to fill `DEPEND`/`RDEPEND`/`BDEPEND` via the multi-line
   `extractVar`. (**D2**)
2. Widen `depAtomRe` and repair `stripVersion` with the anchored `depVerRe`. (**D9**)
3. Add `Depth int` to `UpstreamResult`; apply the `2 * Depth` indent in
   `internal/upstream/output.go`'s Package cell and in
   `internal/cli/upstream_tui.go` (`:141` and `nonTTYTable` at `:201-215`).
4. Rewrite `dependencies.go`: delete `DepNode` and the six/eight functions listed under
   "`DepNode` is DELETED"; add the new flat `ScanDependencies`. (**D3, D4, D5, D6,
   D7, D8**)
5. Thread one `visited` map from `RunWithTargets` through the whole walk.
6. Call `ScanDependencies` from the top-level walk, appending child rows immediately
   after their parent. (**D1**)
7. Rewrite `dependencies_test.go` per the Tests section.
8. Run the suites; fix fallout; hand back a list of every deleted function and test.

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
go test ./internal/upstream/... ./cmd/... -count=1
```

Grep assertions (all must print nothing):

```bash
grep -rn 'DepNode\|MergeDepStatuses\|resolveDepEbuild\|buildDepNode\|buildRemoteIdForProvider' --include=*.go .
grep -rn 'PVVal:    "1.0.0"\|PVVal: "1.0.0"' --include=*.go ./internal/upstream
```

And this must print something:

```bash
grep -n 'func resolveOne\|func ScanDependencies\|Depth' internal/upstream/dependencies.go internal/upstream/output.go
```

### Tier 2 — read-only manual. Safe to run, but **MANUAL** — report results, do not
treat a failure here as blocking.

Needs `~/Developer/gentoo-overlay` and network. A failure may be rate-limiting rather
than a defect; re-run before concluding.

```bash
larry upstream --overlay ~/Developer/gentoo-overlay -v
```

- Indented rows appear beneath at least one package. The overlay has intra-overlay
  deps — e.g. `sci-ml/lemonade` → `acct-user/lemonade`,
  `media-gfx/comfyui` → `dev-python/comfyui-*`. Confirm the exact pairs rather than
  assuming them; the overlay changes.
- No package appears twice anywhere in the output:

  ```bash
  larry upstream --overlay ~/Developer/gentoo-overlay -v \
    | grep -oE '[a-z0-9-]+/[A-Za-z0-9_+.-]+' | sort | uniq -d
  # expect: no output
  ```

- Dependency rows show the dependency's **real** current version, never `1.0.0`.
- No row is indented more than six spaces (depth 3 × 2).

### Tier 3 — DO NOT RUN.

Nothing in this plan is Tier 3.

---

## Out of scope

Provider correctness (plans 15, 17a, 17b). The **Source** cell, the hyperlink gate,
`RenderTable`'s signature change and the golden file — all **plan 19**, which runs after
this one. Do not add a `hyperlinks bool` parameter here. Do not fix unrelated bugs you
notice; report them.
