# 24 — `prepare`: go pipeline

**Why:** The go pipeline cannot succeed under any input. It constructs a path to an
ebuild that does not exist, reads it, and returns the read error. Beyond that first
failure it builds the tarball with the wrong root, writes it to only one of the two
required locations, and tries to upload release assets to the *upstream project's*
GitHub repo.

**Precondition:** 12 (`internal/atom`), 20b (`20b-PLAN-EBUILD-DETECT.md`, type
detection), 21 (manifest).

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent on this machine, this
plan's text is authoritative — do not guess at the script's behaviour.**
Function: `prepare_go` (L445–497).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. In particular: the known-red baseline is
> exactly `TestExtractVar/multi_line` in `internal/prepare` plus one `go vet` finding at
> `internal/git/git.go:318`. Do not chase either.

The script, in full (L445–497):

```fish
gh_url  = first https://github.com/<owner>/<repo> in the ebuild file
tag     = "v$_pv" with a trailing -rN stripped
work    = mktemp -d
curl -sL "$gh_url/archive/refs/tags/$tag.tar.gz" | tar xz -C "$work"
src_dir = the single top-level directory in $work
cd "$src_dir" && go mod vendor
pkg_dir = dirname of the ebuild
tar -czf "$pkg_dir/$_p-deps.tar.gz" -C "$src_dir" vendor
mkdir -p $DISTDIR
cp "$pkg_dir/$_p-deps.tar.gz" "$DISTDIR/$_p-deps.tar.gz"
generate_manifest $ebuild_path
```

Note what the script does **not** do: it never creates or uploads a GitHub release.

---

## Precondition check — run these first

```bash
go doc ./internal/atom StripRevision
go doc ./internal/atom ParseEbuildPath
go doc ./internal/atom ParsedEbuild
grep -n 'func runManifest' internal/prepare/manifest.go
grep -n 'HTTPClient' internal/prepare/prepare.go
```

Expected (plan 12):

```go
func StripRevision(s string) string            // anchored -r\d+$ only
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
```

Expected (plan 21): `func runManifest(overlay, catPkg, distdir string) error` — the
**three-argument `catPkg`** form. If `runManifest`'s second parameter is still named
`path`, plan 21 has not landed.

Expected (already at HEAD): `internal/prepare/prepare.go:25` declares
`HTTPClient *http.Client` on `PrepareConfig`.

**If any of these is missing, STOP and report.** Do not implement the prerequisite
yourself.

---

## Step 0 — make the pipeline testable (do this before anything else)

Three of the tests below are **unwritable against today's code**, because `PrepareGo`
constructs its own collaborators and hardcodes the github.com host:

| Line | Hardcoded collaborator |
|---|---|
| `internal/prepare/go.go:41` | `clientGH := github.NewClient()` |
| `internal/prepare/go.go:63` | `runner := github.NewClient().Runner()` |
| `internal/prepare/go.go:166` | `clientHTTP := httpclient.NewClient()` inside `downloadGoSource` |
| `internal/prepare/go.go:134, 165` | the `github.com` / `gh api` host, not injectable |

Add two fields to `PrepareConfig` (`internal/prepare/prepare.go:18-28`), next to the
existing `HTTPClient` at `:25`:

```go
// GitHubBaseURL is the scheme+host used to build archive URLs.
// Empty means "https://github.com". Tests set it to an httptest server URL.
GitHubBaseURL string
// Runner executes external commands (`go mod vendor`). Nil means the real runner.
Runner github.CmdRunner
```

`github.CmdRunner` already exists (`internal/github/github.go:15-18`):

```go
type CmdRunner interface {
    Run(cmd *exec.Cmd) error
    Output(cmd *exec.Cmd) ([]byte, error)
}
```

`prepareGo` must read **both** new fields plus the existing `cfg.HTTPClient`, with these
defaults applied in one place at the top of the function:

```go
base := cfg.GitHubBaseURL
if base == "" {
    base = "https://github.com"
}
runner := cfg.Runner
if runner == nil {
    runner = github.NewClient().Runner()
}
```

Every test below sets `GitHubBaseURL: srv.URL` (an `httptest.NewServer`) and a
**recording runner** whose `Run` stub, when `cmd.Args` starts with `go mod vendor`,
creates `<cmd.Dir>/vendor/x/y.go` with the bytes `package y\n` and returns nil. Put that
stub in `internal/prepare/go_test.go` as:

