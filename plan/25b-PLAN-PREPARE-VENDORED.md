# 25b — `prepare`: the vendored pipeline

**Why:** `PrepareVendored` derives the package name by splitting the version string at
its first hyphen, so **every hyphenated package name fails** with
`no vendored deps found in SRC_URI`. The `.larry.yaml` `vendor_dirs` key is parsed and
never read, so the build always runs at the extract root. The pipeline declares an npm
asset layout (`node_modules`) that the npm pipeline does not produce (`npm-cache`). And
four helpers in the file are dead.

**Precondition:** 12 (`internal/atom`), 21 (manifest with `catPkg`), 22b
(`22b-PLAN-PREPARE-NPM-PACKAGING.md` — it owns the `npm-cache` contract this plan
aligns to).

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent on this machine, this
plan's text is authoritative — do not guess at the script's behaviour.** The script has
**no** vendored pipeline; this is larry-only surface (the `MIXED_DEPS.md` named in
`internal/ebuild/ebuild.go:11` is not present in this repo), so
the text below is authoritative for behaviour and only the manifest step has a script
analogue (`generate_manifest`, L177–197).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. In particular: the known-red baseline is
> exactly `TestExtractVar/multi_line` in `internal/prepare` plus one `go vet` finding at
> `internal/git/git.go:318`. Do not chase either.

**Sibling plan:** the rpi-kernel decision and the `files/prepare.sh` escape hatch moved
to `plan/25a-PLAN-PREPARE-ESCAPE-HATCH.md`. The two share nothing.

---

## Precondition check — run these first

```bash
go doc ./internal/atom ParseEbuildPath
go doc ./internal/atom ParsedEbuild
grep -n 'func runManifest' internal/prepare/manifest.go
grep -n 'npm-cache' internal/prepare/npm.go
grep -n 'func createNPMDepsTarball' internal/prepare/vendored.go
```

Expected:

```go
// plan 12
func ParseEbuildPath(path string) (*ParsedEbuild, error)

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

// plan 21
func runManifest(overlay, catPkg, distdir string) error   // catPkg, NOT a file path
```

Expected (plan 22b): `internal/prepare/npm.go` still produces an `npm-cache/` tarball —
`npmDepsTarballHasCache` at `npm.go:341` and `npmCacheDir` at `npm.go:390`.

**If `ParseEbuildPath` or the 3-arg `catPkg` `runManifest` is missing, STOP and
report.** Do not implement the prerequisite yourself.

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| C1 | `internal/prepare/vendored.go:72` | `parseDepSpecsFromURI` derives the package name as `filepath.Base(strings.Split(pVal, "-")[0])`. For `fish-lsp-1.1.3` that is `fish`, so `releases/download/fish/` never matches → `no vendored deps found in SRC_URI` for **every hyphenated package name** |
| C2 | `internal/prepare/vendored.go:385` + `config.go:14-16, 49-67` | `DepSpec.SourcePath` (`vendored.go:30`) is never populated, so `buildDir := filepath.Join(extractDir, ds.SourcePath)` is always the extract root. The `.larry.yaml` `vendor_dirs` key is parsed at `config.go:49-67` into `Config.VendorDirs` (`config.go:16`) and **never read** — `PrepareVendored` does not even call `LoadConfig` |
| C3 | `internal/prepare/vendored.go:49` vs `internal/prepare/npm.go:390` | `parseDepSpec` sets `TarballContent = "node_modules"` for npm assets; the npm pipeline produces `npm-cache`. `npmDepsTarballHasCache` (`npm.go:341`, used at `npm.go:87`) would reject vendored output |
| C4 | `internal/prepare/vendored.go:182, 220, 228, 332` | `createNPMDepsTarball`, `copyFile`, `copyDir`, `sha256OfFile` are dead (`golangci-lint` reports the first three as `unused`; `sha256OfFile` has exactly one caller, `internal/prepare/vendored_test.go:346`) |
| C5 | `internal/prepare/vendored.go:417` | `PrepareVendored` ends with `runManifest(cfg.Overlay, ei.Path, distdir)` — plan 21 replaced that signature with `runManifest(overlay, catPkg, distdir)` |

---

## Decisions — already made, do not re-open

**C1 — derive PN from `internal/atom`, not from string splitting.** Change the
signature:

```go
// before
func parseDepSpecsFromURI(srcURI, pVal string) []DepSpec
// after
func parseDepSpecsFromURI(srcURI string, pe *atom.ParsedEbuild) []DepSpec
```

and pass `pe.PN` to `extractSRCURIForPN` (`vendored.go:84`). `parseDepSpec`
(`vendored.go:40`) keeps its `pVal` parameter — it only uses it for nothing today, so
drop the unused parameter while you are there.

**C2 — KEEP `vendor_dirs` and WIRE IT UP.** Do not delete the config key. In
`PrepareVendored`, after resolving the parsed ebuild and before the per-dep loop:

```go
pkgCfg, err := LoadConfig(ei.Path)   // internal/prepare/config.go:23
if err != nil {
    return fmt.Errorf("vendored: read .larry.yaml: %w", err)
}
```

