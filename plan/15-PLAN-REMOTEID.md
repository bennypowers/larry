# 15 — `metadata.xml` parsing & upstream repo extraction

**Why:** Two whole provider classes (GitLab, SourceHut) never resolve, because the
extraction regexes match a literal `gitlab/` and a `~`-less SourceHut path. Separately,
`metadata.xml` cannot be parsed at all — the struct expects `<pkg-metadata>` with
top-level `<remote-id>`, while Gentoo writes `<pkgmetadata><upstream><remote-id>`. The
"preserve upstream remote-id" feature in `import` is therefore a no-op, and `upstream`
never consults `metadata.xml` at all.

**Precondition:** none — this plan is standalone.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent, this plan's text is
authoritative — do not guess at the script's behaviour.**
Functions: `extract_github_repo` (L975–988), `extract_gitlab_repo` (L990–1005),
`extract_gitea_repo` (L1007–1019).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting.

---

## Precondition check (run first)

```bash
go doc bennypowers.dev/larry/internal/remoteid ParseMetadata
# expect: func ParseMetadata(data []byte) (*Metadata, error)

go doc bennypowers.dev/larry/internal/remoteid ResolveRemoteId
# expect: func ResolveRemoteId(ebuildPath string) ([]*RemoteId, error)
#   ^^ NOTE: this is remoteid.ResolveRemoteId. It is a DIFFERENT function from
#      upstream.ResolveRemoteId. It already walks up from the ebuild path looking
#      for metadata.xml (metadata.go:70-88). You will CALL it. Do not duplicate it.

go doc bennypowers.dev/larry/internal/remoteid ParseRemoteIdFromStrings
# expect: func ParseRemoteIdFromStrings(rtype string, content string) (*RemoteId, error)

go doc bennypowers.dev/larry/internal/upstream ResolveProviderType
# expect: func ResolveProviderType(srcURI, homepage string, inherit []string, content string) string
#   ^^ NOTE: NO ebuild-path parameter. Step 6 adds one.

go doc bennypowers.dev/larry/internal/upstream ResolveRemoteId
# expect: func ResolveRemoteId(ei *ebuild.EbuildInfo, providerType string) (*remoteid.RemoteId, error)
#   ^^ NOTE: takes *ebuild.EbuildInfo, which HAS a populated .Path field
#      (set at internal/ebuild/info.go:66 and :163).

go doc bennypowers.dev/larry/internal/ebuild EbuildInfo
# expect a struct whose FIRST field is: Path string
```

If any signature differs from the above, the repo has moved out from under this plan —
**STOP and report**, do not improvise.

---

## Defects being fixed

| ID | Location | Defect | Verified |
|----|----------|--------|----------|
| D1 | `internal/remoteid/metadata.go:20` | `XMLName xml.Name \`xml:"pkg-metadata"\`` | parsing a real file → `expected element type <pkg-metadata> but have <pkgmetadata>` |
| D2 | `internal/remoteid/metadata.go:23` | `RawRemotes []rawRemoteIdXML \`xml:"remote-id"\`` matches only direct children | canonical `<upstream><remote-id>` yields **0** remotes, no error |
| D3 | `internal/upstream/upstream.go:811-817, 839-861` | `extractRepoFromURLs(src, home, "gitlab")` builds the regex `gitlab/(…)/(…)` — a **literal** `gitlab/` | `https://gitlab.com/GNOME/geary/…` → Host `""` → `gitlab: missing host in RemoteId` |
| D4 | `internal/upstream/upstream.go:825-831` | SourceHut pattern `sr.ht/([A-Za-z0-9._-]+)/…` — `~` is not in the character class | `git.sr.ht/~sircmpwn/scdoc` → no match |
| D5 | `internal/upstream/upstream.go:850-861` | `.git` suffix not stripped | `https://github.com/owner/repo.git` → repo `repo.git` → every `gh` call 404s |
| D6 | `internal/upstream` (whole package) | `metadata.xml` `<remote-id>` is never consulted; only SRC_URI/HOMEPAGE heuristics | `gui-apps/nirius` declares `<remote-id type="sourcehut">~tsdh/nirius</remote-id>` and still reports UNKNOWN |
| D7 | `internal/upstream/upstream.go:672` | `strings.Contains(text, "gitlab.")` matches `https://about.gitlab.com` in HOMEPAGE | — |
| D8 | `internal/remoteid/remoteid.go:145-156` | `ParseLegacyUpstream` is a stub returning `nil, nil` | — |
| D9 | `internal/remoteid/remoteid.go:137`, `internal/upstream/upstream.go:816, :830` | `Path` is built as `"/" + path`, contradicting the documented contract and producing `gitlab.com:/group/project` | — |

