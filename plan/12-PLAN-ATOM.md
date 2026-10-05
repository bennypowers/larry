# 12 — `internal/atom`: one atom & ebuild-filename parser

**Why:** Five independent copies of "split an ebuild filename into PN and PV" exist and
all five are wrong in the same way. They corrupt every package whose name contains `-r`,
and none implement the script's atom resolution. Everything downstream (`bump`, `test`,
`import`, `upstream`) inherits the damage.

**Precondition:** none — do this first.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish). Cited below as
`script <func> (L…)`. If that file is absent, this plan's text is authoritative.
Functions: `parse_atom` (L585–626), `find_latest_ebuild` (L630–641),
`find_ebuild_for_atom` (L643–664), `next_revision` (L666–674), `parse_ebuild`
(L109–116), `get_version_from_filename` (L886–891),
`find_latest_versioned_ebuild` (L903–917).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting.

---

## Dependency on `internal/version` (read before coding)

This package orders versions. `internal/version` is buggy today and plan 14 fixes it.
Measured 2026-10-05 — everything this plan needs already works:

```
Parse ok:  9.0  10.0  1.0-r2  9999  0.1.14  6.18.46_p20260824  1.0_rc1  367  2026.40.0  20250410
Parse ERR: 1.2.3a
(*Version).Compare:  9.0<10.0   1.0<1.0-r2   1.0<9999   0.1.14>0.1.10
```

Plan 12 is therefore runnable standalone. Three rules follow:

1. **There is no `version.Compare` package function today** — only the method
   `(*version.Version).Compare(*version.Version)`, reached via `version.Parse`.
   Plan 14 adds the package-level `version.Compare(a, b string) int`. Define one
   unexported helper here and route every comparison through it:

   ```go
   // cmpPV orders two PV strings. Replace the body with `return version.Compare(a, b)`
   // once plan 14 has landed.
   func cmpPV(a, b string) int {
       va, errA := version.Parse(a)
       vb, errB := version.Parse(b)
       switch {
       case errA == nil && errB == nil:
           return va.Compare(vb)
       case errA == nil: // parseable sorts above unparseable
           return 1
       case errB == nil:
           return -1
       default:
           return strings.Compare(a, b)
       }
   }
   ```

   Do **not** write a second comparison algorithm, and do not edit `internal/version`.
2. **Never drop an ebuild because its PV fails to parse.** The current code does exactly
   that (D5, and plan 16 D1). Unparseable PVs sort *below* every parseable one.
3. **Do not use `1.2.3a` as an "unparseable" fixture.** Plan 14 makes it parseable. Use
   `nightly` or `abc`.

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D1 | `internal/upstream/upstream.go:547-573` | `extractVersionFromFilename` / `extractPkgName` strip revisions with `strings.LastIndex(base, "-r")` — unanchored, no digit check |
| D2 | `internal/ebuild/info.go:258-284` | duplicate of D1 |
| D3 | `internal/prepare/prepare.go:175-182` | duplicate of D1 |
| D4 | `internal/bump/bump.go:227, 265, 363` | duplicate of D1; `:124-127` runs it over the **full path** |
| D5 | `internal/upstream/upstream.go:400-413` | `findEbuilds` splits at the last `-` with no `-rN` strip → `bar-3.0.0-r1.ebuild` parses as pkg `bar-3.0.0`, version `r1`, then is **silently dropped** |
| D6 | everywhere | no `parse_atom`: `dev-util/javy-8.1.1` is treated as a directory name |
| D7 | `internal/bump/bump.go:153-204` | `findLatestEbuild` sorts 9999 last instead of excluding it |
| D8 | `internal/bump/bump.go:287-308` | `incrementRevision` returns `max(-rN)+1` over the whole directory instead of `latest ebuild's revision + 1` |
| D9 | `internal/test/local.go:92-121`, `cmd/larry/test.go:81-106` | relies on `os.ReadDir`'s lexical ordering and takes the last element (no sort at all) → `foo-9.0` beats `foo-10.0`, and `foo-9999` always wins |

