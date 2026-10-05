# 21 — `prepare`: manifest generation, DISTDIR, `--all`, failure aggregation

**Why:** The manifest step is the last stage of every pipeline, so its bugs mask all the
others. Right now it deletes the package `Manifest` and then regenerates entries for a
single ebuild, destroying every sibling version's entry. `prepare --all` is a silent
no-op. DISTDIR can be the empty string.

**Precondition:** 13a (`cmd/larry` test harness + `ResolveDistdir`), 20b (detection).
Plan 13 has been split: **13a** (`13a-PLAN-CLI-HARNESS.md`) owns the harness,
**13b** (`13b-PLAN-CLI-OUTPUT.md`) owns logging and `ResolveGitHubRepo`.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish). Cited below as
`script <func> (L…)`. **If that file is absent on this machine, this plan's text is
authoritative — do not guess at the script's behaviour.**
Functions: `generate_manifest` (L177–195), `do_prepare_one` (L553–583), the `prepare`
dispatch block (L1800–1845).

The script, exactly:

```fish
mkdir -p $DISTDIR
cd $OVERLAY_DIR
npm_fetch_all_distfiles $cat_pkg          # see plan 22b
rm -f $OVERLAY_DIR/$cat_pkg/Manifest
pkgdev manifest -d $DISTDIR $cat_pkg
```

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. Known-red baseline: `go vet` at
> `internal/git/git.go:318`, plus `internal/prepare TestExtractVar/multi_line` **if plan
> 20a has not yet deleted it**. Do not chase anything else you did not break.

---

## Precondition check — run this first

```bash
go doc bennypowers.dev/larry/internal/atom ParseEbuildPath
# expect: func ParseEbuildPath(path string) (*ParsedEbuild, error)

go doc bennypowers.dev/larry/internal/config Config.ResolveDistdir
# expect: func (c *Config) ResolveDistdir(override string) string
#         (plan 13a guarantees it never returns "")

go doc bennypowers.dev/larry/internal/ebuild ParsePackageType
# expect: func ParsePackageType(s string) (PackageType, error)

grep -n 'func execCmd\|func fixtureOverlay' cmd/larry/testhelp_test.go
# expect: both present
```

**If `ParseEbuildPath` is missing, plan 12 has not landed. If `ResolveDistdir` is
missing, plan 13a has not landed. If `ParsePackageType` is missing, plan 20b has not
landed. In every case STOP and report — do not implement the missing piece yourself.**

> **Plan 13a owns the `execCmd` / `fixtureOverlay` harness in
> `cmd/larry/testhelp_test.go`. If it is absent, STOP and report. Do not hand-roll a
> second harness.**

Confirm the starting shape:

