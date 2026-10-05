# 20b — One type detector + `--type` validation (`internal/ebuild`)

**Why:** Two rival type detectors exist and they return *different types for the same
file*. The npm path and the vendored path are checked in opposite orders, so every npm
ebuild in the reference overlay is at risk of being routed to the vendored pipeline —
which uploads a GitHub release asset. On top of that, `--type npmm` is silently accepted
and falls through to the generic pipeline.

**Precondition:** 20a (`ebuild.ParseInherit`, `ebuild.BuildVariableMap(raw, *atom.ParsedEbuild)`),
which in turn needs 12.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish). Cited below as
`script <func> (L…)`. **If that file is absent on this machine, this plan's text is
authoritative — do not guess at the script's behaviour.**
Functions: `detect_type` (L118–128), the `--type` validation in `do_prepare_one`
(L553–583).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. Known-red baseline: `go vet` at
> `internal/git/git.go:318`, plus `internal/prepare TestExtractVar/multi_line` **if plan
> 20a has not yet deleted it**. Do not chase anything else you did not break.

---

## Precondition check — run this first

```bash
go doc bennypowers.dev/larry/internal/ebuild ParseInherit
# expect: func ParseInherit(raw string) []string

go doc bennypowers.dev/larry/internal/ebuild BuildVariableMap
# expect: func BuildVariableMap(raw string, pe *atom.ParsedEbuild) VariableMap

grep -rn 'func extractInherit' internal/upstream/upstream.go
# expect: NO OUTPUT (20a deleted it)
```

**If `ParseInherit` is missing, plan 20a has not landed. STOP and report. Do not write
it yourself** — this plan's `TestDetectType_GoModule` multi-line case cannot pass without
it, because the old `extractInherit` returned only the first `inherit` line and did not
join backslash continuations.

For the `cmd/larry` test file:

```bash
grep -n 'func execCmd\|func fixtureOverlay' cmd/larry/testhelp_test.go
# expect: both present
```

**Plan 13a (`13a-PLAN-CLI-HARNESS.md`) owns the `execCmd` / `fixtureOverlay` harness in
`cmd/larry/testhelp_test.go`. If it is absent, STOP and report. Do not hand-roll a
second harness.**

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| T1 | `internal/ebuild/detect.go:36` vs `:97` | `DetectTypeForce` checks **vendored before npm**; `DetectTypeFromInfoForce` checks **npm before vendored**. Overlay npm ebuilds carry both markers, so the two disagree on the same file |
| T2 | `internal/ebuild/detect.go:102-104` and `:111-113` | tests `strings.Contains(info.SRC_URI, "CRATES=")` — `CRATES=` is never inside a SRC_URI value. The identical block is duplicated verbatim; both copies are dead |
| T3 | `internal/ebuild/detect.go:54` | `strings.HasPrefix(line, "INHERITED=") \|\| strings.Contains(line, "inherit go-module")` → any ebuild with an `INHERITED=` line is classified `go`; `inherit foo go-module bar` matches only by luck; backslash continuations are missed entirely |
| T4 | `internal/ebuild/detect.go:59-74` and `:130-140` | two rpi-kernel branches. The **file-based** one at `:67` keys off an explicit `PV=` assignment line, which real ebuilds never carry, so it is unreachable. The **info-based** one at `:138` reads `info.PV()`, which `upstream.ParseEbuildInfo` fills from the *filename* — reachable, but it fires on any `inherit linux-info` package whose version contains `_p`, which is a normal Gentoo patch-level suffix |
| T5 | `internal/ebuild/detect.go:18, 23` | `DetectType` and `DetectTypeForce` have **zero callers anywhere, tests included** — verified with `grep -rn 'DetectTypeForce\|DetectType(' --include=*.go .`. You may rewrite them freely; nothing depends on their current behaviour |
| T6 | `cmd/larry/prepare.go:110-112` + `internal/prepare/prepare.go:73-84` | `cfg.ForceType = ebuild.PackageType(prepareType)` casts any string; unknown values fall through `switch pkgType` into `default:` → `prepareGeneric`. `larry prepare --type npmm` "succeeds" |
| T7 | `internal/ebuild/detect.go:36`, `:107` | the vendored rule is `Contains("releases/download/") && Contains("${PN}/")` — two **independent** substrings. `app-admin/cockpit/cockpit-367.ebuild` has `SRC_URI="https://github.com/cockpit-project/${PN}/releases/download/${PV}/${P}.tar.xz"`: the `${PN}/` matches the **owner path segment**, not the release tag. Both substrings are present → the ebuild is classified `vendored` → `larry prepare` attempts a GitHub release upload for an ordinary upstream release tarball |

T1 and T7 are the blockers.

---

## Target behaviour

### One implementation

