# 16 — `upstream`: package discovery, filtering & upstream names

**Why:** `larry upstream` silently drops packages. Any revbumped package disappears; any
package with both a released and a live ebuild is reported as LIVE and hidden; the
documented positional filters do nothing; and PyPI/npm names ignore the `MY_PN` and
custom-variable forms the script supports.

**Precondition:** 12 (`internal/atom`), 13a (`cmd/larry` test harness), 14 (version),
15 (remote-id).

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent on this machine, this
plan's text is authoritative — do not guess at the script's behaviour.**
Functions: `find_all_ebuilds` (L899–901), `find_latest_versioned_ebuild` (L903–917),
`check_package` (L1385–1512), `do_upstream` (L1530–1594), `get_pypi_name` (L1034–1047),
`get_npm_name` (L937–958), `expand_pn` (L1021–1032),
`should_skip_package` (L1352–1358).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. In particular: the known-red baseline is
> exactly `TestExtractVar/multi_line` in `internal/prepare` plus one `go vet` finding at
> `internal/git/git.go:318`. Do not chase either.

---

## Precondition check — run these first

```bash
go doc ./internal/atom Atom
go doc ./internal/atom ParsedEbuild
go doc ./internal/atom ParseAtom
go doc ./internal/atom ListEbuilds
go doc ./internal/atom FindLatest
go doc ./internal/atom ErrNoEbuilds
go doc ./internal/version Compare
go doc ./internal/remoteid RemoteId
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

Expected (plan 14): `func Compare(a, b string) int`.
Expected (plan 15): `remoteid.RemoteId` has `Host` and `Path` fields, and `Path` carries
**no** leading slash.
Expected (plan 13a): `cmd/larry/testhelp_test.go` declares

```go
func execCmd(t *testing.T, args ...string) (stdout, stderr string, err error)
func fixtureOverlay(t *testing.T, remotes map[string]string, ebuilds map[string]string) string
```

**If `cmd/larry/testhelp_test.go` is absent, STOP** — plan 13a has not landed and the
`cmd/larry/upstream_test.go` half of this plan cannot be written.

**If any of these is missing, STOP and report.** The prerequisite plan has not landed.
Do not implement the missing piece yourself.

---

## Defects being fixed

| ID | Location | Defect | Verified |
|----|----------|--------|----------|
| D1 | `internal/upstream/upstream.go:385-430` | `findEbuilds` splits at the last `-`, so `bar-3.0.0-r1.ebuild` → version `r1` → `version.Parse` fails → `continue` → package dropped entirely | a dir with only `bar-3.0.0-r1.ebuild` yields zero ebuilds |
| D2 | `internal/upstream/upstream.go:415-421` | keeps the highest version per package; `9999` wins → row is LIVE and hidden by the default filter | dir with `foo-2.0.0-r1` + `foo-9999` → only LIVE |
| D3 | `cmd/larry/upstream.go:73-82` | `cobra.MaximumNArgs(1)` with `if len(args) > 1 { categories = args[1:] }` → `categories` is always empty; the one allowed positional is never used as an overlay path either | `larry upstream dev-util/cem` silently checks everything |
| D4 | `cmd/larry/upstream.go:44,68` | `Use:`/`Long` advertise bare categories; the script filters by `cat/pkg` **atom** (`check_package -a atom`, L1385) | — |
| D5 | `internal/upstream/upstream.go:426-429` | rows come from map iteration → non-deterministic order | consecutive runs reorder |
| D6 | `internal/upstream/upstream.go:776-785` | PyPI name uses `PYPI_PN` only; no `MY_PN` fallback (script L1041–1045) | `dev-python/python-nmcli` (`MY_PN="nmcli"`) queries `python-nmcli`, reports latest `0.1.1` vs current `1.8.0` |
| D7 | `internal/upstream/upstream.go:1152-1194` | npm name expands only `${MY_NPM_NAME}`, `${MY_NPM_SCOPE}`, `${PN}`; the script expands **every** `^[A-Z_]+="…"` plus `${PV}`/`${P}` (L948–956) | — |
| D8 | `internal/upstream/upstream.go:1163-1173` | the no-`/-/` fallback truncates at the first `/`, breaking `@scope/name` | — |
| D9 | `internal/upstream/upstream.go:1032-1041` | live-only packages emit `Current: ""`, never fetch upstream; no `HAS-RELEASE` status (script L1405–1419) | `app-shells/zoxide` shows blank current |
| D10 | `internal/upstream/upstream.go:640-646`, called at `:1000` | `shouldSkip` returns two booleans that are always equal | — |
| D11 | `internal/upstream/upstream.go:185-206` | exported `Run()` passes `registry: nil` → every result UNKNOWN; no production callers | — |

---

## Target behaviour

### Discovery (mirrors `find_latest_versioned_ebuild`, script L903–917)

For each `<overlay>/<category>/<package>/` directory:

1. Call `atom.FindLatest(pkgDir)`.
2. On success, that `*atom.ParsedEbuild` is the row's ebuild; it is not live-only.
3. **`FindLatest` returns `ErrNoEbuilds` when the only ebuilds are live, and it does
   NOT return the live ebuild.** So on `errors.Is(err, atom.ErrNoEbuilds)`, call
   `atom.ListEbuilds(pkgDir)` and take the entry whose `Live == true` (there is at most
   one in practice; if several, take the last in `ListEbuilds` order). Mark the target
   live-only.
4. On any other error, or when `ListEbuilds` yields nothing, skip the directory.

Skip `acct-group/*` and `acct-user/*` (already correct). Emit rows in ascending
`category/package` string order, computed with `sort.Slice` over the target list —
never map iteration (**D5**).

### Live handling (script `check_package` L1405–1419)

- live-only **and not** verbose → omit the row entirely.
- live-only **and** verbose → still query upstream.
  Set `Current` to the literal string `9999 (live)` **in `upstream.go` when building the
  row**. `UpstreamResult` gets no new field for this; `internal/upstream/output.go`
  needs no change for it.
  Status is `StatusHasRelease` when the provider returned a version, else `StatusLive`.
- live **plus** a versioned sibling → `FindLatest` already returned the versioned one;
  check it normally.

### Positional arguments

`larry upstream [cat/pkg ...]`.

**This behaviour has never been implemented.** `cmd/larry/upstream.go:76` reads only
`--overlay` and the args slice is dropped on the floor. The `Long` help at
`cmd/larry/upstream.go:52-56` and `:71` already promises
`larry upstream /path/to/overlay`, so this is a first implementation of a documented
promise, not a restoration.

Classification rule — **state it once, apply it once**:

> An argument is the **overlay root** iff `os.Stat` says it is a directory **and** it
> contains a `profiles/` subdirectory. At most one such argument is allowed; a second
> one is an error (`"multiple overlay paths given: %q and %q"`). **Every other argument
> is a `category/package` atom** and is resolved with `atom.ParseAtom(overlayRoot, arg)`.

Resolution order: scan all args for the overlay root first, then resolve the atoms
against it, so `larry upstream dev-util/cem /path/to/overlay` works regardless of order.
`Args:` becomes `cobra.ArbitraryArgs`.

An atom that `atom.ParseAtom` rejects is a hard error returned from `RunE` — never a
silent skip. The error must name the atom.

### Bare-category filtering is DROPPED — this is deliberate

`RunWithCategories` (`internal/upstream/upstream.go:952`) →
`walkAndCheckCategories` (`:968`, the `if len(categories) > 0` block at `:983-995`)
implements real, working per-category filtering, and it has a green test:
`TestRunWithCategories_FilterByCategory` at `internal/upstream/upstream_test.go:519-542`.

Renaming the parameter to "atoms" **deletes a working feature and breaks that test.**
That is the intended outcome, because the script has no bare-category mode — it only
ever takes atoms (`check_package -a atom`). So:

- Positional args are `cat/pkg` atoms **only**. Bare categories are no longer accepted.
- Rename `RunWithCategories` → `RunWithTargets` and change the `categories []string`
  parameter to `targets []*atom.Atom`.
- **Update `Use:` at `cmd/larry/upstream.go:44`** from `"upstream [categories...]"` to
  `"upstream [cat/pkg...] [overlay-path]"`.
- **Update the `Long` examples at `:66-71`**: replace
  `larry upstream dev-python dev-lisp  # Check specific categories` with
  `larry upstream dev-util/cem         # Check one package`.
- **DELETE `TestRunWithCategories_FilterByCategory`** (`upstream_test.go:519-542`). Its
  coverage is replaced by `TestUpstreamPositionalAtomFilter` below. Say so in your
  report.

### PyPI name (script `get_pypi_name` L1034–1047)

`PYPI_PN` → `MY_PN` → PN, each passed through the existing `expandVar`
(`internal/upstream/upstream.go:532-543`), which already handles `${PN}`, `${PN//-/_}`,
`${PN/-/_}` and the unbraced spellings.

### npm name (script `get_npm_name` L937–958)

**Do not use `ebuild.ExpandVariables` with `ebuild.BuildVariableMap`.**
`ebuild.BuildVariableMap(info *EbuildInfo)` (`internal/ebuild/srcuri.go:178-204`) scans
**only `info.SRC_URI`** for variable assignments. A `MY_SCOPE="@acme"` declared on its
own ebuild line is invisible to it, so `TestNpmName_CustomVar` below **cannot** pass
that way.

Build the map locally in `internal/upstream` instead:

```go
// npmAssignRe mirrors the script's `grep -E '^[A-Z_]+="[^"]*"'`, widened to allow
// digits in the variable name (MY_NPM_2 etc).
var npmAssignRe = regexp.MustCompile(`(?m)^([A-Z][A-Z0-9_]*)="([^"]*)"$`)

func npmVarMap(ei *ebuild.EbuildInfo) ebuild.VariableMap {
    vars := make(ebuild.VariableMap)
    for _, m := range npmAssignRe.FindAllStringSubmatch(ei.Raw, -1) {
        vars[m[1]] = m[2]
    }
    // Core vars last so they always win, matching the script's ordering (L953-955).
    vars["PN"] = ei.Pkg
    vars["PV"] = ei.PVVal
    vars["P"] = ei.Pkg + "-" + ei.PVVal
    return vars
}
```

then `ebuild.ExpandVariables(npmLine, npmVarMap(ei))`.

> **Plan 20 supersedes this.** Plan 20 lands one shared fixpoint expander and one
> variable-map builder that scans the whole ebuild. When it does, `npmVarMap` is
> deleted and this call site switches to the shared builder. Leave a
> `// TODO(plan 20): replace with the shared whole-ebuild variable map.` comment on
> `npmVarMap`.

Algorithm: take the **first** line of `ei.Raw` containing `mirror://npm/` (script L943:
`head -1`), expand it, strip everything up to and including `mirror://npm/`, then drop
everything from `/-/` onward.

**Scoped URL with no `/-/`:** the script (L957, `string replace -r '/-/.*' ''`) leaves
the whole remaining path untouched. We **deliberately diverge** and keep only the
`@scope/name` prefix, because the script's output is not a valid npm package name.
Literal case: `mirror://npm/@acme/widget/widget-1.0.0.tgz` → `@acme/widget`.
Unscoped with no `/-/`: `mirror://npm/widget/widget-1.0.0.tgz` → `widget`.
Record this divergence in a comment on the function.

### `HAS-RELEASE` status — exact mechanics

The plan previously said "make `IsSignificant()` exclude both unless verbose". **That is
not implementable.** `func (s Status) IsSignificant() bool` is declared at
`internal/upstream/output.go:53` and takes no verbosity argument; the `-v` decision
lives entirely in `cmd/larry/upstream.go:100-108` (non-TUI) and `:134-142` (TUI). Do
this instead:

1. In the `const` block at `internal/upstream/output.go:18-26`, append `StatusHasRelease`
   **after** `StatusUnknown`, so no existing iota value shifts:

   ```go
   StatusUnknown                // Could not determine upstream status
   StatusHasRelease             // Live-only ebuild, but upstream has a release
   ```

2. Add the `String()` case in the switch at `:29-47`, returning `"HAS-RELEASE"`.
3. Add it to the exclusion list in `IsSignificant()` at `:53-55`:

   ```go
   return s != StatusUpToDate && s != StatusLive && s != StatusDeprecated &&
       s != StatusHasRelease
   ```

4. **Leave `IsSignificant`'s signature alone and leave the `-v` handling in
   `cmd/larry/upstream.go` alone.** The two `-v` blocks already do the right thing once
   `HAS-RELEASE` is excluded.

---

## Tests (write first — every one must fail before you implement)

### `internal/upstream/upstream_test.go`

Fixture convention for this group: a `t.TempDir()` overlay, `<tmp>/profiles/repo_name`
containing `test`, and package dirs created with `os.MkdirAll`. Each ebuild body is the
minimal valid EAPI-8 form unless the test needs more.

- `TestFindEbuilds_RevisionOnly` (**D1**) — `dev-libs/bar/bar-3.0.0-r1.ebuild` is the
  only ebuild. Expect exactly one target, `Ebuild.PVR == "3.0.0-r1"`,
  `Ebuild.PV == "3.0.0"`, `LiveOnly == false`.
- `TestFindEbuilds_LiveAndVersioned` (**D2**) — dir with `foo-1.2.3.ebuild`,
  `foo-2.0.0-r1.ebuild`, `foo-9999.ebuild`. Expect exactly one target,
  `Ebuild.PVR == "2.0.0-r1"`, `LiveOnly == false`.
- `TestFindEbuilds_LiveOnly` (**D9**) — dir with only `foo-9999.ebuild`. Expect one
  target, `LiveOnly == true`, `Ebuild.PV == "9999"`.
- `TestFindEbuilds_Deterministic` (**D5**) — overlay with
  `dev-a/p1`, `dev-a/p2`, `dev-b/p1`. Call discovery three times; assert the three
  `[]string` of `Category + "/" + PN` are all exactly
  `["dev-a/p1", "dev-a/p2", "dev-b/p1"]`.
- `TestSkipsAcctPackages` — overlay with `acct-group/x/x-0.ebuild`,
  `acct-user/x/x-0.ebuild`, `dev-util/y/y-1.0.ebuild`. Expect exactly one target,
  `dev-util/y`.
- `TestLiveOnly_Verbose_HasRelease` (**D9**) — live-only `app-shells/zoxide/zoxide-9999.ebuild`
  with `SRC_URI="https://github.com/ajeetdsouza/zoxide/archive/v${PV}.tar.gz"`, and a
  hand-written fake provider registered for `remoteid.TypeGitHub` that returns
  `version.Parse("1.2.3")`. Expect one row with
  `Current == "9999 (live)"`, `Latest == "1.2.3"`, `Status == StatusHasRelease`.
- `TestLiveOnly_Verbose_NoRelease` — same fixture, fake provider returns
  `(nil, errors.New("no releases"))`. Expect `Status == StatusLive`,
  `Current == "9999 (live)"`, `Latest == ""`.
- `TestStatusHasRelease_IsNotSignificant` — `StatusHasRelease.IsSignificant() == false`
  and `StatusHasRelease.String() == "HAS-RELEASE"`.
- `TestStatusIotaUnchanged` — pins the const block so a future insert cannot shift it:
  `StatusOutdated == 0`, `StatusUpToDate == 1`, `StatusAhead == 2`,
  `StatusNewerTag == 3`, `StatusLive == 4`, `StatusDeprecated == 5`,
  `StatusUnknown == 6`, `StatusHasRelease == 7`.
- `TestPypiName_MyPN` (**D6**) — ebuild body
  `MY_PN="nmcli"` + `SRC_URI="mirror://pypi/n/nmcli/nmcli-1.8.0.tar.gz"`, PN
  `python-nmcli`, no `PYPI_PN`. Expect the resolved `RemoteId.Name == "nmcli"`.
- `TestPypiName_Precedence` — both `PYPI_PN="aaa"` and `MY_PN="bbb"` →
  `RemoteId.Name == "aaa"`.
- `TestPypiName_ExpandForms` — table:
  - PN `foo-bar`, `PYPI_PN="${PN//-/_}"` → `foo_bar`
  - PN `a-b-c`, `PYPI_PN="${PN/-/_}"` → `a_b-c`
  - PN `foo`, `PYPI_PN="${PN}"` → `foo`
  - PN `foo-bar`, no `PYPI_PN`, no `MY_PN` → `foo-bar`
- `TestNpmName_CustomVar` (**D7**) — PN `widget`, ebuild body exactly:

  ```
  EAPI=8
  MY_SCOPE="@acme"
  DESCRIPTION="Test"
  SRC_URI="mirror://npm/${MY_SCOPE}/widget/-/widget-1.0.0.tgz"
  LICENSE="MIT"
  SLOT="0"
  ```

  Expect `@acme/widget`. **This is the test that `ebuild.BuildVariableMap` cannot
  pass** — it is the regression guard for using `npmVarMap` instead.
- `TestNpmName_ExpandsPVAndP` — PN `widget`, PV `1.0.0`,
  `SRC_URI="mirror://npm/${PN}/-/${P}.tgz"` → `widget`.
- `TestNpmName_FirstLineOnly` — two `mirror://npm/` lines in `ei.Raw`, the second inside
  a `webui? ( … )` block; the **first** one wins. Body:

  ```
  SRC_URI="mirror://npm/widget/-/widget-1.0.0.tgz"
  SRC_URI+=" webui? ( mirror://npm/other/-/other-2.0.0.tgz )"
  ```

  Expect `widget`.
- `TestNpmName_ScopedNoDashDash` (**D8**) — input
  `SRC_URI="mirror://npm/@acme/widget/widget-1.0.0.tgz"` (note: **no** `/-/`), PN
  `widget`. Expect `@acme/widget`. The comment in the test must say this intentionally
  diverges from the script, which would return
  `@acme/widget/widget-1.0.0.tgz`.
- `TestNpmName_UnscopedNoDashDash` — `mirror://npm/widget/widget-1.0.0.tgz` → `widget`.

**Delete** `TestRunWithCategories_FilterByCategory` (`upstream_test.go:519-542`).

**Delete** `Run`, `CheckUpstream` and `CheckUpstreamSimple`
(`internal/upstream/upstream.go:26`, `:139`, `:185`) **together with the eight tests
that call `CheckUpstream`** — `upstream_test.go` lines 76, 113, 147, 181, 235, 270, 304,
327, i.e. the whole `// --- CheckUpstream tests ---` block starting at `:38`
(`TestCheckUpstream_PyPIOutdated`, `_MockOutdated`, `_MockUpToDate`, `_MockAhead`,
`_SkipAcctGroup`, `_SkipAcctUser`, `_LivePackage`, `_PrereleaseFiltered`,
`_PrereleaseIncluded`, `_UnknownProvider`). These are the Phase-1 anti-pattern called
out in `PLAN.md`: they test a helper no production path calls. Their coverage is
replaced by the new `RunWithTargets` tests above. **List every deleted function and test
in your final report.**

### `cmd/larry/upstream_test.go` — **this plan CREATES the file**

(Plan 18 later **appends** to it. Do not let plan 18's rows leak in here.)

Uses `execCmd` / `fixtureOverlay` from plan 13a's `cmd/larry/testhelp_test.go`.
Set `t.Setenv("LARRY_COLOR", "never")` in each test so assertions are on plain text.

- `TestUpstreamPositionalAtomFilter` (**D3, D4**) — fixture overlay with
  `dev-util/cem/cem-1.0.0.ebuild` and `dev-python/other/other-2.0.0.ebuild`.
  `execCmd(t, "upstream", "--overlay", ov, "-v", "dev-util/cem")`.
  stdout contains `dev-util/cem` and does **not** contain `dev-python/other`.
- `TestUpstreamPositionalMultipleAtoms` — three packages in the fixture; pass two atoms;
  stdout contains both and not the third.
- `TestUpstreamPositionalOverlayPath` — `execCmd(t, "upstream", "-v", ov)` with no
  `--overlay`. stdout contains the fixture's package. (`ov` has `profiles/repo_name`,
  which `fixtureOverlay` creates.)
- `TestUpstreamOverlayPathPlusAtom` — `execCmd(t, "upstream", "-v", "dev-util/cem", ov)`
  → one row, `dev-util/cem`. Argument order must not matter.
- `TestUpstreamTwoOverlayPathsErrors` — two directory args that both contain
  `profiles/` → non-nil error whose message contains `multiple overlay paths`.
- `TestUpstreamBareCategoryErrors` — `execCmd(t, "upstream", "--overlay", ov, "dev-util")`
  → non-nil error. Pins the deliberate removal of bare-category filtering.
- `TestUpstreamUnknownAtomErrors` — `dev-util/does-not-exist` → non-nil error whose
  message contains `dev-util/does-not-exist`. Not silence, not an empty table.

---

## Implementation steps

Every step has a test above; write the test first and watch it fail.

1. **`StatusHasRelease`** — exactly the four sub-steps in "HAS-RELEASE status — exact
   mechanics". Tests: `TestStatusHasRelease_IsNotSignificant`, `TestStatusIotaUnchanged`.
   Also add its colour case to the switch in `internal/cli/upstream_tui.go:185-199`
   (reuse `lipgloss.Color("39")`, the same blue as `StatusLive`) so the TUI compiles
   with an exhaustive switch.
2. **Discovery.** Replace `findEbuilds` (`upstream.go:385-430`) with:

   ```go
   type PackageTarget struct {
       Atom     *atom.Atom
       Ebuild   *atom.ParsedEbuild
       LiveOnly bool
   }

   func discoverTargets(overlayRoot string) ([]*PackageTarget, error)
   ```

   Walk `<overlay>/<category>/<package>/`, apply the `ErrNoEbuilds` fallback exactly as
   specified under "Discovery", skip `acct-group`/`acct-user`, and
   `sort.Slice` the result by `Category + "/" + PN`. Return a slice, never a map.
   (**D1, D2, D5, D9**)
3. **`shouldSkip`** (`upstream.go:640-646`) collapses to
   `func shouldSkip(category string) bool`; fix its one call site at `:1000`. (**D10**)
4. **`RunWithTargets`.** Rename `RunWithCategories` (`:952`) and change
   `categories []string` → `targets []*atom.Atom`. `walkAndCheckCategories` (`:968`)
   now iterates `discoverTargets(overlayRoot)` and, when `len(targets) > 0`, keeps only
   the targets whose `Category`+`PN` appear in that list. Delete the `os.ReadDir`
   category walk at `:973-1000` and the recursion tail at `:1143-1148`.
5. **Live-only rows.** Where the row is built (`upstream.go:1120-1131`), when
   `target.LiveOnly` set `result.Current = "9999 (live)"` and pick
   `StatusHasRelease`/`StatusLive` per the rules above.
6. **PyPI name** — `ResolveRemoteId`'s `case "pypi":` (`upstream.go:776-785`): add the
   `MY_PN` fallback, routing all three through `expandVar`. (**D6**)
7. **npm name** — add `npmVarMap`, rewrite `extractNpmNameFromSRCURI`
   (`upstream.go:1152-1194`) per "npm name" above. (**D7, D8**)
8. **CLI.** `cmd/larry/upstream.go`: `Args: cobra.ArbitraryArgs`; implement the
   classification rule; update `Use:` and the `Long` examples; pass the resolved atom
   list to `RunWithTargets`. (**D3, D4**)
9. **Deletions.** Remove `Run`, `CheckUpstream`, `CheckUpstreamSimple` and their tests
   as listed above; remove `TestRunWithCategories_FilterByCategory`. (**D11**)
10. Run the suites; fix fallout; hand back a list of every deleted function and test.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**. Read that section before running
> anything.

### Tier 1 — automated, required. Run these.

```bash
go build ./...
go vet ./...                 # internal/git/git.go:318 is the known baseline finding
go test ./... -count=1       # no NEW failures vs. your recorded baseline
golangci-lint run ./...
```

Baseline reminder: `internal/prepare TestExtractVar/multi_line` is already red at HEAD
and belongs to plan 20/22 — do not chase it.

Targeted:

```bash
go test ./internal/upstream/... ./cmd/... -count=1
```

Grep assertions (all must print nothing):

```bash
grep -rn 'func RunWithCategories\|func CheckUpstreamSimple\|func CheckUpstream\b' --include=*.go .
grep -rn 'TestRunWithCategories_FilterByCategory' --include=*.go .
grep -rn 'BuildVariableMap' --include=*.go ./internal/upstream
```

### Tier 2 — read-only manual. Safe to run, but **MANUAL** — report results, do not
treat a failure here as blocking.

These need `~/Developer/gentoo-overlay` and network. Skip if either is unavailable; a
failure may be rate-limiting rather than a defect, so re-run before concluding.

```bash
larry upstream --fresh --overlay ~/Developer/gentoo-overlay -v > /tmp/a
larry upstream --overlay ~/Developer/gentoo-overlay -v > /tmp/b
diff /tmp/a /tmp/b          # identical ordering
```

- **Row count:** the number of package rows equals the number of
  `<overlay>/<category>/<package>/` directories minus the `acct-*` ones. Compute it,
  do not hardcode it:

  ```bash
  find ~/Developer/gentoo-overlay -mindepth 2 -maxdepth 2 -type d \
    -not -path '*/.git/*' -not -path '*/profiles/*' -not -path '*/metadata/*' \
    -not -path '*/eclass/*' -not -path '*/licenses/*' -not -path '*/scripts/*' \
    -not -path '*/acct-*' | wc -l
  ```

  (At the time of writing that is 79 total directories, 2 of them `acct-*`. Do not
  encode a hard count — the overlay changes.)
- No duplicate rows.
- No blank `Current` column, except live-only rows, which read `9999 (live)`.
- `dev-python/python-nmcli` reports source `nmcli` and a latest ≥ `1.8.0`.
- `larry upstream --overlay ~/Developer/gentoo-overlay dev-util/cem` prints one row.
- `larry upstream --overlay ~/Developer/gentoo-overlay dev-python` errors (bare
  categories are no longer accepted).

### Tier 3 — DO NOT RUN.

Nothing in this plan is Tier 3. If you believe you need to write into
`~/Developer/gentoo-overlay` to prove something, stop and say so in your report.

---

## Out of scope

GitHub tag-selection strategy and the `CHECK-TAG` status (plans 17a/17b). Dependency
recursion and `UpstreamResult.Depth` (plan 18). Table rendering, the Source cell,
hyperlinks and the golden file (plan 19). The shared variable expander (plan 20) — leave
the `TODO(plan 20)` comment and move on. Do not fix unrelated bugs you notice; report
them.