---

## Target behaviour

### metadata.xml shape — what Gentoo actually writes

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkgmetadata SYSTEM "https://www.gentoo.org/dtd/metadata.dtd">
<pkgmetadata>
  <maintainer type="person">
    <email>…</email><name>…</name>
  </maintainer>
  <upstream>
    <remote-id type="github">owner/repo</remote-id>
    <remote-id type="sourcehut">~user/project</remote-id>
    <remote-id type="gitlab">group/project</remote-id>
    <remote-id type="pypi">name</remote-id>
    <bugs-to>https://…</bugs-to>
  </upstream>
  <longdescription>…</longdescription>
</pkgmetadata>
```

`<upstream>` may also contain `<bugs-to>`, `<maintainer>`, `<doc>`, `<changelog>`, and
the file may have `<longdescription>`. **Ignore unknown children** — `encoding/xml`
does this for free; do not add fields for them.

Use `xml:"upstream>remote-id"` **only**. Decided: no tolerant path for a bare top-level
`<remote-id>`. No fixture in this repo needs one after the test updates below.

### `RemoteId.Path` contract: NO leading slash

| Forge | `<remote-id>` content | Host | Path |
|---|---|---|---|
| GitLab | `gitlab.com:group/project` | `gitlab.com` | `group/project` |
| GitLab | `gitlab.gnome.org/GNOME/geary` | `gitlab.gnome.org` | `GNOME/geary` |
| SourceHut | `~tsdh/nirius` | `git.sr.ht` | `~tsdh/nirius` |
| SourceHut | `git.sr.ht/~user/project` | `git.sr.ht` | `~user/project` |

`(*RemoteId).String()` (`remoteid.go:43-56`) currently formats GitLab/SourceHut as
`fmt.Sprintf("%s:%s%s", r.Type, r.Host, r.Path)` — **no separator**, which only ever
worked because `Path` carried a leading slash. Change `remoteid.go:49-50` to
`fmt.Sprintf("%s:%s/%s", r.Type, r.Host, r.Path)`.

### Repo extraction, per the script

| Forge | Pattern | RemoteId |
|---|---|---|
| GitHub | `https?://github\.com/<owner>/<repo>` | `Owner`, `Repo` (strip `.git`) |
| Codeberg | `https?://codeberg\.org/<owner>/<repo>` | `Owner`, `Repo` (strip `.git`) |
| GitLab | `https?://(gitlab\.[A-Za-z0-9.-]+)/<path…>` | `Host` = the matched hostname; `Path` = everything after it, **truncated at the first `/-/`**, `.git` stripped |
| SourceHut | `https?://(git\.)?sr\.ht/(~<user>/<project>)` | `Host` = `git.sr.ht` (always), `Path` = `~user/project` |

**Candidate handling (script L981–983):** split the SRC_URI / HOMEPAGE text on
whitespace into candidate tokens, **skip any token containing `${`**, then apply the
regex to the remaining tokens in order. First match wins. This applies to **all four**
forges, not just GitHub.

**Precedence:** `metadata.xml` `<remote-id>` wins over URL heuristics. Fall back to
SRC_URI/HOMEPAGE scanning only when no usable remote-id exists.

### Where the regexes live

**Decided: `internal/remoteid`.** No import cycle — `remoteid` does not import
`upstream`, and `upstream` already imports `remoteid`. New exported entry point:

```go
// ExtractFromURLs scans srcURI then homepage for a URL belonging to forge and
// returns the matching RemoteId, or nil when nothing matches.
func ExtractFromURLs(forge RemoteIdType, srcURI, homepage string) *RemoteId
```

`internal/upstream`'s `extractRepoFromURLs` (`:839-848`) and `extractOwnerRepo`
(`:850-861`) are **deleted**.

---

## Test fixture: the real `gui-apps/nirius/metadata.xml`

