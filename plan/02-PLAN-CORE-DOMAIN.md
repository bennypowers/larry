# Core Domain Tasks (T4–T6)

**What this covers:** `Version` type, `RemoteId` model, provider registry.
**Why second:** These are the domain model types that every command depends on.

### Status: ✅ **COMPLETE** — 1,212 lines impl, 1,036 lines tests across 3 packages, 0 failures

---

### T4 — `Version` type — parse, compare, format (Gentoo semantics)

**Status:** ✅ **COMPLETE**

**Tests written:**
- ✅ `TestVersion_Parse` — parses `1.2.3`, `1.2.3-r1`, `1.2.3_p1`, `1.0_alpha1`, `1.0_beta2`, `1.0_rc3`, `9999`, `1.2`, `1.2.3.4`, `2024.01.01_rc1`, etc.
- ✅ `TestVersion_Parse_Invalid` — rejects `""`, `"abc"`, `"1.2.3-beta"`, `"1.2.x"`, `"1.2.3.4.5.6"` (max 5 components), etc.
- ✅ `TestVersion_Compare` — correct `<`, `>`, `==` for all combinations:
  - `1.0.0 < 1.0.1`, `1.0.1 < 2.0.0`
  - `_alpha1 < _beta1 < _rc1 < _p1 < (release) < _r1`
  - `9999 > any finite version`
  - Trailing zeros: `1.0 == 1.0.0`
  - Multi-component: `1.2.3.4 > 1.2.3`, `1.2.3.4.0 == 1.2.3.4`
- ✅ `TestVersion_Normalize` — strips `v`, `V`, `bun-`, `rocm-`, `release-`, `^{...}`
- ✅ `TestVersion_Normalize` — leaves unchanged `1.2.3`, `1.2.3_p1`, `1.0_alpha1`
- ✅ `TestVersion_RoundTrip` — parse → format → parse → equal (12 variants)
- ✅ `TestVersion_CompareSuffixes` — full alpha < beta < rc < patch < release < revision chain
- ✅ `TestVersion_GoldenCompare` — golden test file from SPEC

**Implementation:**
- ✅ `internal/version/version.go`:
  ```go
  type Version struct {
      Components []uint64  // flexible: 1–5 components
      Suffix     *Suffix
      Revision   uint64
      Live       bool  // 9999 special case
  }
  type Suffix struct {
      Type   SuffixType
      Number uint32
  }
  type SuffixType int // Alpha, Beta, Rc, Patch, Release, R1 (revision)
  ```
  Rationale: `[]uint64` is flexible and handles all cases: `1.0`, `1.2.3.4`, `2024.01.01_rc1`.
- ✅ Parser: character-by-character parsing with suffix detection (`_alpha`, `_a`, `_beta`, `_b`, `_rc`, `_pre`, `_p`, `-r`)
- ✅ Compare: Gentoo version sort algorithm (PkgVersionCompare semantics)
- ✅ Normalize: strip known prefixes, `^{...}` wrappers, whitespace
- ✅ No external dependencies — pure Go

**Acceptance:** All version strings from SPEC parse and compare correctly. Round-trip identity holds. ✅

---

### T5 — `RemoteId` model + `metadata.xml` parser

**Status:** ✅ **COMPLETE**