Then, inside the loop over `depSpecs`, before computing `buildDir`:

```go
if pkgCfg != nil {
    if dirs, ok := pkgCfg.VendorDirs[pe.PN]; ok && len(dirs) > 0 {
        ds.SourcePath = dirs[0]
    }
}
buildDir := filepath.Join(extractDir, ds.SourcePath)
```

(`ds` is the loop variable; take it by value and assign to the local copy. Only the
first entry is used — multiple dirs per package is a future feature, and the loop
variable must not be mutated in the slice.)

**`VendorDirs` is `map[string][]string`, keyed by package name** (`config.go:16`,
parsed at `config.go:49-67` from a YAML mapping). `vendor_dirs: [sub/pkg]` is a YAML
*list* and parses to an **empty map** — it will silently do nothing. The correct fixture
shape is:

```yaml
vendor_dirs:
  fish-lsp:
    - sub/pkg
```

**C3 — `npm-cache` is the contract.** Change `vendored.go:49` from `"node_modules"` to
`"npm-cache"`. Plan 22b makes the same decision from the other side; the single shared
assertion lives in this plan (`TestVendored_NpmAssetContractIsNpmCache`).

**C4 — delete the dead helpers.** But first:

```bash
grep -n 'createNPMDepsTarball' internal/prepare/
```

If that returns **nothing**, plan 22b already deleted it (`22b-PLAN-PREPARE-NPM-PACKAGING.md`
step 8) — delete only the remaining
three (`copyFile`, `copyDir`, `sha256OfFile`) and say so in your report. Deleting a
function means deleting its tests too (`sha256OfFile` has test callers).

**C4 has no unit test by design** — it is verified by the `unused` linter, same as plan
28a's dead-code item.

---

## AGENTS.md tension — explicitly out of scope

`createVendorTarball` (`vendored.go:160`) and `extractTarball` (`vendored.go:131`) both
shell out to the `tar` binary via `exec.Command`. AGENTS.md says
"`archive/tar`, `compress/gzip` — archiving (not tar)". **This plan does not convert
them**, for one concrete reason: they must handle `.tar.xz` and `.tar.zst`, for which Go
has no stdlib codec, so a partial conversion would leave two code paths. Record it as a
follow-up in your report; do not convert it here. (Plan 24 *does* convert `go.go`,
because that path is `.tar.gz`-only.)

---

## Tests (write first — every one must fail before you implement)

### `internal/prepare/vendored_test.go`

- **`TestParseDepSpecs_HyphenatedPackageName`** (**C1**)
  Input: `pe = &atom.ParsedEbuild{PN: "fish-lsp", PV: "1.1.3", P: "fish-lsp-1.1.3"}` and
  ```
  srcURI = "https://github.com/bennypowers/overlay/releases/download/fish-lsp/fish-lsp-1.1.3-npm-deps.tar.xz"
  ```
  Expected: exactly **one** `DepSpec`, with
  `Type == DepNPM`, `DistName == "fish-lsp-1.1.3-npm-deps.tar.xz"`,
  `TarballContent == "npm-cache"`.
- **`TestParseDepSpecs_SimpleName`**
  Input: `pe = &atom.ParsedEbuild{PN: "javy", PV: "9.1.0", P: "javy-9.1.0"}` and
  ```
  srcURI = "https://github.com/bennypowers/overlay/releases/download/javy/javy-9.1.0-cargo-deps.tar.xz"
  ```
  Expected: one `DepSpec`, `Type == DepCargo`, `TarballContent == "vendor"`.
  Pins "unchanged behaviour for non-hyphenated names".
- **`TestParseDepSpecs_NoMatch`**
  Input: same `pe` as above, `srcURI = "https://example.com/javy-9.1.0.tar.gz"`.
  Expected: empty slice, no panic.
- **`TestVendored_NpmAssetContractIsNpmCache`** (**C3**; also satisfies plan 22b's D9 —
  the two plans share this contract, so the assertion lives here and plan 22b
  cross-references it)
  Input: `parseDepSpec("pkg-1.0.0-npm-deps.tar.xz")`.
  Expected: `TarballContent == "npm-cache"` exactly. Assert the literal string, not a
  constant from `npm.go`, so the two files cannot drift apart silently.
- **`TestVendored_VendorDirsConfigIsHonoured`** (**C2**)
  Fixture: `t.TempDir()/overlay/dev-util/fish-lsp/` containing
  `fish-lsp-1.1.3.ebuild` and a `.larry.yaml` whose **exact bytes** are
  ```yaml
  vendor_dirs:
    fish-lsp:
      - sub/pkg
  ```
  Expected: with a recording `runVendorCommand` seam, the recorded `workDir` is
  `<extractDir>/sub/pkg` — **not** `<extractDir>`. Assert the full path, with
  `filepath.Join(extractDir, "sub", "pkg")`.
- **`TestVendored_VendorDirsAbsentMeansExtractRoot`** (**C2**)
  Same fixture with **no** `.larry.yaml`.
  Expected: recorded `workDir == extractDir` exactly.