```go
type recRunner struct{ calls [][]string }

func (r *recRunner) Run(cmd *exec.Cmd) error {
    r.calls = append(r.calls, cmd.Args)
    if len(cmd.Args) >= 3 && cmd.Args[1] == "mod" && cmd.Args[2] == "vendor" {
        if err := os.MkdirAll(filepath.Join(cmd.Dir, "vendor", "x"), 0o755); err != nil {
            return err
        }
        return os.WriteFile(filepath.Join(cmd.Dir, "vendor", "x", "y.go"), []byte("package y\n"), 0o644)
    }
    return nil
}

func (r *recRunner) Output(cmd *exec.Cmd) ([]byte, error) {
    r.calls = append(r.calls, cmd.Args)
    return nil, nil
}
```

---

## Name collision — resolve it, do not work around it

Today there are two symbols:

- `internal/prepare/go.go:21` — `func PrepareGo(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error` (the real body)
- `internal/prepare/prepare.go:149-152` — `func prepareGo(...)`, a one-line pass-through
  to `PrepareGo`, called from the dispatch at `internal/prepare/prepare.go:79`

**Decision: keep the exported `PrepareGo` in `go.go`. Delete the `prepareGo` wrapper
(`prepare.go:149-152`) and call `PrepareGo(ei, cfg)` directly from the dispatch at
`prepare.go:79`.** Every test name below says `PrepareGo` and means the exported
function. (`prepareNpm` and `prepareCargo` are *not* in scope — leave them alone.)

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D1 | `internal/prepare/go.go:75` | passes `filepath.Join(moduleRoot, ei.Pkg+"-"+ei.PV()+".ebuild")` — a fabricated path inside the extracted source. `createGoVendorTarball` (`:253-256`) does `os.ReadFile` on it and returns the error. **`PrepareGo` always fails**: `go: create vendor tarball: read ebuild: …` |
| D2 | `internal/prepare/go.go:271` | `tar czf dest -C vendorDir .` puts vendor's *contents* at the archive root. The go-module eclass expects a `vendor/` directory at the root |
| D3 | `internal/prepare/go.go:270` | writes only to distdir. The script writes `$pkg_dir/$_p-deps.tar.gz` **and** copies to DISTDIR. `copyToDistdir` (`:308`) exists and is never called |
| D4 | `internal/prepare/go.go:77-96, 299-305` | adds GitHub release create+upload that the script does not have, **targeting the upstream `owner/repo`** from `findGoRepo` rather than the overlay's own repo. `uploadGoRelease` (`:299`) runs `gh release upload <tag> /tmp/<filename> -F <filename>` — a hardcoded `/tmp` path and `-F`, which is not a `gh release upload` flag |
| D5 | `internal/prepare/go.go:135` | calls `gh api <owner>/<repo>/releases/tags/vX`, missing the `repos/` prefix, so it always errors; the archive-URL fallback at `:162` then works by accident |
| D6 | `internal/prepare/go.go:70-72` | hardcodes `/usr/portage/distfiles` when `cfg.Distdir == ""` — the exact "Single-DISTDIR consistency" footgun in AGENTS.md |
| D7 | `internal/prepare/go.go:259-267` | filename inconsistency: `createGoVendorTarball` writes `<pVal>-deps.tar.gz` where `pVal` derives from the fabricated path, while `:92` expects `<Pkg>-<PV>-deps.tar.gz` |
| D8 | `internal/prepare/go.go:102` | calls `runManifest(cfg.Overlay, ei.Path, distDir)` — plan 21 replaced that signature with `runManifest(overlay, catPkg, distdir)` |
| D9 | `internal/prepare/go.go:241, 271` and `:203` | shells out to `tar` for both create and extract, contrary to AGENTS.md ("`archive/tar`, `compress/gzip` — archiving (not tar)") |

---

## Target behaviour

```go
func PrepareGo(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error
```

1. Resolve `owner/repo` from the first `https://github.com/<o>/<r>` in the
   variable-expanded ebuild text (`findGoRepo`, `go.go:106-131`, is fine — keep it).
   Error clearly when absent.
2. `tag := "v" + atom.StripRevision(ei.PV())`.
3. Download `<cfg.GitHubBaseURL>/<o>/<r>/archive/refs/tags/<tag>.tar.gz` with
   `cfg.HTTPClient` (falling back to `httpclient.NewClient()` when nil) and extract it
   with `archive/tar` + `compress/gzip` into `os.MkdirTemp`
   (AGENTS.md: no `curl`, no `tar` subprocess). **Non-200 is a hard error — no gh-api
   probe, no fallback URL.** (**D5, D9**)