Symptom: `kornia-rs-0.1.14.ebuild` → PN `kornia`, PV `kornia`, because
`LastIndex("kornia-rs-0.1.14", "-r")` finds the `-r` of `-rs` at index 6.

---

## Deliverable

New package `internal/atom`. Imports: stdlib + `internal/version` only.

```go
package atom

type ParsedEbuild struct {
    Path     string // absolute path
    Category string // "dev-util"
    PN       string // "fish-lsp"  — from the PARENT DIRECTORY NAME, never the filename
    PVR      string // "1.1.3-r2"
    PV       string // "1.1.3"
    Revision int    // 2 (0 when absent)
    P        string // "fish-lsp-1.1.3"
    PF       string // "fish-lsp-1.1.3-r2"
    Live     bool   // PV == "9999"
}

type Atom struct {
    Category string
    PN       string
    Version  string // "" when the atom carried no version
    PkgDir   string // <overlay>/<category>/<pn>
}

// Sentinels — errors.New, wrapped with %w so errors.Is works.
var (
    ErrInvalidAtom      = errors.New("invalid atom")
    ErrCategoryNotFound = errors.New("category not found")
    ErrPackageNotFound  = errors.New("package not found")
    ErrNoEbuilds        = errors.New("no ebuilds found")
    ErrNotAnEbuild      = errors.New("not an ebuild of this package")
)

func StripRevision(s string) string            // anchored -r\d+$ only
func SplitRevision(pvr string) (string, int)
func NextRevision(pvr string) string           // "1.0"->"1.0-r1"; "1.0-r3"->"1.0-r4"
func ParseEbuildPath(path string) (*ParsedEbuild, error)
func ParseAtom(overlay, atom string) (*Atom, error)
func ListEbuilds(dir string) ([]*ParsedEbuild, error)
func FindLatest(dir string) (*ParsedEbuild, error)
func FindForAtom(a *Atom) (*ParsedEbuild, error)
```

**Ordering rule.** Ascending by `cmpPV(PV)`; ties broken by `Revision`; live (`9999`)
last; unparseable PVs immediately **before** the first parseable one, ordered among
themselves by `strings.Compare(PVR)`. Deterministic across runs.

**Error contract — two distinct rules, do not conflate:**
- `ParseEbuildPath` returns `ErrNotAnEbuild` when the basename does not start with
  `<PN>-` or does not end in `.ebuild`.
- `ListEbuilds` **skips** files that return `ErrNotAnEbuild` (they belong to another
  package) and **never** skips a file that parses structurally but whose PV fails
  `version.Parse`. Only the latter is the "never discard" rule.

`FindLatest` returns the highest-versioned **non-live** ebuild, or `ErrNoEbuilds` when
there is none — even if a live ebuild exists (script L631).
`FindForAtom`: exact version when `a.Version != ""`, else `FindLatest`, else the live
ebuild as a last resort (script L643–664).

`ParseAtom` validates `^[a-zA-Z0-9_-]+/[a-zA-Z0-9_.+-]+$`, stats the category dir, tries
an exact directory match, then walks hyphens backwards longest-first testing directory
existence (script L600–624).

---

## Tests (write first — every one must fail before you implement)

### `internal/atom/atom_test.go`

- `TestStripRevision` — `1.0-r1`→`1.0`; `1.0-r12`→`1.0`; `foo-rs`→`foo-rs`;
  `foo-release`→`foo-release`; `1.0-rc1`→`1.0-rc1`; `1.0`→`1.0`; `-r1`→`-r1`.