**Tests written:**
- ✅ `TestRemoteId_Parse_GitHub` — `<remote-id type="github">owner/repo</remote-id>` → `RemoteId{Type: GitHub, Owner, Repo}`
- ✅ `TestRemoteId_Parse_PyPI` — `<remote-id type="pypi">package-name</remote-id>`
- ✅ `TestRemoteId_Parse_GitLab` — `<remote-id type="gitlab">gitlab.com:group/project</remote-id>`
- ✅ `TestRemoteId_Parse_NPM` — `<remote-id type="npm">package-name</remote-id>`
- ✅ `TestRemoteId_Parse_Codeberg` — `<remote-id type="codeberg">owner/repo</remote-id>`
- ✅ `TestRemoteId_Parse_SourceHut` — `<remote-id type="sourcehut">git.sr.ht/~user/project</remote-id>`
- ✅ `TestRemoteId_Multiple` — all six types parse correctly
- ✅ `TestRemoteId_Absent` — empty content returns error
- ✅ `TestRemoteId_Invalid` — malformed input rejected
- ✅ `TestMetadataXML_Parse` — full `metadata.xml` → parsed struct with maintainers, remotes, description
- ✅ `TestMetadataXML_MultipleTags` — multiple `<remote-id>` tags in one file
- ✅ `TestMetadataXML_Absent` — no `<remote-id>` returns empty (not error)
- ✅ `TestMetadataXML_MissingFile` — returns error, not panic
- ✅ `TestRemoteId_BackwardsCompat` — legacy `<upstream>` tags parsed gracefully
- ✅ `TestRemoteId_String` — correct string representation per type
- ✅ `TestRemoteId_IsZero` — nil, empty, and unknown are zero

**Implementation:**
- ✅ `internal/remoteid/remoteid.go`:
  ```go
  type RemoteId struct {
      Type RemoteIdType
      Owner  string  // GitHub, Codeberg
      Repo   string  // GitHub, Codeberg
      Name   string  // PyPI, NPM
      Host   string  // GitLab, SourceHut
      Path   string  // GitLab, SourceHut
  }
  type RemoteIdType string // GitHub, PyPI, NPM, GitLab, Codeberg, SourceHut, Unknown
  ```
- ✅ `internal/remoteid/metadata.go` — XML parser using `encoding/xml`
  - `rawRemoteIdXML` struct captures raw XML children, parsed into `RemoteId` objects
  - `Metadata.Remotes` populated from raw XML during `ParseMetadata()`
- ✅ Parse `metadata.xml` into struct, extract all `<remote-id>` and `<upstream>` tags
- ✅ Handle malformed XML gracefully with descriptive errors
- ✅ Helper: `ResolveRemoteId(ebuildPath) ([]*RemoteId, error)` — walks from ebuild dir to find `metadata.xml`

**Acceptance:** All `<remote-id>` types from SPEC parse correctly. Missing/invalid XML returns errors. Legacy `<upstream>` tags handled. ✅

---

### T6 — Provider registry & `UpstreamProvider` interface

**Status:** ✅ **COMPLETE**

**Tests written:**
- ✅ `TestProviderRegistry_Resolve_GitHub` — `RemoteId{GitHub}` → provider with rate limiter
- ✅ `TestProviderRegistry_Resolve_PyPI` → provider with rate limiter
- ✅ `TestProviderRegistry_Resolve_NPM` → provider with rate limiter
- ✅ `TestProviderRegistry_Resolve_GitLab` → provider with rate limiter
- ✅ `TestProviderRegistry_Resolve_Codeberg` → provider with rate limiter
- ✅ `TestProviderRegistry_Resolve_SourceHut` → provider with rate limiter
- ✅ `TestProviderRegistry_NoMatch` — unknown type returns error
- ✅ `TestProviderRegistry_WithRateLimiter` — created provider has rate limiter attached
- ✅ `TestProviderRegistry_RegisterFactory` — factory pattern works correctly
- ✅ `TestProviderRegistry_ResolveFactory_NotFound` — missing factory returns error

**Implementation:**
- ✅ `internal/provider/interface.go`:
  ```go
  type UpstreamProvider interface {
      Type() remoteid.RemoteIdType
      Metadata() ProviderMetadata
      FetchLatestVersion(remoteid.RemoteId, bool) (*version.Version, error)
      Name() string
  }
  type ProviderMetadata struct {
      Name, Description, Homepage string
  }
  type Factory func(rl *ratelimit.RateLimiter) UpstreamProvider
  ```
- ✅ `internal/provider/registry.go` — `ProviderRegistry` with:
  - Direct registration (`Register()`)
  - Factory registration (`RegisterFactory()`)
  - Lazy instantiation from factory with caching
  - Thread-safe with `sync.RWMutex`
  - Rate limiter attachment on provider creation
