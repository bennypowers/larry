# 22b — `prepare`: npm prefetch, deps tarball, and release upload

**Why:** Preparing one version of an npm package cannot checksum its siblings, because
their distfiles were never downloaded. The deps tarball is always xz-compressed no
matter what extension the asset name claims. `npm install` is run with flags the
reference script does not use, so the cache it produces differs from the one the ebuild
expects. And `gh release create` is invoked without `--notes`, which fails on a
non-TTY stdin — so the very first upload for any package always fails.

**Precondition:** 22a (`ebuild.ParseSrcUri`), 21 (manifest + the `Prefetch` hook),
13b (`(*config.Config).ResolveGitHubRepo`).

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish). Cited below as
`script <func> (L…)`. **If that file is absent on this machine, this plan's text is
authoritative — do not guess at the script's behaviour.**
Functions: `prepare_npm` (L199–298), `npm_fetch_all_distfiles` (L132–175).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. Known-red baseline: `go vet` at
> `internal/git/git.go:318`. Do not chase anything else you did not break.

---

## Precondition check — run this first

```bash
go doc bennypowers.dev/larry/internal/ebuild ParseSrcUri
# expect: func ParseSrcUri(s string, vars VariableMap) []SrcUriEntry

go doc bennypowers.dev/larry/internal/config Config.ResolveGitHubRepo
# expect: func (c *Config) ResolveGitHubRepo(overlay, remote string) (string, error)

grep -n 'Prefetch func(pkgDir, distdir string) error' internal/prepare/manifest.go
# expect: one hit inside manifestRequest

grep -n 'func resolveGitHubRepo' cmd/larry/prepare.go
# expect: NO OUTPUT — plan 13b deleted the private text-parsing duplicate
```

**If `ParseSrcUri` is missing, plan 22a has not landed. If `ResolveGitHubRepo` is
missing, plan 13b has not landed. If `manifestRequest` has no `Prefetch` field, plan 21
has not landed. STOP and report in every case — do not implement the missing piece
yourself.**

If `cmd/larry/prepare.go` still defines `resolveGitHubRepo`, plan 13b has not finished:
that function `strings.ToLower`s every line of `.git/config` (`prepare.go:209`), so it
returns a lowercased `owner/repo` and will 404 against any mixed-case repository name.
**Do not patch it here — stop and report that 13b is incomplete.**

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D5 | not implemented | `npm_fetch_all_distfiles`: before manifesting, download the npm tarballs of **every** ebuild version in the package. Without it, manifesting one version cannot checksum the others, and plan 21's `pkgdev manifest cat/pkg` target makes that visible |
| D6 | `internal/prepare/npm.go:419` | hardcodes `tar cJf` (xz) while the asset name comes from SRC_URI; a `.tar.gz` / `.tar.zst` asset name receives xz bytes. The script uses `tar -caf`, letting tar pick the compressor from the output extension |
| D7 | `internal/prepare/vendored.go:306-312` | `ghReleaseCreate` runs `gh release create <tag>` with no `--title` / `--notes`; `gh` requires notes on a non-TTY stdin, so the first upload for any package always fails. Script passes `--title "$_pn deps" --notes "npm dependency cache for $_pn"` |
| D8 | `internal/prepare/npm.go:408` | the working tree has `npm install --ignore-scripts --cache <dir> <tgz>`. **Two** divergences from the script, not one: (a) `--ignore-scripts` is not in the script, and (b) `--cache` is a **global** flag that must come *before* the `install` subcommand. The script, the comment the diff itself added at `npm.go:402`, and the NOTE in `dev-util/vtsls/vtsls-0.3.0.ebuild:17` all say `npm --cache ./npm-cache install <tgz>` |
| D9 | `internal/prepare/npm.go:341-380` vs `internal/prepare/vendored.go:32, 49` | the npm pipeline produces an `npm-cache/` tarball while `vendored.go` declares `TarballContent = "node_modules"`; `npmDepsTarballHasCache` (which only accepts `npm-cache/`) would reject the vendored pipeline's own output |

**The rest of the working-tree change to `npmCreateDepsTarball` is correct and must be
kept**: the dedicated `tmpDir/npm-cache` directory, the `.npmrc` in `tmpDir`, passing
the `.tgz` path directly to `npm install`, and `cmd.Dir = tmpDir`. Only the flag order
and `--ignore-scripts` are wrong.

---

## Step 0 — add the two subprocess seams (do this first)