Delete both existing bodies. The single implementation operates on the **raw, unexpanded
ebuild text**:

```go
// detectFromRaw is the one and only detection algorithm.
func detectFromRaw(raw string) PackageType
```

Order — the first match wins:

| # | Type | Condition (all against `raw`) |
|---|------|-------------------------------|
| 1 | `TypeNPM` | `strings.Contains(raw, "mirror://npm/")` |
| 2 | `TypeCargo` | `cratesRe.MatchString(raw)` where ``cratesRe = regexp.MustCompile(`(?m)^CRATES=`)`` **OR** `ParseInherit(raw)` contains `"cargo"` |
| 3 | `TypeGo` | `ParseInherit(raw)` contains `"go-module"` |
| 4 | `TypeVendored` | `strings.Contains(raw, "releases/download/${PN}/")` — the **single literal substring**, unexpanded, with the tag segment included (**T7**) |
| 5 | `TypeGeneric` | otherwise |

Why this order: an npm ebuild legitimately contains
`releases/download/${PN}/<P>-deps.tar.xz`, so vendored must be checked last (**T1**).
`INHERITED=` is never consulted (**T3**). `strings.Contains(…, "CRATES=")` against
`SRC_URI` is gone (**T2**).

**rpi-kernel:** remove the branch from the detector entirely (**T4**). Keep the
`TypeRpiKernel` constant in `internal/ebuild/ebuild.go` and keep `"rpi-kernel"` in
`ParsePackageType`'s accepted set — **plan 25a (`25a-PLAN-PREPARE-ESCAPE-HATCH.md`)
owns the decision to remove both**. Do **not** write an rpi-kernel detection test in
this plan; 25a owns `TestDetectType_RpiKernelEbuildIsGeneric`.

### The two public entry points become thin

```go
func DetectType(ebuildPath string) (PackageType, error)        // ReadFile → detectFromRaw
func DetectTypeForce(ebuildPath string, force PackageType) (PackageType, error)
func DetectTypeFromInfo(info *EbuildInfo) (PackageType, error)
func DetectTypeFromInfoForce(info *EbuildInfo, force PackageType) (PackageType, error)
```

- `force != ""` short-circuits, as today.
- `DetectTypeFromInfo*` uses `info.Raw` (set by `upstream.ParseEbuildInfo` at
  `upstream.go:480`). If `info.Raw == ""`, fall back to
  `detectFromRaw(info.SRC_URI + "\n" + strings.Join(info.Inherit, " "))` prefixed by
  `"inherit "` so `ParseInherit` sees it — i.e. build
  `info.SRC_URI + "\ninherit " + strings.Join(info.Inherit, " ") + "\n"`.
