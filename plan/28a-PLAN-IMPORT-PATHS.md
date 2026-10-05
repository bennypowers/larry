# 28a — `larry import`: source resolution, target path and version selection

**Why:** Import writes the ebuild to a path built by joining an **absolute** source path
onto the target overlay, with the ebuild filename used as a directory component —
`<overlay>/var/db/repos/guru/dev-lisp/foo/foo-1.0.0.ebuild/foo-1.0.0.ebuild` — and
`MkdirAll` creates that whole bogus tree, even under `--dry-run`. The auto-search of
`/var/db/repos/*` is unreachable because `--from` defaults to the user's own overlay,
so `larry import dev-lisp/foo` tries to import a package into itself. And the version
picker ranks `foo-9999` highest, so a live ebuild is copied whenever one exists.

**Precondition:** 12 (`internal/atom`), 13a (`13a-PLAN-CLI-HARNESS.md` — config + the
`cmd/larry` test harness), 14 (version).

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent on this machine, this
plan's text is authoritative — do not guess at the script's behaviour.**
Functions: `find_package_in_repos` (L1596–1613), `do_import` (L1615–1737).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. In particular: the known-red baseline is
> exactly `TestExtractVar/multi_line` in `internal/prepare` plus one `go vet` finding at
> `internal/git/git.go:318`. Do not chase either.

**Sibling plan:** `plan/28b-PLAN-IMPORT-METADATA.md` covers metadata.xml, the maintainer
config, provenance output and the prepare invocation. **28b depends on this plan
landing first** (plus plan 15).

---

## Precondition check — run these first

```bash
go doc ./internal/atom Atom
go doc ./internal/atom ParsedEbuild
go doc ./internal/atom ParseAtom
go doc ./internal/atom FindLatest
go doc ./internal/atom ListEbuilds
go doc ./internal/version Compare
go doc ./internal/config ResolveOverlay
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
    Path     string
    Category string
    PN       string
    PVR      string
    PV       string
    Revision int
    P        string
    PF       string
    Live     bool   // PV == "9999"
}

func ParseAtom(overlay, atom string) (*Atom, error)
func ListEbuilds(dir string) ([]*ParsedEbuild, error)
func FindLatest(dir string) (*ParsedEbuild, error)   // highest non-live, else ErrNoEbuilds
```

Expected (plan 14): `func Compare(a, b string) int`, with `1.2.3-r1 > 1.2.3`.

**If `cmd/larry/testhelp_test.go` with `execCmd` and `fixtureOverlay` does not exist,
plan 13a has not landed. STOP and report — do not hand-roll a second `cmd/larry`
harness.**

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D1 | `internal/importcmd/import.go:528-531` | `targetCatDir := filepath.Join(targetOverlay, filepath.Dir(srcEbuildPath))` with an **absolute** source path, then `targetPkgDir := filepath.Join(targetCatDir, filepath.Base(srcEbuildPath))`. Result: `<overlay>/var/db/repos/guru/dev-lisp/foo/foo-1.0.0.ebuild/foo-1.0.0.ebuild`, and `MkdirAll` at `:531` creates the whole bogus tree. Script (L1692): `dst_dir = "$OVERLAY_DIR/$category/$package"` |
| D2 | `cmd/larry/import.go:56-57` | `SourceOverlay: config.ResolveOverlay(importFrom)` — with `--from` unset this returns the **local** overlay, so `findSourceOverlay` (`import.go:42-47`) takes the explicit branch and the `/var/db/repos/*` scan at `:49-71` never runs. `TargetOverlay` at `:57` is then set to the same path |
| D3 | `internal/importcmd/import.go:60-69` | even when reached, the scan has no equivalent of the script's "skip our own overlay" rule (L1603–1605) |
| D4 | `internal/importcmd/import.go:42-47` | `--from guru` (a repo *name*) is not supported: `os.Stat("guru")` fails and the whole import aborts. The script retries as `/var/db/repos/$from_repo/$category/$package` (L1659–1666) |
| D5 | `internal/importcmd/import.go:120-125, 149-151, 509` | live ebuilds are not excluded — `findEbuild(srcCatDir, catPkg, false)` at `:509` passes `liveOnly=false`, which filters **nothing**, and `compareVersions` ranks `9999` highest, so `foo-9999.ebuild` is copied whenever present. `isLive()` at `:149-151` also matches any `_p` substring, so `_p20240101`, `_pre1` and `_patch1` are misclassified as live. Script (L1683): `find … ! -name "*-9999.ebuild" … \| sort -V` |
| D6 | `internal/importcmd/import.go:185-225` (plus its helpers `:227-267`) | `compareVersions` splits on `.`, truncates at the first non-digit, and ignores revisions and `_alpha/_beta/_rc`. `1.2.3-r1 == 1.2.3`. `internal/version` already exists and is not used |
| D12 | `internal/importcmd/import.go:531, 537-542` | `--dry-run` calls `MkdirAll` at `:531` **before** the early return at `:537`, and the returned result has `MetaPath == ""` so `cmd/larry/import.go:71-73` prints nothing about metadata |
| D13 | `internal/importcmd/import.go:20, 269-276` | `--force` is stored on `ImportConfig` and never read; `isVersionNewer` is never called |
| D15 | `internal/importcmd/import.go:454, 464, 589` | `autoDetectOverlay` (`:454`), `autoDetectOverlayDirs` (`:464`) and `findOverlaysViaPortageq` (`:589`) are dead. `golangci-lint` reports only `findOverlaysViaPortageq` as `unused`; the other two are kept alive by each other |

