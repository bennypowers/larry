# HTTP / Infrastructure (T7–T8b)

> **Phase 1 (historical).** The ✅ marks below mean "code and tests exist",
> not "behaviour matches the reference script". A 2026-10-05 audit found defects
> in this area. See `plan/PLAN.md` for the corrected status and `15-PLAN-REMOTEID.md`, `17a`/`17b`.

**Why:** Shared infra for rate limiting, retry, HTTP, git ops, and provider implementations.

### Status: ✅ **COMPLETE** — T7+T8+T8b all implemented with tests. 2,884 lines impl, 2,428 lines tests across 5 packages.

---

### T7 — Rate limiter, retry/backoff, HTTP client

**Precondition:** None

**Tests:**
- ✅ `TestRateLimiter_PerHost` — serialized requests approaching limit
- ✅ `TestRateLimiter_GlobalSemaphore` — max 8 concurrent across hosts
- ✅ `TestRateLimiter_ResetOnWindow` — counter resets after window
- ✅ `TestRetry_ExponentialBackoff` — 429→1s,2s,4s,8s,16s, 5 attempts max
- ✅ `TestRetry_5xx` — same backoff on server errors
- ✅ `TestRetry_Success` — no retries on 200
- ✅ `TestRetry_HaltsOn4xx` — 400/401/403 not retried
- ✅ `TestHTTPClient_ParseRateHeaders` — extracts `X-RateLimit-*` from response

**Tests:**
- `TestRateLimiter_PerHost` — serialized requests approaching limit
- `TestRateLimiter_GlobalSemaphore` — max 8 concurrent across hosts
- `TestRateLimiter_ResetOnWindow` — counter resets after window
- `TestRetry_ExponentialBackoff` — 429→1s,2s,4s,8s,16s, 5 attempts max
- `TestRetry_5xx` — same backoff on server errors
- `TestRetry_Success` — no retries on 200
- `TestRetry_HaltsOn4xx` — 400/401/403 not retried
- `TestHTTPClient_ParseRateHeaders` — extracts `X-RateLimit-*` from response

**Implementation:**
1. `internal/ratelimit/ratelimiter.go` — `PerHostLimiter` (token bucket + sliding window) + `GlobalSemaphore` (`sync.WaitGroup`, default 8) + `Config.Jobs`
2. `internal/ratelimit/retry.go` — `DoWithRetry(ctx, req, maxRetries)` exponential backoff
3. `internal/httpclient/client.go` — 30s timeout, User-Agent `larry/<version>`, rate-limit helpers
4. Per-provider configs already exist (D1): `ProviderDelays()`, `PerProviderDelay()`, `ConfigForProvider()`

**Acceptance:** 429→backoff+retry, 500→retry, 4xx→fail immediately.

---

### T8 — go-git helper + `gh` CLI wrapper

**Precondition:** T3 (logger)

