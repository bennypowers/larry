# 17a — `upstream`: `Resolution`, `ResolverWithTags`, and tag-date status derivation

**Why:** The script has a second verdict path. When GitHub has no usable release and
only a dated tag is available, it compares *dates*, not versions, and emits `NEWER-TAG`
or `CHECK-TAG`. Go declares `StatusNewerTag` and never assigns it, and has no
`StatusCheckTag` at all. This plan builds that plumbing — the type, the interface, the
status, and the caller-side derivation — **with no network and no `gh`**. Plan 17b then
makes the real GitHub provider produce the input. Plan 19's golden file needs the two
statuses to exist.

**Precondition:** 14 (version semantics), 16 (discovery).

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent on this machine, this
plan's text is authoritative — do not guess at the script's behaviour.**
Functions: the `fetch_result == 2` branch of `check_package` (L1469–1497),
`extract_date_from_string` (L1064–1071), `is_version_like` (L1128–1130),
`strip_tag_prefix` (L1120–1126).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. Known-red baseline: exactly
> `TestExtractVar/multi_line` in `internal/prepare`, plus one `go vet` finding at
> `internal/git/git.go:318`. Do not chase either.

---

## Precondition check — run these first

```bash
go doc ./internal/version ExtractDate
go doc ./internal/version Version.IsPrerelease
go doc ./internal/version Compare
go doc ./internal/upstream Status
go doc ./internal/upstream UpstreamResult
go doc ./internal/provider UpstreamProvider
```

Expected (plan 14):

```go
func ExtractDate(s string) (string, bool)
func Compare(a, b string) int
```

and **`IsPrerelease()` must return `false` for `1.0_p1`** — plan 14 D3 narrows it to
alpha/beta/pre/rc only. Verify with a one-off:

```bash
cat > /tmp/pre_check_test.go <<'EOF'
package version
import "testing"
func TestPlan17aPrecondition(t *testing.T) {
    v, err := Parse("1.0_p1")
    if err != nil { t.Fatal(err) }
    if v.IsPrerelease() { t.Fatal("IsPrerelease(1.0_p1) must be false — plan 14 has not landed") }
}
EOF
cp /tmp/pre_check_test.go internal/version/ && go test ./internal/version -run TestPlan17aPrecondition -count=1
rm internal/version/pre_check_test.go
```

Expected (plan 16): `go doc ./internal/upstream Status` lists `StatusHasRelease`.

**If any of these is missing, STOP and report.** The prerequisite plan has not landed.
Do not implement the missing piece yourself.

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D4a | `internal/upstream/output.go:22` | `StatusNewerTag` is declared and assigned nowhere in production code |
| D4b | `internal/upstream/output.go:18-26` | `StatusCheckTag` does not exist |
| D4c | `internal/upstream/upstream.go:1060-1131` | the per-package resolution only ever calls `FetchLatestVersion`; there is no path by which a dated-tag result could reach the row builder |