```bash
grep -n 'func runManifest\|func verifyManifestValid\|func RunManifestCommand\|func GetManifestArgs' internal/prepare/manifest.go
# expect: 28, 113, 107, 102
grep -rn 'runManifest(' internal/prepare/ | grep -v _test
# expect exactly 5 call sites: prepare.go:130 npm.go:134 cargo.go:63 go.go:102 vendored.go:417
```

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D1 | `internal/prepare/manifest.go:41` (uncommitted change) | runs `pkgdev manifest -d <distdir> <absolute ebuild path>`. Combined with `rm -f Manifest` at `:53-55`, this **wipes every other version's Manifest entries**. The comment at `:25-31` claims it "matches the fish script"; it does not — the script targets `category/package` |
| D2 | `internal/prepare/prepare.go:290-294` vs `manifest.go:41` | `manifestArgs` still returns `{path, "digest"}` (ebuild-style) while `runManifest` hardcodes pkgdev-style args. `findManifestRunner` (`prepare.go:274-288`) now prefers `pkgdev` but falls back to `ebuild`, in which case the hardcoded args produce the invalid `ebuild manifest -d <distdir> <path>` |
| D3 | `internal/prepare/manifest.go:20-23` | `manifestRunnerArgs` is dead after the uncommitted change. `manifestArgs` is **still tested** at `internal/prepare/prepare_test.go:442-467` (`TestManifestArgs`), asserting the old `{path,"digest"}` shape — that test goes red the moment you change the arg builder |
| D4 | `internal/prepare/manifest.go` | no `mkdir -p $DISTDIR` before invoking the runner |
| D5 | `cmd/larry/prepare.go:81-88` | DISTDIR can resolve to `""` → `pkgdev manifest -d "" …` and `DISTDIR=` in the child env. Plan 13 fixes resolution; **this plan must stop accepting `""`** |
| D6 | `internal/prepare/manifest.go:47` | sets `GENTOO_MIRRORS=` (empty) — not in the script; disables mirror fallback for non-local distfiles |
| D7 | `internal/prepare/manifest.go:113-141` | `verifyManifestValid` fails when the Manifest has zero `DIST` lines; meta/live ebuilds with no SRC_URI legitimately produce none. Its signature `(ebuildPath string)` gives it **no access to SRC_URI**, so it cannot tell "no distfiles expected" from "manifest broken" |
| D8 | `cmd/larry/prepare.go:116-118` → `internal/prepare/prepare.go:87-100, 247-266` | `--all` calls `PrepareAll(overlayRoot)` → `collectEbuilds` is **non-recursive**, finds zero ebuilds at the overlay root, returns `nil`. Silent success, nothing done |
| D9 | `internal/prepare/prepare.go:94-98`, `cmd/larry/prepare.go:138-144` | aborts on first failure; the script counts failures, continues, and exits 1 at the end (L1836–1844) |
| D10 | `cmd/larry/prepare.go:156` | `filepath.SplitList(path)` splits on `:` (the PATH-list separator), not `/`; the `category/package` branch is unreachable |
| D11 | `cmd/larry/prepare.go:125-136, 151-185` | `resolvePrepareTarget` resolves the path internally but returns only `(*EbuildInfo, os.FileInfo, error)`; the caller then uses the **unresolved** `args[0]` at `:132` and `:136`, so `larry prepare dev-util/foo` from outside the overlay stats OK and then fails to open |
| D12 | `internal/prepare/manifest.go:96-109` | `FindManifestRunner`, `GetManifestArgs` and `RunManifestCommand` are **exported passthroughs with the old signatures** and no callers outside the package. Changing `runManifest`/`manifestArgs` without touching them is a **compile error** |

---

## Target behaviour

### 1. `runManifest` takes a request struct

Five positional arguments would be error-prone across five call sites. Use a struct:

```go
// manifestRequest is the input to runManifest.
type manifestRequest struct {
	Overlay   string // overlay root; becomes cmd.Dir
	CatPkg    string // "dev-util/fish-lsp" — NEVER a file path (D1)
	Distdir   string // must be non-empty (D5)
	Distfiles []string // distfile basenames the caller expects; may be nil
	// Prefetch runs after the distdir mkdir and BEFORE the Manifest is removed.
	// May be nil. A non-nil error aborts without touching the Manifest.
	Prefetch func(pkgDir, distdir string) error
}

func runManifest(req manifestRequest) error
```

Order of operations — this ordering is the AGENTS.md "Manifest integrity depends on
pipeline order" learning, do not reshuffle it:

1. `if req.Distdir == ""` → error `"manifest: distdir is empty"` (**D5**).
2. `os.MkdirAll(req.Distdir, 0o755)` (**D4**).
3. `pkgDir := filepath.Join(req.Overlay, req.CatPkg)`.
4. If `req.Prefetch != nil`, call `req.Prefetch(pkgDir, req.Distdir)`; return its error
   unchanged. **Nothing has been deleted yet at this point.**
5. `os.Remove(filepath.Join(pkgDir, "Manifest"))` — ignore `os.IsNotExist`.
6. Resolve the runner and its args (below), then invoke through `ManifestRunner`.
7. `verifyManifestValid(pkgDir, req.Distfiles)`.