Write this verbatim to `internal/remoteid/testdata/metadata/nirius.xml` so the test
does not depend on `~/Developer/gentoo-overlay` being present:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkgmetadata SYSTEM "https://www.gentoo.org/dtd/metadata.dtd">
<pkgmetadata>
	<maintainer type="person">
		<email>web@bennypowers.com</email>
		<name>Benny Powers</name>
	</maintainer>
	<upstream>
		<remote-id type="sourcehut">~tsdh/nirius</remote-id>
		<bugs-to>https://todo.sr.ht/~tsdh/nirius</bugs-to>
	</upstream>
	<longdescription>
		Nirius provides utility commands for the niri wayland compositor.
		It includes a daemon (niriusd) and a client (nirius) for features
		like focus-or-spawn, scratchpad, follow-mode, window marks, and
		moving windows between workspaces.
	</longdescription>
</pkgmetadata>
```

---

## Existing tests that must change (do these FIRST, as part of your red step)

Every one of these encodes the broken `<pkg-metadata>` shape or the leading-slash
`Path`. They will not pass after the fix, and updating them **is** the red step.

### `internal/remoteid/metadata_test.go`
- `:11-18` (`TestParseMetadata`) — `<pkg-metadata>` → `<pkgmetadata>` (3 places:
  DOCTYPE, open tag, close tag).
- `:51-53` (`TestParseMetadataFile`) — same rename.
- `:115-124` (`TestMetadataXML_Parse`) — rename the root **and** wrap the two
  `<remote-id>` elements in `<upstream>…</upstream>`.
- `:168-173` (`TestRemoteId_MultipleTags`) — rename the root and wrap the four
  `<remote-id>` elements in `<upstream>`.
- `:217-219` (`TestMetadataXML_Absent`) — rename the root.

### `internal/remoteid/remoteid_test.go`
- `:99-114` (`TestRemoteId_Parse_SourceHut`) — expected `Path` changes from
  `"/~user/project"` to `"~user/project"`.
- `:200-221` (`TestRemoteId_String`) — the two input literals lose their leading
  slash: `Path: "/group/project"` → `"group/project"` and
  `Path: "/~user/project"` → `"~user/project"`. The **expected strings are
  unchanged** (`gitlab:gitlab.com/group/project`,
  `sourcehut:git.sr.ht/~user/project`) — that is exactly what the
  `"%s:%s/%s"` fix buys.

### `internal/importcmd` — required to keep the Tier 1 gate green
`internal/importcmd/import.go:403` and `:421` write `<pkg-metadata>` with top-level
`<remote-id>`; `import_test.go:264`, `:351`, `:488` contain `<pkg-metadata>` fixtures
that are round-tripped through `remoteid.ParseMetadataFile` at `import_test.go:274`,
`:372`, `:503`, `:531`. After the parser fix those round trips break.

**Minimal required change (do not go further — plan 28 owns `import`):** make
`writeMetadata` emit
```
<?xml version="1.0" encoding="UTF-8"?>
<pkgmetadata>
  <maintainer>…</maintainer>
  <upstream>
    <remote-id type="…">…</remote-id>
  </upstream>
</pkgmetadata>
```
emitting the `<upstream>` block only when there is at least one remote-id. Update the
three test fixtures to match. Note in your report that `import.go:443`
(`r.Host + ":" + r.Path`) now produces `gitlab.com:group/project` instead of
`gitlab.com:/group/project` — that is the correct Gentoo form.

---

## New tests (write first — every one must fail before you implement)

### `internal/remoteid/metadata_test.go`
- `TestParseMetadata_RealShape` — the canonical document from "Target behaviour"
  parses; **4** remote-ids with the right types and values; `<bugs-to>` and
  `<longdescription>` are ignored without error. (**D1, D2**)
- `TestParseMetadata_NoUpstreamBlock` — a maintainer-only `<pkgmetadata>` parses,
  0 remotes, nil error.
- `TestParseMetadata_Malformed` — `[]byte("not valid xml")` returns an error and does
  not panic.
- `TestParseMetadataFile_Nirius` — parses `testdata/metadata/nirius.xml`; exactly one
  remote-id with `Type == TypeSourceHut`, `Host == "git.sr.ht"`,
  `Path == "~tsdh/nirius"`.
- `TestRemoteId_SourceHutString` — `ParseRemoteIdFromStrings("sourcehut", "~tsdh/nirius")`
  → `Host "git.sr.ht"`, `Path "~tsdh/nirius"`, and
  `String() == "sourcehut:git.sr.ht/~tsdh/nirius"`.
  (Not a "round trip" — `String()` is a label, not a parser input.)

### `internal/remoteid/extract_test.go` (new file)
- `TestExtractFromURLs_GitHub` —
  `https://github.com/owner/repo/archive/v1.0.tar.gz` → `Owner "owner"`, `Repo "repo"`.