4. `srcDir` = the single top-level directory of the extraction. Error if there is not
   exactly one, or if it contains no `go.mod`.
5. `go mod vendor` through `runner` with `cmd.Dir = srcDir`.
6. Build `<P>-deps.tar.gz` with `vendor/` as the archive root, using `archive/tar` +
   `compress/gzip`. (**D2, D7, D9**)
7. **Write order is fixed, matching the script's `tar -czf "$pkg_dir/…"` then
   `cp … "$DISTDIR/…"`:** write the tarball to `filepath.Dir(ei.Path)` **FIRST**, then
   call `copyToDistdir(<that path>, cfg.Distdir)`. Do not build it in a temp dir, do not
   write distdir first, do not tar twice. (**D3**)
8. No GitHub release interaction at all. Delete `checkGoReleaseExists` (`:280`),
   `createGoRelease` (`:290`), `uploadGoRelease` (`:299`), `downloadGoSource` (`:133`,
   the `gh api` probe) and their call sites. (**D4, D5**)
   *If* a future go package needs hosted deps, that belongs in the vendored pipeline
   (plan 25b) with the overlay's own repo, not here.
9. Call plan 21's manifest with `category/package`, derived from the real ebuild path:

   ```go
   pe, err := atom.ParseEbuildPath(ei.Path)
   if err != nil {
       return fmt.Errorf("go: parse ebuild path: %w", err)
   }
   catPkg := pe.Category + "/" + pe.PN
   if err := runManifest(cfg.Overlay, catPkg, cfg.Distdir); err != nil {
       return fmt.Errorf("go: pkgdev manifest: %w", err)
   }
   ```
   (**D8**)

`cfg.Distdir == ""` is an error (`go: distdir is empty`), not a default. (**D6**)

---

## Tests (write first — every one must fail before you implement)