`npmCreateDepsTarball` calls `exec.Command("npm", …)` at `npm.go:408` and
`exec.Command("tar", …)` at `npm.go:419` and runs them directly. There is **no seam**,
so `TestNpm_InstallArgs` and `TestNpm_TarballCompressionFromExtension` cannot be written
against the current code. Add two package-level vars in `internal/prepare/npm.go`:

```go
// npmRunner executes the npm subprocess. Tests replace it.
var npmRunner = func(cmd *exec.Cmd) error {
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprint(os.Stderr, string(out))
		return err
	}
	return nil
}

// archiveRunner executes the tar subprocess. Tests replace it.
var archiveRunner = func(cmd *exec.Cmd) error { return cmd.Run() }
```

Route both call sites through them. Tests stub the var and restore it with
`t.Cleanup`. A stub that inspects `cmd.Args` and `cmd.Dir` and then writes a plausible
output file is enough.

---

## Target behaviour

### D5 — prefetch every sibling version's distfiles

```go
// npmPrefetchPackage downloads, into distdir, every mirror://npm/ distfile declared by
// every *.ebuild in pkgDir. Files already present and non-empty are skipped.
// Errors on individual ebuilds are logged and skipped; only a download failure for a
// file that is genuinely missing aborts.
func npmPrefetchPackage(pkgDir, distdir string) error
```

For each `*.ebuild` in `pkgDir` (ignore `*.ebuild.bak`): parse it with
`upstream.ParseEbuildInfo`, build `vars` with `ebuild.BuildVariableMap(raw, pe)`, call
`ebuild.ParseSrcUri`, keep the `mirror://npm/` entries, and download each missing one to
`filepath.Join(distdir, e.Filename)`.

Wire it into the npm call site's `manifestRequest` (plan 21 step 4 left it `nil`):

```go
req := manifestRequest{
	Overlay: cfg.Overlay, CatPkg: pe.Category + "/" + pe.PN,
	Distdir: distDir, EbuildPath: ei.Path,
	Distfiles: distfileNames,          // see below
	Prefetch:  npmPrefetchPackage,
}
```

`distfileNames` is the `Filename` of every entry `ebuild.ParseSrcUri` returned for
**this** ebuild (npm entries and release-asset entries alike). This makes plan 21's
`verifyManifestValid` strict branch live for the first time — plan 21 wired it dormant
on purpose.

`npmPrefetchPackage` runs **before** the Manifest is removed. That is the AGENTS.md
"Manifest integrity depends on pipeline order" rule: fetch → verify → delete manifest →
regenerate. Plan 21's `runManifest` already enforces the ordering; do not fetch
anywhere else.

### D6 — compressor follows the asset extension

Replace `exec.Command("tar", "cJf", outputTarball, "-C", tmpDir, "npm-cache")` with the
script's exact invocation:

```go
cmd := exec.Command("tar", "-caf", outputTarball, "-C", tmpDir, "npm-cache")
```

`-c` create, `-a` choose the compressor from the output filename's extension, `-f` the
output file. One command covers `.tar.xz`, `.tar.gz` and `.tar.zst`.

> **Explicit exception to AGENTS.md.** AGENTS.md's "Native implementations (no
> subprocess)" list says to use `archive/tar` + `compress/gzip` rather than `tar`. That
> cannot cover this case: the stdlib has no xz or zstd **writer**, and the only
> compression dependency in `go.mod` is `github.com/ulikunitz/xz`, which this repo uses
> for **reading** only (`npm.go:351`). There is no zstd writer available at all.
> Shelling out to `tar -caf` is therefore the correct call, and it matches the script
> byte for byte. Record this exception in your handback so it can be added to the
> AGENTS.md archiving bullet.

### D7 — `gh release create` needs a title and notes

```go
func ghReleaseCreate(tag, repo, pn string, runner github.CmdRunner) error {
	cmd := exec.Command("gh", "release", "create", tag,
		"--title", pn+" deps",
		"--notes", "npm dependency cache for "+pn)
	if repo != "" {
		cmd.Args = append(cmd.Args, "--repo", repo)
	}
	return runner.Run(cmd)
}
```

The signature gains `pn`. Its callers are `npm.go:103` and anything in `vendored.go`;
update them all. `ghUploadAsset` (`vendored.go:315`) already passes `--clobber` — leave
it alone.

### D8 — the npm install invocation