- `TestSplitRevision` — `1.0-r3`→(`1.0`,3); `1.0`→(`1.0`,0).
- `TestNextRevision` — `1.0`→`1.0-r1`; `1.0-r3`→`1.0-r4`; `1.0-r9`→`1.0-r10`.
- `TestParseEbuildPath_Simple` — `dev-util/javy/javy-9.1.0.ebuild` → PN `javy`, PV `9.1.0`.
- `TestParseEbuildPath_HyphenatedName` —
  `dev-python/kornia-rs/kornia-rs-0.1.14.ebuild` → PN `kornia-rs`, PV `0.1.14`.
  **Regression test for D1, D2, D3, D4 and D5** — all five parsers fail this one case.
- `TestParseEbuildPath_HyphenatedNameWithRevision` —
  `dev-python/python-requests/python-requests-2.31.0-r1.ebuild` → PN `python-requests`,
  PV `2.31.0`, Revision 1, PVR `2.31.0-r1`.
- `TestParseEbuildPath_Live` — `foo-9999.ebuild` → `Live == true`.
- `TestParseEbuildPath_UnderscoreSuffixes` — `6.18.46_p20260824`, `1.0_rc1`, `1.0_pre2`
  survive intact (not mistaken for revisions).
- `TestParseEbuildPath_Mismatch` — basename not prefixed by the directory name →
  `errors.Is(err, ErrNotAnEbuild)`.
- `TestParseAtom_NoVersion` — `dev-util/fish-lsp` → PN `fish-lsp`, Version "".
- `TestParseAtom_WithVersion` — `dev-util/javy-8.1.1`, only `dev-util/javy/` on disk →
  PN `javy`, Version `8.1.1`.
- `TestParseAtom_HyphenWalkPrefersLongestDir` — both `dev-util/fish` and
  `dev-util/fish-lsp` exist; `dev-util/fish-lsp-1.1.3` → PN `fish-lsp`, Version `1.1.3`.
- `TestParseAtom_InvalidShape` — `notanatom`, `a/b/c`, `""` → `ErrInvalidAtom`.
- `TestParseAtom_CategoryMissing` → `ErrCategoryNotFound`.
- `TestParseAtom_PackageMissing` → `ErrPackageNotFound`.
  (The `TestParseAtom_*` group is the regression suite for **D6**.)
- `TestFindLatest_NumericOrdering` — `foo-9.0`, `foo-10.0` → `foo-10.0` (**D9**).
- `TestFindLatest_ExcludesLive` — `foo-1.0`, `foo-9999` → `foo-1.0` (**D7**).
- `TestFindLatest_LiveOnly` — only `foo-9999` → `errors.Is(err, ErrNoEbuilds)` (**D7**).
- `TestFindLatest_RevisionTiebreak` — `foo-1.0`, `foo-1.0-r2` → `foo-1.0-r2`.
- `TestFindForAtom_ExactVersion` / `_ExactVersionMissing` / `_LiveFallback`.
- `TestListEbuilds_Deterministic` — three runs over one fixture return identical slices.
- `TestListEbuilds_KeepsUnparseableVersion` — dir with `foo-1.0.ebuild` and
  `foo-nightly.ebuild` returns **two** entries, unparseable first. Guards D5 / plan 16 D1.
- `TestListEbuilds_SkipsForeignBasename` — a stray `other-1.0.ebuild` in `foo/` is
  skipped without error, while `foo-nightly.ebuild` is kept. Pins the two-rule contract.

### `internal/bump/bump_test.go` (end-to-end case for D8)

- `TestBump_NextRevisionFromLatestNotDirMax` — temp package dir with
  `foo-1.0.0-r3.ebuild` and `foo-2.0.0.ebuild`; a revision bump via the exported
  `bump.Bump(...)` creates **`foo-2.0.0-r1.ebuild`**, not `foo-2.0.0-r4.ebuild`.
  (The pure-helper case lives in `atom_test.go`; `incrementRevision` is unexported, so
  package `atom` cannot reach it.)

---

## Implementation steps

1. Create `internal/atom/{atom.go,atom_test.go}`. Anchored regex:
   `var revRe = regexp.MustCompile(`-r([0-9]+)$`)`. Add `cmpPV` exactly as given above.
