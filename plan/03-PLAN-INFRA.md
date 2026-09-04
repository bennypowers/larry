# HTTP / Infrastructure Tasks (T7–T8)

**What this covers:** Rate limiter, retry/backoff, HTTP client, go-git helpers, GitHub API.
**Why third:** Shared infrastructure that the domain types and commands depend on.

---

### T7 — Rate limiter, retry/backoff, HTTP client wrapper

**Precondition:** None (standalone infra)

**Scope note:** Rate limiter + retry/backoff apply **only** to direct-HTTP providers (PyPI, npm, GitLab, Codeberg, SourceHut). GitHub goes through the `gh` CLI (T8), which handles its own rate limiting — it is out of scope here.

**Tests:**
- `TestRateLimiter_PerHost` — same host, requests serialized when approaching limit
- `TestRateLimiter_GlobalSemaphore` — max 8 concurrent across all hosts
- `TestRateLimiter_ResetOnWindow` — counter resets after time window
- `TestRetry_ExponentialBackoff` — 429 triggers 1s, 2s, 4s, 8s, 16s delays, max 5 attempts
- `TestRetry_5xx` — same backoff on server errors
- `TestRetry_Success` — no retries on 200
- `TestRetry_HaltsOn4xx` — 400/401/403 not retried
- `TestHTTPClient_ParseRateHeaders` — extracts `X-RateLimit-Remaining`, `X-RateLimit-Reset` from response

**Note:** `TestHTTPClient_GraphQLHeaders` has been removed. There are no raw GitHub GraphQL calls via `net/http` anymore — all GitHub interaction goes through the `gh` CLI (T8).

**Implementation:**
1. `internal/ratelimit/ratelimiter.go`:
   - `PerHostLimiter` — token bucket + sliding window per host
   - `GlobalSemaphore` — `sync.WaitGroup`-based concurrency cap (default 8)
   - Configurable via `Config.Jobs`
2. `internal/ratelimit/retry.go` — `DoWithRetry(ctx, req, maxRetries)` with exponential backoff
3. `internal/httpclient/client.go` — shared HTTP client with:
   - Timeout (30s per request)
   - User-Agent: `larry/<version>`
   - Automatic retry wrapper integration
   - Rate-limit header extraction helpers
4. Per-provider rate limit configs (GitLab, PyPI, npm, Codeberg, SourceHut defaults)

**Acceptance:** Under load, concurrent requests are throttled. 429 responses trigger backoff and retry. 500s trigger retry. 4xx fails immediately.

---

### T8 — go-git helper + `gh` CLI wrapper for GitHub

**Precondition:** T3 complete

**Design note:** Drop the native `internal/github/github.go` HTTP client entirely — no `httpclient`/`net/http` calls to the GitHub REST API. Replace it with a thin wrapper around the `gh` CLI (subprocess via `os/exec`) for: `LatestRelease`, checking/creating a release, uploading assets, downloading tarballs. Keep `go-git` for what it's actually good for: reading the local `origin` remote URL, listing tags, resolving tag→commit.

**Auth note:** `gh` reads `GITHUB_TOKEN` from the environment directly — no need for `gh auth login`. This is explicit: "native auth" and "use the `gh` CLI" are not in tension. The token is passed through the environment to the subprocess.

**Tests (go-git — native):**
- `TestGoGit_RemoteURL` — opens local repo, reads `origin` remote URL
- `TestGoGit_ParseRemoteURL` — `git@github.com:owner/repo.git` → `{Owner, Repo}`
- `TestGoGit_ParseRemoteURL_https` — `https://github.com/owner/repo.git` → `{Owner, Repo}`
- `TestGoGit_ParseRemoteURL_Failure` — non-GitHub URL returns error
- `TestGoGit_ListTags` — enumerates all tags sorted by commit date
- `TestGoGit_ResolveTagCommit` — `v1.2.3` → commit hash

**Tests (gh CLI — subprocess stub mocking):**
- `TestGitHub_LatestRelease` — `gh release view --json tagName,url` → tag + URL (stub `os/exec`)
- `TestGitHub_UploadAsset` — `gh release upload` handles existing asset (skip/clobber) (stub `os/exec`)
- `TestGitHub_AssetCollision_ChecksumMatch` — `gh release view --json assets` returns existing with same SHA-256 → skip
- `TestGitHub_AssetCollision_ChecksumMismatch` — existing asset with different SHA-256 → upload with `--clobber` (stub `os/exec`)
- `TestGitHub_AssetCollision_New` — no existing asset → upload (stub `os/exec`)
- `TestGitHub_DownloadTarball` — `gh release download` source tarball from `v${PV}` tag (stub `os/exec`)

All `os/exec` stub mocking follows the same pattern as `portageq`/`pkgdev`/`podman` — wrap `exec.Command` in an interface so it can be replaced in tests.

**Implementation:**
1. `internal/git/git.go`:
   ```go
   type GitRepo struct { /* internal */ }
   func OpenRepo(path string) (*GitRepo, error)
   ```
   Using `github.com/go-git/go-git/v5`:
   - `GetRemoteURL(repo *GitRepo, name string) (string, error)` — reads remote from repo config
   - `ListTags(repo *GitRepo) ([]string, error)` — enumerates refs/tags, sorted
   - `ResolveTag(repo *GitRepo, tag string) (hash plumbing.Hash, err error)` — tag → commit
   - `ParseRemoteURL(url string) (owner, repo string, err error)` — regex on known formats
