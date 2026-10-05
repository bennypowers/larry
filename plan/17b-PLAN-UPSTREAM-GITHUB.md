# 17b — GitHub provider: the 4-tier resolution cascade

**Why:** GitHub is the dominant provider in this overlay and it picks the wrong version.
The implementation queries the wrong endpoints, never inspects GitHub's own
`prerelease`/`draft` fields, throws away the tag date it fetches, and shells out to the
`git` CLI in violation of AGENTS.md. Plan 17a built the `Resolution` type and the
status derivation; this plan makes the provider produce them.

**Precondition:** 17a (`provider.Resolution`, `provider.ResolverWithTags`,
`upstream.StatusCheckTag`), and transitively 14 and 16.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent on this machine, this
plan's text is authoritative — do not guess at the script's behaviour.**
Functions: `fetch_github_version` (L1132–1220),
`fetch_github_latest_tag_by_date` (L1073–1118), `strip_tag_prefix` (L1120–1126),
`is_version_like` (L1128–1130).

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. Known-red baseline: exactly
> `TestExtractVar/multi_line` in `internal/prepare`, plus one `go vet` finding at
> `internal/git/git.go:318`. Do not chase either.

### Subprocess policy for this plan (resolves an AGENTS.md tension)

AGENTS.md says "native implementations, no subprocess" and lists `net/http` and
`go-git`. Read it precisely:

- **The `gh` subprocess IS sanctioned** for GitHub API access. AGENTS.md's own
  "External Tool Delegation Policy" lists `gh` as available, and `gh` is how
  `GITHUB_TOKEN` auth is handled natively. All four tiers below keep using `gh`.
- **The `git` CLI is NOT.** AGENTS.md: "`go-git` — remote resolution, tag enumeration,
  commit lookup (replaces `git` CLI)". The `exec.Command("git", "ls-remote", …)` at
  `internal/providers/github.go:173-180` is **the only subprocess this plan removes**.

---

## Precondition check — run these first

```bash
go doc ./internal/provider Resolution
go doc ./internal/provider ResolverWithTags
go doc ./internal/upstream Status
go doc ./internal/version ExtractDate
go doc ./internal/github Client
go doc ./internal/github StubRunner
go doc ./internal/github NewClientWithRunner
```

Expected (plan 17a):

```go
type Resolution struct {
    Version string
    Tag     string
    TagDate string
    ByDate  bool
}
type ResolverWithTags interface {
    UpstreamProvider
    FetchResolution(rid remoteid.RemoteId, allowPrereleases bool) (*Resolution, error)
}
```

and `go doc ./internal/upstream Status` lists `StatusCheckTag`.

Expected (plan 14): `func ExtractDate(s string) (string, bool)`, and
**`IsPrerelease()` must return `false` for `1.0_p1`** (plan 14 D3). Verify:

```bash
cat > internal/version/pre_check_test.go <<'EOF'
package version
import "testing"
func TestPlan17bPrecondition(t *testing.T) {
    v, err := Parse("1.0_p1")
    if err != nil { t.Fatal(err) }
    if v.IsPrerelease() { t.Fatal("IsPrerelease(1.0_p1) must be false — plan 14 has not landed") }
}
EOF
go test ./internal/version -run TestPlan17bPrecondition -count=1; rm internal/version/pre_check_test.go
```

Expected (already at HEAD, verified 2026-10-05):

```go
func NewClientWithRunner(r CmdRunner) *Client
func (c *Client) Gh(args ...string) ([]byte, error)   // runs exec.Command("gh", args...)
type StubRunner struct { Outputs map[string][]byte; Errors map[string]error }
func NewStubRunner() *StubRunner
```