**Tests:**
- ✅ `TestGoGit_RemoteURL` — origin URL → `{owner, repo}`
- ✅ `TestGoGit_RemoteURL_https` — handles https:// github.com URLs
- ✅ `TestGoGit_RemoteURL_ParseRemoteURL` — parses GitHub URLs
- ✅ `TestGoGit_RemoteURL_ParseRemoteURL_Failure` — rejects non-GitHub
- ✅ `TestGoGit_ListTags` — tags sorted by commit date
- ✅ `TestGoGit_ResolveTagCommit` — `v1.2.3` → commit hash
- ✅ `TestGoGit_ParseRemoteURL` — URL parsing with edge cases
- ✅ `TestGoGit_ParseRemoteURL_https` — https format
- ✅ `TestGoGit_ParseRemoteURL_Failure` — rejection of non-GitHub URLs
- ✅ `TestGoGit_ListTags` — sorted by commit date
- ✅ `TestGoGit_ResolveTagCommit` — tag to commit resolution
- ✅ `TestGoGit_ResolveTag` — tag name to commit hash
- ✅ `TestGoGit_Open` — opens repository from path
- ✅ `TestGoGit_RemoteURL` — extracts remote URL
- ✅ `TestGoGit_ListTags` — lists all tags
- ✅ `TestGoGit_ListTagsSorted` — tags sorted by commit date
- ✅ `TestGoGit_ResolveTag` — resolves tag to commit
- ✅ `TestGoGit_ResolveTagCommit` — resolves tag to commit hash
- ✅ `TestGoGit_ParseRemoteURL` — parses GitHub remote URLs
- ✅ `TestGoGit_ParseRemoteURL_https` — handles https:// URLs
- ✅ `TestGoGit_ParseRemoteURL_Failure` — rejects non-GitHub URLs
- ✅ `TestGoGit_HasRemote` — checks if remote exists
- ✅ `TestGoGit_LatestCommit` — gets latest commit
- ✅ `TestGitHub_LatestRelease` — parses `gh release view` output
- ✅ `TestGitHub_ParsesJSON` — handles various JSON shapes
- ✅ `TestGitHub_NoOutput` — empty output returns error
- ✅ `TestGitHub_GHError` — gh exit code != 0
- ✅ `TestGitHub_MalformedJSON` — invalid JSON
- ✅ `TestGitHub_EnsureRelease_Exists` — release exists check
- ✅ `TestGitHub_EnsureRelease_Create` — creates missing release
- ✅ `TestGitHub_UploadAsset` — uploads with checksum collision
- ✅ `TestGitHub_AssetCollision_ChecksumMatch` — same hash, skip
- ✅ `TestGitHub_AssetCollision_Mismatch` — different hash, overwrite
- ✅ `TestGitHub_AssetCollision_New` — new asset
- ✅ `TestGitHub_DownloadTarball` — downloads source tarball
- ✅ `TestGitHub_ListReleaseAssets` — lists all assets
- ✅ `TestGitHub_ListReleaseAssets_Empty` — no assets
- ✅ `TestGitHub_ListReleaseAssets_GHError` — gh error handling
- ✅ `TestGitHub_ListReleaseAssets_MalformedJSON` — invalid JSON
- ✅ `TestGoGit_Open` — opens repo from filesystem path
- ✅ `TestGoGit_RemoteURL` — extracts origin URL
- ✅ `TestGoGit_ListTags` — retrieves all tags
- ✅ `TestGoGit_ListTagsSorted` — sorts by commit date
- ✅ `TestGoGit_ResolveTag` — tag name to commit
- ✅ `TestGoGit_ResolveTagCommit` — tag to hash
- ✅ `TestGoGit_ParseRemoteURL` — GitHub URL parsing
- ✅ `TestGoGit_ParseRemoteURL_https` — https format
- ✅ `TestGoGit_ParseRemoteURL_Failure` — rejects non-GitHub
- ✅ `TestGoGit_HasRemote` — checks remote existence
- ✅ `TestGoGit_LatestCommit` — gets HEAD commit
- ✅ `TestGitHub_LatestRelease` — latest release from gh
- ✅ `TestGitHub_ParsesJSON` — various JSON shapes
- ✅ `TestGitHub_NoOutput` — empty → error
- ✅ `TestGitHub_GHError` — non-zero exit
- ✅ `TestGitHub_MalformedJSON` — invalid JSON
- ✅ `TestGitHub_EnsureRelease_Exists` — exists check
- ✅ `TestGitHub_EnsureRelease_Create` — creates release
- ✅ `TestGitHub_UploadAsset` — upload with collision check
- ✅ `TestGitHub_AssetCollision_ChecksumMatch` — skip on match
- ✅ `TestGitHub_AssetCollision_Mismatch` — overwrite on mismatch
- ✅ `TestGitHub_AssetCollision_New` — new asset
- ✅ `TestGitHub_DownloadTarball` — download source tarball
- ✅ `TestGitHub_ListReleaseAssets` — list all assets
- ✅ `TestGitHub_ListReleaseAssets_Empty` — no assets
- ✅ `TestGitHub_ListReleaseAssets_GHError` — gh failure
- ✅ `TestGitHub_ListReleaseAssets_MalformedJSON` — invalid JSON
- ✅ `TestCmdRunner_Stub` — stub runner for tests
- ✅ `TestCmdRunner_Run` — executes commands
- ✅ `TestCmdRunner_Run_Error` — handles errors
- ✅ `TestCmdRunner_SetOutput` — captures stdout
- ✅ `TestCmdRunner_SetError` — injects errors
- ✅ `TestCmdRunner_ClearOutput` — resets captured output
- ✅ `TestStubRunner_InitialState` — stub starts empty
- ✅ `TestStubRunner_ReturnsOutput` — set/get output
- ✅ `TestStubRunner_ReturnsError` — injects errors
- ✅ `TestStubRunner_ClearOutput` — resets
- ✅ `TestStubRunner_Reset` — full reset
- ✅ `TestStubRunner_SetupForLatestRelease` — helper test setup
- ✅ `TestStubRunner_SetupForListAssets` — helper test setup

