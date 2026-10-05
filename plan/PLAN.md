# `larry` — Implementation Plan (Index)

**Stack:** Go 1.22+ · cobra · viper · bubbletea · termenv
**Reference spec:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines)
**Test discipline:** TDD — red test first, green before merge.

---

## Current state (audited 2026-10-05)

Phase 1 (T1–T23) built the whole surface: 98 Go files, ~25.8k lines, 24 packages,
every subcommand registered. Phase 1 plan files mark these "complete", and the
*structure* is complete. A line-by-line audit against the reference script found that a
large fraction of that structure is **wired wrong, unreachable, or silently degraded**.

Hard facts at audit time:

| Signal | Value |
| --- | --- |
| `go build ./...` | clean |
| `go vet ./...` | 1 issue (`internal/git/git.go:318` self-assignment) |
| `go test ./...` | **1 failing** (`TestExtractVar/multi_line`, `internal/prepare`) |
| `golangci-lint run ./...` | **82 issues** shown — but that is *truncated* output (`max-issues-per-linter: 50`, `max-same-issues: 3`). With `--max-issues-per-linter=0 --max-same-issues=0`: **319 issues** (280 errcheck, 24 staticcheck, 12 unused, 2 ineffassign, 1 govet). Plans 29b/29c use the untruncated numbers |
| `larry upstream` on the reference overlay | runs; 3 wrong rows, 1 duplicated row, 3 bogus "latest" values |
| `larry prepare --all` | silent no-op (returns nil, does nothing) |
| `larry prepare` (go pipeline) | always fails — fabricated ebuild path |
| `larry test --compile/--merge/--phase` | flags parsed then discarded; always runs `clean` only |
| `larry test --emerge` | cannot work — `Containerfile.larry-test` is Ubuntu with no Portage |
| `larry import` | writes to `<overlay>/var/db/repos/.../foo-1.0.0.ebuild/foo-1.0.0.ebuild` |
| `config.Load()` | ✅ wired in `PersistentPreRunE` (plan 13a) |
| global flags | ✅ persistent flags registered on root (plan 13a) |
| bare `larry` | ✅ exits 1, no "hello" (plan 13a) |

Phase 2 is therefore **parity and correctness**, not new surface area.

---

## Phase 1 — build-out (historical)

Plan files `01`–`11` describe what was built. Their per-task ✅ marks reflect "code and
tests exist", not "behaviour matches the script". Treat them as design background; the
corrective work is in Phase 2. Status corrections:

| # | Task | File | Real status |
|---|------|------|-------------|
| 1 | Foundation (scaffold, config, logging, CLI) | `01-PLAN-FOUNDATION.md` | ✅ (13a) |
| 2 | Core domain (version, remoteid, provider registry) | `02-PLAN-CORE-DOMAIN.md` | ⚠️ version ordering + metadata.xml broken → `14`, `15` |
| 3 | Infra: rate limiter, retry, HTTP client | `03-PLAN-INFRA.md#T7` | ✅ |
| 3b | go-git helper + `gh` CLI wrapper | `03-PLAN-INFRA.md#T8` | ✅ (13a) |
| 3c | Provider implementations (6 providers) | `03-PLAN-INFRA.md#T8b` | ⚠️ GitLab/SourceHut dead on arrival → `15`, `17b` |
| 4 | Ebuild helper (portageq, info, SRC_URI, detection) | `04-PLAN-EBUILD.md` | ⚠️ 3 rival expanders, 2 rival detectors → `20a`, `20b` |
| 5 | Upstream command | `05-PLAN-UPSTREAM.md` | ⚠️ discovery/output/deps defects → `16`–`19` |
| 6a | Bump: file creation | `06-PLAN-BUMP.md#T12a` | ⚠️ atom/revision defects → `12` |
| 6b | Bump: invoke prepare | `06-PLAN-BUMP.md#T12b` | ✅ implemented (not in the status table) |
| 7 | Prepare: generic + dispatch | `07-PLAN-PREPARE.md#T13` | ⚠️ → `20a`, `20b`, `21` |
| 7b | Prepare: npm | `07-PLAN-PREPARE.md#T14` | ⚠️ → `22a`, `22b` |
| 7c | Prepare: cargo | `07-PLAN-PREPARE.md#T15` | ⚠️ → `23a`, `23b` |
| 7d | Prepare: go | `07-PLAN-PREPARE.md#T16` | 🔴 non-functional → `24` |
| 7e | Prepare: rpi-kernel | `07-PLAN-PREPARE.md#T17` | 🔴 removed; detection left dangling → `25a` |
| 7f | Prepare: vendored | `07-PLAN-PREPARE.md#T18` | ⚠️ → `25b` |
| 7g | Prepare: dry-run/staging | `07-PLAN-PREPARE.md#T19` | ⚠️ dry-run has side effects → `21`, `28a` |
| 8 | Import command | `08-PLAN-IMPORT.md` | 🔴 writes to wrong path → `28a`, `28b` |
| 9 | Test: local mode | `09-PLAN-TEST.md#T20` | 🔴 flags discarded, env unset → `26` |
| 9b | Test: podman mode + Containerfile | `09-PLAN-TEST.md#T21` | 🔴 image has no Portage → `27a`, `27b` |
| 10 | TUI | `10-PLAN-TUI.md` | ✅ |
| 11 | Polish (integration tests, docs) | `11-PLAN-POLISH.md` | ⚠️ 319 lint issues (82 shown by default), 1 red test → `29a`, `29b`, `29c` |