Environment passed to the child: `os.Environ()` plus `DISTDIR=<req.Distdir>`.
**No `GENTOO_MIRRORS` entry at all** (**D6**) — do not set it to anything, not even a
non-empty value.

### 2. Runner and args can no longer disagree (D2, D3)

```go
type manifestStyle int

const (
	stylePkgdev manifestStyle = iota // pkgdev manifest -d <distdir> <cat/pkg>, cwd = overlay
	styleEbuildDigest                // <ebuild path> digest, cwd = overlay
)

func findManifestRunner() (bin string, style manifestStyle, err error)
func manifestArgs(style manifestStyle, distdir, catPkg, ebuildPath string) []string
```

`findManifestRunner` keeps today's preference order from `prepare.go:274-288`: `pkgdev`
first, then `ebuild`, then an error. It now also returns the style, so the fallback can
no longer be handed pkgdev-shaped args.

`manifestArgs`:
- `stylePkgdev` → `[]string{"manifest", "-d", distdir, catPkg}`
- `styleEbuildDigest` → `[]string{ebuildPath, "digest"}`

`styleEbuildDigest` needs a concrete ebuild path, which `manifestRequest` does not carry.
Add `EbuildPath string` to the struct for that one purpose and document it as
"only consulted when the runner is `ebuild`; `pkgdev` ignores it". Call sites already
have it.

Delete `manifestRunnerArgs` (`manifest.go:20-23`).

### 3. Exported seams (D12 + testability)

`manifestRunner` is an unexported package var and `PrepareFromFile` is a plain func, so
neither the subprocess nor a per-target failure can be stubbed from `cmd/larry`'s
external test package. Export both seams:

```go
// internal/prepare/manifest.go
// ManifestRunner executes the resolved manifest subprocess. Tests replace it.
var ManifestRunner = func(bin string, args []string, dir string, env []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprint(os.Stderr, stdout.String(), stderr.String())
		return err
	}
	logger.Debug("stdout:\n" + stdout.String())
	logger.Debug("stderr:\n" + stderr.String())
	return nil
}

// internal/prepare/prepare.go
// PrepareFile prepares one ebuild. Tests replace it to simulate per-target failures.
var PrepareFile = PrepareFromFile
```

`cmd/larry` must call `prepare.PrepareFile(...)`, never `prepare.PrepareFromFile(...)`
directly, or the seam does nothing.

**Delete** `GetManifestArgs` (`manifest.go:101-104`) and `RunManifestCommand`
(`manifest.go:106-109`). Both have zero callers outside the package — verified.
Keep `FindManifestRunner` only if something still calls it; otherwise delete it too and
say so in your handback.

### 4. `verifyManifestValid` gets a usable signature (D7)

```go
// verifyManifestValid checks <pkgDir>/Manifest.
// expectedDistfiles may be empty, which means "no DIST lines required".
func verifyManifestValid(pkgDir string, expectedDistfiles []string) error
```

Rules, in order:
1. `<pkgDir>/Manifest` missing → error **only if** `len(expectedDistfiles) > 0`.
   Otherwise return nil.
2. Read it. Every line starting with `DIST` must match
   ``regexp.MustCompile(`^DIST\s+\S+\s+[1-9]\d*\s+\S+\s+\S+`)``. A `DIST` line that
   does not match is **always** an error — this is the AGENTS.md "manifest integrity"
   guard against `DIST foo.tar.gz 0 BLAKE2B  SHA512 `.
3. Zero matching `DIST` lines is an error **only if** `len(expectedDistfiles) > 0`.
4. Do **not** cross-check individual filenames in this plan. `expectedDistfiles` is only
   used as a boolean "did we expect any distfiles at all".

**Who passes a non-empty `Distfiles`?** Nobody in this plan — all five call sites pass
`nil`, which makes "zero DIST lines" acceptable everywhere. That is deliberate: D7 is
about removing a *false failure*, and computing the expected list correctly needs
`ebuild.ParseSrcUri`, which plan 22a introduces. **Plan 22b is the first caller to pass
a real list.** Write `TestVerifyManifest_ZeroDistWithExpectationsIsInvalid` anyway so the
strict branch is covered, and record in your handback that the strict branch is wired
but dormant until plan 22b.