```go
cmd := exec.Command("npm", "--cache", npmCacheDir, "install", npmTarball)
cmd.Dir = tmpDir
```

`--cache` **before** `install`; **no** `--ignore-scripts`. Nothing else on that line
changes.

### D9 — one asset contract: `npm-cache`

`internal/prepare/vendored.go:32` declares `TarballContent string // "node_modules" or
"vendor"` and `:49` sets `"node_modules"` for the npm case. Change the npm case to
`"npm-cache"` so it matches what `npmCreateDepsTarball` actually produces and what
`npmDepsTarballHasCache` (`npm.go:341-380`) accepts. Delete the now-unreachable
`createNPMDepsTarball` (`vendored.go:179-…`), which copies a `node_modules` directory.

`vendored.go` is plan **25b**'s file (`25b-PLAN-PREPARE-VENDORED.md`), but execution
order guarantees 22b runs first, and 25b explicitly names plan 22 as the owner of the
`npm-cache` contract it aligns to. **Write `TestVendored_NpmAssetContractIsNpmCache`
here, in `internal/prepare/vendored_test.go`, now. Plan 25b inherits it** — and 25b must
not re-delete `createNPMDepsTarball`.

---

## Tests (write first — every one must be red before you implement)

### `internal/prepare/npm_test.go`

- `TestNpm_InstallArgs` (**D8**) — stub `npmRunner` with a recorder. Assert
  `cmd.Args == []string{"npm","--cache",<cacheDir>,"install",<tgzPath>}` — in that
  exact order — that no element is `--ignore-scripts`, and that `cmd.Dir` is the temp
  dir (the same directory that holds `npm-cache` and `.npmrc`), **not** an extracted
  package directory.
- `TestNpm_TarballCompressionFromExtension` (**D6**) — table over asset names
  `deps.tar.xz`, `deps.tar.gz`, `deps.tar.zst`. Let the **real** `archiveRunner` run
  (stub only `npmRunner`, and have the stub create a `npm-cache/index.json` file under
  the temp dir so tar has something to archive). Assert the first bytes of the produced
  file:

  | asset name | magic bytes |
  |---|---|
  | `*.tar.xz` | `fd 37 7a 58 5a` |
  | `*.tar.gz` | `1f 8b` |
  | `*.tar.zst` | `28 b5 2f fd` |

  For the `.tar.zst` row, `t.Skip("zstd not on PATH")` when
  `exec.LookPath("zstd")` fails — GNU tar delegates zstd to an external binary.
  Also assert the argv is `tar -caf <out> -C <tmp> npm-cache` with a stubbed
  `archiveRunner` in a separate sub-case.
- `TestNpm_PrefetchesSiblingVersions` (**D5**) — a `t.TempDir()` package dir with
  `foo-1.0.ebuild` and `foo-2.0.ebuild`, each with a distinct `mirror://npm/` SRC_URI;
  an `httptest` registry; call `npmPrefetchPackage(pkgDir, distdir)`. Assert **both**
  `foo-1.0.tgz` and `foo-2.0.tgz` land in distdir.
- `TestNpm_PrefetchSkipsExisting` — pre-create a non-empty `foo-1.0.tgz`; assert the
  server was asked only for `foo-2.0.tgz`.
- `TestNpm_PrefetchIgnoresBakFiles` — an extra `foo-0.9.ebuild.bak` in the package dir
  is not parsed and contributes no download.
- `TestNpm_PrefetchRunsBeforeManifest` — stub `prepare.ManifestRunner` with a recorder
  that, when invoked, checks whether both distfiles already exist. Assert they did.
- `TestNpm_ReleaseCreatePassesNotes` (**D7**) — `github.NewStubRunner()`; assert the
  recorded argv contains `--title` immediately followed by `fish-lsp deps`, and
  `--notes` immediately followed by `npm dependency cache for fish-lsp`.
- `TestNpm_ReleaseUploadUsesClobber` — an existing release → the recorded argv is
  `gh release upload <tag> <path> --clobber`; `gh release create` is **not** invoked.

### `internal/prepare/vendored_test.go`

- `TestVendored_NpmAssetContractIsNpmCache` (**D9**) — the npm branch at
  `vendored.go:49` yields `TarballContent == "npm-cache"`. Follow the existing
  stub-runner pattern in this file (`TestVendored_GhReleaseView_Exists` at `:257`
  threads a `github.CmdRunner` explicitly) rather than inventing a new one.

---