(`D1`, `D2`, `D3`, `D5`, `D6` are plan 17b's.)

---

## Target behaviour

### 1. `Resolution` and `ResolverWithTags` — exact home and shape

Both go in **`internal/provider/interface.go`**, next to `UpstreamProvider`:

```go
// Resolution is a provider's answer when it may have found a dated tag rather
// than a release version. It replaces the script's "exit code 2" signal.
type Resolution struct {
    Version string // normalized version string; "" when only a dated tag was found
    Tag     string // raw tag name from the tag-by-date path
    TagDate string // "YYYYMMDD"
    ByDate  bool   // true => the caller must use the date comparison path
}

// ResolverWithTags is implemented by providers that can additionally resolve a
// dated tag when no release version is available. Callers must type-assert and
// fall back to UpstreamProvider.FetchLatestVersion when the assertion fails.
type ResolverWithTags interface {
    UpstreamProvider
    FetchResolution(rid remoteid.RemoteId, allowPrereleases bool) (*Resolution, error)
}
```

**Only `*providers.GitHubProvider` will implement it** (plan 17b). Its existing
`FetchLatestVersion` stays exactly as it is, so the other five providers and
`ProviderRegistry.Resolve` are untouched. Do not widen `UpstreamProvider`.

### 2. `StatusCheckTag`

Append to the `const` block at `internal/upstream/output.go:18-26`, **after**
`StatusHasRelease` (which plan 16 appended after `StatusUnknown`), so no iota value
shifts:

```go
StatusUnknown                // Could not determine upstream status
StatusHasRelease             // Live-only ebuild, but upstream has a release
StatusCheckTag               // Only a non-version-like tag was found; needs a human
```

Add `case StatusCheckTag: return "CHECK-TAG"` to `String()` (`:29-47`).
`IsSignificant()` (`:53-55`) must **keep returning `true`** for it — it needs a human,
so it shows by default. Add its colour to the switch in
`internal/cli/upstream_tui.go:185-199` and to the status-column switch in
`internal/upstream/output.go:186-199`; use `lipgloss.Color("220")` (the same yellow as
`StatusNewerTag`).

### 3. Caller-side derivation (script L1469–1497)

New unexported function in `internal/upstream`:

```go
// deriveTagStatus implements the script's `fetch_result == 2` branch.
// currentPV is the ebuild's PV. Returns the status and the Latest cell text.
func deriveTagStatus(currentPV string, res *provider.Resolution) (Status, string)
```

Pseudocode, transcribed from the script:

```
tagName := regexp `^[vV](?=\d)` stripped from res.Tag
             -- Go has no lookahead; implement as:
             -- if len(t) > 1 && (t[0]=='v'||t[0]=='V') && t[1] >= '0' && t[1] <= '9' { t = t[1:] }
latest  := tagName
curDate, curOK := version.ExtractDate(currentPV)

if isVersionLike(currentPV) && isVersionLike(tagName):
    if curOK && res.TagDate != "":
        res.TagDate >  curDate -> (StatusNewerTag,  latest)   // plain string compare, both "YYYYMMDD"
        res.TagDate == curDate -> (StatusUpToDate,  latest)
        res.TagDate <  curDate -> (StatusAhead,     latest)
    else:
        switch version.Compare(currentPV, tagName) {
        case -1: (StatusOutdated,  latest)
        case  0: (StatusUpToDate,  latest)
        case  1: (StatusAhead,     latest)
        }
else:
    (StatusCheckTag, latest)
```

with

```go
// isVersionLike mirrors the script's `string match -qr '^\d+[\.\d]*'` (L1129).
var versionLikeRe = regexp.MustCompile(`^[0-9][0-9.]*`)
func isVersionLike(s string) bool { return versionLikeRe.MatchString(s) }
```

Note the script's two date strings are both `YYYYMMDD`, so `>`/`==`/`<` on the raw
strings is correct and `strconv` is not needed. Say so in a comment.

### 4. Call site

In `internal/upstream`'s per-package resolution — the provider-call block inside
`walkAndCheckCategories` at roughly `upstream.go:1060-1100`, immediately where
`p.FetchLatestVersion(*rid, allowPrereleases)` is called today:

```go
if rt, ok := p.(provider.ResolverWithTags); ok {
    res, rerr := rt.FetchResolution(*rid, allowPrereleases)
    if rerr == nil && res != nil && res.ByDate {
        status, latest := deriveTagStatus(einfo.PVVal, res)
        // build the row directly from status/latest; skip the version-compare path
        ...
        continue
    }
    if rerr == nil && res != nil && res.Version != "" {
        latestV, err = version.Parse(res.Version)
    } else {
        latestV, err = p.FetchLatestVersion(*rid, allowPrereleases)
    }
} else {
    latestV, err = p.FetchLatestVersion(*rid, allowPrereleases)
}
```

Keep the existing cache behaviour for the non-`ByDate` path. **Do not cache `ByDate`
results in this plan** — the cache stores a bare version string and cannot represent a
tag+date pair. Note that limitation in your report.

---

## Tests (write first — every one must fail before you implement)

**No network, no `gh`, no `httptest`.** Every test here drives a hand-written fake.

### The fake provider — write it once, at the top of the test file

```go
// fakeTagResolver implements provider.ResolverWithTags with canned answers.
type fakeTagResolver struct {
    res *provider.Resolution
    err error
    ver *version.Version
}

func (f *fakeTagResolver) Type() remoteid.RemoteIdType { return remoteid.TypeGitHub }
func (f *fakeTagResolver) Name() string                { return "fake" }
func (f *fakeTagResolver) Metadata() provider.ProviderMetadata {
    return provider.ProviderMetadata{Name: "fake"}
}
func (f *fakeTagResolver) FetchLatestVersion(rid remoteid.RemoteId, pre bool) (*version.Version, error) {
    if f.ver == nil {
        return nil, errors.New("fake: no version")
    }
    return f.ver, nil
}
func (f *fakeTagResolver) FetchResolution(rid remoteid.RemoteId, pre bool) (*provider.Resolution, error) {
    return f.res, f.err
}
```

Compile-time assertion in the test file:
`var _ provider.ResolverWithTags = (*fakeTagResolver)(nil)`.

### `internal/upstream/upstream_test.go` — unit tests on `deriveTagStatus`

Exact inputs and expected outputs:

| Test | `currentPV` | `Resolution` | Expect |
|---|---|---|---|
| `TestStatus_NewerTag` (**D4a**) | `6.18.46_p20260824` | `{Tag:"6.18.47", TagDate:"20270101", ByDate:true}` | `StatusNewerTag`, latest `6.18.47` |
| `TestStatus_TagDateEqual` | `6.18.46_p20260824` | `{Tag:"v6.18.46", TagDate:"20260824", ByDate:true}` | `StatusUpToDate`, latest `6.18.46` |
| `TestStatus_TagDateOlder` | `6.18.46_p20260824` | `{Tag:"6.18.45", TagDate:"20250101", ByDate:true}` | `StatusAhead`, latest `6.18.45` |
| `TestStatus_CheckTag` (**D4b**) | `1.2.3` | `{Tag:"nightly-build", TagDate:"20260101", ByDate:true}` | `StatusCheckTag`, latest `nightly-build` |
| `TestStatus_ByDate_NoDatesFallsBackToVersionCompare` | `1.2.3` | `{Tag:"v1.3.0", TagDate:"", ByDate:true}` | `StatusOutdated`, latest `1.3.0` |

Two extras that pin the `v`-strip rule (the script's `^[vV](?=\d)`, L1471):

- `TestStatus_StripsVPrefixOnlyBeforeDigit` — `{Tag:"v1.3.0"}` → latest `1.3.0`;
  `{Tag:"verbose-1.0"}` → latest `verbose-1.0` (the `e` is not a digit, so nothing is
  stripped), status `StatusCheckTag`.

### `internal/upstream/upstream_test.go` — one integration test through the call site

- `TestResolverWithTags_DrivesTagStatus` (**D4c**) — a `t.TempDir()` overlay with
  `sys-kernel/rpi/rpi-6.18.46_p20260824.ebuild` whose
  `SRC_URI="https://github.com/raspberrypi/linux/archive/x.tar.gz"`. Register a
  `&fakeTagResolver{res: &provider.Resolution{Tag:"6.18.47", TagDate:"20270101", ByDate:true}}`
  for `remoteid.TypeGitHub` in a fresh `provider.NewProviderRegistry()`. Run
  `RunWithTargets`. Expect exactly one row with `Status == StatusNewerTag` and
  `Latest == "6.18.47"`.
- `TestResolverWithTags_FallsBackToVersionWhenNotByDate` — same fixture,
  `res: &provider.Resolution{Version:"7.0", ByDate:false}` → `Status == StatusOutdated`,
  `Latest == "7.0"`.
- `TestPlainProviderStillUsesFetchLatestVersion` — register a provider that does **not**
  implement `ResolverWithTags` (the existing mock in `upstream_test.go` is fine) and
  assert the row still resolves. Guards against the type assertion breaking the other
  five providers.

### `internal/upstream/output_test.go`

- `TestStatusString_CheckTag` — `StatusCheckTag.String() == "CHECK-TAG"`.
- `TestStatusColor_CheckTag` — with `t.Setenv("LARRY_COLOR", "always")`, `RenderTable`
  over a single `StatusCheckTag` row emits the SGR sequence for colour 220
  (`\x1b[38;5;220m`); with `t.Setenv("LARRY_COLOR", "never")` it does not.
- `TestStatusIotaUnchanged` (extend plan 16's) — add
  `StatusCheckTag == 8`.
- `TestCheckTag_IsSignificant` — `StatusCheckTag.IsSignificant() == true`.

---

## Implementation steps

Every step has a test above; write the test first and watch it fail.

1. Add `Resolution` and `ResolverWithTags` to `internal/provider/interface.go`.
   Nothing implements `ResolverWithTags` yet — that is expected and is not a lint
   problem, because both are exported.
2. Add `StatusCheckTag` + its `String()` case + both colour switches
   (`internal/upstream/output.go:186-199`, `internal/cli/upstream_tui.go:185-199`).
   **Expect `golangci-lint`'s `unused` to stay quiet** (the const is exported), but if
   you split this across commits, note that step 2 is only *assigned* in step 4.
3. Add `isVersionLike` + `deriveTagStatus` to `internal/upstream`.
4. Wire the call site in `walkAndCheckCategories` exactly as shown under
   "Call site". (**D4c**)
5. Run the suites; fix fallout.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**. Read that section before running
> anything.

### Tier 1 — automated, required. Run these.

```bash
go build ./...
go vet ./...                 # internal/git/git.go:318 is the known baseline finding
go test ./... -count=1       # no NEW failures vs. your recorded baseline
golangci-lint run ./...
```

Targeted:

```bash
go test ./internal/upstream/... ./internal/provider/... -count=1
```

Grep assertions:

```bash
grep -n 'StatusCheckTag' internal/upstream/output.go internal/upstream/upstream.go \
  internal/cli/upstream_tui.go
# expect: declared, String()-cased, coloured in BOTH switches, and assigned in upstream.go

grep -rn 'FetchResolution' --include=*.go ./internal/providers
# expect: no output — plan 17b owns the implementation
```

### Tier 2 — read-only manual. Nothing in this plan needs it.

`deriveTagStatus` has no reachable production producer until plan 17b lands, so the
reference overlay cannot exercise it. Do not try to force it.

### Tier 3 — DO NOT RUN.

Nothing in this plan is Tier 3.

---

## Out of scope

The GitHub provider itself — tiers 1–4, the `gh` argv, `git.ListRemoteTags`, the
`TagLister` seam, D1/D2/D3/D5/D6 — all belong to **plan 17b**. Do not touch
`internal/providers/github.go`, `internal/github/github.go` or `internal/git` here.
Table rendering and hyperlinks (plan 19). Do not fix unrelated bugs you notice; report
them.