### 5. Two `--all` entry points — do NOT collapse them (D8)

`cmd/larry/prepare.go:130-132` routes a **directory argument** through the same
`prepare.PrepareAll`. Replacing `PrepareAll` wholesale with a bulk `pkgdev` call would
turn `larry prepare dev-util/foo` from "run the npm/cargo pipeline for every ebuild in
that package" into "regenerate a Manifest". That is a feature regression and it
contradicts plans 22 and 23. Two separate functions:

```go
// PrepareManifestAll runs ONE bulk `pkgdev manifest -d <distdir>` from the overlay root.
// Used only by `larry prepare --all` with no positional arguments.
// It does NOT run any type-specific pipeline.
func PrepareManifestAll(overlay, distdir string) error

// PrepareDir runs the full per-type pipeline for every *.ebuild in dir,
// counting failures instead of aborting on the first one (D9).
// This is today's PrepareAll, renamed, plus failure counting.
func PrepareDir(dir string, cfg *PrepareConfig) error
```

**Keep `collectEbuilds`** (`prepare.go:247-266`) — `PrepareDir` needs it. Delete the
name `PrepareAll`.

`PrepareDir` and the multi-argument loop both use the same failure contract (**D9**):
attempt every target; on completion, if `n > 0` failed, return an error whose message is
exactly `fmt.Errorf("%d package(s) failed", n)`; each individual failure is logged to
stderr as it happens. `cmd/larry` returns that error, which the root command turns into
exit code 1.

Document in `prepareCmd.Long` that `--all` is manifest-only and does not run the
type-specific pipelines.

### 6. `resolvePrepareTarget` returns the resolved path (D10, D11)

```go
func resolvePrepareTarget(path, overlay string) (resolved string, fi os.FileInfo, err error)
```

- `os.Stat(path)` succeeds → return `path` as-is.
- Else, if `strings.Count(path, "/") == 1` and neither segment is empty, try
  `filepath.Join(overlay, path)`. **Use `strings.Split(path, "/")`, never
  `filepath.SplitList`** — `filepath.SplitList` splits on `:` (**D10**).
- Else try `filepath.Join(overlay, path)` as a whole.
- On total failure: `fmt.Errorf("path %q not found (tried %q)", path, candidate)`.

It no longer parses the ebuild — that was only used for validation and the caller
re-parses anyway. Every caller must use the returned `resolved` (**D11**).

---

## Tests (write first — every one must be red before you implement)

### `internal/prepare/manifest_test.go`

All of these stub `prepare.ManifestRunner` with a recorder:

```go
type call struct{ bin string; args []string; dir string; env []string }
```
and restore the original with `t.Cleanup`.

- `TestRunManifest_TargetsCategoryPackage` (**D1**) — `t.TempDir()` overlay containing
  `dev-util/foo/foo-1.0.ebuild`. `runManifest(manifestRequest{Overlay: ov, CatPkg:
  "dev-util/foo", Distdir: dd, EbuildPath: eb})`. Assert exactly one call with
  `args == []string{"manifest","-d",dd,"dev-util/foo"}` and `dir == ov`.
  Today the fourth arg is the absolute ebuild path.
- `TestRunManifest_SiblingVersionsSurvive` (**D1**) — overlay package
  `dev-util/foo/` containing `foo-1.0.ebuild`, `foo-2.0.ebuild` and a `Manifest` holding
  `DIST foo-1.0.tar.gz 10 BLAKE2B aa SHA512 bb` and
  `DIST foo-2.0.tar.gz 20 BLAKE2B cc SHA512 dd`. The stub runner writes **both** lines
  back (that is what real pkgdev does for a `cat/pkg` target). Assert: (a) the runner
  received `"dev-util/foo"`, not a path; (b) the post-run Manifest contains both DIST
  lines. **This is the Tier 1 substitute for the Tier 3 cockpit check below.**