`internal/prepare/go_test.go`. **The existing 22 `TestGo_Pipeline_*` functions test the
broken internals** (`downloadGoSource`, `checkGoReleaseExists`, `createGoRelease`,
`uploadGoRelease`, `createGoVendorTarball`'s 3-arg form). Delete the ones whose subject
you delete and list them in your report.

Shared fixture for every happy-path test — define it once as a helper:

```go
// goFixture starts an httptest server serving a gzip tar at
// /<owner>/<repo>/archive/refs/tags/<tag>.tar.gz containing exactly one entry,
// the directory "proj-1.0.0/", plus the file "proj-1.0.0/go.mod" with bytes
// "module example.com/proj\n\ngo 1.22\n".
// Any other path 404s.
func goFixture(t *testing.T, owner, repo, tag string) *httptest.Server
```

And a package fixture: `t.TempDir()/overlay/dev-util/proj/proj-1.0.0.ebuild` whose
content is

```
EAPI=8
inherit go-module
HOMEPAGE="https://github.com/acme/proj"
SRC_URI="https://github.com/acme/proj/archive/refs/tags/v${PV}.tar.gz -> ${P}.tar.gz"
```

with `ei.Pkg="proj"`, `ei.PVVal="1.0.0"`, `ei.PVal="proj-1.0.0"`,
`ei.Path=<that file>`, `ei.HOMEPAGE="https://github.com/acme/proj"`, and
`cfg = &PrepareConfig{Overlay: <overlay>, Distdir: t.TempDir(), GitHubBaseURL: srv.URL,
Runner: &recRunner{}, HTTPClient: srv.Client()}`.
Stub `runManifest` out via the plan-21 seam so no `pkgdev` runs.

| Test | Exact input | Exact expected output |
|---|---|---|
| `TestPrepareGo_HappyPath` | the fixture above | returns `nil`; `<overlay>/dev-util/proj/proj-1.0.0-deps.tar.gz` exists **and** `<distdir>/proj-1.0.0-deps.tar.gz` exists. **Regression test for D1** |
| `TestPrepareGo_TarballRootIsVendorDir` | same | `tar.NewReader(gzip.NewReader(f))` over the package-dir tarball yields header names that **all** have the prefix `vendor/`; at least one is exactly `vendor/x/y.go`; **none** is `./` or `x/y.go`. (**D2**) |
| `TestPrepareGo_WritesBothLocations` | same | `sha256.Sum256` of the two files is byte-equal, and both are non-empty. (**D3**) |
| `TestPrepareGo_WriteOrderIsPkgDirThenDistdir` | same fixture, except `cfg.Distdir` points at an **existing regular file** (create it with `os.WriteFile(filepath.Join(t.TempDir(), "notadir"), nil, 0o644)`), so `copyToDistdir`'s `os.MkdirAll` fails | `PrepareGo` returns a non-nil error **and** `<overlay>/dev-util/proj/proj-1.0.0-deps.tar.gz` already exists on disk. Pins "package dir first, distdir second". (**D3**) |
| `TestPrepareGo_NoGitHubReleaseCalls` | same, but `recRunner` records every `cmd.Args` | no recorded call has `cmd.Args[0] == "gh"`; the only recorded call is `["go","mod","vendor"]`. (**D4**) |
| `TestPrepareGo_TagStripsRevision` | `ei.PVVal = "1.0.0-r2"`, `ei.PVal = "proj-1.0.0-r2"`; server registered for tag `v1.0.0` | server receives exactly one request, for path `/acme/proj/archive/refs/tags/v1.0.0.tar.gz`; no request for `v1.0.0-r2`. |
| `TestPrepareGo_MissingTagIsError` | server 404s every path | error is non-nil and its `Error()` contains `404`; the server recorded exactly **one** request (no fallback second fetch). (**D5**) |
| `TestPrepareGo_EmptyDistdirIsError` | `cfg.Distdir = ""` | error is non-nil, `Error()` contains `distdir`; the server recorded **zero** requests. (**D6**) |
| `TestPrepareGo_DepsFilename` | the fixture | both files are named exactly `proj-1.0.0-deps.tar.gz` (i.e. `<PN>-<PV>-deps.tar.gz`). (**D7**) |
| `TestPrepareGo_ManifestTargetsCatPkg` | the fixture, with the plan-21 manifest seam recording its arguments | recorded call is `runManifest(<overlay>, "dev-util/proj", <distdir>)` — second argument is `dev-util/proj`, **not** a path ending in `.ebuild`. (**D8**) |
| `TestPrepareGo_NoGoMod` | server serves a tar whose single dir `proj-1.0.0/` contains only `README.md` | error non-nil, `Error()` contains `go.mod`. |
| `TestPrepareGo_NoGitHubURL` | `ei.HOMEPAGE = ""`, `ei.SRC_URI = "https://example.com/x.tar.gz"` | error non-nil, `Error()` contains `github`; server recorded zero requests. |
| `TestPrepareGo_MultipleTopLevelDirsIsError` | server serves a tar with `a/` and `b/` at top level | error non-nil, `Error()` mentions `exactly one`. |

---

## Implementation steps

Every step below has a test in the table. Do not implement a step whose test is not red
first.

1. **Step 0** (above): add `GitHubBaseURL` and `Runner` to `PrepareConfig`; read them,
   `cfg.HTTPClient`, and their defaults at the top of `PrepareGo`.
   Covers: all tests (they cannot even compile without it).
2. Delete the `prepareGo` wrapper (`prepare.go:149-152`); change the dispatch at
   `prepare.go:79` to `return PrepareGo(ei, cfg)`.
   Covers: nothing new — mechanical, verified by `go build ./...`.
3. Remove the `/usr/portage/distfiles` default at `go.go:70-72`; return
   `fmt.Errorf("go: distdir is empty")` **before** any network call.
   Covers: `TestPrepareGo_EmptyDistdirIsError`. (**D6**)
4. Replace `downloadGoSource` (`go.go:133-169`) with a direct fetch of
   `<base>/<owner>/<repo>/archive/refs/tags/<tag>.tar.gz` using `cfg.HTTPClient`;
   non-200 → error. Delete the `gh api` probe and the fallback-URL branch.
   Covers: `TestPrepareGo_MissingTagIsError`, `TestPrepareGo_TagStripsRevision`. (**D5**)
5. Rewrite `extractGoSource` (`go.go:203`) with `compress/gzip` + `archive/tar`; keep its
   signature. Make `findGoModuleRoot` (`:209`) reject "not exactly one top-level dir".
   Covers: `TestPrepareGo_NoGoMod`, `TestPrepareGo_MultipleTopLevelDirsIsError`. (**D9**)
6. Change `createGoVendorTarball`'s signature to `(srcDir, outPath string) error` — no
   ebuild path, no distdir. Delete the `os.ReadFile` and the `pVal` regex derivation
   (`go.go:253-267`). Walk `srcDir/vendor` and write `archive/tar` headers whose `Name`
   is `vendor/` + the path relative to `srcDir/vendor`; wrap in `compress/gzip`.
   Covers: `TestPrepareGo_TarballRootIsVendorDir`, `TestPrepareGo_DepsFilename`.
   (**D1, D2, D7, D9**)
7. In `PrepareGo`: compute `out := filepath.Join(filepath.Dir(ei.Path), ei.P()+"-deps.tar.gz")`,
   call `createGoVendorTarball(srcDir, out)`, **then** `copyToDistdir(out, cfg.Distdir)`.
   Covers: `TestPrepareGo_HappyPath`, `_WritesBothLocations`,
   `_WriteOrderIsPkgDirThenDistdir`. (**D3**)
8. Delete `checkGoReleaseExists`, `createGoRelease`, `uploadGoRelease` and their call
   sites at `go.go:77-96`.
   Covers: `TestPrepareGo_NoGitHubReleaseCalls`. (**D4**)
9. Swap the manifest call at `go.go:102` to the `catPkg` form shown in "Target
   behaviour" step 9.
   Covers: `TestPrepareGo_ManifestTargetsCatPkg`. (**D8**)
10. Hand back listing every function and test you deleted.

**Checkpoint — after step 7, `go test ./internal/prepare/ -run TestPrepareGo -count=1`
must be green for everything except the release/manifest tests.** If budget is running
short, stop there and hand back; steps 8–9 are a clean second session.

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

Baseline reminder: `internal/prepare TestExtractVar/multi_line` is already red at HEAD
and belongs to plan 20a/22a; `go vet` already reports `internal/git/git.go:318`. Do not
chase either.

Also Tier 1, and the real proof — none of these needs an overlay or the network:

```bash
go test ./internal/prepare/ -run 'TestPrepareGo' -count=1 -v
```

`TestPrepareGo_TarballRootIsVendorDir` opens the produced archive and asserts the
`vendor/` prefix. `TestPrepareGo_HappyPath` is the regression test for the
always-fails bug.

**Tier 2 — read-only, safe to run:**

```bash
grep -rn 'usr/portage/distfiles' internal/
# expect: no output

grep -rn 'uploadGoRelease\|createGoRelease\|checkGoReleaseExists\|downloadGoSource' internal/
# expect: no output

grep -n 'func prepareGo' internal/prepare/prepare.go
# expect: no output (the wrapper is gone)

grep -n 'exec.Command("tar"' internal/prepare/go.go
# expect: no output
```

**Tier 3 — DO NOT RUN. Report verbatim for a human.**

```bash
# Tier 3 — DO NOT RUN. Report verbatim.
# Writes into ~/Developer/gentoo-overlay and runs real ebuild phases.
cd ~/Developer/gentoo-overlay
larry prepare net-vpn/netbird/netbird-0.80.0.ebuild     # a go-module package
tar tzf net-vpn/netbird/netbird-0.80.0-deps.tar.gz | head -3   # paths start with vendor/
larry prepare -v net-vpn/netbird/netbird-0.80.0.ebuild 2>&1 | grep -i distdir
#   -> prints the resolved DISTDIR; ls that directory for netbird-0.80.0-deps.tar.gz
ebuild net-vpn/netbird/netbird-0.80.0.ebuild digest
ebuild net-vpn/netbird/netbird-0.80.0.ebuild unpack      # eclass finds vendor/
git checkout net-vpn/netbird && rm -f net-vpn/netbird/*-deps.tar.gz   # restore
```

"The resolved DISTDIR" means whatever plan 13a's `config.ResolveDistdir` returns for this
machine (`--distdir` → `$LARRY_DISTDIR` → config file `distdir` → `$PORTAGE_DISTDIR` →
`$XDG_CACHE_HOME/larry/distfiles`). Do not assume `~/.cache/larry/distfiles`; print it
rather than hardcoding it.

## Out of scope

Manifest mechanics (plan 21). Hosting vendored deps on GitHub releases (plan 25b).
`prepareNpm` / `prepareCargo` and their wrappers. Do not fix unrelated bugs you
notice — report them.