---

## Phase 2 — parity & correctness (active)

Each file is one self-contained agent session: red tests, then implementation, then a
stated acceptance check. Respect preconditions — the first four unblock the rest.

| #    | ?    | File                                | Scope                                                                                                                                                                                                                                                                            | Precondition          |
| ---- | ---- | ----------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------- |
| 12   | ✅   | `12-PLAN-ATOM.md`                   | `internal/atom`: one atom/filename parser; kill 5 duplicates                                                                                                                                                                                                                     | —                     |
| 13a  | ✅   | `13a-PLAN-CLI-HARNESS.md`           | `ExitError`+`main.go`, the `cmd/larry` test harness (`execCmd`/`fixtureOverlay`), persistent flags incl. a root `--overlay`, `config.Load()` wiring, `ResolveOverlay`/`ResolveDistdir`/`ResolveCacheDir`, maintainer keys                                                        | —                     |
| 13b  |      | `13b-PLAN-CLI-OUTPUT.md`            | `internal/output` rewrite (`Msg`/`Warn`/`Error` prefixes, injectable writers), replace bare `fmt.Printf` in the commands, `(*Config).ResolveGitHubRepo` via go-git, read `--remote`                                                                                              | 13a                   |
| 14   |      | `14-PLAN-VERSION.md`                | Gentoo suffix order (`_p`, `_pre`), `IsPrerelease`, lenient parse, `v`-prefix                                                                                                                                                                                                    | —                     |
| 15   |      | `15-PLAN-REMOTEID.md`               | `metadata.xml` parsing; GitLab/SourceHut/`.git` repo extraction                                                                                                                                                                                                                  | —                     |
| 16   |      | `16-PLAN-UPSTREAM-DISCOVERY.md`     | `-rN` ebuilds, live-vs-versioned, ordering, positional **atom** filters, `MY_PN`, npm names, `HAS-RELEASE`. **Creates `cmd/larry/upstream_test.go`**                                                                                                                             | 12, 13a, 14, 15       |
| 17a  |      | `17a-PLAN-UPSTREAM-STATUS.md`       | `provider.Resolution` + `ResolverWithTags`, `StatusCheckTag`, tag-date status derivation. No network, fake provider only                                                                                                                                                         | 14, 16                |
| 17b  |      | `17b-PLAN-UPSTREAM-GITHUB.md`       | GitHub 4-tier cascade, `gh` argv fixes, draft/prerelease fields, `git.ListRemoteTags` (drops the `git` CLI subprocess)                                                                                                                                                           | 17a                   |
| 18   |      | `18-PLAN-UPSTREAM-DEPS.md`          | Extract `resolveOne`; flat recursive dep scan (depth 0–3, in-overlay, global dedup). **Owns `UpstreamResult.Depth` + the Package-cell indent.** Appends to `cmd/larry/upstream_test.go`                                                                                          | 12, 13a, 16           |
| 19   |      | `19-PLAN-UPSTREAM-OUTPUT.md`        | `SourceInfo` (label/URL/glyph), hyperlink gate via explicit `hyperlinks bool`, `RenderTable` error, golden file. **Runs after 18** — its golden needs a depth-1 row                                                                                                              | 15, 16, 17a, 17b, 18  |
| 20a  |      | `20a-PLAN-EBUILD-VARS.md`           | One expander: bash `//`/`/`/`::` operators, `BuildVariableMap(raw, *atom.ParsedEbuild)`, `ebuild.ParseInherit` (multi-line + continuations). Kills `npmResolveSRCURI`, `expandEbuildVars`, `resolveVariables`, `parseEbuildInfo` — and the known-red `TestExtractVar/multi_line` | 12                    |
| 20b  |      | `20b-PLAN-EBUILD-DETECT.md`         | One detector (npm→cargo→go→vendored→generic), vendored keyed on the single literal `releases/download/${PN}/`, `ParsePackageType` validation. **Creates `internal/ebuild/detect_test.go` + `testdata/ebuilds/`**                                                                 | 20a, 13a              |
| 21   |      | `21-PLAN-PREPARE-MANIFEST.md`       | `pkgdev manifest cat/pkg`, `manifestRequest` + `Prefetch` hook, DISTDIR mkdir, `PrepareManifestAll` vs `PrepareDir`, failure aggregation                                                                                                                                         | 13a, 20b              |
| 22a  |      | `22a-PLAN-PREPARE-NPM-SRCURI.md`    | `SrcUriEntry` + `ParseSrcUri` (USE groups, `->` renames, continuations); npm download of every entry; the `${P}`-tag guard                                                                                                                                                       | 20a, 21               |
| 22b  |      | `22b-PLAN-PREPARE-NPM-PACKAGING.md` | `npmPrefetchPackage` wired into the plan-21 hook, `tar -caf` by extension, `npm --cache … install`, `gh release create --title/--notes`, `npm-cache` asset contract                                                                                                              | 22a, 13b              |
| 23a  |      | `23a-PLAN-PREPARE-CARGO-CRATES.md`  | `registry+` filter, dedup+sort, **insert** `CRATES=` *before* `inherit`, `.bak` rollback, `lookPath` seam. No network                                                                                                                                                            | 20a, 20b, 21          |
| 23b  |      | `23b-PLAN-PREPARE-CARGO-FETCH.md`   | `fetchCargoLock` + `fetcher` seam (drops `gh api`), raw URLs only, two subdirs, no default-branch fallback, SourceHut forge + tag rules, pycargoebuild in `os.MkdirTemp`                                                                                                         | 23a                   |
| 24   |      | `24-PLAN-PREPARE-GO.md`             | `GitHubBaseURL`+`Runner` on `PrepareConfig` (makes the pipeline testable), fix fabricated path, `vendor/` tarball root, pkg-dir-then-distdir write order, drop bogus upload, `catPkg` manifest                                                                                   | 12, 20b, 21           |
| 25a  |      | `25a-PLAN-PREPARE-ESCAPE-HATCH.md`  | Delete `TypeRpiKernel`, reject `--type rpi-kernel`, `docs/examples/rpi-kernel-prepare.sh`; `files/prepare.sh` gets backup→run→manifest→rollback; sandbox warning through the logger (it floods `go test` today)                                                                  | 12, 13a, 13b, 20b, 21 |
| 25b  |      | `25b-PLAN-PREPARE-VENDORED.md`      | PN from `internal/atom` (fixes every hyphenated name), wire `.larry.yaml` `vendor_dirs` → `DepSpec.SourcePath`, `npm-cache` asset contract, `catPkg` manifest, delete 4 dead helpers                                                                                             | 12, 21, 22b           |
| 26   |      | `26-PLAN-TEST-LOCAL.md`             | Wire phases + precedence, script's 14-phase set, `RunWithEnv` + sandbox env, single `ebuild` call, banner/summary, exit codes, short + bare `--<phase>` flags, atom-based ebuild pick                                                                                            | 12, 13a, 13b          |
| 27a  |      | `27a-PLAN-TEST-CONTAINERFILE.md`    | `gentoo/stage3` Containerfile with `ARG REPO/VIDEO_CARDS/AMDGPU_TARGETS`; delete the Ubuntu-shaped `TestContainerfile_Exists`, add 7 real tests. **Zero Go code**                                                                                                                | —                     |
| 27b  |      | `27b-PLAN-TEST-CONTAINER-ARGS.md`   | emerge argv from the atom, `-v` mounts incl. binpkgs, `podman image exists`, pretend-before-build, nil-guard order, `checkPodmanAvailable` must use its runner, `--image`                                                                                                        | 12, 13a, 26           |
| 28a  |      | `28a-PLAN-IMPORT-PATHS.md`          | `resolveSourceOverlay` (path / repo-name / scan-skipping-own), target from `atom.ParseAtom`, non-live highest version via `atom.FindLatest`, `--force` guard, dry-run writes nothing, delete 3 dead overlay finders                                                              | 12, 13a, 14           |
| 28b  |      | `28b-PLAN-IMPORT-METADATA.md`       | Byte-exact `<pkgmetadata>` golden (tabs, DOCTYPE, `<upstream>` omitted when empty), flat `MaintainerName`/`MaintainerEmail`, propagate the metadata parse error, provenance lines, injectable `Prepare` hook                                                                     | 28a, 13a, 13b, 15     |
| 29a  |      | `29a-PLAN-HYGIENE-MECHANICAL.md`    | `go vet` (1), `ineffassign` (2), `unused` (12) + the 17-entry dead-code list, `.gitignore`/`gaze.log`/`pi.log`, Makefile (`make lint` is broken today)                                                                                                                           | all                   |
| 29b  |      | `29b-PLAN-HYGIENE-ERRCHECK.md`      | `errcheck` — **280, not 49**; the 49 was truncated output. 222 in tests (`t.Setenv`/`t.TempDir` rewrites), 58 in production                                                                                                                                                      | all                   |
| 29c  |      | `29c-PLAN-HYGIENE-STATICCHECK.md`   | `staticcheck` — **24, not 18**; incl. `SA4000`/`SA4006` assertions that do not assert. Plus `TestExtractVar/multi_line` and a bounded, delete-only test-quality sweep                                                                                                            | all                   |