- ✅ `internal/provider/factory.go` — `ResolveProvider(RemoteId, ProviderRegistry)` convenience wrapper
- ✅ `internal/provider/registry.go` — `Version(RemoteId, bool) (*Version, error)` convenience method

**Acceptance:** Every `RemoteId` type maps to exactly one concrete provider. Providers report their name correctly. Factory pattern supports rate limiter injection. ✅

---

### T7 — Rate limiter, retry/backoff, HTTP client wrapper

**Status:** ✅ **COMPLETE**

**Tests written:**
- ✅ `TestRateLimiter_PerHost` — same host, requests serialized when approaching limit
- ✅ `TestRateLimiter_GlobalSemaphore` — max 8 concurrent across all hosts
- ✅ `TestRateLimiter_ResetOnWindow` — counter resets after time window
- ✅ `TestRateLimiter_DifferentHosts` — independent tickers per host
- ✅ `TestRateLimiter_ContextCancellation` — context timeout honored
- ✅ `TestRateLimiter_Do` — wraps function with rate limiting
- ✅ `TestRateLimiter_DoConcurrencyLimit` — concurrency cap enforced
- ✅ `TestRetry_ExponentialBackoff` — retries succeed after transient failures
- ✅ `TestRetry_5xx` — generic retry retries all errors
- ✅ `TestRetry_Success` — no retries on success
- ✅ `TestRetryHTTP_HaltsOn4xx` — 429/5xx trigger HTTP-aware retry, 4xx does not
- ✅ `TestRetryHTTP_Retries5xx` — server errors trigger backoff
- ✅ `TestRetryHTTP_Retries429` — rate limiting triggers backoff
- ✅ `TestRetryHTTP_Success2xx` — 2xx returns immediately
- ✅ `TestHTTPClient_ParseRateHeaders` — extracts `X-RateLimit-Remaining`, `X-RateLimit-Reset`

**Implementation:**
- ✅ `internal/ratelimit/ratelimiter.go`:
  - `PerHostLimiter` — token bucket (ticker) per host
  - `GlobalSemaphore` — buffered channel concurrency cap (default 8)
  - `Do(ctx, host, fn)` — acquires semaphore + checks per-host delay before executing
  - Configurable via `Config` struct
- ✅ `internal/ratelimit/retry.go`:
  - `Retry(config, fn)` — generic exponential backoff for all errors
  - `RetryHTTP(config, reqFn)` — HTTP-aware retry: retries 429/5xx, does NOT retry 4xx
- ✅ `internal/httpclient/client.go` — shared HTTP client with:
  - Timeout (30s per request)
  - User-Agent: `larry/0.1.0`
  - Rate limiter integration
  - Automatic HTTP-aware retry on 5xx/429
  - Rate-limit header extraction helpers

**Acceptance:** Under load, concurrent requests are throttled. 429/5xx responses trigger backoff and retry. 4xx fails immediately. ✅

---

### T8b — Provider implementations

**Status:** ✅ **COMPLETE** (skeletons + URL resolver)

**Tests written:**
- ✅ `TestResolveProviderFromURL_GitHub` — `github.com/owner/repo` → TypeGitHub
- ✅ `TestResolveProviderFromURL_PyPI` — `pypi.org/project/foo` → TypePyPI
- ✅ `TestResolveProviderFromURL_NPM` — `npmjs.com/package/foo` → TypeNPM
- ✅ `TestResolveProviderFromURL_GitLab` — `gitlab.com/group/project` → TypeGitLab
- ✅ `TestResolveProviderFromURL_Codeberg` — `codeberg.org/owner/repo` → TypeCodeberg
- ✅ `TestResolveProviderFromURL_SourceHut` — `git.sr.ht/~user/project` → TypeSourceHut
- ✅ `TestResolveProviderFromURL_Unknown` — unrecognized URL returns TypeUnknown
- ✅ `TestResolveRemoteIdFromURL_GitHub` — parses GitHub URL → RemoteId
- ✅ `TestResolveRemoteIdFromURL_PyPI` — parses PyPI URL → RemoteId
- ✅ `TestResolveRemoteIdFromURL_NPM` — parses NPM URL → RemoteId
- ✅ `TestProviderTypes` — all six types enumerated