- `TestExtractFromURLs_StripsDotGit` —
  `https://github.com/owner/repo.git` → `Repo "repo"`. (**D5**)
- `TestExtractFromURLs_GitLabDotCom` —
  `https://gitlab.com/GNOME/geary/-/archive/44.0/geary-44.0.tar.gz`
  → `Host "gitlab.com"`, `Path "GNOME/geary"`. (**D3**)
- `TestExtractFromURLs_GitLabSelfHosted` —
  `https://gitlab.gnome.org/GNOME/geary` → `Host "gitlab.gnome.org"`,
  `Path "GNOME/geary"`. (**D3**)
- `TestExtractFromURLs_GitLabNested` —
  `https://gitlab.com/a/b/c` → `Host "gitlab.com"`, `Path "a/b/c"`.
- `TestExtractFromURLs_SourceHut` — two rows:
  `https://git.sr.ht/~sircmpwn/scdoc` → `Host "git.sr.ht"`, `Path "~sircmpwn/scdoc"`;
  `https://sr.ht/~tsdh/nirius` → `Host "git.sr.ht"`, `Path "~tsdh/nirius"`.
  (**D4**) (The host is normalised to `git.sr.ht` in both cases.)
- `TestExtractFromURLs_Codeberg` —
  `https://codeberg.org/owner/repo/archive/v1.tar.gz` → `Owner "owner"`, `Repo "repo"`.
- `TestExtractFromURLs_SkipsUnexpandedVars` — run for **all four** forges. Input is a
  two-token SRC_URI where the first token contains `${`:
  - github: `"https://github.com/${PN}/${PN}/archive/v1.tar.gz https://github.com/owner/repo/archive/v1.tar.gz"`
    → `owner/repo`
  - codeberg: same shape with `codeberg.org` → `owner/repo`
  - gitlab: `"https://gitlab.com/${PN}/x/-/archive/1/x.tar.gz https://gitlab.com/group/project/-/archive/1/x.tar.gz"`
    → `Path "group/project"`
  - sourcehut: `"https://git.sr.ht/~${PN}/x https://git.sr.ht/~user/project"`
    → `Path "~user/project"`
- `TestExtractFromURLs_IgnoresAboutGitlabCom` — `srcURI == ""`,
  `homepage == "https://about.gitlab.com"` → returns `nil` for
  `TypeGitLab`. (**D7**)
- `TestExtractFromURLs_NoMatch` — unrelated URL → `nil`, no panic.

### `internal/upstream/upstream_test.go`
- `TestDetectProvider_IgnoresAboutGitlabCom` — `detectProviderFromSRCURI("", "https://about.gitlab.com")`
  does **not** return `"gitlab"`. (**D7**)
- `TestResolveRemoteId_PrefersMetadataXml` — build a `t.TempDir()` package dir
  `gui-apps/nirius/` containing the nirius `metadata.xml` fixture **and** an ebuild
  whose `SRC_URI` points at `https://github.com/someone/nirius/archive/v1.tar.gz`.
  `ResolveProviderType(ebuildPath, …)` returns `"sourcehut"` and
  `ResolveRemoteId(ei, "sourcehut")` returns `Host "git.sr.ht"`,
  `Path "~tsdh/nirius"`. (**D6**)
- `TestResolveRemoteId_FallsBackToURLs` — same dir with **no** `metadata.xml`:
  provider type `"github"`, `Owner "someone"`, `Repo "nirius"`.
- `TestResolveRemoteId_IgnoresUnparseableCategoryMetadata` — package dir has no
  `metadata.xml`, but its parent (category) dir has one rooted at `<catmetadata>`.
  `remoteid.ResolveRemoteId` returns an error; the upstream call site must **swallow
  it** and fall back to URL heuristics rather than failing the package.
- `TestResolveSourceLabel_GitLab` — `resolveSourceLabel(&remoteid.RemoteId{Type:
  remoteid.TypeGitLab, Host: "gitlab.gnome.org", Path: "GNOME/geary"})` returns exactly
  `"GitLab:gitlab.gnome.org/GNOME/geary"`.
  **This asserts `resolveSourceLabel` (`upstream.go:863`), not `RemoteId.String()`** —
  the capital `G` comes from the string literal in that function; `String()` uses the
  lowercase type constant and returns `gitlab:…`.