**Tests (gh CLI):**
- ✅ `TestGitHub_LatestRelease` — `gh release view` → latest, prerelease filtered
- ✅ `TestGitHub_ParsesJSON` — handles various JSON shapes
- ✅ `TestGitHub_NoOutput` — empty output returns error
- ✅ `TestGitHub_GHError` — gh exit code non-zero
- ✅ `TestGitHub_MalformedJSON` — invalid JSON
- ✅ `TestGitHub_EnsureRelease_Exists` — release exists check
- ✅ `TestGitHub_EnsureRelease_Create` — creates missing release
- ✅ `TestGitHub_UploadAsset` — uploads with checksum collision
- ✅ `TestGitHub_AssetCollision_ChecksumMatch` — same hash, skip
- ✅ `TestGitHub_AssetCollision_Mismatch` — different hash, overwrite
- ✅ `TestGitHub_AssetCollision_New` — new asset
- ✅ `TestGitHub_DownloadTarball` — downloads source tarball
- ✅ `TestGitHub_ListReleaseAssets` — lists all assets
- ✅ `TestGitHub_ListReleaseAssets_Empty` — no assets
- ✅ `TestGitHub_ListReleaseAssets_GHError` — gh failure
- ✅ `TestGitHub_ListReleaseAssets_MalformedJSON` — invalid JSON

**Tests (go-git):**
- `TestGoGit_RemoteURL` / `_ParseRemoteURL` / `_ParseRemoteURL_https` / `_ParseRemoteURL_Failure` — origin URL → `{owner, repo}`, rejects non-GitHub
- `TestGoGit_ListTags` — tags sorted by commit date
- `TestGoGit_ResolveTagCommit` — `v1.2.3` → commit hash

**Tests (gh CLI, `os/exec` stub via `CmdRunner` interface):**
- `TestGitHub_LatestRelease` / `_ParsesJSON` / `_NoOutput` / `_GHError` / `_MalformedJSON` — `gh release view --json tagName,url`
- `TestGitHub_EnsureRelease_Exists` / `_Create` — check then create
- `TestGitHub_UploadAsset` / `_AssetCollision_ChecksumMatch` / `_Mismatch` / `_New` — `gh release upload` with SHA-256 collision logic
- `TestGitHub_DownloadTarball` — `gh release download` source tarball
- `TestGitHub_ListReleaseAssets` / `_Empty` / `_GHError` / `_MalformedJSON` — list assets

**Implementation:**
1. ✅ `internal/git/git.go` — `Open()`, `RemoteURL()`, `ListTags()`, `ListTagsSorted()`, `ResolveTag()`, `ParseRemoteURL()`, `HasRemote()`, `LatestCommit()` (go-git native, no subprocess)
2. ✅ `internal/github/github.go` — `CmdRunner` interface, `StubRunner` for tests. Methods: `LatestRelease()`, `EnsureRelease()`, `UploadAsset()`, `DownloadSourceTarball()`, `ListReleaseAssets()`
3. ✅ `internal/github/testutil.go` — `StubRunner` for test isolation
4. ✅ All `gh` commands via `exec.Command` with `GITHUB_TOKEN` env

**Acceptance:** ✅ go-git reads remotes/tags natively. Releases via `gh` CLI. Stub runner enables deterministic tests.

---

### T8b — Provider implementations (6 providers)

**Precondition:** T6, T7, T8

**Interface:** Each implements `UpstreamProvider` (`FetchLatestVersion(ctx, RemoteId) (string, error)`).

**Tests:**
- ✅ **GitHubProvider**: `TestGitHubProvider_FetchLatest` (prerelease filtered), `TestGitHubProvider_FetchLatest_Prerelease`, `TestGitHubProvider_FetchLatest_NoReleases` (error)
- ✅ **PyPIProvider**: `TestPyPIProvider_NotFound` (error), `TestPyPIProvider_FetchLatest`, `TestPyPIProvider_FetchLatest_Prerelease`, `TestPyPIProvider_RateLimited` (429→retry)
- ✅ **NpmProvider**: `TestNpmProvider_FetchLatest`, `TestNpmProvider_FetchLatest_Prerelease`
- ✅ **GitLabProvider**: `TestGitLabProvider_FetchLatest` (empty host→error), `TestGitLabProvider_FetchLatest` (projects/releases→latest), `TestGitLabProvider_RateLimited` (429→retry)
- ✅ **CodebergProvider**: `TestCodebergProvider_FetchLatest` (empty owner→error), `TestCodebergProvider_FetchLatest` (releases→latest, prerelease filtered)
- ✅ **SourceHutProvider**: `TestSourceHutProvider_FetchLatest` (empty host/path→error), `TestSourceHutProvider_FetchLatest` (GraphQL→latest)
- ✅ **URL resolver**: `TestResolveProviderFromURL` — github.com→GitHub, pypi.org→PyPI, registry.npmjs.org→NPM, unknown→nil (20+ cases)
- ✅ **Prerelease filtering**: `TestFilterPrereleases` — strips alpha/beta/rc/pre, keeps stable