---

## Target behaviour

1. **Source resolution**, in this order (**D2, D3, D4**):
   1. `--from <path>` that `os.Stat`s as a directory → use it;
   2. `--from <name>` that does not stat → `/var/db/repos/<name>`; if *that* does not
      stat, error `source not found: <name>`;
   3. no `--from` → scan `/var/db/repos/*/`, **skipping any entry whose
      `filepath.Clean` equals the target overlay's**, first match containing
      `<category>/<package>` wins;
   4. nothing found → error naming `--from`.

   New signature, replacing `findSourceOverlay`:

   ```go
   // resolveSourceOverlay returns the overlay root holding catPkg and the repo name
   // (the overlay directory's base name) for the provenance line.
   func resolveSourceOverlay(from, targetOverlay, catPkg, reposRoot string) (path, repoName string, err error)
   ```

   `reposRoot` is a parameter, not the hardcoded `/var/db/repos` at `:50`, so tests can
   point it at `t.TempDir()`.
2. **Target** is always `filepath.Join(targetOverlay, a.Category, a.PN)`, built from
   `atom.ParseAtom`'s result — never from the source path. (**D1**)
3. **Source ebuild** is the highest-versioned **non-live** ebuild, selected with
   `atom.FindLatest` (which already excludes `9999` and orders with `internal/version`).
   Delete the whole local block: `findEbuild` (`:94-141`), `ebInfo` (`:143-147`),
   `isLive` (`:149-151`), `extractEbuildInfo` (`:154-183`), `compareVersions`
   (`:185-226`), `isLiveVersion` (`:227-229`), `extractLiveSuffix` (`:231-236`),
   `splitVersion` (`:238-253`), `parseIntOrZero` (`:255-267`). (**D5, D6**)
4. **Version guard** (**D13**): if the target package dir already has an
   equal-or-higher version, refuse with
   `target already has <PVR> (>= <source PVR>); use --force to overwrite` unless
   `--force`. Compare with `version.Compare` on `PVR`, via `atom.FindLatest` on the
   target dir; `atom.ErrNoEbuilds` from the target means "nothing there, proceed".
   Replace `isVersionNewer` (`:269-276`) with this.
5. **`--dry-run` performs no filesystem writes at all** — including `MkdirAll` — and
   reports every path it would create, with `MetaPath` populated. Move the `MkdirAll`
   below the dry-run return. (**D12**, and `PLAN.md` → Cross-task conventions →
   Dry-run.)
6. Delete `autoDetectOverlay`, `autoDetectOverlayDirs`, `findOverlaysViaPortageq`.
   (**D15**)

---

## Tests (write first — every one must fail before you implement)

### `internal/importcmd/import_test.go`

Shared fixture helper — build it once:

```go
// importFixture creates reposRoot/<repo>/<cat>/<pn>/ for each named repo and
// writes the given ebuild basenames as empty files. Returns reposRoot.
func importFixture(t *testing.T, repos map[string][]string, catPkg string) string
```