- `TestResolveSourceURL_GitLabAndSourceHut` —
  `resolveSourceURL` (`upstream.go:895`) returns `https://gitlab.gnome.org/GNOME/geary`
  and `https://git.sr.ht/~tsdh/nirius` (one slash, not zero and not two).

### `internal/providers/sourcehut_test.go`
- `TestSourceHut_BuildsRefsURL` — with `Host "git.sr.ht"`, `Path "~tsdh/nirius"`, the
  refs URL built at `sourcehut.go:220` is
  `https://git.sr.ht/~tsdh/nirius/refs` — exactly one slash between host and path.
- `TestSourceHut_AcceptsPathWithoutLeadingSlash` — `fetchViaGraphQL`'s path check
  (`sourcehut.go:99-107`) accepts `~user/project` and extracts `user` / `project`.

---

## Implementation steps

1. **`internal/remoteid/metadata.go`** — `XMLName xml.Name \`xml:"pkgmetadata"\`` and
   `RawRemotes []rawRemoteIdXML \`xml:"upstream>remote-id"\``. (**D1, D2**)
2. **Delete the legacy stubs (D8).** Zero non-test callers — verified. Remove:
   - `ParseLegacyUpstream` (`remoteid.go:145-156`)
   - the `Legacy *LegacyUpstream` field (`metadata.go:25`)
   - the `LegacyUpstream` type (`metadata.go:34-38`)

   Leave `TestRemoteId_BackwardsCompat` (`remoteid_test.go:244`) alone; it exercises
   `ParseRemoteId`, not the deleted stub.
3. **`ParseRemoteIdFromStrings` Path fix (D9).** `remoteid.go:137`:
   `Path: "/" + path` → `Path: path`. Add the SourceHut `~` rule: when
   `rtype == "sourcehut"` and the content starts with `~`, set `Host = "git.sr.ht"` and
   `Path = content`. Update the doc comment at `remoteid.go:26`.
4. **`(*RemoteId).String()`** — `remoteid.go:49-50`:
   `"%s:%s%s"` → `"%s:%s/%s"`.
5. **New `internal/remoteid/extract.go`** with the four compiled package-level regexes
   and `ExtractFromURLs`, implementing the extraction table and the whitespace-token /
   `${`-skip rule. Strip a `.git` suffix in all four. Truncate the GitLab path at the
   first `/-/`.
6. **Thread `metadata.xml` into provider resolution (D6).**
   - Change the signature to
     `func ResolveProviderType(ebuildPath, srcURI, homepage string, inherit []string, content string) string`.
     An empty `ebuildPath` skips the metadata lookup.
   - Inside it, **first** call `remoteid.ResolveRemoteId(ebuildPath)` (the function at
     `internal/remoteid/metadata.go:70` — it already walks up for `metadata.xml`;
     **do not write a second walker**). On a non-nil error, treat it as "no
     remote-ids" and continue. On success, return `string(rids[0].Type)` for the first
     remote-id whose type is one of `github, gitlab, pypi, npm, codeberg, sourcehut`.
     The `RemoteIdType` constants (`remoteid.go:12-20`) are already exactly those
     lowercase provider-type strings, so no mapping table is needed — this is the
     inverse of `mapTypeToRemoteIdType` (`upstream.go:751`).
   - Fall through to the existing SRC_URI / metadata heuristics on miss.
   - **Call sites to update** (pass `einfo.Path` / `ebi.Path`):
     `internal/upstream/upstream.go:272`, `internal/upstream/upstream.go:1044`,
     `internal/bump/bump.go:327`.
   - **`internal/upstream/upstream.go:46-52` inlines the same logic instead of calling
     `ResolveProviderType`.** Replace that block with
     `providerType := ResolveProviderType(ei.Path, ei.SRC_URI, ei.HOMEPAGE, ei.Inherit, ei.Raw)`
     so metadata precedence applies on this path too.
   - In `ResolveRemoteId` (`upstream.go:774`), before the URL heuristics, call
     `remoteid.ResolveRemoteId(ei.Path)` and return the first remote-id whose type
     matches `providerType`. `ei.Path` is already populated
     (`internal/ebuild/info.go:66`, `:163`) — use it; do not add a parameter.
     **Call sites unchanged:** `upstream.go:69`, `upstream.go:290`,
     `upstream.go:1061`, `internal/bump/bump.go:333`.