**If any of these is missing, STOP and report.**

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D1 | `internal/github/github.go:91-121` | Tier 1 is `gh release list --limit 1 --json tagName,name` instead of `gh api repos/O/R/releases/latest`; the `prerelease`/`draft` fields are never requested, so prerelease detection is guessed from the tag string |
| D2 | `internal/github/github.go:124-140` | Tier 2 is `gh api repos/O/R/tags?per_page=1&sort=updated`. The list-tags endpoint has **no** `sort` parameter, so `.[0]` is an arbitrary tag — and `internal/providers/github.go:75-85` then takes `max(release, tag)`, letting a junk tag win |
| D3 | `internal/providers/github.go:248-307` | `fetchLatestTagByDate` parses `committedDate` / `tagger.date` into the struct and **returns only the tag name**; the date is discarded |
| D5 | `internal/providers/github.go:113` | error text `latest release %q is a prerelease` interpolates `bestTag`, which is always `""` on that path |
| D6 | `internal/providers/github.go:299-306` | the script's pre-release *tag-name* filter for the tag-by-date path (`alpha\|beta\|rc\|dev\|nightly\|canary`, case-insensitive, L1113) is absent |
| D7 | `internal/providers/github.go:172-180` | `exec.Command("git", "ls-remote", …)` — a `git` CLI subprocess, forbidden by AGENTS.md |

Field symptoms: `app-admin/cockpit` 367 → "latest 310.10";
`dev-libs/migraphx` 7.2.4 → "latest 10.0";
`sys-kernel/raspberrypi-kernel` → "latest 1.20230405".

---

## Step 0 (DO THIS FIRST) — make the provider testable

**Tests cannot use `httptest`.** The GitHub provider never issues an HTTP request; it
shells out to `gh`. Only tiers 1 and 2 currently go through `p.Client`
(`*github.Client`), which is injectable via the existing
`github.NewClientWithRunner(r CmdRunner)` + `github.StubRunner`
(`internal/github/testutil.go`). Tier 3 (`fetchLatestTagByRemote`, `github.go:172`) and
tier 4 (`fetchLatestTagByDate`, `github.go:248`) are **package-level functions that call
`exec.Command` directly — there is no seam at all.**

**Drop every mention of `httptest` for GitHub.** Instead:

1. Convert both package-level functions to methods on `*GitHubProvider`:
   `func (p *GitHubProvider) fetchLatestTagByRemote(owner, repo string) (string, error)`
   and
   `func (p *GitHubProvider) fetchLatestTagByDate(owner, repo string) (tag, date string, err error)`.
   Update the two call sites at `github.go:93` and `:96`.
2. Route the GraphQL call through `p.Client.Gh("api", "graphql", "-f", "query="+graphqlTagsQuery, "-f", "owner="+owner, "-f", "name="+repo)`
   so `StubRunner` intercepts it. Hoist the query string to a package-level
   `const graphqlTagsQuery = ...` (byte-identical to the current literal at
   `github.go:250-263`) so tests can build the stub key from the same constant.
   `StubRunner` keys on the **full argv**: `"gh " + strings.Join(cmd.Args[1:], " ")`
   (see `cmdArgs` in `internal/github/testutil.go`), so the test key is

   ```go
   key := "gh api graphql -f query=" + providers.GraphqlTagsQueryForTest +
          " -f owner=o -f name=r"
   ```

   Export the constant for tests via a small `internal/providers/testutil.go`
   (`const GraphqlTagsQueryForTest = graphqlTagsQuery`), or make the tests
   package-internal (`package providers`) and use the unexported name directly —
   **prefer package-internal tests**, `internal/providers/github_test.go` is already
   `package providers`.
3. Add a seam for tier 3:

   ```go
   type GitHubProvider struct {
       RateLimiter *ratelimit.RateLimiter
       Client      *github.Client
       // TagLister lists remote tag names for a clone URL. Defaults to
       // git.ListRemoteTags; tests substitute a closure.
       TagLister   func(url string) ([]string, error)
   }
   ```

   with `if p.TagLister == nil { p.TagLister = git.ListRemoteTags }` in the same lazy-init
   spot as `if p.Client == nil { p.Client = github.NewClient() }` (`github.go:52-54`).

---

## Target behaviour

Each tier is tried in order; the first version-like result wins.

### Tier 1 — official latest release

**One call, not the script's two.** The script makes two separate `gh api` calls
(L1142 for `.tag_name`, L1147 for `.prerelease`) and never checks `.draft` — its L1142
comment says the endpoint "handles draft filtering automatically". We deliberately
improve on that:

```go
out, err := p.Client.Gh("api", "repos/"+owner+"/"+repo+"/releases/latest")
```

**No `--jq`.** (The previous revision of this plan said
`--jq '.tag_name,.prerelease,.draft'`, which emits three bare scalars on three lines
with no parsing rule — that is unimplementable. It is removed.) Unmarshal:

