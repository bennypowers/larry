# Core Domain Tasks (T4–T6)

**What this covers:** `Version` type, `RemoteId` model, provider registry.
**Why second:** These are the domain model types that every command depends on.

---

### T4 — `Version` type — parse, compare, format (Gentoo semantics)

**Precondition:** None (standalone domain type)

**Tests:**
- `TestVersion_Parse` — parses `1.2.3`, `1.2.3-r1`, `1.2.3_p1`, `1.0_alpha1`, `1.0_beta2`, `1.0_rc3`, `9999`
- `TestVersion_Parse_Invalid` — rejects malformed strings
- `TestVersion_Compare` — correct `<`, `>`, `==` for all combinations:
  - `1.0.0 < 1.0.1`, `1.0.1 < 2.0.0`
  - `_alpha1 < _beta1 < _rc1 < _p1 < (release) < _r1`
  - `9999 > any finite version`
- `TestVersion_Normalize` — strips `v`, `V`, `bun-`, `rocm-`, `release-`, `^{...}`
- `TestVersion_Normalize` — leaves unchanged `1.2.3`, `1.2.3_p1`, `1.0_alpha1`
- `TestVersion_RoundTrip` — parse → format → parse → equal
- `TestVersion_CompareSuffixes` — alpha vs beta, rc vs patch, etc.

**Implementation:**
1. `internal/version/version.go`:
   ```go
   type Version struct {
       Components []uint64  // flexible: 1, 2, 3, 4, or 5 components (not just Major/Minor/Patch)
       Suffix     *Suffix
       Revision   uint64
       Live       bool  // 9999 special case
   }
   type Suffix struct {
       Type   SuffixType
       Number uint32
   }
   type SuffixType int // Alpha, Beta, Pre, Rc, Patch, Release
   ```
   Rationale: `Major, Minor, Patch uint64` cannot represent real-world version strings with 1, 2, 4, or 5 components (e.g. `1.0`, `1.2.3.4`, `2024.01.01_rc1`). A `[]uint64` slice is flexible and handles all cases.
2. Parser: regex → components. Handle `_alpha`, `_beta`, `_rc`, `_p`, `-r`
3. Compare: Gentoo version sort algorithm (PkgVersionCompare semantics)
4. Normalize: strip known prefixes
5. No external dependencies — pure Go

**Acceptance:** 100% of version strings from SPEC parse and compare correctly. Round-trip identity holds.

---

### T5 — `RemoteId` model + `metadata.xml` parser

**Precondition:** T4 complete

**Tests:**
- `TestRemoteId_Parse_GitHub` — `<remote-id type="github">owner/repo</remote-id>` → `RemoteId{Type: GitHub, Owner, Repo}`
- `TestRemoteId_Parse_PyPI` — `<remote-id type="pypi">package-name</remote-id>`
- `TestRemoteId_Parse_GitLab` — `<remote-id type="gitlab">gitlab.com:group/project</remote-id>`
- `TestRemoteId_Parse_NPM` — `<remote-id type="npm">package-name</remote-id>`
- `TestRemoteId_Parse_Codeberg` — `<remote-id type="codeberg">owner/repo</remote-id>`
- `TestRemoteId_Parse_SourceHut` — `<remote-id type="sourcehut">git.sr.ht/~user/project</remote-id>`
- `TestRemoteId_Multiple` — multiple `<remote-id>` tags in one file
- `TestRemoteId_Absent` — no `<remote-id>` returns nil, not error
- `TestMetadataXML_Parse` — full `metadata.xml` → parsed struct with maintainers, remotes, description
- `TestMetadataXML_MissingFile` — returns error, not panic
- `TestRemoteId_BackwardsCompat` — legacy `<upstream>` tags parsed gracefully

**Implementation:**
1. `internal/remoteid/remoteid.go`:
   ```go
   type RemoteId struct {
       Type   RemoteIdType
       Owner  string  // GitHub, Codeberg
       Repo   string  // GitHub, Codeberg
       Name   string  // PyPI, NPM
       Host   string  // GitLab, SourceHut
       Path   string  // GitLab, SourceHut
   }
   type RemoteIdType int // GitHub, PyPI, NPM, GitLab, Codeberg, SourceHut
   ```
2. `internal/remoteid/metadata.go` — XML parser using `encoding/xml`
3. Parse `metadata.xml` into struct, extract all `<remote-id>` and `<upstream>` tags
4. Handle malformed XML gracefully with descriptive errors
5. Helper: `ResolveRemoteId(ebuildPath) ([]RemoteId, error)` — walks from ebuild dir to find `metadata.xml`

**Acceptance:** All `<remote-id>` types from SPEC parse correctly. Missing/invalid XML returns errors. Legacy `<upstream>` tags handled.

---

### T6 — Provider registry & `UpstreamProvider` interface

**Precondition:** T4, T5, T7 complete (rate limiter needed for `TestProviderRegistry_WithRateLimiter`)

**Tests:**
- `TestProviderRegistry_Resolve_GitHub` — `RemoteId{GitHub}` → `*GitHubProvider`
- `TestProviderRegistry_Resolve_PyPI` → `*PyPIProvider`
- `TestProviderRegistry_Resolve_NPM` → `*NpmProvider`
- `TestProviderRegistry_Resolve_GitLab` → `*GitLabProvider`
- `TestProviderRegistry_Resolve_Codeberg` → `*CodebergProvider`
- `TestProviderRegistry_Resolve_SourceHut` → `*SourceHutProvider`
- `TestProviderRegistry_NoMatch` — unknown type returns nil
- `TestProviderRegistry_WithRateLimiter` — created provider has rate limiter attached

**Implementation:**
1. `internal/provider/interface.go`:
   ```go
   type UpstreamProvider interface {
       FetchLatestVersion(allowPrereleases bool) (*version.Version, error)
       FetchMetadata() (*ProviderMetadata, error)
       Name() string
   }
   type ProviderMetadata struct {
       Name, Description, Homepage string
   }
   ```
2. `internal/provider/registry.go` — `ProviderRegistry` maps `RemoteIdType` → provider factory
3. `internal/provider/factory.go` — `ResolveProvider(RemoteId) (UpstreamProvider, error)`
4. Each provider struct embeds a rate limiter, and either an HTTP client (PyPI, npm, GitLab, Codeberg, SourceHut) or the gh-CLI wrapper (T8, GitHub) depending on transport
5. Prerelease filter function as method on each provider (regex from SPEC)

**Acceptance:** Every `RemoteId` type maps to exactly one concrete provider. Providers report their name correctly.