### Execution order

**Numeric order is correct.** Every plan's preconditions are strictly lower-numbered, so
running `12 → 13 → … → 29` in sequence never violates a dependency. An agent that just
takes the next unfinished file is always safe.

Numeric order is not the *only* valid order. Independent branches, if you want to
parallelise:

| Branch | Files | Unblocked by |
|---|---|---|
| Foundations | `12`, `13`, `14`, `15` | nothing — all four can start at once |
| Upstream | `16` → `17a` → `17b` → `18` → `19` | `12`, `13a`, `14`, `15` |
| Prepare | `20a` → `20b` → `21` → `22a` → `22b`; `23a` → `23b`; `24`; `25a`; `22b` → `25b` | `12`, `13a` |
| Test | `26` → `27b`; `27a` standalone | `12`, `13a`, `13b` |
| Import | `28a` → `28b` | `12`, `13a`, `13b`, `14`, `15` |

The Upstream, Prepare, Test and Import branches are mutually independent once the
foundations land. **Within Prepare, `20a → 20b → 21` is a strict chain**; after `21`
lands, the pipelines (`22a → 22b`, `23a → 23b`, `24`, `25a`, `22b → 25b`) are mutually
independent. `22b` additionally needs `13b` for
`(*config.Config).ResolveGitHubRepo`, and `25b` needs `22b` for the `npm-cache` asset
contract. **`27a` has no preconditions at all** — it touches only
`Containerfile.larry-test` and `containerfile_test.go` and can be done at any time. **Within Upstream
there is no parallelism: `16 → 17a → 17b → 18 → 19` is a strict chain.** `17b` needs
`17a`'s `Resolution` type; `18` needs `16`'s `RunWithTargets` and creates the
`resolveOne` seam; `19` needs `18`'s `UpstreamResult.Depth` because its committed golden
file contains a depth-1 row. `29a`, `29b` and `29c` run last; they are mutually independent and each must
re-measure its own baseline before starting.