| Test | Exact input | Exact expected output |
|---|---|---|
| `TestImport_TargetPath` (**D1**) | source `<reposRoot>/guru/dev-lisp/foo/foo-1.0.0.ebuild`, target overlay `t.TempDir()` | the ebuild lands at exactly `<target>/dev-lisp/foo/foo-1.0.0.ebuild`; additionally assert `<target>/var` does **not** exist. **Regression test for D1.** |
| `TestImport_AutoSearchSkipsOwnOverlay` (**D2, D3**) | `reposRoot` containing `mine/dev-lisp/foo/foo-1.0.0.ebuild` and `guru/dev-lisp/foo/foo-2.0.0.ebuild`; `targetOverlay = <reposRoot>/mine`; `from = ""` | the chosen source overlay is `<reposRoot>/guru` and the copied ebuild is `foo-2.0.0.ebuild` |
| `TestImport_AutoSearchNoneFound` | `reposRoot` with only `mine/` holding the package, target = `mine` | error mentioning `--from`; nothing written |
| `TestImport_FromRepoName` (**D4**) | `from = "guru"`, `reposRoot` holding `guru/dev-lisp/foo/foo-1.0.0.ebuild` | resolves to `<reposRoot>/guru`; `repoName == "guru"` |
| `TestImport_FromAbsolutePath` | `from = <reposRoot>/guru` | same result as above; `repoName == "guru"` |
| `TestImport_FromUnknownRepoName` | `from = "nosuch"` | error whose text contains `nosuch`; nothing written |
| `TestImport_ExcludesLiveEbuild` (**D5**) | source dir holds `foo-1.0.0.ebuild` and `foo-9999.ebuild` | `foo-1.0.0.ebuild` is copied; `foo-9999.ebuild` is not |
| `TestImport_PostReleaseIsNotLive` (**D5**) | source dir holds `foo-1.0_p20240101.ebuild` only | it is copied (it is a normal version, not live) |
| `TestImport_PreReleaseIsNotLive` (**D5**) | source dir holds `foo-1.0_pre1.ebuild` only | it is copied |
| `TestImport_VersionCompareUsesVersionPkg` (**D6**) | source dir holds `foo-1.2.3.ebuild` and `foo-1.2.3-r1.ebuild` | `foo-1.2.3-r1.ebuild` is copied |
| `TestImport_NumericOrdering` (**D6**) | source dir holds `foo-9.0.ebuild` and `foo-10.0.ebuild` | `foo-10.0.ebuild` is copied |
| `TestImport_RefusesEqualOrNewerWithoutForce` (**D13**) | source `foo-1.0.0`, target already holds `foo-1.0.0.ebuild` with distinct bytes `OLD\n`; `force = false` | returns an error containing `--force`; the target ebuild's bytes are still `OLD\n` |
| `TestImport_ForceOverwrites` (**D13**) | same, `force = true` | returns nil; the target ebuild's bytes now equal the source's |
| `TestImport_AllowsNewerWithoutForce` (**D13**) | source `foo-2.0.0`, target holds `foo-1.0.0.ebuild` | returns nil; **both** files exist in the target (import adds, it does not replace) |
| `TestImport_DryRunNoWrites` (**D12**) | `dryRun = true`, target overlay is an empty `t.TempDir()` | after the call, `os.ReadDir(target)` returns **zero** entries; the returned `ImportResult` has non-empty `EbuildPath` **and** non-empty `MetaPath` |
| `TestImport_CopiesFilesDir` | source package dir has `files/foo.patch` with bytes `PATCH\n` | `<target>/dev-lisp/foo/files/foo.patch` exists with bytes `PATCH\n` |