2. `ParseEbuildPath`: `PN = filepath.Base(filepath.Dir(path))`;
   `Category = filepath.Base(filepath.Dir(filepath.Dir(path)))`;
   `PVR = strings.TrimPrefix(base, PN+"-")`, returning `ErrNotAnEbuild` on mismatch.
3. `ParseAtom`, `ListEbuilds`, `FindLatest`, `FindForAtom` per the contract above.
4. **Checkpoint — `go test ./internal/atom/ -count=1` green before touching anything
   else.** If budget is running short, stop here and hand back; steps 5–8 are a clean
   second session.
5. **Rewrite `internal/bump/bump_test.go` first.** It is the *only* test file
   referencing the helpers you are about to delete (11 references to
   `extractPVFromName`, `extractPNPV`, `extractRevisionFromName`, `incrementRevision`,
   `findLatestEbuild`). Repoint those tests at `internal/atom`'s exported API, then add
   `TestBump_NextRevisionFromLatestNotDirMax`. Deleting the helpers before this leaves
   `internal/bump` not compiling.
6. Replace call sites and delete the old helpers:
   - `internal/upstream/upstream.go` — `extractVersionFromFilename`, `extractPkgName`,
     and the `findEbuilds` split at `:400-429`.
   - `internal/ebuild/info.go:258-284`.
   - `internal/prepare/prepare.go:175-182`.
   - `internal/bump/bump.go` — the five helpers; `incrementRevision` becomes
     `atom.NextRevision(latest.PVR)`, derived from the latest ebuild only (**D8**).
   - `internal/test/local.go:92-121`; `cmd/larry/test.go:81-106`.
   - `internal/importcmd/import.go:120-126` ebuild selection.
7. Run the suites; fix fallout.
8. Hand back listing every helper and test you deleted.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**. Tier 1 and Tier 2 only — nothing
> here mutates anything.

**Tier 1 — run:**
```bash
go build ./... && go vet ./...
go test ./internal/atom/... ./internal/bump/... ./internal/upstream/... \
        ./internal/ebuild/... ./internal/test/... ./internal/prepare/... \
        ./internal/importcmd/... ./cmd/... -count=1
go test ./... -count=1     # no NEW failures vs. your recorded baseline
golangci-lint run ./...
```
Baseline reminder: `internal/prepare TestExtractVar/multi_line` is already red at HEAD
and belongs to plan 20/22 — do not chase it.

```bash
# No ad-hoc revision stripping left outside internal/version.
grep -rn 'LastIndex(.*"-r")' --include=*.go \
  ./cmd ./internal/upstream ./internal/ebuild ./internal/prepare \
  ./internal/bump ./internal/test ./internal/importcmd
# expect: no output.  internal/version/version.go:97 is EXPECTED to remain — plan 14 owns it.

grep -rn 'func extractPkgName\|func extractPVFromName\|func extractPNPV\|func extractRevisionFromName' --include=*.go .
# expect: no output
```

**Tier 1 — fixture proof of the headline bug** (no overlay needed): a `t.TempDir()`
overlay containing `dev-python/kornia/kornia-0.8.3.ebuild` and
`dev-python/kornia-rs/kornia-rs-0.1.14.ebuild` yields two distinct packages with PNs
`kornia` and `kornia-rs`. Today it yields a phantom duplicate.

**Tier 2 — optional, needs `~/Developer/gentoo-overlay` + network; skip if unavailable:**
```bash
larry upstream --overlay ~/Developer/gentoo-overlay -v | grep kornia
# expect exactly two rows: dev-python/kornia (0.8.3) and dev-python/kornia-rs (0.1.14)
larry test --pretend dev-util/javy-8.1.1    # resolves instead of erroring
```

## Out of scope

Version *comparison* semantics, including `internal/version/version.go:97` (plan 14).
Upstream provider selection (plans 15, 16). Do not fix unrelated bugs you notice —
report them.