```go
var rel struct {
    TagName    string `json:"tag_name"`
    Prerelease bool   `json:"prerelease"`
    Draft      bool   `json:"draft"`
}
```

- `Draft` true → discard. (Defence in depth: the endpoint is documented never to return
  drafts, but the field is free now that we parse the whole object.)
- `Prerelease` true and `!allowPrereleases` → discard.
- else `stripTagPrefix(rel.TagName)`.

Replace `(*github.Client).LatestRelease` (`internal/github/github.go:91-121`) with a new
method carrying these three fields:

```go
func (c *Client) LatestRelease(owner, repo string) (tag string, prerelease, draft bool, err error)
```

Update its other call sites (`grep -rn 'LatestRelease' --include=*.go .`) — note the
current signature returns `(tag, url string, err error)` and the `url` return is
unused by `internal/providers/github.go`.

### Tier 2 — newest release from the release list

Exactly the script's call (L1160–1166):

```
gh release list -R <owner>/<repo> --limit 100 --json tagName -q '.[0].tagName'
```

plus `--exclude-pre-releases` **iff** `!allowPrereleases`, inserted immediately after
`--limit 100`.

**Never request `isPrerelease`.** The previous revision wrote `--json tagName[,isPrerelease]`;
that bracket notation is not a `gh` syntax and has no defined meaning. Always
`--json tagName -q '.[0].tagName'`. Output is a bare string plus a newline; `TrimSpace`
it, and treat `""` / `null` as "no result".

Replace `(*github.Client).LatestTag` (`internal/github/github.go:124-140`) with:

```go
func (c *Client) LatestReleaseTag(owner, repo string, allowPrereleases bool) (string, error)
```

Delete the `tags?per_page=1&sort=updated` call entirely (**D2**).

### Reconcile tiers 1 and 2 (script L1174–1190)

If both are version-like, return the newer (`version.Compare`); else whichever one is
version-like; else neither and fall through to tier 3.

### Tier 3 — remote tags, via go-git

**There is NO `ListRemote` in `internal/git`.** The complete exported API today
(verified with `go doc -all ./internal/git`) is:

```
func DebugReadAuthor(repoPath string) (name, email string)
func ParseRemoteURL(url string) (owner, repo string, err error)
func ResolveOverlayDir() (string, error)
func Open(path string) (*Repo, error)
(*Repo): Commit, CommitByTag, CommitWithDir, GetRemote, HasRemote, LatestCommit,
         ListTagsSorted, RemoteURL, Remotes, ResolveTag, Tags, Worktree
```

go-git's remote-listing API is `(*git.Remote).List(*git.ListOptions)`. **Add** to
`internal/git`:

```go
// ListRemoteTags lists tag ref short names on a remote repository without cloning.
// Peeled refs (".../x^{}") are dropped; the unpeeled ref is always present alongside.
func ListRemoteTags(url string) ([]string, error) {
    remote := gogit.NewRemote(memory.NewStorage(), &config.RemoteConfig{
        Name: "origin",
        URLs: []string{url},
    })
    refs, err := remote.List(&gogit.ListOptions{})
    if err != nil {
        return nil, fmt.Errorf("list remote tags for %s: %w", url, err)
    }
    var tags []string
    for _, ref := range refs {
        if !ref.Name().IsTag() {
            continue
        }
        name := ref.Name().Short()
        if strings.HasSuffix(name, "^{}") {
            continue
        }
        tags = append(tags, name)
    }
    return tags, nil
}
```

Imports: `gogit "github.com/go-git/go-git/v5"`,
`"github.com/go-git/go-git/v5/config"`, `"github.com/go-git/go-git/v5/storage/memory"`.
(`github.com/go-git/go-git/v5 v5.19.2` is already in `go.mod`.)

On the `^{}` rule: go-git returns annotated-tag peels as **separate**
`refs/tags/<name>^{}` entries. **Drop any ref name ending in `^{}`; the unpeeled ref is
always present alongside it**, so nothing is lost. Do not try to strip the suffix and
dedupe.