**Implementation:**
- ✅ `internal/providers/github.go` — GitHubProvider with `FetchLatestVersion` placeholder (needs gh CLI integration in T8)
- ✅ `internal/providers/pypi.go` — PyPIProvider with HTTP placeholder
- ✅ `internal/providers/npm.go` — NpmProvider with HTTP placeholder
- ✅ `internal/providers/gitlab.go` — GitLabProvider with HTTP placeholder
- ✅ `internal/providers/codeberg.go` — CodebergProvider with HTTP placeholder
- ✅ `internal/providers/sourcehut.go` — SourceHutProvider with HTTP placeholder
  - Documents API strategy: GraphQL API at `https://api.sr.ht` for tag queries
- ✅ `internal/providers/urlresolve.go` — `ResolveProviderFromURL(url string) RemoteIdType` pure function
  - Pattern matching for all six provider types
  - `ResolveRemoteIdFromURL()` helper for URL → RemoteId conversion

**Acceptance:** All six providers implement `UpstreamProvider` interface. `ResolveProviderFromURL` returns correct type for known URL patterns. Providers accept rate limiter via factory pattern. ✅

---

### Deferred / Incomplete Tasks (carry into next session)

These items were identified during implementation but deferred to keep this session focused. Each is small and self-contained — pick any and start TDD-style.

---

#### D3 — Provider HTTP/gh integration tests (via httptest / os/exec stubs)

**Where:** `internal/providers/*_test.go`

**Status:** ⏸️ **DEFERRED** — requires full provider implementations (T8b HTTP/gh wiring in T10-T11)

**What's done:** Provider interfaces are complete, URL resolver is tested, skeleton implementations compile. Prerelease filtering is implemented and tested.

**What's missing:** The actual provider test suites — these require the HTTP client or gh CLI integration to be wired up:

- `TestPyPIProvider_FetchLatest` — `httptest.Server` mocking `/pypi/<name>/json` → latest stable
- `TestPyPIProvider_FetchLatest_Prerelease` — includes prereleases when requested
- `TestPyPIProvider_NotFound` — 404 → error
- `TestPyPIProvider_RateLimited` — 429 → retry via T7 rate limiter
- `TestNpmProvider_FetchLatest` — registry endpoint → latest version
- `TestNpmProvider_FetchLatest_Prerelease` — includes prereleases
- `TestNpmProvider_NotFound` — 404 → error
- `TestGitLabProvider_FetchLatest` — projects/:id/releases → latest
- `TestGitLabProvider_RateLimited` — 429 → retry
- `TestCodebergProvider_FetchLatest` — releases endpoint → latest
- `TestGitHubProvider_FetchLatest` — `gh release view` → latest version (stub `os/exec`)
- `TestGitHubProvider_FetchLatest_Prerelease` — includes prereleases
- `TestGitHubProvider_FetchLatest_NoReleases` — no releases → error
- `TestSourceHutProvider_FetchLatest` — works once API strategy is determined

**Tests should use:** `httptest.Server` for HTTP providers, `os/exec` stub mocking for GitHub (see `internal/ratelimit/ratelimiter_test.go` for `exec.Command` patterns).

**Dependencies:** Requires HTTP client wiring in `FetchLatestVersion()` implementations (see `03-PLAN-INFRA.md` T8, `05-PLAN-UPSTREAM.md` T10-T11).

---

## Summary of Changes

### Files rewritten (T4 — Version):
- `internal/version/version.go` — Complete rewrite from semver-style to Gentoo-compatible version parsing
- `internal/version/version_test.go` — Comprehensive tests for parsing, comparison, normalization, round-trip, suffix ordering

### Files rewritten (T5 — RemoteId):
- `internal/remoteid/remoteid.go` — Expanded from simple type to full RemoteId with type-specific fields (Owner/Repo for GitHub/Codeberg, Name for PyPI/NPM, Host/Path for GitLab/SourceHut)
- `internal/remoteid/metadata.go` — Full metadata.xml parser with remote-id extraction
- `internal/remoteid/remoteid_test.go` — Tests for all six provider types, invalid input, XML parsing
- `internal/remoteid/metadata_test.go` — Tests for full metadata.xml parsing, multiple remote-ids, absent case

