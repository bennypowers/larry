# 14 — Gentoo version semantics

**Why:** `internal/version` is the comparator behind every OUTDATED / UP-TO-DATE /
AHEAD verdict. It inverts `_p` ordering, collapses `_pre` into `_rc`, classifies
post-releases as pre-releases (which makes every provider *reject* them), and rejects a
large class of real upstream tags outright.

**Precondition:** none — this plan is standalone and touches only `internal/version`
and `internal/providers`.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent, this plan's text is
authoritative — do not guess at the script's behaviour.**
Functions: `compare_versions` / `_norm_ver` (L1270–1313), `is_version_like`
(L1128–1130), `strip_tag_prefix` (L1120–1126), `extract_date_from_string`
(L1064–1071).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting.

---

## The authoritative ordering

This was generated from real Portage `vercmp`. **It overrides any contrary intuition,
any comment in the existing code, and any assertion in the existing tests.** Use it
verbatim.

```
1.0_alpha1 < 1.0_alpha2 < 1.0_beta1 < 1.0_pre1 < 1.0_rc1 < 1.0 < 1.0-r1 < 1.0-r2
          < 1.0_p1 < 1.0_p1-r1 < 1.0_p2 < 1.0.1 < 9999
```

```
vercmp: 1.0 vs 1.0_p1               = -1
vercmp: 1.0_pre1 vs 1.0_rc1         = -1
vercmp: 1.0_p1 vs 1.0-r1            = +1
vercmp: 9.0 vs 10.0                 = -1
vercmp: 1.2.3 vs 1.2.3a             = -1
vercmp: 2.0.0_rc1 vs 2.0.0          = -1
vercmp: 367 vs 310.10               = +1
vercmp: 6.18.46_p20260824 vs 1.20230405 = +1
vercmp: 20250410 vs 20260519        = -1
```

> **Note, and put this in a comment next to `suffixKey`:** the suffix rank is compared
> **before** the revision, so `1.0_p1 > 1.0-r1`. This matches Portage `vercmp`. An
> earlier revision of this plan asserted the opposite; it was wrong.

---

## Precondition check (run first)

```bash
go doc bennypowers.dev/larry/internal/version Parse
# expect: func Parse(s string) (*Version, error)

go doc bennypowers.dev/larry/internal/version Normalize
# expect: func Normalize(s string) string

go doc bennypowers.dev/larry/internal/version Version.Compare
# expect: func (v *Version) Compare(other *Version) int

go doc bennypowers.dev/larry/internal/version Version.IsPrerelease
# expect: func (v *Version) IsPrerelease() bool

go doc bennypowers.dev/larry/internal/version Version
# expect a struct with fields: Components []uint64; Suffix *Suffix; Revision uint64; Live bool
```

```bash
go doc bennypowers.dev/larry/internal/version Compare
go doc bennypowers.dev/larry/internal/version ExtractDate
# expect: BOTH to fail with "no symbol". You are CREATING these. If they already
# exist, someone has done part of this plan — STOP and report.
```

If any of the first five differs, the repo has moved out from under this plan —
**STOP and report**.

---

## Defects being fixed

| ID | Location | Defect | Verified |
|----|----------|--------|----------|
| D1 | `internal/version/version.go:13-24, 310-332` | `suffixKey` ranks Patch(4) **below** Release(5) | `Compare("1.0","1.0_p1") == 1` — says `1.0 > 1.0_p1` |
| D2 | `internal/version/version.go:176-178` | `pre` is mapped to `Rc` | `Compare("1.0_pre1","1.0_rc1") == 0` |
| D3 | `internal/version/version.go:343-350` | `IsPrerelease()` returns true for `Patch`. Providers (`pypi.go:99`, `npm.go:102`, `github.go:335`, `gitlab.go:176`, `codeberg.go:119`, `sourcehut.go:294`) then reject `_p` versions → UNKNOWN | — |
| D4 | `internal/version/version.go:186-203` | `parseComponents` requires 1–5 purely numeric dot components | rejects `1.2.3a`, `1.0.0-beta.1`, `2.0.0-rc1`, `1.2.3.4.5.6` |
| D5 | `internal/version/version.go:80-84` | `Normalize` strips a leading `v`/`V` unconditionally | `Normalize("vulkan-1.0") == "ulkan-1.0"` |
| D6 | `internal/version/version.go:60-62`, `internal/providers/github.go:161` | `^{}` peeled-tag stripping expects `"^{ "` (with a trailing space) in the provider, and a **fully-wrapped** `^\^{(.+?)\}$` in `version.Normalize` | `v1.0^{}` is never stripped by either |
| D7 | `internal/version/version.go:73` | `Normalize` strips `"rel-"`, a heuristic that belongs nowhere | — |
| D8 | `internal/providers/github.go:18-19` | `dateRegex = (20?\d{5,7})` instead of the script's `(20\d{6})`; 6-digit matches then get blindly prefixed with `20` (`github.go:241-243`) | — |

