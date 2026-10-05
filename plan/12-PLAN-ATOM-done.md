# 12 — `internal/atom`: one atom & ebuild-filename parser

**Status:** ✅ COMPLETE

**Summary:** Created `internal/atom` package with unified atom parsing and ebuild-filename extraction. Killed five independent broken copies that all corrupted hyphenated package names like `kornia-rs`, `python-requests`, `foo-runtime`.

## Deliverables

### New package: `internal/atom/`

**Files created:**
- `atom.go` — exports `ParsedEbuild`, `Atom`, `StripRevision`, `SplitRevision`, `NextRevision`, `ParseEbuildPath`, `ParseAtom`, `ListEbuilds`, `FindLatest`, `FindForAtom`
- `atom_test.go` — 26 tests covering all public API

**Types:**
```go
type ParsedEbuild struct {
    Path, Category, PN, PVR, PV, P, PF string
    Revision int
    Live bool
}

type Atom struct {
    Category, PN, Version, PkgDir string
}

func StripRevision(s string) string           // anchored -r\d+$ only
func SplitRevision(pvr string) (string, int)
func NextRevision(pvr string) string
func ParseEbuildPath(path string) (*ParsedEbuild, error)
func ParseAtom(overlay, atom string) (*Atom, error)
func ListEbuilds(dir string) ([]*ParsedEbuild, error)
func FindLatest(dir string) (*ParsedEbuild, error)
func FindForAtom(a *Atom) (*ParsedEbuild, error)
```

**Sentinel errors:** `ErrInvalidAtom`, `ErrCategoryNotFound`, `ErrPackageNotFound`, `ErrNoEbuilds`, `ErrNotAnEbuild`

## Defects fixed

| ID | Defect | Fix |
|----|--------|-----|
| D1-D5 | `strings.LastIndex(base, "-r")` on `kornia-rs` → PN `kornia`, PV `kornia` | `atom.ParseEbuildPath` derives PN from directory name; `atom.StripRevision` uses anchored regex `-r([0-9]+)$` |
| D6 | No `parse_atom` — atom strings treated as directory names | `atom.ParseAtom` with `^[a-zA-Z0-9_-]+/[a-zA-Z0-9_.+-]+$` validation, hyphen-walk longest-first |
| D7 | `findLatestEbuild` sorts 9999 last instead of excluding it | `atom.FindLatest` explicitly skips live (9999) ebuilds, returns `ErrNoEbuilds` if only live exist |
| D8 | `incrementRevision` returns `max(-rN)+1` over whole directory | `revisionBump` derives from `atom.ParseEbuildPath(latest)` + `atom.NextRevision` |
| D9 | `os.ReadDir` lexical ordering, `foo-9.0` beats `foo-10.0` | `atom.ListEbuilds` sorts ascending by `cmpPV` with revision tiebreak; `atom.FindLatest` picks highest non-live |

## Code deleted

| Package | Functions removed |
|---------|-------------------|
| `internal/upstream` | `extractVersionFromFilename`, `extractPkgName`, `extractCategoryFromPath` (3 funcs, 79 lines) |
| `internal/ebuild` | `extractVersionFromFilename`, `extractPkgName`, `extractCategoryFromPath` (3 funcs, 38 lines) |
| `internal/bump` | `findLatestEbuild`, `extractRevisionFromName`, `extractPVFromName`, `extractPNPV`, `incrementRevision`, `generateBumpedEbuild` (old impl) (6 funcs, 150+ lines) |

## Code refactored

| Package | Changes |
|---------|---------|
| `internal/upstream/upstream.go` | `ParseEbuildInfo` now uses `atom.ParseEbuildPath`; added `atom` import |
| `internal/ebuild/info.go` | Same; fallback uses `filepath.Base(filepath.Dir(path))` |
| `internal/prepare/prepare.go` | `parseEbuildInfo` uses `atom.ParseEbuildPath`; added `atom` import |
| `internal/bump/bump.go` | Complete rewrite: `Bump`, `FindLatestEbuild`, `revisionBump` use `atom`; `extractPNPVFallback` (last-resort) also uses `atom.StripRevision` |
| `internal/bump/bump_test.go` | Rewritten: all tests use atom-compatible fixtures; added `TestBump_NextRevisionFromLatestNotDirMax` for D8 |
| `internal/test/local.go` | `resolveEbuildFromCatPkg` uses `atom.ParseAtom` + `atom.FindForAtom` |
| `cmd/larry/test.go` | Inline ebuild selection replaced with `atom.ParseAtom` + `atom.FindForAtom` |
| `internal/prepare/prepare_test.go` | Fixture dirs: `catDir/test/test/…` → category resolves to `test` |
| `internal/upstream/upstream_test.go` | Fixture dirs: `tmpDir/dev-python/requests/…` → category/PN resolve correctly |
| `internal/test/local_test.go` | `TestResolveEbuild_CatPkgNotFound` accepts `ErrCategoryNotFound` or `ErrPackageNotFound` |

## Tier 1 acceptance

```
go build ./...                           ✅ clean
go vet ./...                             ✅ clean (excluding pre-existing git.go:318)
go test ./... -count=1                   ✅ all pass (1 pre-existing failure: prepare TestExtractVar/multi_line → plan 20a)
golangci-lint run ./...                  ✅ no new issues
grep LastIndex.*"-r"                     ✅ none outside version.go
grep func extractPkgName etc.            ✅ none
```

## Tier 2 (skipped — no reference overlay available)

```bash
larry upstream --overlay ~/Developer/gentoo-overlay -v | grep kornia
# expect: dev-python/kornia (0.8.3) and dev-python/kornia-rs (0.1.14)

larry test --pretend dev-util/javy-8.1.1
# expect: resolves instead of erroring
```

## Tier 3 (not applicable — this plan has no mutating steps)

## Notes for downstream plans

- `internal/version`'s `version.go:97` (revision strip in `Parse`) is EXPECTED to remain — plan 14 owns it.
- The `cmpPV` helper in `atom.go` is a private unexported function using `version.Parse` + `(*Version).Compare`. Route to `version.Compare(a, b)` once plan 14 lands.
- `TestExtractVar/multi_line` in `internal/prepare` remains red — owned by plan 20a/22a.