---

## Module structure

```
cmd/larry/               # CLI entry points (main, root, bump, prepare, import, test, upstream)
internal/
├── atom/                # NEW (12): atom + ebuild filename parsing, ebuild selection
├── config/              # Viper config, overlay/DISTDIR/cache resolution
├── version/             # Gentoo version parse/compare
├── remoteid/            # metadata.xml parser + RemoteId model
├── provider/            # UpstreamProvider interface + registry
├── providers/           # github, gitlab, pypi, npm, codeberg, sourcehut
├── ratelimit/           # Token bucket + retry/backoff
├── httpclient/          # Shared HTTP client
├── git/                 # go-git: remotes, tags, commits
├── github/              # gh CLI wrapper (releases, upload, download)
├── ebuild/              # portageq, info parser, SRC_URI expansion, type detection
├── prepare/             # generic, npm, cargo, go, vendored pipelines + manifest
├── bump/                # Version/revision bump
├── upstream/            # Version check + cache + dep scanning + output
├── importcmd/           # Import pipeline
├── test/                # Local + podman test runners
├── output/              # Logger, color, progress
└── cli/                 # TUI models (bubbletea)
docs/ · testdata/ · Containerfile.larry-test · Makefile
```

## Testing strategy

| Level | Tool | Coverage |
|-------|------|----------|
| Unit | `go test` | Every `internal/` package |
| Table-driven | `go test` tables | Parsing, comparison, detection |
| Mock HTTP | `net/http/httptest` | Provider API calls |
| Mock subprocess | `CmdRunner` interface | `portageq`, `ebuild`, `pkgdev`, `gh`, `podman`, `npm`, `go` |
| Integration | `go test` + test overlay | Full command pipelines |
| Golden files | `testdata/` | Version compare, metadata.xml, table output |
| Manual | reference overlay | `larry upstream`, `ebuild … digest`, container emerge |