- `info == nil` → `TypeGeneric, nil` (today's behaviour at `:92-94`).

Both entry points must now return the **same** type for the same file. That is what
`TestDetectType_AgreesWithInfoVariant` pins.

### `--type` validation (T6)

```go
// ParsePackageType validates a --type value.
// Accepted: npm, cargo, go, vendored, rpi-kernel, generic.
func ParsePackageType(s string) (PackageType, error)
```

- Input is matched **exactly** (no case folding, no trimming beyond
  `strings.TrimSpace`).
- `""` → `(TypeUnknown, nil)` so "no flag given" is not an error.
- Anything else unrecognised → error whose message lists the valid values, mirroring the
  script's `Unknown type: $type`. Required exact format:

  ```
  unknown type "npmm": valid types are npm, cargo, go, vendored, rpi-kernel, generic
  ```

Wire it at `cmd/larry/prepare.go:109-112`:

```go
if prepareType != "" {
    pt, err := ebuild.ParsePackageType(prepareType)
    if err != nil {
        return err
    }
    cfg.ForceType = pt
}
```

Leave `prepare.Prepare`'s `switch` alone — with validation at the boundary, `default:`
is now only reached by `TypeGeneric`, `TypeRpiKernel` and `TypeUnknown`.

---

## Fixtures

Create the directory **`internal/ebuild/testdata/ebuilds/`**. Go resolves `testdata`
relative to the *package* directory, so a `testdata/ebuild/` at the repo root (which
already exists and holds `cargo.ebuild`, `go.ebuild`, `npm.ebuild`) is **not** reachable
from `internal/ebuild` tests. Do not try to reuse it.

One-line helper, at the top of `detect_test.go`:

```go
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "ebuilds", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}
```

Files to create:

| File | Provenance | Must contain |
|---|---|---|
| `npm-with-deps-release.ebuild` | copy of `~/Developer/gentoo-overlay/dev-util/fish-lsp/fish-lsp-1.1.5.ebuild` | both `mirror://npm/` and `releases/download/${PN}/` |
| `cargo-literal-crates.ebuild` | **hand-write** | `EAPI=8`, a 3-entry `CRATES="…"` block starting at column 0, `inherit cargo` *after* it. Do **not** copy `dev-python/kornia-rs/kornia-rs-0.1.14.ebuild` — it is ~1550 lines |
| `cargo-no-crates.ebuild` | **hand-write** | `EAPI=8`, `inherit cargo`, `SRC_URI="…\n\t${CARGO_CRATE_URIS}\n"`, **no** `CRATES=` anywhere |
| `go-module.ebuild` | copy of `~/Developer/gentoo-overlay/net-vpn/netbird/netbird-0.80.0.ebuild` | `inherit … go-module …` |
| `go-module-continuation.ebuild` | **hand-write** | `inherit foo \` newline tab `go-module bar` |
| `inherited-line.ebuild` | **hand-write** | one line `INHERITED="toolchain-funcs"` and nothing else type-bearing |
| `vendored.ebuild` | **hand-write** | `SRC_URI="https://github.com/o/r/releases/download/${PN}/${P}-vendor.tar.xz"`, no npm/cargo/go markers |
| `upstream-release.ebuild` | copy of `~/Developer/gentoo-overlay/app-admin/cockpit/cockpit-367.ebuild` | `releases/download/${PV}/` with `${PN}/` as the **owner** segment |
| `generic.ebuild` | copy of `~/Developer/gentoo-overlay/x11-themes/phinger-cursors/phinger-cursors-2.1.ebuild` | nothing type-bearing |

Copying from the reference overlay is a **read**; that is Tier 2 and allowed.

**Do not** use `gui-apps/nirius/nirius-0.7.1.ebuild` as a "cargo via inherit, no literal
CRATES" fixture. It has a literal 40-entry `CRATES=` block at line 6 *and* `inherit cargo`
at line 49, so it exercises neither path cleanly. (It is plan 23a/23b's fixture.)

---

## Tests (write first — every one must be red before you implement)

### `internal/ebuild/detect_test.go` — **create this file**, it does not exist

Every case below runs **both** entry points. Build the `*EbuildInfo` for the second one
as `&EbuildInfo{Raw: raw, SRC_URI: …, Inherit: ParseInherit(raw)}`.

| Test | Fixture | Expect |
|---|---|---|
| `TestDetectType_NpmWithVendoredDepsURL` (**T1**) | `npm-with-deps-release.ebuild` | `TypeNPM` from both entry points. Today `DetectTypeForce` says `TypeVendored` |
| `TestDetectType_CratesLiteral` (**T2**) | `cargo-literal-crates.ebuild` | `TypeCargo` |
| `TestDetectType_CargoEclassNoCrates` | `cargo-no-crates.ebuild` | `TypeCargo` |
| `TestDetectType_GoModule` (**T3**) | `go-module.ebuild` | `TypeGo` |
| `TestDetectType_GoModuleContinuation` (**T3**) | `go-module-continuation.ebuild` | `TypeGo`. Needs `ParseInherit` from 20a |
| `TestDetectType_InheritedLineIsNotGo` (**T3**) | `inherited-line.ebuild` | `TypeGeneric` |
| `TestDetectType_Vendored` | `vendored.ebuild` | `TypeVendored` |
| `TestDetectType_UpstreamReleaseTarballIsGeneric` (**T7**) | `upstream-release.ebuild` | `TypeGeneric`. Today both detectors say `TypeVendored` |
| `TestDetectType_Generic` | `generic.ebuild` | `TypeGeneric` |
| `TestDetectType_AgreesWithInfoVariant` (**T1, T5**) | table over all nine fixtures | `DetectType(path) == DetectTypeFromInfo(info)` for every row |
| `TestDetectType_ForceWins` | `generic.ebuild`, `force = TypeNPM` | `TypeNPM` from both `*Force` entry points |
| `TestDetectType_NilInfo` | — | `DetectTypeFromInfo(nil)` → `(TypeGeneric, nil)` |

`TestParsePackageType` (**T6**) — table:

| input | want type | want error |
|---|---|---|
| `npm` | `TypeNPM` | nil |
| `cargo` | `TypeCargo` | nil |
| `go` | `TypeGo` | nil |
| `vendored` | `TypeVendored` | nil |
| `rpi-kernel` | `TypeRpiKernel` | nil |
| `generic` | `TypeGeneric` | nil |
| `` (empty) | `TypeUnknown` | nil |
| `npmm` | — | non-nil, message contains `npmm`, `npm`, `cargo`, `go`, `vendored`, `generic` |
| `NPM` | — | non-nil |

### `internal/ebuild/detect_overlay_test.go` — **create**, behind a build tag

```go
//go:build manual

package ebuild
```

`TestDetectType_ReferenceOverlaySweep`: read `$LARRY_TEST_OVERLAY`, defaulting to
`filepath.Join(os.Getenv("HOME"), "Developer", "gentoo-overlay")`. `t.Skip` if the
directory does not exist. `filepath.WalkDir` every `*.ebuild`, call `DetectType`, and
assert:

- every file whose text contains `mirror://npm/` detects as `TypeNPM`
  (today's expected hits include `dev-util/fish-lsp`, `dev-util/vtsls`,
  `dev-util/eslint_d`, `dev-util/pi-coding-agent`, `dev-util/yaml-language-server`,
  `dev-util/fixjson`, `dev-util/emmet-language-server`, `dev-util/stylelint-lsp`,
  `dev-util/vscode-langservers-extracted`);
- `app-admin/cockpit/cockpit-367.ebuild` is **not** `TypeVendored`.

This test is **read-only**: it opens files and never writes. It is Tier 2. Run it with
`go test -tags manual ./internal/ebuild/ -run ReferenceOverlaySweep -count=1`.

### `cmd/larry/prepare_test.go` (**T6**)

> **Plan 13a owns the `execCmd` / `fixtureOverlay` harness in
> `cmd/larry/testhelp_test.go`. If it is absent, STOP and report. Do not hand-roll a
> second harness.** Plan 21 also appends to `cmd/larry/prepare_test.go` — append, do not
> overwrite.

- `TestPrepareTypeValidation_Rejects` — `execCmd(t, "prepare", "--type", "npmm", <ebuild>)`
  returns a non-nil error whose message contains `unknown type "npmm"` and the word
  `cargo`. Assert that **no** manifest runner was invoked.
- `TestPrepareTypeValidation_Accepts` — `execCmd(t, "prepare", "--type", "generic", "--dry-run", <ebuild>)`
  returns nil.

---

## Implementation steps

Each step has its test named above. Write the test, watch it fail, then implement.

1. Create `internal/ebuild/testdata/ebuilds/` and all nine fixtures.
2. Create `internal/ebuild/detect_test.go` with the fixture helper and every
   `TestDetectType_*`. Run it — expect `NpmWithVendoredDepsURL`,
   `GoModuleContinuation`, `InheritedLineIsNotGo`, `UpstreamReleaseTarballIsGeneric`
   and `AgreesWithInfoVariant` to fail.
3. Rewrite `internal/ebuild/detect.go`: add `detectFromRaw` with the five-step order and
   the `(?m)^CRATES=` regex; reduce the four public functions to thin wrappers; delete
   both rpi-kernel branches (keep the constant).
4. Add `ParsePackageType` (same file is fine) with the exact error string. Test:
   `TestParsePackageType`.
5. Wire `ParsePackageType` into `cmd/larry/prepare.go:109-112`. Tests: the two
   `TestPrepareTypeValidation_*`.
6. Add `internal/ebuild/detect_overlay_test.go` behind `//go:build manual`.
7. `go build ./... && go test ./... -count=1`. The `internal/prepare` detection tests at
   `prepare_test.go:36-92` exercise `DetectTypeFromInfoForce`; if any row now expects a
   different type, **update the expectation** — the new order is correct — and list the
   changes in your handback.

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
grep -n 'INHERITED=' internal/ebuild/detect.go
# expect: no output

grep -n 'TypeRpiKernel' internal/ebuild/detect.go
# expect: no output (the constant lives in ebuild.go and stays)

grep -n 'Contains(info.SRC_URI, "CRATES=")' internal/ebuild/detect.go
# expect: no output

grep -c 'releases/download/\${PN}/' internal/ebuild/detect.go
# expect: 1  — one literal substring, not two independent Contains calls
```

**Tier 2 — read-only manual. Safe to run.**

```bash
go test -tags manual ./internal/ebuild/ -run ReferenceOverlaySweep -count=1 -v
# reads ~/Developer/gentoo-overlay; writes nothing. Skips cleanly if absent.
```

**Tier 3 — MUTATING. DO NOT RUN. Report verbatim for a human.**

```bash
# Tier 3: writes into the reference overlay and may upload a GitHub release asset.
cd ~/Developer/gentoo-overlay
larry prepare --type bogus dev-util/fish-lsp/fish-lsp-1.1.5.ebuild   # must exit non-zero
larry prepare app-admin/cockpit/cockpit-367.ebuild                   # must NOT take the vendored path
```

The `--type bogus` line exits before touching anything, but it is listed here because
the second line does not. Prove both with the Tier 1 tests instead:
`TestPrepareTypeValidation_Rejects` and `TestDetectType_UpstreamReleaseTarballIsGeneric`.

---

## Out of scope

Variable expansion (plan 20a). The rpi-kernel constant's fate (plan 25a). The pipelines
themselves (plans 21–25). Do not fix unrelated bugs you notice — report them.