7. **Replace the extraction helpers.** In `ResolveRemoteId`, the `github`, `gitlab`,
   `codeberg` and `sourcehut` arms (`upstream.go:807-833`) call
   `remoteid.ExtractFromURLs(<type>, ei.SRC_URI, ei.HOMEPAGE)`. **Drop the `"/" + path`
   at `upstream.go:816` and `:830`** (**D9**). Delete `extractRepoFromURLs`
   (`:839-848`) and `extractOwnerRepo` (`:850-861`).
8. **Fix every `Host`+`Path` concatenation** now that `Path` has no leading slash.
   Run `grep -rn 'rid.Host' internal/upstream internal/providers` and fix all of:
   - `internal/upstream/upstream.go:876` → `"GitLab:" + rid.Host + "/" + rid.Path`
   - `internal/upstream/upstream.go:886` → `"Sourcehut:" + rid.Host + "/" + rid.Path`
   - `internal/upstream/upstream.go:904` and `:908` →
     `fmt.Sprintf("https://%s/%s", rid.Host, rid.Path)`
   - `internal/providers/sourcehut.go:220` →
     `fmt.Sprintf("https://%s/%s/refs", rid.Host, rid.Path)`
   - `internal/providers/sourcehut.go:99-107` — the `"/~"` prefix check and
     `TrimPrefix(path, "/~")` become `"~"` / `TrimPrefix(path, "~")`
   - `internal/providers/gitlab.go:64-81` already normalises a missing leading slash
     (`:70-72`) and strips it again at `:81`, so it works either way. **Verify with a
     test; no change required.** Simplifying it is optional.
9. **Tighten GitLab detection (D7).** `internal/upstream/upstream.go:672` —
   replace `strings.Contains(text, "gitlab.com/") || strings.Contains(text, "gitlab.")`
   with a match against `regexp.MustCompile(`https?://gitlab\.[A-Za-z0-9.-]+/`)`.
10. **Update `internal/importcmd`'s writer and fixtures** per "Existing tests that must
    change". Nothing else in `import` — plan 28 owns it.
11. Run the suites; fix fallout. Hand back listing every function, type and test you
    deleted.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**.

**Tier 1 — automated, run these. This is the gate; the plan is not done until all are
green.**
```bash
go build ./...
go vet ./...
go test ./internal/remoteid/... ./internal/upstream/... ./internal/providers/... \
        ./internal/importcmd/... -count=1
go test ./... -count=1        # no NEW failures vs. your recorded baseline
golangci-lint run ./...
```
**Known-red baseline at HEAD (do not chase):**
`TestExtractVar/multi_line` in `internal/prepare` (plan 20/22 owns it) and one
`go vet` finding at `internal/git/git.go:318` (self-assignment; plan 29 owns it).
If your measured baseline differs, record it and continue.

**Tier 1 — source-level greps (run these; both must print nothing):**
```bash
grep -rn 'func extractRepoFromURLs\|func extractOwnerRepo\|func ParseLegacyUpstream' --include='*.go' .
# expect: no output

grep -rn 'Path: "/" *+' --include='*.go' internal
# expect: no output
```

**Tier 2 — read-only manual. Needs `~/Developer/gentoo-overlay` + network. Safe to
run; a failure may be rate-limiting, so re-run before concluding. Skip and say so if
the overlay is absent.**
```bash
larry upstream --fresh --overlay ~/Developer/gentoo-overlay -v | grep nirius
# expect: a SourceHut source and a real version, not UNKNOWN.

larry upstream --fresh --overlay ~/Developer/gentoo-overlay -v | grep -i gitlab
# expect: rows labelled like "GitLab:gitlab.gnome.org/GNOME/geary" — one slash
# between host and path, capital G (that label comes from resolveSourceLabel at
# internal/upstream/upstream.go:863, not from RemoteId.String()).
```

**Tier 3 — none.** Nothing in this plan mutates state outside the repo.

---

## Out of scope

- Which release or tag a provider picks → plan 17.
- Rendering of the Source column → plan 19.
- Everything in `internal/importcmd` beyond the `writeMetadata` root-element /
  `<upstream>` wrapper change needed to keep the suite green → plan 28.

If you find an unrelated bug, write it in your final report; do not fix it.