## Implementation steps

Each step has its test named above. Write the test, watch it fail, then implement.

1. **Step 0** — add `npmRunner` and `archiveRunner`; route `npm.go:408` and `npm.go:419`
   through them. No behaviour change yet; `go test ./internal/prepare/ -count=1` must
   stay as green as it was.
2. **D8** — reorder to `npm --cache <dir> install <tgz>`, drop `--ignore-scripts`.
   Test: `TestNpm_InstallArgs`.
3. **D6** — switch to `tar -caf`.
   Test: `TestNpm_TarballCompressionFromExtension`.
4. **D7** — add `pn` to `ghReleaseCreate`, pass `--title` / `--notes`, update callers.
   Test: `TestNpm_ReleaseCreatePassesNotes`, `TestNpm_ReleaseUploadUsesClobber`.
5. **D5** — write `npmPrefetchPackage`; set `Prefetch` and `Distfiles` on the npm
   pipeline's `manifestRequest`.
   Tests: the four `TestNpm_Prefetch*`.
6. **D9** — change the npm `TarballContent` to `npm-cache`; delete
   `createNPMDepsTarball`.
   Test: `TestVendored_NpmAssetContractIsNpmCache`.
7. Repoint the GitHub-repo lookup at `(*config.Config).ResolveGitHubRepo(overlay,
   remote)` if any npm code path still reads `cfg.GitHubRepo` from the deleted private
   helper. `cfg.GitHubRepo` itself stays — plan 13b fills it in `cmd/larry`.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**.

**Tier 1 — automated, required. Run all of these.**

```bash
go build ./...
go vet ./...                  # expect ONLY internal/git/git.go:318 (known baseline)
go test ./... -count=1        # no NEW failures vs. your recorded baseline
golangci-lint run ./...       # per AGENTS.md, required before any push
```

```bash
go test ./internal/prepare/... -count=1

grep -n 'ignore-scripts' internal/prepare/npm.go
# expect: no output

grep -n '"cJf"' internal/prepare/npm.go
# expect: no output

grep -n 'node_modules' internal/prepare/vendored.go
# expect: only the "vendor"-side / generic text, never the npm branch

grep -rn 'func createNPMDepsTarball' internal/prepare/
# expect: no output
```

**Tier 2 — read-only manual. Safe to run.**

```bash
# Confirm the ebuilds themselves document the flag order this plan restores.
grep -n 'npm --cache' ~/Developer/gentoo-overlay/dev-util/vtsls/vtsls-0.3.0.ebuild
# expect: line 17 — npm --cache ./npm-cache install $(portageq envvar DISTDIR)/${P}.tgz

tar --help | grep -- '-a,'
# expect: tar on this box supports -a (auto-compress by extension)
```

**Tier 3 — MUTATING. DO NOT RUN. Report these verbatim for a human.**

> **DO NOT RUN.** Every command below calls `gh release upload`, which **publishes a
> real asset to a real GitHub repository**. It is not reversible by `git checkout` and
> it is not reversible by anything else you can do from here. It also rewrites
> `Manifest` files inside `~/Developer/gentoo-overlay`.

```bash
# Tier 3 — DO NOT RUN. Full npm pipeline, including a real release upload.
cd ~/Developer/gentoo-overlay
larry prepare dev-util/fish-lsp/fish-lsp-1.1.5.ebuild
grep -c '^DIST' dev-util/fish-lsp/Manifest    # expect entries for 1.1.4 AND 1.1.5 (D5)
ebuild dev-util/fish-lsp/fish-lsp-1.1.5.ebuild digest   # no empty entries
git checkout dev-util/fish-lsp                 # restores the Manifest, NOT the upload
```

```bash
# Tier 3 — DO NOT RUN. Real-build verification of the D8 flag change, per AGENTS.md
# "Regression verification with real builds".
cd ~/Developer/gentoo-overlay
ebuild dev-util/fish-lsp/fish-lsp-1.1.5.ebuild install
# the install phase must succeed with `npm --offline --cache` against the new tarball
```

Prove D5, D6, D7 and D8 at Tier 1 with the tests above instead.

---

## Out of scope

Manifest mechanics (plan 21). SRC_URI parsing and the `${P}` guard (plan 22a). The
vendored pipeline beyond the `npm-cache` contract line and `ghReleaseCreate`
(plan 25b). cargo and go pipelines (plans 23, 24). Do not fix unrelated bugs you
notice — report them.