- `TestRunManifest_CreatesDistdir` (**D4**) — `Distdir` is `filepath.Join(t.TempDir(),
  "nested","dd")` and does not exist beforehand; after the call it does.
- `TestRunManifest_EmptyDistdirIsError` (**D5**) — `Distdir: ""` → non-nil error, and
  the stub runner was **not** called.
- `TestRunManifest_NoGentooMirrorsInEnv` (**D6**) — scan the recorded `env` slice;
  assert **no element** has the prefix `"GENTOO_MIRRORS="`. (The old test name
  `..._DoesNotClearGentooMirrors` implied a config knob that does not exist — there is
  no such knob; the requirement is simply "the child env contains no `GENTOO_MIRRORS`
  entry at all".) Also assert exactly one element equals `"DISTDIR="+dd`.
- `TestRunManifest_PrefetchRunsBeforeManifestRemoval` — a `Prefetch` that records
  whether `<pkgDir>/Manifest` still exists when it runs. Assert it saw the file
  **present**, and that it was handed `pkgDir == filepath.Join(overlay, catPkg)` and the
  same distdir.
- `TestRunManifest_PrefetchErrorAborts` — `Prefetch` returns `errors.New("boom")`.
  Assert: the returned error wraps/contains `boom`, the stub runner was **not** called,
  and the pre-existing `Manifest` is **still on disk**.
- `TestRunManifest_NilPrefetchIsFine` — `Prefetch: nil` → no panic, runner called once.
- `TestManifestArgs_MatchesRunner` (**D2, D3**) — table:

  | style | distdir | catPkg | ebuildPath | want |
  |---|---|---|---|---|
  | `stylePkgdev` | `/d` | `dev-util/foo` | `/ov/dev-util/foo/foo-1.0.ebuild` | `["manifest","-d","/d","dev-util/foo"]` |
  | `styleEbuildDigest` | `/d` | `dev-util/foo` | `/ov/dev-util/foo/foo-1.0.ebuild` | `["/ov/dev-util/foo/foo-1.0.ebuild","digest"]` |

  **Delete `TestManifestArgs` at `internal/prepare/prepare_test.go:442-467`** — it
  asserts the old one-argument `{path,"digest"}` shape and cannot be kept. Replacing it
  with `TestManifestArgs_MatchesRunner` is the whole of D3.
- `TestVerifyManifest_NoSrcUriIsValid` (**D7**) — pkgDir with a `Manifest` containing
  only `AUX foo.patch 10 BLAKE2B aa SHA512 bb`, `expectedDistfiles = nil` → nil error.
- `TestVerifyManifest_MissingManifestNoExpectationsIsValid` — pkgDir with **no**
  `Manifest`, `expectedDistfiles = nil` → nil error.
- `TestVerifyManifest_ZeroDistWithExpectationsIsInvalid` — pkgDir with a `Manifest`
  containing only an `AUX` line, `expectedDistfiles = []string{"foo-1.0.tar.gz"}` →
  non-nil error.
- `TestVerifyManifest_EmptyDistEntryIsInvalid` — `Manifest` line
  `DIST foo.tar.gz 0 BLAKE2B  SHA512 ` with `expectedDistfiles = nil` → non-nil error.
  (Zero size fails `[1-9]\d*`; the blank hash fails `\S+`.) Guards the AGENTS.md
  "manifest integrity" learning.
- `TestVerifyManifest_WellFormedDistIsValid` —
  `DIST foo-1.0.tar.gz 1234 BLAKE2B abc SHA512 def` → nil error.

### `internal/prepare/prepare_test.go`

- `TestPrepareDir_AggregatesFailures` (**D9**) — `t.TempDir()` package dir with
  `foo-1.0.ebuild`, `foo-2.0.ebuild`, `foo-3.0.ebuild`; stub `prepare.PrepareFile` so
  the **middle** one (sorted by name) returns an error. Assert: all three were
  attempted (the recorder saw three paths), and the returned error message is exactly
  `1 package(s) failed`.

### `cmd/larry/prepare_test.go`

> **Plan 13a owns the `execCmd` / `fixtureOverlay` harness in
> `cmd/larry/testhelp_test.go`. If it is absent, STOP and report. Do not hand-roll a
> second harness.** Plan 20b also adds tests to `cmd/larry/prepare_test.go`; append, do
> not overwrite.

- `TestPrepareAll_InvokesBulkPkgdev` (**D8**) — stub `prepare.ManifestRunner`;
  `execCmd(t, "prepare", "--all", "--overlay", ov, "--distdir", dd)` issues **exactly
  one** call with `args == []string{"manifest","-d",dd}` and `dir == ov`.
- `TestPrepareDirArgument_RunsPipelineNotBulkManifest` (**regression guard for the
  D8 fix**) — stub `prepare.PrepareFile` as a recorder;
  `execCmd(t, "prepare", "dev-util/foo", "--overlay", ov, "--distdir", dd)` where
  `dev-util/foo/` holds two ebuilds → `PrepareFile` called **twice**, and
  `ManifestRunner` called **zero** times directly from the command.
- `TestPrepareMultipleTargets_AggregatesFailures` (**D9**) — three ebuild paths; stub
  `prepare.PrepareFile` so the second fails. Assert all three were attempted, the
  returned error contains `1 package(s) failed`, and `execCmd`'s error is non-nil (the
  root command maps that to exit 1).
