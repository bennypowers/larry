# 20a — One variable expander (`internal/ebuild`)

**Why:** Three rival `${VAR}` expanders exist. The one the npm pipeline actually uses
knows five hardcoded variable names and runs a single pass, so any ebuild using a
differently named helper variable leaks a literal `${…}` into a download URL. The one
correct expander has no caller.

**Precondition:** 12 (`internal/atom`).

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish). Cited below as
`script <func> (L…)`. **If that file is absent on this machine, this plan's text is
authoritative — do not guess at the script's behaviour.**
Functions: `expand_ebuild_vars` (L75–106).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. In particular: the known-red baseline is
> exactly `internal/prepare TestExtractVar/multi_line` plus the `go vet` finding at
> `internal/git/git.go:318`. Do not chase anything else you did not break.

---

## Precondition check — run this first

```bash
go doc bennypowers.dev/larry/internal/atom ParsedEbuild
# expect: a struct with fields Path, Category, PN, PVR, PV, Revision, P, PF, Live

go doc bennypowers.dev/larry/internal/atom ParseEbuildPath
# expect: func ParseEbuildPath(path string) (*ParsedEbuild, error)
```

**If either command errors or prints nothing, plan 12 has not landed. STOP and report.
Do not write `internal/atom` yourself.**

Also confirm the starting shape of the code you are about to change:

```bash
grep -n 'func BuildVariableMap' internal/ebuild/srcuri.go
# expect: 178:func BuildVariableMap(info *EbuildInfo) VariableMap

grep -n 'func extractInherit' internal/upstream/upstream.go
# expect: 625:func extractInherit(content string) []string
```

