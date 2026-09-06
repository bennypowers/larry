# HTTP / Infrastructure (T7–T8b)

**Why:** Shared infra for rate limiting, retry, HTTP, git ops, and provider implementations.

---

### T7 — Rate limiter, retry/backoff, HTTP client

**Precondition:** None

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

### T8 — go-git helper + `gh` CLI wrapper (replaces native HTTP client)

**Precondition:** T3 (logger)

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
1. `internal/git/git.go` — `Open()`, `RemoteURL()`, `Tags()`, `ListTagsSorted()`, `ResolveTag()`, `CommitByTag()`, `ParseRemoteURL()`, `HasRemote()`, `LatestCommit()`
2. `internal/github/github.go` — `CmdRunner` interface (abstract `exec.Command`), `StubRunner` for external tests. Methods: `LatestRelease()`, `EnsureRelease()`, `UploadAsset()`, `DownloadSourceTarball()`, `ListReleaseAssets()`
3. `internal/github/testutil.go` — `StubRunner` for external test packages
4. All `gh` commands via `exec.Command` with `GITHUB_TOKEN` in env.

**Acceptance:** go-git reads remotes/tags natively. Releases via `gh` CLI.

---

### T8b — Provider implementations (6 providers)

**Precondition:** T6, T7, T8

**Interface:** Each implements `UpstreamProvider` (`FetchLatestVersion(ctx, RemoteId) (string, error)`).

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
1. `internal/providers/github.go` — wraps `internal/github`, rate-limited, `NewGitHubProviderWithClient()` for testing
2. `internal/providers/pypi.go` — `httpclient.GetWithRetry()` → `/pypi/<name>/json`, `NewPyPIProviderWithClient()`
3. `internal/providers/npm.go` — `httpclient.GetWithRetry()` → `/registry/<name>/latest`, `NewNpmProviderWithClient()`
4. `internal/providers/gitlab.go` — `httpclient.GetWithRetry()` → `projects/:id/releases` (fallback: `repository/tags`), `NewGitLabProviderWithClient()`
5. `internal/providers/codeberg.go` — `httpclient.GetWithRetry()` → `/api/v1/repos/:owner/:repo/releases` (Gitea), `NewCodebergProviderWithClient()`
6. `internal/providers/sourcehut.go` — GraphQL `api.sr.ht/gitea` / HTML fallback, `extractTagsFromHTML()`, `NewSourceHutProviderWithClient()`
7. `internal/providers/urlresolve.go` — `ResolveProviderFromURL(url) (RemoteIdType, error)`
8. `internal/providers/provider_test.go` — all provider tests

**Acceptance:** All 6 providers resolve latest versions. Prerelease filtering. Rate limiting for HTTP providers.

---

## T10 Integration

HTTP provider tests via `httptest.Server` use the factory added to `httpclient/client.go`:
```go
func NewClientWithTransport(rl *ratelimit.RateLimiter, transport http.RoundTripper) *Client
```
This enables T10's `TestUpstream_Check*Outdated` tests to hit mock servers. No other deferred items carry forward.