- `TestResolvePrepareTarget_CategoryPackage` (**D10, D11**) — `t.Chdir` to an unrelated
  temp dir; `resolvePrepareTarget("dev-util/foo", ov)` returns
  `filepath.Join(ov, "dev-util", "foo")` and `fi.IsDir() == true`.
  Today `filepath.SplitList("dev-util/foo")` returns a 1-element slice, so this branch
  never runs.
- `TestResolvePrepareTarget_EbuildPath` — an absolute path to an existing ebuild is
  returned unchanged, `fi.IsDir() == false`.
- `TestResolvePrepareTarget_PackageDir` — an absolute existing directory is returned
  unchanged, `fi.IsDir() == true`.
- `TestResolvePrepareTarget_Missing` — `dev-util/nope` → error mentioning both
  `dev-util/nope` and the overlay-joined candidate.
- `TestPrepare_EmptyDistdirIsRejected` (**D5**) — with `LARRY_DISTDIR` and
  `PORTAGE_DISTDIR` both unset and no `--distdir`, `execCmd(t, "prepare", <ebuild>)`
  returns a non-nil error mentioning `distdir`, and `ManifestRunner` was not called.

---

## Implementation steps

Each step has its test named above. Write the test, watch it fail, then implement.

1. Add `ManifestRunner` and `PrepareFile` as exported package vars.
   Tests: nothing yet — this just unblocks the rest.
2. Introduce `manifestStyle`, change `findManifestRunner` to
   `(string, manifestStyle, error)`, rewrite `manifestArgs(style, distdir, catPkg,
   ebuildPath)`, delete `manifestRunnerArgs`.
   Tests: `TestManifestArgs_MatchesRunner`; delete `TestManifestArgs`.
3. Rewrite `runManifest` to take `manifestRequest` and follow the seven-step order.
   Drop the `GENTOO_MIRRORS=` line.
   Tests: the eight `TestRunManifest_*`.
4. Update **all five** call sites. Each derives `CatPkg` from the ebuild path with
   `internal/atom`:
   ```go
   pe, err := atom.ParseEbuildPath(ei.Path)
   if err != nil { return fmt.Errorf("manifest: %w", err) }
   req := manifestRequest{
       Overlay: cfg.Overlay, CatPkg: pe.Category + "/" + pe.PN,
       Distdir: distDir, EbuildPath: ei.Path, Prefetch: nil,
   }
   ```
   The five sites: `prepare.go:130`, `npm.go:134`, `cargo.go:63`, `go.go:102`,
   `vendored.go:417`. `Prefetch` is `nil` at all five in this plan — **plan 22b passes
   `npmPrefetchPackage` at the npm site**.