- **`TestVendored_VendorDirsForOtherPackageIsIgnored`** (**C2**)
  Same fixture, but `.larry.yaml` is
  ```yaml
  vendor_dirs:
    some-other-pkg:
      - sub/pkg
  ```
  Expected: recorded `workDir == extractDir`. Pins the map keying on PN.
- **`TestVendored_ManifestTargetsCatPkg`** (**C5**)
  Fixture: the happy path with a `runManifest` stub recording its arguments.
  Expected: the stub is called once with `catPkg == "dev-util/fish-lsp"` — **not** a
  path ending in `.ebuild`.

To make the three `VendorDirs` tests writable, `runVendorCommand` (`vendored.go:425`)
needs a seam. Add a package-level `var runVendorCommandFn = runVendorCommand` and call
through it from `PrepareVendored`; tests swap it and restore with `t.Cleanup`. Do the
same for the download/extract pair if the happy path otherwise needs network — or keep
the tests at the level of a helper
`resolveBuildDir(extractDir string, pe *atom.ParsedEbuild, pkgCfg *Config, ds DepSpec) string`
that you extract first and test directly. **Pick the helper if you are short on
budget**; it is the smaller change and still covers C2 end-to-end for the path logic.

---

## Implementation steps

Every step has a test above. Write the test red first.

1. Change `parseDepSpecsFromURI` to take `*atom.ParsedEbuild` and pass `pe.PN` to
   `extractSRCURIForPN`. Drop `parseDepSpec`'s unused `pVal` parameter.
   Covers: `TestParseDepSpecs_HyphenatedPackageName`, `_SimpleName`, `_NoMatch`.
   (**C1**)
2. Change `vendored.go:49` to `tarballContent = "npm-cache"`.
   Covers: `TestVendored_NpmAssetContractIsNpmCache`. (**C3**)
3. In `PrepareVendored` (`vendored.go:345`): call `atom.ParseEbuildPath(ei.Path)` once at
   the top, then `LoadConfig(ei.Path)`, then set `ds.SourcePath` from
   `pkgCfg.VendorDirs[pe.PN][0]` when present, per the C2 decision above. Extract
   `resolveBuildDir` if you went that route.
   Covers: `TestVendored_VendorDirsConfigIsHonoured`,
   `_VendorDirsAbsentMeansExtractRoot`, `_VendorDirsForOtherPackageIsIgnored`. (**C2**)
4. Swap the final `runManifest(cfg.Overlay, ei.Path, distdir)` at `vendored.go:417` to
   `runManifest(cfg.Overlay, pe.Category+"/"+pe.PN, distdir)`.
   Covers: `TestVendored_ManifestTargetsCatPkg`. (**C5**)
5. Run `grep -n 'createNPMDepsTarball' internal/prepare/`. Delete whichever of
   `createNPMDepsTarball`, `copyFile`, `copyDir`, `sha256OfFile` still exist, plus their
   tests.
   Covers: nothing — verified by the `unused` linter. (**C4**)
6. Hand back listing every function and test you deleted, and the `tar`-subprocess
   follow-up from the AGENTS.md section above.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**.

**Tier 1 — run these. All four are required.**

```bash
go build ./...
go vet ./...                      # expect ONLY internal/git/git.go:318 (baseline)
go test ./... -count=1            # no NEW failures vs. your recorded baseline
golangci-lint run ./...           # per AGENTS.md, required before any push
```

Also Tier 1 — the real proof, no overlay and no network needed:

```bash
go test ./internal/prepare/ -run 'TestParseDepSpecs|TestVendored' -count=1 -v

golangci-lint run --enable-only=unused ./internal/prepare/...
# expect: no findings naming vendored.go
```

`TestParseDepSpecs_HyphenatedPackageName` is the automated proof of C1.

**Tier 2 — read-only, safe to run:**

```bash
grep -n 'strings.Split(pVal' internal/prepare/vendored.go
# expect: no output

grep -n 'node_modules' internal/prepare/vendored.go
# expect: no output

grep -n 'runManifest(cfg.Overlay, ei.Path' internal/prepare/
# expect: no output
```

**Tier 3 — DO NOT RUN. Report verbatim for a human.**

```bash
# Tier 3 — DO NOT RUN. Report verbatim.
# Runs a real vendored prepare: downloads source, runs npm/cargo, UPLOADS a GitHub
# release asset to the overlay's own repo (cannot be quietly undone), writes the overlay.
cd ~/Developer/gentoo-overlay
larry prepare --type vendored dev-util/fish-lsp/<latest>.ebuild
#   expect: no "no vendored deps found in SRC_URI"
#   expect: the uploaded asset's tarball root is npm-cache/, not node_modules/
git -C ~/Developer/gentoo-overlay checkout dev-util/fish-lsp   # restore the ebuild
#   NOTE: the release asset upload is NOT undone by git checkout.
```

## Out of scope

The rpi-kernel decision and `files/prepare.sh` (plan 25a). npm/cargo/go pipelines
(plans 22a/22b, 23a/23b, 24). Manifest mechanics (plan 21). Converting `createVendorTarball` /
`extractTarball` off the `tar` subprocess (see the AGENTS.md section above). Do not fix
unrelated bugs you notice — report them.