Then in the provider: `p.TagLister("https://github.com/" + owner + "/" + repo + ".git")`,
keep tags whose `stripTagPrefix` result matches `^[0-9]`, sort with `internal/version`,
take the highest. Reuse the existing `sortAndLatestVersion` (`github.go:204-225`).
**Delete `exec.Command("git", …)` at `internal/providers/github.go:173-180`** and drop
the now-unused `os/exec` import if nothing else in the file needs it. (**D7**)

### Tier 4 — tag by commit date (GraphQL)

Keep the query verbatim (it already matches the script, L1090–1103):
`refs(refPrefix:"refs/tags/", orderBy:{field: TAG_COMMIT_DATE, direction: DESC}, first: 10)`.

For each node in order:

1. Skip if `regexp.MustCompile(`(?i)alpha|beta|rc|dev|nightly|canary`)` matches the raw
   tag name (script L1113). (**D6**)
2. `date, ok := version.ExtractDate(tagName)` — the tag name wins.
3. Else take the first non-empty of `committedDate`, `tagger.date`,
   `target.target.committedDate`; `date = strings.ReplaceAll(raw[:10], "-", "")`
   (guard `len(raw) >= 10`).
4. First node that produces a non-empty date → return `(tagName, date, nil)`. (**D3**)

The existing JSON struct at `github.go:277-292` already captures `committedDate` and
`tagger.date` but **not** `target.target.committedDate` — add that nested field.

### `FetchResolution` — the `ResolverWithTags` implementation

```go
func (p *GitHubProvider) FetchResolution(rid remoteid.RemoteId, allowPrereleases bool) (*provider.Resolution, error)
```

Tiers 1→2→reconcile→3 produce `&provider.Resolution{Version: v.Format()}`.
Only when all three yield nothing does tier 4 run, producing
`&provider.Resolution{Tag: tag, TagDate: date, ByDate: true}`.
Add `var _ provider.ResolverWithTags = (*GitHubProvider)(nil)` to the file.

**Keep `FetchLatestVersion` exactly as the public entry point it is today** (reimplement
its body as a thin wrapper over `FetchResolution` that returns an error when
`ByDate` is true), so `registry.Resolve` and the other five providers are untouched.

### D5 — the error message

`github.go:113` currently names `bestTag`, which is `""` on that path. Name the tag that
was actually rejected: `fmt.Errorf("github: latest tag %q is a prerelease", tag)`.

---

## Tests (write first — every one must fail before you implement)

`internal/providers/github_test.go`, `package providers`. **No `httptest`, no network.**
Every test builds `p := &GitHubProvider{Client: github.NewClientWithRunner(stub)}` with
`stub := github.NewStubRunner()`, and for tier-3 tests also sets `p.TagLister`.

Stub keys are the full argv, space-joined, starting with `gh`. Write a tiny helper:

```go
func key(args ...string) string { return "gh " + strings.Join(args, " ") }
```

Canonical keys used below:

```go
kRel  = key("api", "repos/o/r/releases/latest")
kList = key("release", "list", "-R", "o/r", "--limit", "100",
            "--exclude-pre-releases", "--json", "tagName", "-q", ".[0].tagName")
kListPre = key("release", "list", "-R", "o/r", "--limit", "100",
            "--json", "tagName", "-q", ".[0].tagName")
kGQL  = key("api", "graphql", "-f", "query="+graphqlTagsQuery, "-f", "owner=o", "-f", "name=r")
```