5. Delete `GetManifestArgs` and `RunManifestCommand` (**D12**). `go build ./...` is the
   test for this step.
6. Rewrite `verifyManifestValid(pkgDir, expectedDistfiles)`.
   Tests: the five `TestVerifyManifest_*`.
7. Rename `PrepareAll` → `PrepareDir`, add failure counting; add `PrepareManifestAll`.
   Keep `collectEbuilds`.
   Tests: `TestPrepareDir_AggregatesFailures`, `TestPrepareAll_InvokesBulkPkgdev`,
   `TestPrepareDirArgument_RunsPipelineNotBulkManifest`.
8. Rewrite `resolvePrepareTarget` and the `cmd/larry/prepare.go` dispatch switch; route
   `--all` with no args to `PrepareManifestAll`, a directory to `PrepareDir`, a file to
   `prepare.PrepareFile`, and multiple args through the failure-counting loop. Reject an
   empty distdir before dispatching.
   Tests: the five `TestResolvePrepareTarget_*`, `TestPrepareMultipleTargets_AggregatesFailures`,
   `TestPrepare_EmptyDistdirIsRejected`.
9. Update `prepareCmd.Long` to say `--all` is manifest-only.

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
go test ./internal/prepare/... ./cmd/... -count=1

grep -n 'GENTOO_MIRRORS' internal/prepare/manifest.go
# expect: no output

grep -rn 'func RunManifestCommand\|func GetManifestArgs\|manifestRunnerArgs\|func PrepareAll' internal/
# expect: no output

grep -rn 'filepath.SplitList' cmd/larry/
# expect: no output
```

`TestRunManifest_SiblingVersionsSurvive` is the **Tier 1 proof of D1**. Do not reach for
the reference overlay to prove it.

**Tier 2 — read-only manual. Safe to run.**

```bash
larry prepare --dry-run dev-util/fish-lsp --overlay ~/Developer/gentoo-overlay --distdir /tmp/larry-dd
# dry-run must make no filesystem changes at all, including MkdirAll (PLAN.md cross-task conventions)
```

**Tier 3 — MUTATING. DO NOT RUN. Report these verbatim for a human.**

> **DO NOT RUN ANY OF THIS.** The first block deletes and regenerates a real `Manifest`
> inside `~/Developer/gentoo-overlay` and then `git checkout`s a repo that is not this
> one. Worse: `app-admin/cockpit/cockpit-367.ebuild` is detected as **`TypeVendored`**
> by the pre-20b detector (its SRC_URI contains both `releases/download/` and `${PN}/`),
> so running it would take the vendored pipeline and **attempt a GitHub release
> upload**, which publishes to a real repo and cannot be undone by `git checkout`.
> Plan 20b fixes the classification; even so, do not run this.

```bash
# Tier 3 — DO NOT RUN. For a human, after plan 20b has landed.
cd ~/Developer/gentoo-overlay
cp app-admin/cockpit/Manifest /tmp/before
larry prepare app-admin/cockpit/cockpit-367.ebuild
diff <(sort /tmp/before) <(sort app-admin/cockpit/Manifest)   # expect: no entries lost
git checkout app-admin/cockpit/Manifest                        # restore
```

```bash
# Tier 3 — DO NOT RUN. Bulk manifest over the whole reference overlay.
cd ~/Developer/gentoo-overlay
larry prepare --all        # one pkgdev invocation; non-zero exit on failure
git status --short         # expect: only Manifest files touched
```

---

## Out of scope

Per-type pipelines (plans 22a–25b). Distfile prefetch is a **hook** here — plan 22b
implements `npmPrefetchPackage` and wires it into the npm call site's `Prefetch` field.
Populating `manifestRequest.Distfiles` is plan 22b's job. Do not fix unrelated bugs you
notice — report them.