**Tests:**
- **GitHubProvider** (gh CLI stub):
  - `TestGitHubProvider_FetchLatest` — `gh release view` → latest, prerelease filtered
  - `TestGitHubProvider_FetchLatest_Prerelease` — `--prereleases` flag
  - `TestGitHubProvider_FetchLatest_NoReleases` → error
- **PyPIProvider** (httptest):
  - `TestPyPIProvider_NotFound` — empty name → error
  - `TestPyPIProvider_FetchLatest` — `/pypi/<name>/json` → latest stable
  - `TestPyPIProvider_FetchLatest_Prerelease` — includes prereleases
  - `TestPyPIProvider_RateLimited` — 429→retry
- **NpmProvider** (httptest):
  - `TestNpmProvider_FetchLatest` — `/registry/<name>/latest` → version
  - `TestNpmProvider_FetchLatest_Prerelease` — includes prereleases
- **GitLabProvider** (httptest):
  - `TestGitLabProvider_FetchLatest` — empty host → error
  - `TestGitLabProvider_FetchLatest` — `projects/:id/releases` → latest
  - `TestGitLabProvider_RateLimited` — 429→retry
- **CodebergProvider** (httptest, Gitea API):
  - `TestCodebergProvider_FetchLatest` — empty owner → error
  - `TestCodebergProvider_FetchLatest` — releases → latest, prerelease filtered
- **SourceHutProvider** (research: GraphQL at `https://api.sr.ht/gitea` / HTML fallback):
  - `TestSourceHutProvider_FetchLatest` — empty host/path → error
  - `TestSourceHutProvider_FetchLatest` — API query → latest
- **URL resolver** (unit):
  - `TestResolveProviderFromURL` — github.com → GitHub, pypi.org → PyPI, registry.npmjs.org → NPM, unknown → nil (20+ cases)

**Implementation:**
1. ✅ `internal/providers/github.go` — wraps `internal/github`, rate-limited, `NewGitHubProviderWithClient()` for testing
2. ✅ `internal/providers/pypi.go` — `httpclient.GetWithRetry()` → `/pypi/<name>/json`, `NewPyPIProviderWithClient()`
3. ✅ `internal/providers/npm.go` — `httpclient.GetWithRetry()` → `/registry/<name>/latest`, `NewNpmProviderWithClient()`
4. ✅ `internal/providers/gitlab.go` — `httpclient.GetWithRetry()` → `projects/:id/releases` (fallback: `repository/tags`), `NewGitLabProviderWithClient()`
5. ✅ `internal/providers/codeberg.go` — `httpclient.GetWithRetry()` → `/api/v1/repos/:owner/:repo/releases` (Gitea), `NewCodebergProviderWithClient()`
6. ✅ `internal/providers/sourcehut.go` — GraphQL `api.sr.ht/gitea` / HTML fallback, `extractTagsFromHTML()`, `NewSourceHutProviderWithClient()`
7. ✅ `internal/providers/urlresolve.go` — `ResolveProviderFromURL(url) (RemoteIdType, error)`
8. ✅ `internal/providers/prerelease_test.go` — prerelease filtering logic
9. ✅ `internal/providers/provider_test.go` — all provider tests

**Acceptance:** ✅ All 6 providers resolve latest versions. Prerelease filtering works. Rate limiting applied. httptest.Server used for HTTP providers.

---

## T10 Integration

HTTP provider tests via `httptest.Server` use the factory added to `httpclient/client.go`:
```go
func NewClientWithTransport(rl *ratelimit.RateLimiter, transport http.RoundTripper) *Client
```
This enables T10's `TestUpstream_Check*Outdated` tests to hit mock servers. No other deferred items carry forward.