Observed in the field: `app-admin/cockpit` 367 → "latest 310.10";
`sys-kernel/raspberrypi-kernel` `6.18.46_p20260824` → "latest 1.20230405";
`dev-libs/migraphx` 7.2.4 → "latest 10.0".

### D7 — the decision (do exactly this, do not deliberate)

The three "Go-only heuristics" are **not** all in the same place:

| Heuristic | Where it actually lives | Action |
|---|---|---|
| `"rel-"` prefix strip | `internal/version/version.go:73` | **DELETE** it from the prefix list |
| `pios/` prefix strip | `internal/providers/github.go:147` | **LEAVE IT.** It is plan 17's business. |
| truncate tag at first space | `internal/providers/github.go:166-168` | **LEAVE IT.** Plan 17's business. |
| `^{}` peeled-tag strip | `internal/providers/github.go:161` | **FIX** — see step 6 |

`providers.stripTagPrefix` is otherwise untouched by this plan.

---

## Target behaviour

### 1. Suffix ranks

`alpha=1, beta=2, pre=3, rc=4, release=5, p=6`. Higher wins. Insert a new `Pre`
constant **between `Beta` and `Rc`** in the iota block (`version.go:13-24`).

**Delete the `R1` constant** (`version.go:20`) and its two `case R1:` arms
(`version.go:38-39`, `version.go:327-328`). Nothing anywhere constructs
`Suffix{Type: R1}` — revisions live in `Version.Revision`, not in a `Suffix`. Verified:
`R1` appears only inside `version.go`.

Keep `Release`; `suffixKey(nil)` returns its rank.

`Format()` (`version.go:216-225`) needs a `case Pre: sb.WriteString("pre")` arm.

### 2. `IsPrerelease()`

True only for `Alpha`, `Beta`, `Pre`, `Rc`. **`Patch` (`_p`) is a post-release and must
return false.** Revisions and live versions are not pre-releases.

### 3. Lenient `Parse` — pinned specification

`Parse` runs these steps in this order. Do not reorder.

1. `s = Normalize(s)`. If `s == ""` → error.
2. **Gate:** if `s` does not match `^[0-9]` → error (`invalid version string: %q`).
   This is the only rejection. `1.2.3a` and friends must pass.