| Test | Stub inputs | Expected |
|---|---|---|
| `TestGitHub_Tier1_UsesReleasesLatest` (**D1**) | `kRel` → `{"tag_name":"v1.2.3","prerelease":false,"draft":false}` | `FetchResolution` → `Version == "1.2.3"`; and `stub` was asked for `kRel` (record keys in a slice on a wrapper runner and assert `kRel` is present) |
| `TestGitHub_Tier1_RejectsDraft` | `kRel` → `{"tag_name":"v9.9.9","prerelease":false,"draft":true}`; `kList` → `"v1.2.3\n"` | `Version == "1.2.3"` (tier 1 discarded) |
| `TestGitHub_Tier1_RejectsPrereleaseFlag` (**D1 regression**) | `kRel` → `{"tag_name":"v1.2.3","prerelease":true,"draft":false}` (version-like string, no prerelease marker in the text); `kList` → `"v1.0.0\n"`; `allowPrereleases=false` | `Version == "1.0.0"` |
| `TestGitHub_Tier1_AcceptsPrereleaseWhenAllowed` | same stub, `allowPrereleases=true` | `Version == "1.2.3"` |
| `TestGitHub_Tier2_ExcludesPrereleases` | `kList` and `kListPre` both registered with distinguishable outputs (`"2.0.0\n"` / `"3.0.0-rc1\n"`); `kRel` → error | `allowPrereleases=false` → `2.0.0`; `allowPrereleases=true` → the `kListPre` key was the one used |
| `TestGitHub_ReconcileTiers` | table: (1) `kRel`→`1.0.0`, `kList`→`1.2.0` ⇒ `1.2.0`; (2) `kRel`→`2.0.0`, `kList`→`1.9.0` ⇒ `2.0.0`; (3) `kRel`→`pios/1`, `kList`→`1.5.0` ⇒ `1.5.0` | as stated |
| `TestGitHub_NoSortParamOnTagsEndpoint` (**D2**) | recording runner; all tiers stubbed | **no** recorded argv contains the substring `tags?` or `sort=` |
| `TestGitHub_Tier3_LsRemoteFallback` | `kRel`, `kList` → errors; `p.TagLister = func(string) ([]string, error) { return []string{"v1.0","v10.0","v9.0"}, nil }` | `Version == "10.0"` (numeric, not lexical) |
| `TestGitHub_Tier3_DropsPeeledRefs` | `TagLister` returns `[]string{"v1.0","v1.0^{}","v2.0","v2.0^{}"}` | `Version == "2.0"`, and no `^{}` reaches `version.Parse` (assert by also including a bogus `v99.0^{}` with no unpeeled sibling and expecting `2.0`) |
| `TestGitHub_Tier4_ReturnsTagAndDate` (**D3**) | tiers 1–3 empty (`TagLister` returns `nil, errors.New("no tags")`); `kGQL` → one node `{"name":"firmware","target":{"committedDate":"2024-01-15T00:00:00Z"}}` | `ByDate == true`, `Tag == "firmware"`, `TagDate == "20240115"` |
| `TestGitHub_Tier4_SkipsPrereleaseTagNames` (**D6**) | two nodes: `v2.0-rc1` (committedDate `2026-01-01`), then `20240115-stable` (committedDate `2020-01-01`) | `Tag == "20240115-stable"` |
| `TestGitHub_Tier4_DatePrefersTagName` | one node `{"name":"firmware-20260824","target":{"committedDate":"2020-01-01T00:00:00Z"}}` | `TagDate == "20260824"` |
| `TestGitHub_PrereleaseErrorMessage` (**D5**) | `kRel`→`{"tag_name":"v1.0.0-rc1","prerelease":false,"draft":false}`, `kList`→`""`, `TagLister`→`[]string{"v1.0.0-rc1"}`, `kGQL`→error, `allowPrereleases=false` | `FetchLatestVersion` returns an error whose message contains `"1.0.0-rc1"` and **not** `""`-quoted emptiness |

Recording runner (needed by two tests) — write it in the test file:

```go
type recordingRunner struct {
    inner *github.StubRunner
    keys  []string
}
func (r *recordingRunner) Run(cmd *exec.Cmd) error { r.keys = append(r.keys, strings.Join(cmd.Args, " ")); return r.inner.Run(cmd) }
func (r *recordingRunner) Output(cmd *exec.Cmd) ([]byte, error) { r.keys = append(r.keys, strings.Join(cmd.Args, " ")); return r.inner.Output(cmd) }
```

### `internal/git/git_test.go`

- `TestListRemoteTags_DropsPeels` — **unit-testable without network** only if you have
  a local bare repo. Create one in `t.TempDir()`: `go-git`'s
  `gogit.PlainInit(dir, true)` + a commit + a lightweight tag `v1.0` and an annotated
  tag `v2.0`, then `ListRemoteTags(dir)` (a filesystem path is a valid remote URL for
  go-git). Expect exactly `["v1.0", "v2.0"]` as a set, with no `^{}` entry.
  If annotated-tag creation proves awkward, assert only on the lightweight case and say
  so in your report — do not reach for the network.