2. `internal/github/github.go` — **gh CLI wrapper** (no raw HTTP):
   - `LatestRelease(owner, repo string) (tag, URL string, err error)` → `gh release view --json tagName,url`
   - `EnsureRelease(owner, repo, tag string) error` → `gh release view` checks existence, `gh release create` if missing
   - `UploadAsset(owner, repo, tag, filePath, name string) (err error)` → `gh release upload` with `--clobber` logic
   - `DownloadSourceTarball(owner, repo, tag string, dest string) (path, err error)` → `gh release download`
   - `ListReleaseAssets(owner, repo, tag string) ([]Asset, error)` → `gh release view --json assets`, parse JSON
3. All `gh` commands invoked via `exec.Command` with `GITHUB_TOKEN` in the env. No `httpclient` involvement.
4. Auth: `gh` reads `GITHUB_TOKEN` from the env directly — pass it through when spawning subprocesses.

**Acceptance:** Remote URLs parsed from local repo via go-git. Tags enumerated natively. GitHub releases checked/created/uploaded via `gh` CLI. All GitHub tests use `os/exec` stub mocking.

---

### T8b — Provider implementations

**Precondition:** T6, T7, T8 complete (registry, rate limiter, go-git + gh wrapper)

**What this covers:** The six concrete provider implementations: `providers/{github,gitlab,pypi,npm,codeberg,sourcehut}.go`. Currently T10 consumes `FetchLatestVersion` as if it already works, but nothing builds or tests the providers themselves.

**Tests (per provider, via `httptest.Server` or `os/exec` stub as appropriate):**

- **GitHubProvider** — uses `gh` CLI wrapper (T8), not HTTP client
  - `TestGitHubProvider_FetchLatest` — `gh release view` → latest version, prerelease filtered
  - `TestGitHubProvider_FetchLatest_Prerelease` — `--prereleases` flag includes prereleases
  - `TestGitHubProvider_FetchLatest_NoReleases` — no releases → error with clear message

- **PyPIProvider** — `httptest.Server` mocking
  - `TestPyPIProvider_FetchLatest` — `/pypi/<name>/json` → latest stable
  - `TestPyPIProvider_FetchLatest_Prerelease` — includes prereleases when requested
  - `TestPyPIProvider_NotFound` — 404 → error
  - `TestPyPIProvider_RateLimited` — 429 → retry via T7 rate limiter

- **NpmProvider** — `httptest.Server` mocking
  - `TestNpmProvider_FetchLatest` — registry endpoint → latest version
  - `TestNpmProvider_FetchLatest_Prerelease` — includes prereleases
  - `TestNpmProvider_NotFound` — 404 → error

- **GitLabProvider** — `httptest.Server` mocking
  - `TestGitLabProvider_FetchLatest` — projects/:id/releases → latest
  - `TestGitLabProvider_RateLimited` — 429 → retry

- **CodebergProvider** — `httptest.Server` mocking (Codeberg is a Gitea fork)
  - `TestCodebergProvider_FetchLatest` — releases endpoint → latest

- **SourceHutProvider** — research task
  - `TestSourceHutProvider_Research` — document which API approach works (git-refs-via-HTTP, GraphQL, or tag enumeration)
  - `TestSourceHutProvider_FetchLatest` — works once API strategy is determined
  - ⚠️ **Known unknown:** SourceHut does not have a conventional "latest release" REST endpoint. Options:
    - `git-refs-via-protocol`: `https://git.sr.ht/~user/project/refs` → parse HTML/JSON for latest tag
    - GraphQL API at `https://api.sr.ht`: query `refs` with `type: tag`, sort by date
    - Fallback: use go-git to clone/fetch tags locally (slow, avoid in tests)
  - Test strategy: once API is determined, use `httptest.Server` to mock the response

**Additional test — URL-pattern resolution helper (unit-level only):**
- `TestResolveProviderFromURL_GitHub` — `github.com/owner/repo` → `RemoteIdTypeGitHub`
- `TestResolveProviderFromURL_PyPI` — `pypi.org/project/foo` or `registry.npmjs.org` → appropriate type
- `TestResolveProviderFromURL_Unknown` — unrecognized URL → nil

**Ownership split for SRC_URI fallback:**
- **T8b owns** only the unit-level helper: `ResolveProviderFromURL(url string) (RemoteIdType, error)` — a pure function that matches URL patterns to provider types. This is tested in T8b as shown above.
- **T10 owns** the integration: when `RemoteId` is absent from metadata.xml, the upstream command calls `ResolveProviderFromURL` and proceeds. `TestUpstream_NoRemoteId` in T10 exercises the full end-to-end path (metadata parsing → fallback call → provider resolution → version fetch).
- No duplicate implementation: T8b's `ResolveProviderFromURL` is a single pure function; T10 merely calls it in the right place in its dispatch logic.

**Implementation:**
1. `internal/providers/github.go` — wraps `internal/github` (gh CLI), applies rate limiter
2. `internal/providers/pypi.go` — `httptest.Server`-mockable, rate-limited HTTP
3. `internal/providers/npm.go` — `httptest.Server`-mockable, rate-limited HTTP
4. `internal/providers/gitlab.go` — `httptest.Server`-mockable, rate-limited HTTP
5. `internal/providers/codeberg.go` — `httptest.Server`-mockable, rate-limited HTTP
6. `internal/providers/sourcehut.go` — implement based on research outcome
7. `internal/providers/urlresolve.go` — `ResolveProviderFromURL(url string) (RemoteIdType, error)` — pure URL-pattern matching function (tested in T8b above)
8. Each provider implements `UpstreamProvider` interface from T6

**Acceptance:** All six providers resolve to correct latest versions. Prerelease filtering works. Rate limiter applied for HTTP providers. SourceHut has a documented and tested API strategy. `ResolveProviderFromURL` returns correct type for known URL patterns (integration tested in T10).