### Known test-suite gaps (measured 2026-10-05)

| Package | Test funcs | Source files | Note |
|---|---|---|---|
| `cmd/larry` | **15** | 10 | CLI harness, exit codes, persistent flags, config wiring (plan 13a). Plans 16, 21, 26, 28 add more. |
| `internal/logger` | 0 | 1 | Minor |
| `internal/prepare` | 123 | 9 | 1 failing; highest lint-issue count (29) |

**`cmd/larry` has no tests at all.** That is the single largest gap and the direct cause
of the worst defects: discarded flags (now fixed 13a), `config.Load()` (now wired 13a), the `larry: hello` (now deleted 13a),
debug print, the zero-arg panic, and `--distdir` rejected on every subcommand. Plan 13
13a builds a reusable `cmd/larry` test harness (`execCmd`, `fixtureOverlay`) as its first
deliverable; plans 16, 18, 20b, 21, 26 and 28 consume it.

**Anti-pattern seen in Phase 1:** tests that assert on a helper the production path never
calls pass while the feature is broken. Confirmed instances — `ScanDependencies`
(0 production callers, 1 test file), `CheckUpstream` (0 callers, tested),
`verifyDistfiles` (0 callers, referenced in 3 test files), `setNetworkFeatures` and
`buildPhaseList` (callers exist but sit on a dead branch), `containerfile_test.go`
(matches the string `emerge` inside a comment). Every Phase 2 test must drive the code
path the CLI actually takes; plan 29 item 7 sweeps the rest.

## Environment traps (cost us real time; read once)

**`grep -r … .` from the repo root silently skips files.** In this environment `grep`
is a shell function wrapping `ugrep --ignore-files`, which honours `.gitignore`. Any
ignored path is invisible. **Always pass explicit roots** — `grep -rn … ./cmd ./internal`
— never a bare `.`. A weak model that greps the root and finds nothing will wrongly
conclude a symbol was already deleted.

**Fixed 2026-10-05: `.gitignore` was hiding five source files.** Line 2 was a bare
`larry` (meant for the compiled binary). Bare patterns match *any* path component, so it
matched the `cmd/larry/` **directory**: `bump.go`, `import.go`, `prepare.go`, `test.go`
and `upstream.go` were ignored and untracked — only `main.go` and `root.go` were in git,
because they predated the rule. The pattern is now `/larry`. **The five files are still
untracked**; they need `git add cmd/larry/` before any commit, or a clean clone will not
build. Re-check with `git status --porcelain cmd/`.

## Known limitations in this plan set

