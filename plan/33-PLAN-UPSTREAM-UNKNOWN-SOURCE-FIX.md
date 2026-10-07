# 33 — `upstream`: show correct source label on provider resolution failures

**Why:** `dev-util/fixjson` (GitHub `rhysd/fixjson`) and `gui-apps/nirius`
(SourceHut `~tsdh/nirius`) show `UNKNOWN` with source `unknown` despite having
valid `<remote-id>` entries. The provider resolves (metadata.xml → remote-id),
but `FetchLatestVersion` fails (no tags/releases on the repo). The error branch
in `resolveOne` sets `Source: SourceInfo{}` (empty → "unknown" in rendering).

**Precondition:** 16 (discovery), 19 (`SourceInfo` type).

**Reference script:** script shows the remote-id even when version check fails.

---

## Defects

| ID | Location | Defect |
|----|----------|--------|
| D1 | `upstream/upstream.go:702-712` | error rows don't populate `Source` — shows `unknown` instead of resolved provider |

---

## Target behaviour

In `resolveOne`, when the provider type is resolved and a `RemoteId` is found, but
`FetchLatestVersion` (or `FetchResolution`) fails, include the source info:

```go
// Provider resolved but version fetch failed — show source, not "unknown".
source := resolveSourceInfo(rid)
return UpstreamResult{
    Package: einfo.Category + "/" + einfo.Pkg,
    Current: eb.PVR,
    Latest:  "",
    Status:  StatusUnknown,
    Source:  source,
    Error:   err.Error(),
    Depth:   depth,
}
```

Two error branches need this fix:

1. **Provider resolved, registry resolve succeeded, but FetchLatestVersion failed**
   (upstream.go:756-764) — currently returns `Source: SourceInfo{}`.

2. **Provider resolved, but FetchResolution returned an error**
   (upstream.go:739-745) — currently returns `Source: SourceInfo{}`.

Change both to include `Source: resolveSourceInfo(rid)`.

---

## Tests

### `internal/upstream/output_test.go` — append

- `TestSourceCell_ErrorRowShowsSource` — row with `Status: StatusUnknown`,
  `Source: SourceInfo{Label: "GitHub:rhysd/fixjson", URL: "https://github.com/rhysd/fixjson"}`,
  `Error: "gh api releases/latest: 404 Not Found"`, `hyperlinks == true`.
  Expect the rendered cell contains `https://github.com/rhysd/fixjson` and
  `GitHub:rhysd/fixjson`.

### `internal/upstream/upstream_test.go` — append

- `TestResolveOne_UnknownWithSource` — fixture: `dev-util/fixjson/fixjson-1.1.2-r1.ebuild`
  with `SRC_URI` pointing to GitHub, `<remote-id type="github">rhysd/fixjson</remote-id>`,
  and a **recording fake provider** that returns an error.
  Expect row: `Package == "dev-util/fixjson"`, `Status == StatusUnknown`,
  `Source.Label == "GitHub:rhysd/fixjson"`, `Error != ""`.

---

## Implementation steps

1. In `resolveOne` (upstream.go), find the two error return blocks after successful
   provider resolution (around lines 739-764). Add `Source: resolveSourceInfo(rid)`
   to each.
2. Run new tests.
3. Run full suite: `go test ./internal/upstream/... ./cmd/larry/... -count=1`.

---

## Acceptance

### Tier 1

```bash
go build ./...
go vet ./...
go test ./internal/upstream/... ./cmd/larry/... -count=1
```

Grep assertions (must print nothing):
```bash
grep -n 'StatusUnknown' internal/upstream/upstream.go | grep 'Source:' | grep -v 'resolveSourceInfo'
```

### Tier 2

```bash
larry upstream --fresh --overlay ~/Developer/gentoo-overlay -v | grep fixjson
# expect: Source column shows "GitHub:rhysd/fixjson" (or a glyph + label), NOT "unknown"
```

### Tier 3 — DO NOT RUN

Nothing in this plan is Tier 3.