**D15 is deletion-only — no unit test by design.** It is verified by the `unused`
linter line under Acceptance (same convention as plan 25b's C4).

### `cmd/larry/import_test.go` (plan 13a's harness)

| Test | Exact input | Exact expected output |
|---|---|---|
| `TestImportCmd_DryRunPrintsMetadataPath` (**D12**) | `execCmd(t,"import","--dry-run","dev-lisp/foo")` against a fixture | stdout contains a line matching `  Metadata: ` followed by a path ending in `/dev-lisp/foo/metadata.xml` |
| `TestImportCmd_FromDefaultsToUnset` (**D2**) | `execCmd(t,"import","dev-lisp/foo")` with no `--from` | the resolved source overlay is **not** the target overlay |

---

## Implementation steps

Every step has a test above. Write the test red first.

1. Add `resolveSourceOverlay(from, targetOverlay, catPkg, reposRoot string) (path,
   repoName string, err error)` implementing the four-step order; delete
   `findSourceOverlay` (`:40-72`). Thread `reposRoot` through `ImportConfig` as a new
   field `ReposRoot string` defaulting to `/var/db/repos` when empty.
   Covers: `TestImport_AutoSearchSkipsOwnOverlay`, `_AutoSearchNoneFound`,
   `_FromRepoName`, `_FromAbsolutePath`, `_FromUnknownRepoName`. (**D2, D3, D4**)
2. In `cmd/larry/import.go:55-60`, stop passing `config.ResolveOverlay(importFrom)` as
   `SourceOverlay`; pass the raw `importFrom` string through instead (the resolver now
   owns interpreting it). Keep `TargetOverlay: config.ResolveOverlay("")`.
   Covers: `TestImportCmd_FromDefaultsToUnset`. (**D2**)
3. Build the target from `atom.ParseAtom(targetOverlay, catPkg)`:
   `targetPkgDir := filepath.Join(targetOverlay, a.Category, a.PN)`. Delete
   `import.go:528-529`.
   Covers: `TestImport_TargetPath`. (**D1**)
4. Replace the source-ebuild selection at `:509` with `atom.FindLatest(srcPkgDir)`.
   Delete `findEbuild`, `ebInfo`, `isLive`, `extractEbuildInfo`, `compareVersions`,
   `isLiveVersion`, `extractLiveSuffix`, `splitVersion`, `parseIntOrZero`
   (`import.go:94-267`) **and their tests**.
   Covers: `TestImport_ExcludesLiveEbuild`, `_PostReleaseIsNotLive`,
   `_PreReleaseIsNotLive`, `_VersionCompareUsesVersionPkg`, `_NumericOrdering`.
   (**D5, D6**)
5. Implement the `--force` guard with `atom.FindLatest` on the **target** dir and
   `version.Compare`. Delete `isVersionNewer` (`:269-276`).
   Covers: `TestImport_RefusesEqualOrNewerWithoutForce`, `_ForceOverwrites`,
   `_AllowsNewerWithoutForce`. (**D13**)
6. Move `os.MkdirAll` (`:531`) below the dry-run return (`:537-542`); populate
   `MetaPath` in the dry-run result.
   Covers: `TestImport_DryRunNoWrites`, `TestImportCmd_DryRunPrintsMetadataPath`.
   (**D12**)
7. Delete `autoDetectOverlay` (`:454`), `autoDetectOverlayDirs` (`:464`) and
   `findOverlaysViaPortageq` (`:589`), plus any tests naming them.
   Covers: nothing — verified by the `unused` linter. (**D15**)
8. Hand back listing every function and test you deleted.

**Checkpoint — after step 4, `go test ./internal/importcmd/ -count=1` must be green.**
If budget is short, stop there and hand back; steps 5–7 are a clean second session.

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

Also Tier 1 — the real proof, entirely against `t.TempDir()`:

```bash
go test ./internal/importcmd/ ./cmd/larry/ -count=1 -v

golangci-lint run --enable-only=unused ./internal/importcmd/...
# expect: no findings
```

`TestImport_TargetPath` is the regression test for the headline bug.

**Tier 2 — read-only, safe to run:**

```bash
grep -n 'func compareVersions\|func isVersionNewer\|func extractEbuildInfo\|func autoDetectOverlay\|func findOverlaysViaPortageq' internal/importcmd/import.go
# expect: no output

grep -n 'filepath.Dir(srcEbuildPath)' internal/importcmd/import.go
# expect: no output
```

```bash
# Tier 2: --dry-run must create nothing — that IS defect D12, so verify the tree.
cd ~/Developer/gentoo-overlay
git status --porcelain > /tmp/before.txt
larry import --dry-run dev-lisp/sbcl-unicode-dictionary
# expect: names <overlay>/dev-lisp/sbcl-unicode-dictionary/{<pkg>.ebuild,metadata.xml}
git status --porcelain > /tmp/after.txt
diff /tmp/before.txt /tmp/after.txt     # expect: no differences
```

**Tier 3 — DO NOT RUN. Report verbatim for a human.**

```bash
# Tier 3 — DO NOT RUN. Report verbatim.
# Writes a new package into ~/Developer/gentoo-overlay.
larry import dev-lisp/sbcl-unicode-dictionary
find ~/Developer/gentoo-overlay/var -maxdepth 1    # expect: no such path was created
ls ~/Developer/gentoo-overlay/dev-lisp/sbcl-unicode-dictionary/
larry import --from guru dev-lisp/sbcl-unicode-dictionary   # bare repo name works
git -C ~/Developer/gentoo-overlay clean -fd dev-lisp/sbcl-unicode-dictionary   # restore
```

## Out of scope

metadata.xml, the maintainer config, provenance output and the prepare invocation —
all of that is `plan/28b-PLAN-IMPORT-METADATA.md`. Do not fix unrelated bugs you
notice — report them.