### `internal/github/github_test.go`

- `TestLatestRelease_ParsesPrereleaseAndDraft` — stub `kRel`, assert the three returned
  values for `{"tag_name":"v1","prerelease":true,"draft":true}`.
- `TestLatestReleaseTag_ArgvExcludesPrereleases` — recording runner; assert
  `--exclude-pre-releases` is in argv when `allowPrereleases == false` and absent when
  `true`, and that `--json tagName -q .[0].tagName` is present in both.

---

## Implementation steps

Every step has a test above; write the test first and watch it fail.

0. **Step 0 above** — methods, `graphqlTagsQuery` const, `TagLister` field. No behaviour
   change yet; `go build ./...` must stay green.
1. Add `git.ListRemoteTags` to `internal/git`. (**D7** prerequisite)
2. Rewrite `(*github.Client).LatestRelease` → tier 1 one-call form. (**D1**)
3. Replace `(*github.Client).LatestTag` with `LatestReleaseTag`; delete the
   `tags?per_page=1&sort=updated` call. (**D2**)
4. Rewrite `fetchLatestTagByRemote` onto `p.TagLister`; delete the `exec.Command("git", …)`.
   (**D7**)
5. Make `fetchLatestTagByDate` return `(tag, date, err)`; add the nested
   `target.target.committedDate` field and the prerelease tag-name filter.
   (**D3, D6**)
6. Implement `FetchResolution` + the `var _ provider.ResolverWithTags` assertion;
   reimplement `FetchLatestVersion` as a wrapper over it.
7. Fix the error message at `github.go:113`. (**D5**)
8. Run the suites; fix fallout.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**. Read that section before running
> anything.

### Tier 1 — automated, required. Run these. This is the real gate.

```bash
go build ./...
go vet ./...                 # internal/git/git.go:318 is the known baseline finding
go test ./... -count=1       # no NEW failures vs. your recorded baseline
golangci-lint run ./...
```

Targeted:

```bash
go test ./internal/providers/... ./internal/github/... ./internal/git/... \
        ./internal/upstream/... -count=1
```

Grep assertions (all must print nothing):

```bash
grep -rn 'exec.Command("git"' --include=*.go ./internal ./cmd
grep -rn 'sort=updated\|per_page=1' --include=*.go ./internal
grep -rn 'httptest' --include=*.go ./internal/providers/github_test.go
```

And these must print something:

```bash
grep -n 'func ListRemoteTags' internal/git/git.go
grep -n 'provider.ResolverWithTags = (\*GitHubProvider)' internal/providers/github.go
```

### Tier 2 — read-only manual. Safe to run, but **MANUAL** — report results, do not
treat a failure here as blocking.

Everything below needs `~/Developer/gentoo-overlay`, live GitHub, and a working
`GITHUB_TOKEN`. A failure may be rate-limiting rather than a defect; re-run before
concluding. **The overlay and upstream repos change over time, so none of these is a
deterministic assertion** — they are sanity reads for your report.

```bash
larry upstream --fresh --overlay ~/Developer/gentoo-overlay -v
```

- `app-admin/cockpit` current `367` shows a latest ≥ 367 (or UP-TO-DATE), not `310.10`.
- `dev-libs/migraphx` does not report `10.0`.
- `sys-kernel/raspberrypi-kernel` does not report `1.20230405`; expect `NEWER-TAG` or
  `UP-TO-DATE` via the date path.
- Whether *any* row renders `NEWER-TAG` or `CHECK-TAG` depends on what upstreams have
  tagged this week. **Do not treat its absence as a failure.** If you see one, say so;
  the deterministic proof that tier 4 is live is
  `TestGitHub_Tier4_ReturnsTagAndDate` in Tier 1.

### Tier 3 — DO NOT RUN.

Nothing in this plan is Tier 3. Do not create, edit or delete a GitHub release.

---

## Out of scope

`Resolution`, `ResolverWithTags`, `StatusCheckTag` and `deriveTagStatus` — all plan 17a;
if they are absent, stop. Non-GitHub providers (plan 15 fixed their extraction; their
fetch paths are already single-tier and match the script). Dependency recursion
(plan 18). Table rendering and hyperlinks (plan 19). Do not fix unrelated bugs you
notice; report them.