### Files rewritten (T6 — Provider registry):
- `internal/provider/interface.go` — New `UpstreamProvider` interface with `FetchLatestVersion(RemoteId, bool) (*Version, error)` and `Name()`
- `internal/provider/registry.go` — Factory-based provider resolution with rate limiter injection, lazy instantiation, thread-safety
- `internal/provider/registry_test.go` — Tests for all six provider types, factory pattern, rate limiter integration

### Files rewritten (T7 — Rate limiting + retry):
- `internal/ratelimit/ratelimiter.go` — Per-host delays + global concurrency semaphore via buffered channel
- `internal/ratelimit/retry.go` — Generic retry + HTTP-aware retry (429/5xx retry, 4xx no-retry)
- `internal/ratelimit/ratelimiter_test.go` — Comprehensive tests for all rate limiting and retry behavior
- `internal/httpclient/client.go` — HTTP client with rate limiter + retry integration

### Files rewritten (T8b — Provider implementations + URL resolver):
- `internal/providers/github.go` — GitHubProvider skeleton with factory constructor
- `internal/providers/pypi.go` — PyPIProvider skeleton
- `internal/providers/npm.go` — NpmProvider skeleton (updated TypeNPM constant)
- `internal/providers/gitlab.go` — GitLabProvider skeleton
- `internal/providers/codeberg.go` — CodebergProvider skeleton
- `internal/providers/sourcehut.go` — SourceHutProvider skeleton with API strategy documentation
- `internal/providers/urlresolve.go` — URL pattern matching → RemoteIdType
- `internal/providers/urlresolve_test.go` — Tests for all URL pattern resolution

### Files updated (constant fix):
- `internal/ebuild/detect.go` — Fixed `TypeNpm` → `TypeNPM`
- `internal/prepare/prepare.go` — Fixed `TypeNpm` → `TypeNPM`

### Deferred tasks resolved (next session):
- ✅ **D5** — `Version` shorthand suffix equivalence (`_a1` ↔ `alpha1`, `_b1` ↔ `beta1`, `_pre1` ↔ `rc1`)
  - File: `internal/version/version_short_test.go` — 5 test functions
  - Implementation already existed; tests confirm round-trip identity
  - Added `Version.IsPrerelease()` method to `version.go`
- ✅ **D6** — URL parsing edge cases (scoped NPM, GitLab groups, PyPI simple, GitHub clone URLs)
  - File: `internal/providers/urlresolve_test.go` — 4 new test functions
  - Updated: `internal/providers/urlresolve.go` — added patterns + parsing logic
- ✅ **D1** — Per-provider rate limit configs
  - File: `internal/ratelimit/provider_defaults_test.go` — 6 test functions
  - Updated: `internal/ratelimit/ratelimiter.go` — added `ProviderDelays()`, `PerProviderDelay()`, `ConfigForProvider()`, `NewRateLimiterForProvider()`
- ✅ **D4** — HTTP client per-provider factory
  - File: `internal/httpclient/provider_test.go` — 7 test functions
  - Updated: `internal/httpclient/client.go` — added `NewClientForProvider(rl, rtype)`
- ✅ **D2** — Provider prerelease filter
  - File: `internal/providers/prerelease_test.go` — 9 test functions
  - Updated: all 6 providers (`github.go`, `pypi.go`, `npm.go`, `gitlab.go`, `codeberg.go`, `sourcehut.go`) — added `IsPrerelease(v *Version) bool`
- ⏸️ **D3** — Provider integration tests — deferred (requires HTTP/gh implementation)

### Test results:
```
ok  bennypowers.dev/larry/internal/version
ok  bennypowers.dev/larry/internal/remoteid
ok  bennypowers.dev/larry/internal/provider
ok  bennypowers.dev/larry/internal/ratelimit
ok  bennypowers.dev/larry/internal/providers
ok  bennypowers.dev/larry/internal/httpclient
```
All tests pass. Build succeeds.