3. **Revision:** `var revRe = regexp.MustCompile(`-r([0-9]+)$`)` — anchored. If it
   matches, `Revision` = the capture and `s` loses the match.
   (This replaces the unanchored `strings.LastIndex(s, "-r")` at `version.go:97`, which
   plan 12's acceptance grep explicitly leaves to this plan.)
4. **Dashed pre-release rewrite** — one substitution, applied once:
   ```go
   var dashSuffixRe = regexp.MustCompile(`[-.](alpha|beta|pre|rc)\.?([0-9]*)$`)
   s = dashSuffixRe.ReplaceAllString(s, "_$1$2")
   ```
   Case-sensitive on purpose; uppercase markers are out of scope.
   `2.0.0-rc1` → `2.0.0_rc1`; `1.0.0-beta.1` → `1.0.0_beta1`; `1.2.3a` unchanged.
5. **Suffix:** the existing `_`-split at `version.go:111-120`, with `parseSuffix`
   rewritten to try markers in this order — `alpha, beta, pre, rc, a, b, p` — where
   the numeric tail may be **empty** (number 0) and, if non-empty, must be all digits.
   If the tail is non-empty and not all digits, the suffix is **not** recognised and
   the `_`-part stays in the base.
   `pre` now maps to `Pre`, not `Rc` (**D2**).
6. **Segments** — a new unexported slice, **authoritative for comparison**:
   ```go
   type segment struct {
       num   uint64
       str   string
       isNum bool
   }
   ```
   Split the remaining base into maximal runs of ASCII digits and maximal runs of
   non-digit characters, **dropping `.`, `-` and `_` entirely** (they are boundaries,
   not segments). Store on `Version` as an unexported `segments []segment` field.
7. **`Components []uint64`** keeps its meaning for `Format()` and `Live` only: the
   **longest leading all-numeric dot-run** of the base. It is a legacy field. Verified:
   `.Components` has **zero** readers outside `internal/version`. Never compare with it.
8. **`Live`** stays `true` iff some element of `Components` equals `9999`.

Worked table (base = the string left after steps 3–5):

| input | Revision | Suffix | base | `Components` | `segments` |
|---|---|---|---|---|---|
| `1.2.3` | 0 | nil | `1.2.3` | `[1 2 3]` | `[1 2 3]` |
| `1.2.3a` | 0 | nil | `1.2.3a` | `[1 2]` | `[1 2 3 "a"]` |
| `1.0.0-beta.1` | 0 | `{Beta,1}` | `1.0.0` | `[1 0 0]` | `[1 0 0]` |
| `2.0.0-rc1` | 0 | `{Rc,1}` | `2.0.0` | `[2 0 0]` | `[2 0 0]` |
| `1.2.3.4.5.6` | 0 | nil | `1.2.3.4.5.6` | `[1 2 3 4 5 6]` | `[1 2 3 4 5 6]` |
| `6.18.46_p20260824` | 0 | `{Patch,20260824}` | `6.18.46` | `[6 18 46]` | `[6 18 46]` |
| `1.0_pre` | 0 | `{Pre,0}` | `1.0` | `[1 0]` | `[1 0]` |
| `9999-r1` | 1 | nil | `9999` | `[9999]` | `[9999]` |
| `20250410` | 0 | nil | `20250410` | `[20250410]` | `[20250410]` |
| `2026.40.0` | 0 | nil | `2026.40.0` | `[2026 40 0]` | `[2026 40 0]` |

Note `1.2.3a` → `Components == [1 2]`: the dot-run stops at `3a` because that part is
not all-numeric. That is lossy, which is exactly why `segments` exists.

### 4. `Compare` order of checks

1. nil handling (as today).
2. `Live` beats non-`Live`.
3. **`segments`**, with this rule: before comparing, drop **trailing numeric segments
   whose value is 0** from both sides (so `1.0` == `1.0.0` == `1.0.0.0`, which existing
   tests at `version_test.go:92` and `:122` already require). Then compare pairwise:
   - num vs num → numeric;
   - str vs str → `strings.Compare`;
   - num vs str at the same index → **numeric sorts below alphabetic** (mirrors `sort -V`);
   - all shared positions equal → the **longer** slice wins.
4. Suffix rank (`suffixKey`).
5. Suffix number.
6. `Revision`.

Steps 4–6 in that order are what make `1.0_p1 > 1.0-r1`.

### 5. `Normalize` — pinned specification

```go
var peeledRe = regexp.MustCompile(`\^\{.*\}`)   // ANYWHERE, not anchored
var vPrefixRe = regexp.MustCompile(`^[vV]\.?([0-9])`)
```

1. `strings.TrimSpace`.
2. `s = peeledRe.ReplaceAllString(s, "")` — **anywhere in the string** (**D6**).
3. Strip the first matching prefix from `{"bun-", "rocm-", "release-"}`.
   **`"rel-"` is deleted from this list** (**D7**).
4. `s = vPrefixRe.ReplaceAllString(s, "$1")` — strips `v`/`V`/`v.`/`V.` **only when
   followed by a digit** (**D5**).
5. `strings.TrimSpace`.

**Delete `normalizeRe` (`version.go:60-62`)** — `peeledRe` replaces it.

Consequence you must act on: `Normalize("^{1.2.3}")` now returns `""`, not `"1.2.3"`.
See "Existing tests that must change".

### 6. `ExtractDate`

```go
// ExtractDate finds a YYYYMMDD date beginning with "20" in s.
func ExtractDate(s string) (string, bool)
```
Regex exactly `(20\d{6})` (the script's). No 6-digit fallback, no `"20" +` prefixing.
`internal/providers/github.go` uses it; `dateRegex` (`github.go:18-19`) is deleted.

### 7. Package-level convenience functions

```go
// Compare parses a and b and compares them. Unparseable input sorts BELOW any
// parseable version; two unparseable values fall back to strings.Compare, so the
// result is always a total order and callers never have to drop a version.
func Compare(a, b string) int

// Less is Compare(a, b) < 0, for slices.SortFunc / sort.Slice.
func Less(a, b string) bool
```

These are what `internal/atom` (plan 12), `internal/upstream` (plan 16) and
`internal/importcmd` (plan 28) consume. The total-order guarantee matters: the current
code *drops* ebuilds whose version fails to parse (plan 12 D5, plan 16 D1), and a
comparator that can fail invites that bug back.

---

## Existing tests that must change (do these FIRST, as your red step)

These assert the broken behaviour. They are not "fallout" — flipping them **is** the
red test.

### `internal/providers/prerelease_test.go` — `{"1.0.0_p1", true}` → `false`

Six occurrences, one per provider test:

| line | test |
|---|---|
| 20 | `TestPyPIProvider_IsPrerelease` |
| 53 | `TestNpmProvider_IsPrerelease` |
| 84 | `TestGitLabProvider_IsPrerelease` |
| 114 | `TestCodebergProvider_IsPrerelease` |
| 143 | `TestSourceHutProvider_IsPrerelease` |
| 172 | `TestGitHubProvider_IsPrerelease` |

### `internal/version/version_test.go`

- **`:99`** — `{"1.0_p1", "1.0", -1}` → **`{"1.0_p1", "1.0", 1}`**, and fix the trailing
  comment (`_p` is a post-release, not a pre-release).
- **`:98`** — `{"1.0_rc1", "1.0_p1", -1}` is **already correct** per vercmp. Leave it.
- **`:160`** — `{"^{1.2.3}", "1.2.3"}` in `TestVersion_Normalize`. **DELETE this row.**
  It is incompatible with the "strip `\^\{.*\}` anywhere" rule, which turns
  `^{1.2.3}` into `""`. Replace it with the two rows listed under
  `TestNormalize_PeeledTag` below.
- **`:227-231`** — the `order` slice in `TestVersion_CompareSuffixes`
  (`version_test.go:217`) currently reads
  `… "1.0_rc2", "1.0_p1", "1.0_p2", "1.0", "1.0-r1", "1.0-r2"`.
  Replace the whole slice with the authoritative chain:
  ```go
  order := []string{
      "1.0_alpha1", "1.0_alpha2",
      "1.0_beta1",  "1.0_beta2",
      "1.0_pre1",
      "1.0_rc1",    "1.0_rc2",
      "1.0",
      "1.0-r1",     "1.0-r2",
      "1.0_p1",     "1.0_p1-r1", "1.0_p2",
      "1.0.1",
      "9999",
  }
  ```
- **`:245-251`** — `TestVersion_Compare_PatchVsRc` asserts `1.0_rc1 < 1.0_p1`. That is
  **correct** per vercmp. Keep the assertion; fix the misleading comment at `:249`
  ("`_p` is a patch-level pre-release" — it is a post-release).

### `internal/version/version_short_test.go` — the `_pre` ≡ `_rc` family (**D2**)

This whole file encodes `pre == rc`. Fix:

| line | current | change to |
|---|---|---|
| 18 | `{"1.0_pre1", "1.0_rc1"}` | `{"1.0_pre1", "1.0_pre1"}` |
| 19 | `{"1.0_pre2", "1.0_rc2"}` | `{"1.0_pre2", "1.0_pre2"}` |
| 48 | `{"1.0_pre1", "1.0_rc1"}` | **DELETE the row** (no longer equivalent) |
| 49 | `{"1.0_pre99", "1.0_rc99"}` | **DELETE the row** |
| 87 | `{"1.0_pre1", "1.0_rc1"}` | `{"1.0_pre1", "1.0_pre1"}` |
| 88 | `{"1.0_pre1-r1", "1.0_rc1-r1"}` | `{"1.0_pre1-r1", "1.0_pre1-r1"}` |
| 114 | `{"1.0_pre0", "1.0_rc0"}` | `{"1.0_pre0", "1.0_pre0"}` |
| 143 | `{"1.0_pre1", "1.0_rc1", 0}` | `{"1.0_pre1", "1.0_rc1", -1}` |
| 146 | `{"1.0_pre1", "1.0", -1}` | value is correct; fix the comment (`pre1 < release`) |

---

## New tests (write first — every one must fail before you implement)

### `internal/version/version_test.go`

- `TestCompare_AuthoritativeChain` — for the 15-element `order` slice above, assert
  `Compare(order[i], order[j]) < 0` for every `i < j`, using the **package-level**
  `Compare`. (**D1, D2**)
- `TestCompare_PatchAboveRelease` — `Compare("1.0", "1.0_p1") == -1`. (**D1**)
- `TestCompare_PatchAboveRevision` — `Compare("1.0_p1", "1.0-r1") == 1`. (**D1**)
- `TestCompare_PreBelowRc` — `Compare("1.0_pre1", "1.0_rc1") == -1`. (**D2**)
- `TestIsPrerelease` — table: true for `1.0_alpha1`, `1.0_beta1`, `1.0_pre1`,
  `1.0_rc1`, `1.0_rc1-r1`; **false** for `1.0`, `1.0_p1`, `1.0_p2`, `1.0-r1`, `9999`.
  (**D3**)
- `TestParse_Lenient` — all of these return a nil error:
  `1.2.3a`, `1.0.0-beta.1`, `2.0.0-rc1`, `1.2.3.4.5.6`, `20250410`,
  `6.18.46_p20260824`, `1.0_pre`, `367`, `2026.40.0`.
  Also assert the parse still **rejects** `nightly`, `abc` and `""` (the `^[0-9]`
  gate). (**D4**)
- `TestParse_ComponentsAndSegments` — the worked table in "Target behaviour" item 3,
  row by row: assert `Revision`, `Suffix` (type + number, or nil), `Components`, and
  `Live`. `segments` is unexported, so assert it indirectly through `Compare`.
- `TestCompare_LenientForms` —
  `Compare("1.2.3a","1.2.3") == 1`;
  `Compare("2.0.0-rc1","2.0.0") == -1`;
  `Compare("1.2.3.4.5.6","1.2.3.4.5") == 1`;
  `Compare("1.0.0-beta.1","1.0.0") == -1`.
- `TestNormalize_VPrefix` — `v1.0`→`1.0`; `V1.0`→`1.0`; `v.1.0`→`1.0`;
  `vulkan-1.0`→`vulkan-1.0` (**D5**); `version2`→`version2`; `V.2.1`→`2.1`.
- `TestNormalize_PeeledTag` — `v1.0^{}`→`1.0`; `1.0^{abc}`→`1.0`;
  `1.0^{}-r1`→`1.0-r1`. (**D6**)
- `TestNormalize_KnownPrefixes` — `bun-1.2`→`1.2`; `rocm-6.1`→`6.1`;
  `release-3.0`→`3.0`; and **`rel-3.0`→`rel-3.0`** (unchanged — the heuristic is
  gone). (**D7**)
- `TestExtractDate` — `6.18.46_p20260824`→(`20260824`, true);
  `20250410`→(`20250410`, true); `260519-1_bpo13`→(`""`, false) (6-digit forms must
  **not** match); `foo-2024`→(`""`, false); `1.2024010`→(`""`, false) (7 digits must
  not match). (**D8**)
- `TestCompareFunc_TotalOrder` — over the fixture set
  `{"1.0","1.0_p1","9999","nightly","abc","1.2.3a",""}`:
  `Compare(x,x) == 0` for every x; `Compare(x,y) == -Compare(y,x)` for every pair;
  every parseable value compares `> 0` against every unparseable one;
  `Compare("abc","nightly") == strings.Compare("abc","nightly")`.
- `TestLess_SortsSlice` — `slices.SortFunc(s, func(a,b string) int { return Compare(a,b) })`
  over `["1.0_p1","9999","1.0","1.0_rc1","1.0-r1"]` yields exactly
  `["1.0_rc1","1.0","1.0-r1","1.0_p1","9999"]`.
  (The old plan asserted `1.0_p1 < 1.0-r1` here. That was wrong.)
- `TestCompare_RealWorldRegressions` — the vercmp block at the top of this file, as a
  table:
  `Compare("367","310.10") == 1`;
  `Compare("6.18.46_p20260824","1.20230405") == 1`;
  `Compare("20250410","20260519") == -1`;
  `Compare("9.0","10.0") == -1`.

### `internal/providers/prerelease_test.go`

- `TestProvider_AcceptsPostRelease` — one subtest per provider for **all six**:
  pypi, npm, github, gitlab, **codeberg**, **sourcehut**. For each, `version.Parse("1.0_p1")`
  then `p.IsPrerelease(v)` is `false`, so a caller filtering with
  `allowPrereleases == false` keeps it. (**D3**)
  (`prerelease_test.go:105` already covers codeberg's `IsPrerelease`; this test adds
  the explicit post-release assertion for all six in one place.)

### `internal/providers/github_test.go`

- `TestStripTagPrefix_PeeledTag` — `stripTagPrefix("v1.0^{}")` → `"1.0"`;
  `stripTagPrefix("1.0^{abc}")` → `"1.0"`; `stripTagPrefix("v1.0")` → `"1.0"`.
  (Step 6 has no provider-level test today.)

---

## Implementation steps

1. Reorder the suffix constants: insert `Pre` between `Beta` and `Rc`; delete `R1` and
   its two `case` arms. Update `suffixKey` to `alpha=1, beta=2, pre=3, rc=4,
   release=5, p=6` and add the vercmp comment. Add `case Pre:` to `SuffixType.String()`
   and to `Format()`. (**D1, D2**)
2. Narrow `IsPrerelease()` to alpha/beta/pre/rc (**D3**). Re-run
   `go test ./internal/providers/... -count=1` — the `_p1` rejections should disappear.
3. Rewrite `Normalize` per "Target behaviour" item 5; delete `normalizeRe`.
   (**D5, D6, D7**)
4. Rewrite `Parse` per "Target behaviour" item 3 (steps 1–8) and `Compare` per item 4.
   Keep every existing exported field and method signature.
5. Add `ExtractDate`, plus the package-level `Compare` and `Less`.
6. `internal/providers/github.go`:
   - delete `dateRegex` (`:18-19`) and rewrite `extractVersionFromDate`
     (`:229-249`) to use `version.ExtractDate`; drop the 6-digit `"20" + rawDate`
     branch (`:241-243`). A tag with no 8-digit date is now an error, as the script
     intends. (**D8**)
   - replace the `strings.Index(tag, "^{ ")` block at `:160-163` with
     `tag = peeled-stripped`, i.e. call `version.Normalize` on the tag at that point
     (or inline the same `\^\{.*\}` removal). **Leave `pios/` at `:147` and the
     space-truncation at `:166-168` exactly as they are** — plan 17 owns them.
7. Run `go test ./... -count=1`. `internal/upstream`'s golden tables may shift; update
   a golden **only** where the new value is demonstrably correct against the vercmp
   block above, and list every golden you changed in your report.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**.

**Tier 1 — automated, run these. The plan is not done until all are green.**
```bash
go build ./...
go vet ./...
go test ./internal/version/... ./internal/providers/... -count=1
go test ./... -count=1        # no NEW failures vs. your recorded baseline
golangci-lint run ./...
```
**Known-red baseline at HEAD (do not chase):**
`TestExtractVar/multi_line` in `internal/prepare` (plan 20/22 owns it) and one
`go vet` finding at `internal/git/git.go:318` (self-assignment; plan 29 owns it).
Your green-suite claim is **baseline-relative**: "no new failures versus that
baseline", not "the whole suite is green".

**Tier 1 — source-level grep (run it; it must print nothing):**
```bash
grep -rn '"rel-"' internal/version internal/providers
# expect: no output
```
```bash
grep -rn 'pios/' internal/providers
# expect: EXACTLY ONE hit, internal/providers/github.go:147. It is intentional
# and owned by plan 17. Do not delete it.
```

**Tier 2 — read-only manual. Needs `~/Developer/gentoo-overlay` + network. Safe to
run; a failure here may be GitHub rate-limiting, so re-run before concluding. Skip and
say so if the overlay is absent.**
```bash
larry upstream --fresh --overlay ~/Developer/gentoo-overlay -v
```
Expect: `sys-kernel/raspberrypi-kernel` is no longer reported as AHEAD of `1.20230405`,
and `app-admin/cockpit` no longer claims `310.10` is latest. Some rows need plan 17 as
well — **record which rows are still wrong and hand that list to plan 17** rather than
fixing tag selection here.

**Tier 3 — none.** Nothing in this plan mutates state outside the repo.

---

## Out of scope

- Which tag the GitHub provider *picks*, the 3-tier strategy, `pios/`, and the
  space-truncation heuristic → plan 17.
- Table rendering → plan 19.
- Simplifying plan 12's `cmpPV` helper to `return version.Compare(a, b)` — plans 12 and
  14 run in parallel, so leave it. Note it in your report.
- The `go vet` self-assignment at `internal/git/git.go:318` (plan 29).

If you find an unrelated bug, write it in your final report; do not fix it.