- **`verifyManifestValid`'s strict branch is dormant.** Plan 21 adds
  `verifyManifestValid(pkgDir, expectedDistfiles)` so that "zero DIST lines" is an error
  only when distfiles were expected — but computing that list needs `ParseSrcUri`, which
  lands in 22a. All five call sites pass `nil`, and 22b is the first (npm-only) caller to
  populate it. Cargo, go and generic will keep passing `nil`. Accepted: the strict branch
  is wired and tested, just not yet fed. Revisit if a manifest regression slips through.

## Rules that apply to every Phase 2 plan

Read these once; the individual plans assume them.

**Reference script.** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Plans cite it as `script <func> (L123–456)`. If the file is not present on this machine,
the plan text is authoritative — do not guess at the script's behaviour.

**Reference overlay.** `~/Developer/gentoo-overlay`. Read-only unless a plan explicitly
says Tier 3 (see below).

**Known-red baseline.** Before you start, run `go test ./... -count=1` and record what
already fails. As of 2026-10-05 that is exactly one test:
`TestExtractVar/multi_line` in `internal/prepare` (**plan 20a owns it** — it deletes
`prepare.extractVar` and that test together) plus one
`go vet` finding at `internal/git/git.go:318`. **Do not chase pre-existing failures that
your plan does not own.** If your baseline differs from this, note it and continue.

**Check your preconditions before writing code.** Every plan with a numbered
precondition has a "Precondition check" block of `go doc` commands. Run them first. If a
symbol is missing, the prerequisite plan has not landed — **stop and report**, do not
implement the missing piece yourself.

**Tier 1 gate, every plan.** Done means all of:
```bash
go build ./...
go vet ./...
go test ./... -count=1        # no new failures vs. your recorded baseline
golangci-lint run ./...       # per AGENTS.md, required before any push
```

**Scope discipline.** Fix only the defects your plan lists. If you find an unrelated bug,
write it in your final report; do not fix it. Deleting a function means deleting its
tests too — say so in the report.

## Verification tiers — read before running any Acceptance step

Every plan's **Acceptance** section is split into three tiers. The tier decides whether
you may run a command yourself.

**Tier 1 — automated, required. Run these.**
`go build ./...`, `go vet ./...`, `go test ./... -count=1`, `golangci-lint run ./...`,
and any assertion against a temp-directory fixture the tests create. A plan is not done
until Tier 1 is green.

**Tier 2 — read-only manual. Safe to run.**
Commands that only read: `larry upstream …`, anything with `--pretend` or `--dry-run`,
`grep`/`find` checks. These touch the reference overlay at `~/Developer/gentoo-overlay`
but do not modify it. Network reads are allowed; a failure here may be rate-limiting
rather than a defect, so re-run before concluding.

**Tier 3 — MUTATING. Do not run. Report instead.**
Anything that changes state outside the repo's own temp dirs:

- writes into `~/Developer/gentoo-overlay` (`larry prepare`, `larry bump`,
  `larry import` without `--dry-run`, `ebuild … digest/unpack/install`)
- **uploads a GitHub release asset** (`larry prepare` on an npm or vendored package —
  this publishes to a real repo and cannot be quietly undone)
- `podman build` / `podman rmi` / `podman run`, or any real `emerge`
- anything needing root, a GPU, or `/var/db/repos` write access

When a plan lists a Tier 3 step, **do not execute it**. Finish Tier 1 and Tier 2, then
write the exact Tier 3 commands into your final report for the human to run. If you
believe a Tier 3 step is the only way to prove a fix, say so and stop — do not improvise
a "safe-looking" variant against the real overlay.

If you must exercise a mutating path, do it against a throwaway overlay you created in
`t.TempDir()` or `/tmp`, never the reference overlay.

## Cross-task conventions

- **TUI stream isolation**: when `useTUI`, pipe subprocess stdout/stderr; never inherit.
- **Rollback**: mutating pipelines back up `.bak` and restore on failure.
- **Dry-run**: no filesystem, network, or git side effects — *including* `MkdirAll`.
- **Asset collision** (npm/go upload): `gh release view --json assets` → SHA-256 compare →
  skip if identical, `--clobber` otherwise.
- **`gh` auth**: `GITHUB_TOKEN` through env; no `gh auth login`.
- **Exit codes**: propagate the subprocess exit code; `larry` with no subcommand exits 1.