If those line numbers have moved slightly, that is fine — if the *functions* are gone,
someone else already did this work: stop and report.

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| E1 | `internal/prepare/npm.go:170-184` | `npmResolveSRCURI` expands a hardcoded map of 5 names (`${PN} ${PV} ${P} ${MY_NPM_SCOPE} ${MY_NPM_NAME}`), single pass |
| E2 | `internal/prepare/npm.go:458-470` | `expandEbuildVars` expands 4 names, single pass — and this is what produces the **GitHub release tag and asset name** (`npm.go:77-78`) |
| E3 | `internal/prepare/prepare.go:216-222` | `resolveVariables`: plain `ReplaceAll` over a map keyed by the literal `"${PN}"`, no iteration |
| E4 | `internal/ebuild/srcuri.go:22` | `ExpandVariables` is the good one (fixpoint, 10 iterations) and has **no caller outside its own file** — verified with `grep -rn 'ExpandVariables' --include=*.go .` |
| E5 | `internal/ebuild/srcuri.go:178` | `BuildVariableMap(info *EbuildInfo)` scans only `info.SRC_URI` for assignments; the script scans the **whole file**. It also has **zero non-test callers** |
| E6 | `internal/ebuild/srcuri.go:128-174` | `extractVarName`/`applyBashManip` support **only** `${VAR//pat/repl}`. `${PN/-/_}` returns `hasManip=false` at `:134-136` so the manipulation is silently dropped and the *unsubstituted* value is emitted; `${MY_PN::1}` resolves to a variable literally named `MY_PN::1`, is not found, and the whole `${…}` is emitted verbatim |
| E7 | `internal/upstream/upstream.go:625-633` | `extractInherit` splits on `"\n"`, takes the **first** line starting with `inherit `, and returns immediately. It does not join `\`-continuations and ignores every later `inherit` line. `info.Inherit` is therefore **not** safe to use as "the eclass list" — plan 20b depends on fixing this |

E6 and E7 are the two blockers. Everything else is mechanical.

---

## Target behaviour

### 1. `ExpandVariables` gains three bash operators (E6)

`ExpandVariables(input string, vars VariableMap) string` keeps its signature, its
fixpoint loop and its 10-iteration cap. Only `extractVarName` and `applyBashManip`
change. Supported operators, in the order `extractVarName` must test them:

| Form | Meaning | Example | Result (`PN=fish-lsp`) |
|---|---|---|---|
| `${VAR//pat/repl}` | replace **all** occurrences | `${PN//-/_}` | `fish_lsp` |
| `${VAR/pat/repl}` | replace the **first** occurrence only | `${PN/-/_}` | `fish_lsp` (one `-`, same result — use `${PN//s/S}` vs `${PN/s/S}` to tell them apart) |
| `${VAR::N}` | first `N` bytes | `${PN::4}` | `fish` |
| `${VAR:N:M}` | `M` bytes starting at byte `N` | `${PN:5:3}` | `lsp` |
| `${VAR}` | plain reference | `${PN}` | `fish-lsp` |

Rules:
- `//` must be tested **before** `/`, and `::`/`:` **before** `/`, because `${VAR:0:1}`
  contains no `/` and `${VAR//a/b}` contains `/`.
- An out-of-range offset or length clamps to the string bounds; it is never a panic.
- A non-numeric offset or length means "no manipulation": emit the plain value.
- An **unknown variable name** still emits `${…}` verbatim (today's behaviour at
  `srcuri.go:77-82`). Callers rely on that to detect failure. Do not change it.

### 2. `BuildVariableMap` takes raw text (E5)

```go
// BuildVariableMap collects every simple top-level assignment in the raw ebuild text
// and adds the Portage core variables derived from pe.
// pe may be nil, in which case only the scanned assignments are returned.
func BuildVariableMap(raw string, pe *atom.ParsedEbuild) VariableMap
```

Note the parameter type. **Do not use `atom.Atom`** — plan 12's `Atom` carries only
`Category`, `PN`, `Version`, `PkgDir`, so it cannot supply `P` or `PF`.
`atom.ParsedEbuild` carries `Category`, `PN`, `PV`, `PVR`, `P`, `PF` — exactly the five
you need. Likewise **do not derive these from `ebuild.EbuildInfo`**: that struct has no
`PF` field at all (`internal/ebuild/ebuild.go:18-31`).

The returned map, when `pe != nil`:

| Key | Value |
|---|---|
| `PN` | `pe.PN` |
| `PV` | `pe.PV` |
| `PR` | `"r" + strconv.Itoa(pe.Revision)` |
| `P` | `pe.P` |
| `PF` | `pe.PF` |
| `CATEGORY` | `pe.Category` |

Then, scanned from `raw` and **overriding nothing already set above**: every line
matching `^[A-Z_][A-Z0-9_]*="…"` or `^[A-Z_][A-Z0-9_]*='…'` where the closing quote is
on the same line. Value is stored **unexpanded**; `ExpandVariables`'s fixpoint resolves
nesting. Skip lines inside a `#` comment. Multi-line quoted values (`SRC_URI="` …
`"`) are **not** collected as variables — their content is irrelevant here and
collecting them breaks the single-line regex.

The field named `vars` in call sites must be typed `ebuild.VariableMap`, **not**
`map[string]string`. `VariableMap` is declared at `srcuri.go:17`.

There is no existing production caller of `BuildVariableMap`, so no call site breaks.

### 3. `ParseInherit` moves into `internal/ebuild` (E7)

```go
// ParseInherit returns every eclass named by every `inherit` line in raw,
// joining backslash continuations, in source order, de-duplicated.
func ParseInherit(raw string) []string
```

Algorithm:
1. `raw = strings.ReplaceAll(raw, "\\\n", " ")` — join continuations first.
2. For each line: `strings.TrimSpace`; skip if it starts with `#`.
3. If the line starts with `inherit ` (or is exactly `inherit`), append
   `strings.Fields(rest)` to the accumulator. **Do not return early** — keep scanning.
4. De-duplicate preserving first-seen order. Return `nil` for no matches.

Then in `internal/upstream/upstream.go:468`, replace
`info.Inherit = extractInherit(content)` with
`info.Inherit = ebuild.ParseInherit(content)` and **delete** `extractInherit`
(`upstream.go:625-633`). `internal/upstream` already imports `internal/ebuild`, so there
is no new dependency and no import cycle (`internal/ebuild` must not import
`internal/upstream`).

Leave `info.InheritRaw` (`upstream.go:470-476`) alone — it is a different field with
different consumers.

### 4. The three rival expanders die (E1, E2, E3)

- **Delete** `prepare.npmResolveSRCURI` (`npm.go:167-208`).
- **Delete** `prepare.extractVar` (`npm.go:444-456`). After E1 its only callers are
  `npm.go:175-176`. The package-local `extractVar` in `internal/upstream` and
  `internal/ebuild` are *different functions* — do not touch them.
- **Delete** `prepare.expandEbuildVars` (`npm.go:458-470`).
- **Delete** `prepare.resolveVariables` (`prepare.go:216-222`) **and**
  `prepare.parseEbuildInfo` (`prepare.go:162-214`), its only caller. `parseEbuildInfo`
  has **zero production callers** — verified: the only references are
  `prepare_test.go:117`, `:159` and `:650`. Deleting it is a strict win; the live path is
  `PrepareFromFile` → `upstream.ParseEbuildInfo`.

Replacement at the two npm call sites, so that **behaviour does not regress** while plan
22a is still unwritten:

```go
// npm.go — one helper, used by both former expanders.
func npmVars(ei *ebuild.EbuildInfo) ebuild.VariableMap {
    pe, err := atom.ParseEbuildPath(ei.Path)
    if err != nil {
        pe = nil
    }
    return ebuild.BuildVariableMap(ei.Raw, pe)
}
```

- `npm.go:46` — replace `npmResolveSRCURI(ei.SRC_URI, ei)` with a new local
  `npmFirstRegistryURL(ei)` that reproduces today's observable behaviour on top of the
  shared expander: expand with `npmVars`, `TrimSpace`, cut at the first `"\n"`, require
  the `mirror://npm/` prefix (else return `""`), strip a trailing `" -> name"`, and
  prepend `https://registry.npmjs.org/`. **Plan 22a replaces this wholesale with
  `ParseSrcUri`** — keep it small and do not grow it here.
- `npm.go:77-78` — replace `expandEbuildVars(rawTag, ei)` / `expandEbuildVars(rawAssetName, ei)`
  with `ebuild.ExpandVariables(rawTag, npmVars(ei))` / likewise for the asset name.

---

## Tests (write first — every one must be red before you implement)

### `internal/ebuild/srcuri_test.go` (append; do not rewrite existing tests)

Each row below gives exact input and exact expected output.

- `TestExpandVariables_NestedFixpoint` (**E1–E4**) —
  vars `{"MY_NPM_NAME":"widget", "MY_WEBUI_P":"${MY_NPM_NAME}-0.75.3"}`,
  input `"mirror://npm/${MY_WEBUI_P}.tgz"` →
  want `"mirror://npm/widget-0.75.3.tgz"`.
- `TestExpandVariables_NoInfiniteLoop` —
  vars `{"A":"${B}", "B":"${A}"}`, input `"${A}"` → the call **returns** (the test
  simply must not hang); assert only that the result is a string containing `"$"`.
- `TestExpandVariables_UnknownVarLeftAlone` —
  vars `{"PN":"foo"}`, input `"${NOPE}/${PN}"` → want `"${NOPE}/foo"`.
- `TestExpandVariables_BashManipulation` (**E6**) — table, vars
  `{"PN":"fish-lsp", "MY_PN":"kornia-rs"}`:

  | input | want |
  |---|---|
  | `${PN//-/_}` | `fish_lsp` |
  | `${PN//s/S}` | `fiSh-lSp` |
  | `${PN/s/S}` | `fiSh-lsp` |
  | `${PN/-/_}` | `fish_lsp` |
  | `${MY_PN::1}` | `k` |
  | `${MY_PN::6}` | `kornia` |
  | `${MY_PN:7:2}` | `rs` |
  | `${MY_PN::99}` | `kornia-rs` |
  | `${MY_PN::x}` | `kornia-rs` |

- `TestBuildVariableMap_WholeFile` (**E5**) — raw text:

  ```
  EAPI=8
  MY_NPM_NAME="language-server"
  DESCRIPTION="x"
  SRC_URI="
  	mirror://npm/@vtsls/${MY_NPM_NAME}/-/${MY_NPM_NAME}-${PV}.tgz
  "
  ```
  with `pe = &atom.ParsedEbuild{Category:"dev-util", PN:"vtsls", PV:"0.3.0", P:"vtsls-0.3.0", PF:"vtsls-0.3.0"}`.
  Want: `MY_NPM_NAME == "language-server"`, `PN == "vtsls"`, `PV == "0.3.0"`,
  `P == "vtsls-0.3.0"`, `PF == "vtsls-0.3.0"`, `CATEGORY == "dev-util"`.
  Also assert the map has **no** `SRC_URI` key (multi-line values are not collected).
  Today this fails because `MY_NPM_NAME` is defined outside `SRC_URI`.
- `TestBuildVariableMap_NilParsedEbuild` — same raw, `pe = nil` → `MY_NPM_NAME` present,
  `PN` absent, no panic.
- `TestBuildVariableMap_IgnoresComments` — raw `# MY_NPM_NAME="wrong"\nMY_NPM_NAME="right"\n`
  → `"right"`.
- `TestParseInherit_SingleLine` (**E7**) — `"inherit cargo\n"` → `["cargo"]`.
- `TestParseInherit_MultipleLines` — raw:
  ```
  inherit pam python-single-r1
  DESCRIPTION="x"
  inherit systemd tmpfiles
  ```
  → `["pam","python-single-r1","systemd","tmpfiles"]`. **Today `extractInherit` returns
  only the first two.**
- `TestParseInherit_Continuation` — raw:
  ```
  inherit foo \
  	go-module bar
  ```
  → `["foo","go-module","bar"]`. **Today `extractInherit` returns `["foo","\\"]`.**
- `TestParseInherit_SkipsComments` — `"# inherit go-module\ninherit cargo\n"` → `["cargo"]`.
- `TestParseInherit_Dedup` — `"inherit cargo\ninherit cargo systemd\n"` →
  `["cargo","systemd"]`.
- `TestParseInherit_None` — `"EAPI=8\n"` → `nil`.

### `internal/upstream/upstream_test.go`

- `TestParseEbuildInfo_InheritAccumulatesAllLines` — write a temp ebuild containing the
  `TestParseInherit_MultipleLines` body plus `EAPI=8`, call
  `upstream.ParseEbuildInfo(path)`, assert
  `info.Inherit == ["pam","python-single-r1","systemd","tmpfiles"]`.

### Deletions that take tests with them

- `internal/prepare/npm_test.go` — delete `TestExtractVar` (`:211`, includes the
  already-red `multi_line` subtest) together with `prepare.extractVar`. **This is how
  the known-red baseline goes green. Say so in your handback.**
- `internal/prepare/npm_test.go` — delete any `TestNpm_ResolveSRCURI*` /
  `TestNpm_ExpandEbuildVars*` tests of the deleted functions. Do **not** delete
  `TestNpm_ExtractDistfileName` — `npmExtractDistfileName` survives this plan (plan 22a
  removes it).
- `internal/prepare/prepare_test.go` — delete the three `parseEbuildInfo` tests at
  `:117`, `:159`, `:650` and the `resolveVariables` table at `:173-227`.

---

## Implementation steps

Each step has its test named above. Write the test, watch it fail, then implement.

1. **E6** — rewrite `extractVarName` (`srcuri.go:128-144`) and `applyBashManip`
   (`srcuri.go:146-174`) for the five forms in the table.
   Test: `TestExpandVariables_BashManipulation`.
2. **E7** — add `ParseInherit` to `internal/ebuild` (new file
   `internal/ebuild/inherit.go` is fine). Repoint `upstream.go:468`, delete
   `extractInherit`.
   Tests: the six `TestParseInherit_*` plus
   `TestParseEbuildInfo_InheritAccumulatesAllLines`.
3. **E5** — change `BuildVariableMap`'s signature and body as specified. Add the
   `internal/atom` import to `internal/ebuild`.
   Tests: the three `TestBuildVariableMap_*`.
4. **E1/E2** — add `npmVars` and `npmFirstRegistryURL` to `npm.go`; delete
   `npmResolveSRCURI`, `extractVar`, `expandEbuildVars`; delete their tests.
   Test: the existing npm suite must stay green.
5. **E3** — delete `resolveVariables`, `parseEbuildInfo` and their five tests.
6. `go build ./... && go test ./... -count=1`. Expect the baseline to have **improved**
   by one (`TestExtractVar/multi_line` is gone).

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**.

**Tier 1 — automated, required. Run all of these.**

```bash
go build ./...
go vet ./...                  # expect ONLY internal/git/git.go:318 (known baseline)
go test ./... -count=1        # no NEW failures vs. your recorded baseline;
                              # TestExtractVar/multi_line should now be GONE, not red
golangci-lint run ./...       # per AGENTS.md, required before any push
```

```bash
grep -rn 'MY_NPM_SCOPE' internal/prepare
# expect: no output

grep -rn 'func resolveVariables\|func expandEbuildVars\|func npmResolveSRCURI\|func parseEbuildInfo\|func extractInherit' internal/ cmd/
# expect: no output (internal/upstream/upstream.go:433 ParseEbuildInfo is EXPORTED and stays)

grep -rn 'map\[string\]string' internal/ebuild/srcuri.go
# expect: no output — the type is ebuild.VariableMap
```

**Tier 2 — read-only manual. Safe to run.** None required for this plan.

**Tier 3 — MUTATING. DO NOT RUN.** None in this plan. If you find yourself wanting to
run `larry prepare` against `~/Developer/gentoo-overlay` to check an expansion, don't —
write a `t.TempDir()` fixture instead.

---

## Out of scope

Type detection and `--type` validation (plan 20b). `ParseSrcUri` / `SrcUriEntry`
(plan 22a — it appends **below** `ResolveSRCURI` in `srcuri.go`; this plan owns
everything **above** it). The npm, cargo, go and vendored pipelines (plans 21–25b).
Do not fix unrelated bugs you notice — report them.
