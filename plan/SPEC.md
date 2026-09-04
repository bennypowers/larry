# `larry` — Gentoo Overlay Management Tool

## Overview

`larry` is a command-line tool for managing a Gentoo overlay repository. It automates
the common lifecycle operations for overlay packages: importing from other overlays,
version bumping, preparing distfiles and manifests, testing builds, and checking for
upstream updates.

### External Dependencies

`larry` relies on the following external tools:

| Tool                                                  | Purpose                                                  |
| ----------------------------------------------------- | -------------------------------------------------------- |
| `ebuild` (Portage)                                    | Phase execution, manifest generation                     |
| `pkgdev`                                              | Manifest generation (`pkgdev manifest`)                  |
| `gh` (GitHub CLI)                                     | GitHub releases, GraphQL API, release listing            |
| `podman`                                              | Container-based clean-room testing with GPU passthrough  |
| `npm`                                                 | Dependency resolution for npm packages                   |
| `go`                                                  | Go module vendoring (`go mod vendor`)                    |
| `git`                                                 | Remote URL resolution, tag operations                    |
| `pycargoebuild` (optional)                            | Cargo lockfile → CRATES parsing (when available)         |
| `cargo-ebuild` (optional)                             | Cargo lockfile → CRATES parsing (when available)         |

### Prerequisites

- The overlay must be a Git repository with an `origin` remote pointing to GitHub
  (format: `git@github.com:owner/repo.git` or `https://github.com/owner/repo.git`)
- `DISTDIR` must be accessible and writable
- The overlay category directories must match the Gentoo category/package layout

---

## Overlap with Established Gentoo Tooling

Before implementing custom behavior, evaluate whether existing Gentoo tooling already
provides the needed functionality. The following table maps `larry` operations to
established tools and justifies custom implementation where applicable.

| `larry` Operation | Existing Tool | Overlap Assessment | Custom Justification |
|-------------------|---------------|--------------------|----------------------|
| **Version bump (file copy)** | None (no established tool) | `pkgdev` provides manifest/scan/sign/commit but has **no bump subcommand** | **Justified** — no standard Gentoo tool performs version bumping with eclass variable expansion. Custom implementation is necessary. |
| **Manifest generation** | `pkgdev manifest` | Complete overlap — identical operation | **Always delegate** to `pkgdev manifest`. Never reimplement manifest/digest generation. |
| **Cargo lock → CRATES** | `pycargoebuild`, `cargo-ebuild` | High overlap — both parse `Cargo.lock` and generate `CRATES=` blocks | **Defer to `pycargoebuild`** where available. Native parsing is only justified if `pycargoebuild` is unavailable or if the tool must be self-contained with no Python dependency. |
| **Upstream version checking** | `app-portage/mgorny-dev-scripts` (e.g., `version-check.sh`), `app-portage/portq`, `pybugz` | Partial overlap — `mgorny-dev-scripts` checks PyPI and GitHub; `portq` has update checking | **Justified** — `larry upstream`'s multi-forge support (PyPI, npm, GitHub, GitLab, Codeberg) in a single pass with dependency scanning is distinct enough to warrant custom implementation. |
| **npm distfile fetching** | None (no established tool) | No overlap — `mirror://npm/` is a Gentoo-specific abstraction that standard tools do not resolve | **Justified** — no existing tool handles npm registry resolution for Gentoo overlays. |
| **Go vendoring** | None (no established tool) | Partial overlap — `ebuild` phase handles `go-module` but does not vendor deps | **Justified** — pre-vendoring Go deps into a tarball for `DISTDIR` is a Gentoo overlay-specific pattern not covered by standard tooling. |
| **Container testing** | `pkgtest`, `paludis --repository` isolation | Partial overlap — `pkgtest` exists but has different GPU/overlay passthrough requirements | **Justified** — `larry test`'s GPU passthrough + overlay-as-repo configuration targets a specific workflow not covered by generic testing tools. |
| **Import from other overlays** | Manual (`cp`, `emerge --sync`) | No overlap — no tool automates ebuild import with metadata rewriting | **Justified** — automated import with `<remote-id>` preservation and `metadata.xml` rewriting is unique to `larry`. |

**Implementation directive:**

Where overlap exists, `larry` should **delegate to established tooling** rather than reimplement:

1. Use `pycargoebuild` or `cargo-ebuild` for Cargo lockfile parsing when available; fall back to native parsing only when these tools are unavailable
2. Use `pkgdev manifest` for all manifest generation — never reimplement digest calculation

Where overlap is partial or non-existent, custom implementation is justified and
should be clearly documented in the code with references to the existing tools it
supersedes or complements.

---

## Architecture & Design Principles

### External Tool Delegation vs. Native Implementation

**Specification:** For a greenfield compiled-language implementation, external tool usage
should be strictly minimized. The following delegation policy applies:

| Operation | Delegate To | Reason |
|-----------|-------------|--------|
| Ebuild phase execution (`fetch`, `compile`, `install`, etc.) | `ebuild` (Portage) | Requires full Portage environment (envvars, profiles, sandboxes) |
| Manifest generation | `pkgdev manifest` | Deep integration with Portage's digest system |
| Container orchestration | `podman` | GPU device passthrough, cgroup management |
| Language toolchains | `npm`, `go` | Dependency resolution, vendoring |
| Git operations | `git` **or** native library | `git` subprocess is acceptable; a sufficiently robust native git library (e.g., `libgit2`, `git2-rs`, `libgit2-go`) may be used as an alternative, depending on host language availability and required functionality.
| GitHub CLI operations | `gh` | Auth management, token refresh |
| Cargo lockfile parsing | `pycargoebuild` or `cargo-ebuild` (optional) | Parse `Cargo.lock` → `CRATES=` block; fall back to native when unavailable |

**Portage primitives, language toolchains, git, and cargo-ebuild helpers are the only valid external delegations.**

**Forbidden delegations** — these must be implemented natively:

- **HTTP requests:** Use stdlib HTTP client, not `curl`
- **JSON parsing:** Use a JSON library, not `jq`
- **Text processing:** Use stdlib regex/string operations, not `sed`/`grep`/`awk`
- **Archiving:** Use a tar/gzip library, not `tar`
- **Temp directories:** Use stdlib temp dir utilities, not `mktemp`
- **File finding:** Use filesystem walk APIs, not `find`
- **Version sorting:** Use native comparison logic, not `sort -V`

The only acceptable subprocess spawning is for `ebuild`, `pkgdev`, `npm`, `go`, `podman`, `git`, `gh`, `pycargoebuild`, and `cargo-ebuild`.

### Concurrency & Rate Limiting

`larry upstream` performs potentially hundreds of API queries across multiple upstream
sources. Unbounded concurrency triggers GitHub secondary rate limits; sequential
execution is unacceptably slow. The implementation **must** enforce rate limiting:

**Concurrency model:**

| Setting | Value | Rationale |
|---------|-------|-----------|
| Worker pool max | 8 concurrent queries | Balances throughput against rate limit risk |
| Per-host rate limiter | Exponential backoff | GitHub, GitLab, PyPI, npm, Codeberg each have different limits |
| GitHub unauthenticated | 60 requests/hour | Hard cap; request `GITHUB_TOKEN` for 5,000/hour |
| GitHub authenticated | 5,000 requests/hour (REST), 5,000/hour (GraphQL) | Soft cap; respect `X-RateLimit-Remaining` headers |
| Retry on 429 | Yes | Exponential backoff starting at 1s, max 5 attempts |
| Retry on 5xx | Yes | Same backoff policy |

**Rate limit headers to respect:**

- GitHub REST: `X-RateLimit-Remaining`, `X-RateLimit-Reset`
- GitHub GraphQL: `X-RateLimit-Remaining` in response headers
- GitLab: `RateLimit-Remaining` / `RateLimit-Reset`
- PyPI: No explicit headers; assume generous limits, back off on 429

**Implementation requirement:** Each upstream provider must implement a rate limiter
that tracks per-host request counts and sleeps when approaching limits. A global
semaphore (worker pool) limits total concurrent queries. The `--parallel` flag may
expose this to users, defaulting to the conservative pool size.

### Transaction Boundaries & Atomicity

`larry prepare` mutates local files and remote state in a sequence where partial
failure leaves inconsistent state. The following transaction model applies:

**Transaction phases and rollback points:**

| Step | Operation | Rollback on Failure |
|------|-----------|---------------------|
| 1 | Download distfiles | None needed (idempotent download) |
| 2 | Resolve variables | None needed (read-only) |
| 3 | Build deps tarball | Delete temp files |
| 4 | Upload to GitHub | **No rollback** — orphaned asset |
| 5 | Edit ebuild content | Restore from `.bak` |
| 6 | Generate Manifest | **No rollback** — manifest may be invalid if step 4 was partial |

**Atomicity guarantees:**

- **Ebuild edits are atomic:** Backup → modify → on success remove `.bak`, on failure restore from `.bak`.
- **GitHub uploads are NOT atomic:** An uploaded asset cannot be automatically deleted on downstream failure. This is a documented limitation. Implementations MAY provide a `--cleanup` flag to remove orphaned assets on failure.
- **Manifest generation is NOT atomic:** If it fails, the `Manifest` file may be partially written. `pkgdev manifest` handles this internally, but the implementation should verify the result is valid.

**Dry-run / staging mode:**

`larry prepare` should support a `--dry-run` / `--stage` flag that:
1. Performs all local operations (downloads, variable resolution, tarball creation)
2. Shows what would be uploaded and what ebuild edits would be made
3. Skips all external mutations (no GitHub upload, no ebuild modification, no manifest generation)
4. Returns exit code 0 on success, 1 if any operation would fail

**Environment variable requirements:**

| Variable | Required | Purpose |
|----------|----------|---------|
| `GITHUB_TOKEN` | **Required for >50 packages** | GitHub API auth (REST + GraphQL + release uploads). 5,000 req/hr vs 60 unauthenticated. |
| `GH_TOKEN` or `GH_ENTERPRISE_TOKEN` | Optional fallback | If `GITHUB_TOKEN` is not set, fall back to `gh` CLI's detected auth. |
| `XDG_CACHE_HOME` | Optional | Defaults to `~/.cache` for `DISTDIR` and version query cache. |
| `DISPLAY` / `DBUS_SESSION_BUS_ADDRESS` | Conditional | Required only for `gh` CLI fallback (interactive auth). Not needed when using `GITHUB_TOKEN` directly. |

**Fallback chain:** `GITHUB_TOKEN` env var → `gh` CLI auth (via `GH_TOKEN`/`GH_ENTERPRISE_TOKEN`) → warn on unauthenticated scanning → **hard error** on >50 packages without a token.

---

## Architecture & Design Principles

### Provider Abstraction

**Specification:** Upstream sources (GitHub, GitLab, PyPI, npm, Codeberg, SourceHut) must be modeled as a **Provider abstraction** — a trait or interface with a unified method signature:

```text
trait UpstreamProvider {
    fn fetch_latest_version(&self, allow_prereleases: bool) -> Result<Version, Error>
    fn fetch_metadata(&self) -> Result<ProviderMetadata, Error>  // optional: fetches repo name, description, etc.
}
```

Each provider implementation encapsulates:

- **Identity resolution:** How to determine the package/repo name from the ebuild (via `<remote-id>` or SRC_URI)
- **API protocol:** REST, GraphQL, or raw HTTP + JSON
- **Auth strategy:** Token requirements, authentication headers
- **Rate limit handling:** Per-provider limits and backoff strategies
- **Version extraction:** How to identify the "latest" version (latest release, latest tag, JSON field, etc.)
- **Prerelease filtering:** Logic for excluding alpha/beta/rc/dev/nightly/canary versions

**Concrete providers:**

| Provider | API | Version Source | Prerelease Filter |
|----------|-----|----------------|--------------------|
| `GitHubProvider` | REST (`/releases/latest`) + GraphQL | Release tag + tag commit date | Regex: `alpha|beta|rc|dev|nightly|canary` |
| `GitLabProvider` | REST (`/api/v4/projects/.../repository/tags`) | First tag in API response | Regex: `alpha|beta|rc|dev|nightly` |
| `PyPIProvider` | REST (`/pypi/<name>/json`) | `.info.version` field | Regex: `alpha|beta|rc|dev|snapshot|post` |
| `NpmProvider` | REST (`/registry/<name>/latest`) | `.version` field | Regex: `alpha|beta|rc|dev|nightly|canary|next` |
| `CodebergProvider` | REST (Gitea API `/api/v1/repos/.../releases`) | First release tag | Regex: `alpha|beta|rc|dev|nightly|canary` |
| `SourceHutProvider` | REST (git.sr.ht API) | Tag enumeration | Regex: `alpha|beta|rc|dev|nightly` |

**Composition:** The provider should be instantiated from the package's `metadata.xml` (reading `<remote-id>` tags) rather than requiring callers to know which provider to construct. A `ProviderFactory` or `ProviderRegistry` should handle this mapping:

```text
fn resolve_provider(metadata: Metadata) -> Option<Box<dyn UpstreamProvider>> {
    match metadata.remote_id_type() {
        RemoteIdType::GitHub => Some(Box::new(GitHubProvider { owner_repo: metadata.remote_id() })),
        RemoteIdType::PyPI => Some(Box::new(PyPIProvider { name: metadata.remote_id() })),
        RemoteIdType::NPM => Some(Box::new(NpmProvider { name: metadata.remote_id() })),
        RemoteIdType::GitLab => Some(Box::new(GitLabProvider { host_path: metadata.remote_id() })),
        RemoteIdType::Codeberg => Some(Box::new(CodebergProvider { owner_repo: metadata.remote_id() })),
        RemoteIdType::SourceHut => Some(Box::new(SourceHutProvider { host_path: metadata.remote_id() })),
        None => None, // fallback to SRC_URI scraping
    }
}
```

### Source of Truth: `<remote-id>` from metadata.xml

**Specification:** Both `larry prepare` and `larry upstream` **must** read `<remote-id>` tags from the package's `metadata.xml` as the primary source of truth for upstream identity:

```
<remote-id type="github">owner/repo</remote-id>
<remote-id type="pypi">package-name</remote-id>
<remote-id type="gitlab">gitlab.com:group/project</remote-id>
<remote-id type="npm">package-name</remote-id>
<remote-id type="codeberg">owner/repo</remote-id>
```

**Priority chain:**

1. **`metadata.xml` `<remote-id>`** — parsed and used as the authoritative upstream identifier
2. **SRC_URI/HOMEPAGE regex scraping** — fallback only when no `<remote-id>` is present. This is a best-effort heuristic and may fail for non-identically named packages.
3. **Package name as-is** — last resort if neither mechanism provides a mapping

**Justification:** `<remote-id>` is the Gentoo-standard mechanism for declaring upstream identity (GLEP-76, metadata.dtd). It is the only mechanism that survives ebuild refactoring (variable changes, SRC_URI rewrites) and is not subject to parsing errors from complex SRC_URI lines. Regex scraping of SRC_URI/HOMEPAGE is fragile, non-standard, and error-prone.

**Implementation note:** `metadata.xml` parsing must use an XML library (not regex). The implementation must handle both `remote-id` and legacy `upstream` metadata formats gracefully, preferring `remote-id`.

---

## Commands

### `larry bump`

Create a new versioned ebuild from the latest existing one, then prepare it.

```larry bump [--revision] [--prereleases] <cat/pkg> [<version>]
```

**Arguments:**

| Argument         | Required    | Description                                                                                                                               |
| ---------------- | ----------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `--revision, -r` | Optional    | Create a revision bump instead of version bump. Copies the latest ebuild and appends/increments the revision suffix (e.g., `-r1`, `-r2`). |
| `--prereleases`  | Optional    | Allow prerelease versions when querying upstream for the latest version.                                                                  |
| `<cat/pkg>`      | Required    | The category/package atom (e.g., `dev-util/fish-lsp`).                                                                                    |
| `<version>`      | Conditional | Only required for version bumps when not using `--revision`. If omitted, `larry` queries upstream for the latest version.                 |

**Behavior:**

1. Resolve the package directory from the overlay tree.
2. Find the latest non-live (`-9999`) ebuild in the package directory.
3. Copy it to a new ebuild with the target version or revised filename.
4. Invoke the prepare pipeline on the new ebuild (see `larry prepare`).

**Ebuild filename and variable handling:**

When creating the new ebuild, the implementation must:

1. **Rename the file** from `${PN}-${PV_old}.ebuild` to `${PN}-${PV_new}.ebuild` (or `${PN}-${PV_old}-rN.ebuild` for revisions).
2. **Update internal variables** that may contain hardcoded version strings. Common patterns:
   - `MY_P="${PN}-${PV}"` — already uses `${PV}`, no change needed
   - `MY_P="mytool-1.0.0"` — hardcoded, may need updating if the pattern matches `${P}`
   - `S="${WORKDIR}/${MY_P}"` — depends on `MY_P` which may or may not need updating
3. **Portage's `${PV}` and `${P}` variables** are resolved at ebuild parse time by Portage, not at file-copy time. The ebuild source text should use variables (`${PV}`, `${P}`) rather than hardcoded versions wherever possible.
4. **`pkgdev` does not perform automatic variable substitution** on copied ebuilds. If the original ebuild contains hardcoded version strings, the user or `larry` must update them.

**Recommendation:** Encourage ebuild authors to use `${PV}` and `${P}` consistently. For
bumps, `larry` does **not** automatically scan and replace hardcoded version strings —
it delegates this to the ebuild author. A future enhancement MAY add a `--replace-version`
mode that performs best-effort substitution of `${PV}`-equivalent strings.

**Revision bump (`--revision`) behavior:**

When bumping a revision (`-r1`), the base version `${P}` (package name + version, without
revision) remains unchanged. This has implications for `larry prepare`:

- **npm packages:** The deps tarball `${P}-deps.tar.xz` is shared across revisions. If the
tarball already exists in `DISTDIR` and on the GitHub release (tagged `${PN}`), `larry
prepare` **must skip re-vendoring and re-upload**. It should verify the tarball exists
and proceed directly to manifest generation.
- **go packages:** Same as npm — the deps tarball `${P}-deps.tar.gz` is shared. Skip
vendoring and upload if the file exists in `DISTDIR`.
- **cargo packages:** The `CRATES=` block does not change with revisions. Skip Cargo.lock
download and parsing if the existing `CRATES=` block in the ebuild is identical.
- **rpi-kernel packages:** The `COMMIT=` value does not change with revisions. Skip the
kernel commit lookup.
- **generic packages:** No deps tarball or ebuild mutations. Direct to manifest generation.

**Implementation directive:** Before running the prepare pipeline on a revision bump,
check if the base version's prepare artifacts already exist and are valid. If so, skip
all upstream operations (API queries, downloads, uploads) and proceed directly to
manifest generation.

**Version resolution:**

- **Explicit `<version>` passed:** Skip all upstream lookup. The given version is used directly and passed into `larry prepare` as the target `PV`. No API queries are made.
- **`<version>` omitted (auto-resolution):** Query upstream for the latest version:
  - For PyPI packages: queries `https://pypi.org/pypi/<name>/json`
  - For npm packages: queries `https://registry.npmjs.org/<name>/latest`
  - For GitHub-hosted packages: queries the latest release and tags via the GitHub API
  - Prereleases are filtered out by default unless `--prereleases` is specified

**Name mapping (upstream identity):**

Portage package names (`category/pkg`) do not always match upstream identifiers
(PyPI `Pillow` vs Gentoo `dev-python/pillow`, GitHub `BurntSushi/ripgrep` vs
Gentoo `sys-apps/ripgrep`). Name resolution follows this priority:

1. **`metadata.xml` `<remote-id>` tags** — parsed first:
   - `<remote-id type="pypi">package-name</remote-id>`
   - `<remote-id type="github">owner/repo</remote-id>`
   - `<remote-id type="gitlab">host:path</remote-id>`
   - `<remote-id type="npm">package-name</remote-id>`
   - `<remote-id type="codeberg">owner/repo</remote-id>`
2. **SRC_URI/HOMEPAGE regex scraping** — fallback if no `<remote-id>` is found.
   This is a best-effort heuristic and may fail for non-identically named packages.

If neither mechanism provides a mapping, the package name is used as-is.

---

### `larry prepare`

Download distfiles, resolve dependencies, generate a deps tarball (if applicable),
upload to GitHub release, and generate the overlay Manifest.

```larry prepare [--type <type>] [--all] <ebuild-path> [...]
```

**Arguments:**

| Argument        | Required                  | Description                                                                                                                                         |
| --------------- | ------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--type, -t`    | Optional                  | Force package type detection. Values: `npm`, `cargo`, `go`, `rpi-kernel`, `generic`. If omitted, the type is auto-detected from the ebuild content. |
| `--all, -a`     | Optional                  | Rebuild manifests for every ebuild in the overlay using `pkgdev manifest` directly. Skips individual distfile downloads.                            |
| `<ebuild-path>` | Required (unless `--all`) | One or more ebuild file paths.                                                                                                                      |

**Type detection rules:**

| Type         | Detection Criterion                                  |
| ------------ | ---------------------------------------------------- |
| `npm`        | Ebuild contains `mirror://npm/` in SRC_URI           |
| `cargo`      | Ebuild contains a `CRATES="..."` variable definition |
| `go`         | Ebuild inherits `go-module`                          |
| `rpi-kernel` | (Special case; see below)                            |
| `generic`    | Everything else                                      |

**Variable resolution mechanics:**

Ebuild SRC_URI lines may contain `${PN}`, `${PV}`, `${P}`, and arbitrary custom
ebuild variables (e.g., `MY_WEBUI_P="${MY_WEBUI_N}-0.75.3"`). Variable references
can be nested and may include string manipulation operators.

Since these are bash variable expansions embedded in shell-style assignment lines
within text files, they cannot be safely or correctly expanded with native regex
or string replacement alone. Complex patterns such as:

- Conditional use flags: `webui? ( mirror://npm/... )`
- Nested variable references: `${MY_VAR="${OTHER}-suffix"}`
- Bash string manipulation: `${PV//./_}`, `${PN//-/_}`
- Inherited variables from eclass chains

cannot be reliably expanded without a Portage-aware environment.

**Specification:** Variable expansion **must** be delegated to Portage tooling rather
than raw bash, because ebuilds import eclasses (e.g., `inherit go-module`, `inherit cargo`)
that are only available in the Portage eclass path (`/usr/portage/eclass/`).

**Preferred approach:** Use `portageq envvar` or `portageq get_repo_path` to resolve
ebuild variables within the Portage environment. For example:

```bash
portageq envvar <ebuild-path> ${PN}
portageq envvar <ebuild-path> ${PV}
portageq envvar <ebuild-path> ${P}
```

Alternatively, invoke `ebuild <path> info` and parse the output for expanded variables.
These methods ensure all eclasses are properly sourced and available.

**Fallback (if Portage tooling is unavailable):** If a raw bash approach is used as a
last resort, the implementation must:
1. Set `EBUILD_PHASE=other` to bypass ebuild phase hooks
2. Set `EPREFIX="/"` and `EROOT="/"` to satisfy Portage environment expectations
3. Set `ECLASS_PATH="/usr/portage/eclass:"` to include standard eclasses
4. Provide minimal stubs for `inherit()`, `die()`, `eerror()`, and other eclass functions
5. Handle eclass inheritance chains (e.g., `go-module.eclass` may inherit `cargo.eclass`)

This fallback is **strongly discouraged** due to its fragility.

**Important:** The prepare step must handle `mirror://npm/` specially — this is a
Gentoo virtual mirror that must be rewritten to `https://registry.npmjs.org/` after
variable expansion.

**Transaction safety and rollback:**

All `prepare` operations that mutate external state (GitHub releases, ebuild content)
must observe transaction safety:

- **Ebuild mutations:** Back up the ebuild to a `.bak` file before modifying.
  Remove `.bak` on success; restore from `.bak` on failure.
- **GitHub release uploads:** If subsequent steps (e.g., `pkgdev manifest`) fail,
  the uploaded artifact is orphaned. There is **no automatic cleanup** of orphaned
  GitHub release assets — the user must clean up manually.
- **Atomicity guarantee:** Ebuild edits are atomic (backup/restore). Remote uploads
  are **not** atomic with manifest generation. The sequence is: upload → manifest.
  If manifest fails, the ebuild is restored from backup but the remote asset remains.

**Prepare pipeline per type:**

#### npm

**Ordering deadlock resolution:** The ebuild's SRC_URI already references the GitHub release URL
(`releases/download/${PN}/${P}-deps.tar.xz`). The manifest step (`pkgdev manifest`) requires
each distfile's hash to be recorded in `Manifest`. This creates a chicken-and-egg problem:

1. `pkgdev manifest` needs the distfile to compute its hash
2. The distfile (`${P}-deps.tar.xz`) must be uploaded first so it is available in `DISTDIR`
3. `pkgdev manifest` then reads the uploaded distfile, hashes it, and writes the entry

**Correct sequence (no deadlock):**

1. Download all npm distfiles from the npm registry (resolving `mirror://npm/` URIs)
2. Resolve `${PN}`, `${PV}`, `${P}`, and custom ebuild variables in SRC_URI lines
3. Run `npm install` against the downloaded tarball to resolve transitive dependencies
4. Package the npm cache as `${P}-deps.tar.xz` and copy to `DISTDIR`
5. **Upload** `${P}-deps.tar.xz` to the GitHub release tagged with `${PN}` (the package name, not the full PVR)
6. Generate the Manifest using `pkgdev manifest` — this reads the distfile from `DISTDIR` (or the URL), computes the hash, and writes it to `Manifest`

**Note:** The GitHub release tag must be `${PN}` (not `${P}`), as this is the shared key across revisions.

**Deadlock scenario and failure mode:**

If step 6 fails (manifest generation), the uploaded artifact on GitHub is orphaned. There is
**no automatic cleanup of orphaned release assets** — this is a documented limitation. The
ebuild is restored from `.bak` on failure, but the remote asset persists. Manual cleanup
via `gh release delete-asset` is required.

**GitHub release asset collision strategy:**

When uploading `${P}-deps.tar.xz` to a GitHub release, the asset may already exist (e.g.,
from a previous failed run, or from a revision bump where `${P}` is unchanged).

| Scenario | Behavior |
|----------|----------|
| Asset exists, checksum matches | Skip upload; proceed to manifest generation |
| Asset exists, checksum differs | **Overwrite** with `--clobber` semantics (the tarball content changed, likely due to dependency updates) |
| Asset does not exist | Upload as new asset |
| Revision bump (`-rN`) with existing base tarball | **Skip upload entirely** — the base version's deps tarball is shared across revisions and should already be on the release |

**Implementation directive:** Before uploading, compute the local tarball's SHA-256 hash.
If the release already contains an asset with a matching hash, skip the upload. Use the
`gh release view` command to list existing assets and compare hashes.

#### cargo

1. Locate the upstream repository URL by scanning SRC_URI and HOMEPAGE for GitHub (`github.com`) or SourceHut (`git.sr.ht` / `sr.ht`) URLs. **Prefer `<remote-id>` from `metadata.xml`** (see "Name mapping" above); fall back to regex scraping.
2. Download `Cargo.lock` from the upstream tag. **Search strategy:**
   - First, use the GitHub/Git tree API (or equivalent for the forge) to enumerate all `Cargo.lock` files in the repository
   - If the tree API is unavailable or fails, fall back to a configurable list of common paths:
     `Cargo.lock`, `bindings/python/Cargo.lock`, `src/Cargo.lock`, `crates/Cargo.lock`, `rust/Cargo.lock`
   - Hardcoding arbitrary subdirectories is an anti-pattern; the search must be extensible and
     the default paths should be easily overridable via configuration. A production implementation
     **must** prefer the repository tree API over hardcoded paths.
3. Parse `Cargo.lock` for all registry crates and update the `CRATES="..."` variable in the ebuild. Consider delegating to `pycargoebuild` or `cargo-ebuild` rather than implementing lockfile parsing natively.
4. Generate the Manifest using `pkgdev manifest`

#### go

1. Locate the upstream GitHub repository URL from SRC_URI
2. Download the source tarball from the `v${PV}` tag
3. Run `go mod vendor` inside the extracted source to vendor all Go dependencies
4. Package the `vendor/` directory as `${P}-deps.tar.gz`
5. Copy the deps tarball to `DISTDIR`
6. Generate the Manifest using `pkgdev manifest`

#### rpi-kernel (special)

1. Extract the firmware date from the PV (format: `_pYYYYMMDD`)
2. Look up the kernel commit hash from the Raspberry Pi firmware release on GitHub
3. Update the `COMMIT=` variable in the ebuild with the fetched hash
4. Generate the Manifest using `pkgdev manifest`
5. **Rollback:** If manifest generation fails, restore the original `COMMIT=` value

#### generic

1. Generate the Manifest using `pkgdev manifest` directly

---

### `larry import`

Import the latest ebuild from another overlay (e.g., `::gentoo`, `::guru`) into the local overlay.

```larry import [--from <repo>] <cat/pkg>
```

**Arguments:**

| Argument        | Required | Description                                                                                                            |
| --------------- | -------- | ---------------------------------------------------------------------------------------------------------------------- |
| `--from <repo>` | Optional | Source overlay path or repo name. If omitted, searches all overlays in `/var/db/repos/` (excluding the local overlay). |
| `<cat/pkg>`     | Required | The category/package atom to import.                                                                                   |

**Behavior:**

1. Locate the source package directory in the target or auto-detected overlay
2. Find the latest non-live (`-9999`) ebuild in the source
3. Copy the ebuild, `files/` directory (if present), and create a new `metadata.xml`
4. Rewrite `metadata.xml` with the local overlay's maintainer information while preserving any upstream `<remote-id>` tags
5. Prepare the imported ebuild (see `larry prepare`)

**Output files:**
- `${cat}/${pkg}/${pkg}-${PV}.ebuild`
- `${cat}/${pkg}/metadata.xml` (new, with rewritten maintainer)
- `${cat}/${pkg}/files/` (copied if source had one)

---

### `larry test`

Test-build an ebuild locally or in a clean container.

```larry test [options] <cat/pkg>
```

**Options:**

| Option          | Description                                                                                                                                                                                 | Default |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- |
| `--compile, -c` | Run through the compile phase (`clean fetch unpack compile`)                                                                                                                                | —       |
| `--merge, -m`   | Full build and install to a temporary image (`clean fetch unpack compile install`)                                                                                                          | —       |
| `--pretend, -p` | Show what would be run without executing                                                                                                                                                    | Off     |
| `--emerge, -e`  | Run `emerge` in a clean container via Podman with GPU passthrough                                                                                                                           | Local   |
| `--<phase>`     | Run a specific ebuild phase (any valid `do_*` phase: `clean`, `fetch`, `unpack`, `prepare`, `configure`, `compile`, `install`, `test`, `package`, `preinst`, `postinst`, `prerm`, `postrm`) | —       |

**Argument:**

| Argument    | Description                                                                                                                                    |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `<cat/pkg>` | The package atom. May include a version suffix (e.g., `dev-util/pkg-1.2.3`) to target a specific ebuild, or omit it to use the latest version. |

**Behavior:**

#### Local mode (default)

1. Resolve the target ebuild from the overlay tree
2. Disable network sandbox (`-network-sandbox`) to allow distfile downloads during testing
3. Run the specified Portage phase(s) via `ebuild <ebuild-path> <phases...>`
4. Use a temporary work directory under `/tmp/portage-test`

#### Container mode (`--emerge`)

1. Build a Podman image from the overlay's Containerfile (if not already built)
2. Mount the overlay, `DISTDIR`, and `binpkgs` directory into the container
3. Pass through GPU devices (`/dev/kfd`, `/dev/dri`, `--group-add video`)
4. Run `emerge --verbose --autounmask=y` inside the container

**Image name:** `larry-test` (stored in Podman)
**Cache directories:** Cached in `$XDG_CACHE_HOME/larry/` (defaults to `~/.cache/larry/`)

---

### `larry upstream`

Check all packages (or specified packages) for upstream version updates.

```larry upstream [--fresh] [--verbose] [--prereleases] [cat/pkg ...]
```

**Options:**

| Option          | Description                                                                        |
| --------------- | ---------------------------------------------------------------------------------- |
| `--fresh, -f`   | Clear the version cache and fetch all versions fresh from upstream                 |
| `--verbose, -v` | Include UP-TO-DATE packages in output                                              |
| `--prereleases` | Include GitHub prereleases when checking upstream versions                         |
| `[cat/pkg ...]` | Limit check to specific package(s). If omitted, scans all packages in the overlay. |

**Name resolution:**

Identical to `larry bump` — see the "Name mapping" section above. `<remote-id>`
from `metadata.xml` is the primary source of truth for upstream identity.

**Output columns:**

| Column    | Description                                                                                                              |
| --------- | ------------------------------------------------------------------------------------------------------------------------ |
| `Package` | Category/package atom                                                                                                    |
| `Current` | Version currently in the overlay                                                                                         |
| `Latest`  | Latest version detected from upstream                                                                                    |
| `Status`  | One of: `OUTDATED`, `UP-TO-DATE`, `AHEAD` (overlay is newer), `NEWER-TAG`, `CHECK-TAG`, `HAS-RELEASE`, `LIVE`, `UNKNOWN` |
| `Source`  | Upstream source type and identifier (e.g., `GitHub:owner/repo`, `PyPI:package`, `npm:package`)                           |

**Version resolution by package type:**

| Source Type  | Resolution Method                                                                                                                         |
| ------------ | ----------------------------------------------------------------------------------------------------------------------------------------- |
| **PyPI**     | HTTP GET to `/pypi/<name>/json`, parse JSON response for `.info.version`                                 |
| **npm**      | HTTP GET to `/registry/<name>/latest`, parse JSON response for `.version`                                |
| **GitHub**   | GitHub REST API (`/repos/<owner>/<repo>/releases/latest`) + GraphQL tag query with semver-aware sorting. Prereleases filtered by default. |
| **GitLab**   | HTTP GET to `/api/v4/projects/<path>/repository/tags`, parse JSON for first tag name                     |
| **Codeberg** | HTTP GET to `/api/v1/repos/<owner>/<repo>/releases`, parse JSON for first release tag                    |

**Comparison logic:**

Version comparison normalizes Gentoo-style version suffixes into sortable form:
- `_alpha` → `.a`, `_beta` → `.b`, `_pre` → `.c`, `_rc` → `.d`, `_p` (patch) → `.p`
- Plain releases are tagged `.e` to sort after `_rc` but before `_p`
- Tag prefix stripping: removes `v`, `V`, `bun-`, `rocm-`, `release-`, and git annotation markers (`^{...}`)

**Dependency scanning:**

By default, `larry upstream` recursively checks upstream dependencies of each package
(up to 3 levels deep). Dependency atoms are extracted from `DEPEND=` and `RDEPEND=`
lines using the following rules:

1. **Strip operators:** Remove leading `>=`, `<=`, `>`, `<`, `=`, `~` from atoms.
2. **Strip slot suffixes:** Remove `:0`, `:=`, `:*` and similar slot specifiers.
3. **Strip version specs:** Remove `>=1.0`, `=2.0-r1` etc. from the atom itself (the
category/package part remains).
4. **Strip conditionals:** Handle use-flag conditionals like `python? ( dev-lang/foo )` —
extract the inner atom `dev-lang/foo`.
5. **Strip version constraints:** Remove `>=`, `<`, `=`, `~` prefixes from the remaining
atom string.
6. **Match pattern:** Extract strings matching `[a-z][a-z0-9_-]+/[a-z][a-z0-9_-]+`.

**Examples:**

| Dependency string | Extracted atoms |
|-------------------|--------------------|
| `>=dev-libs/foo-1.0` | `dev-libs/foo` |
| `dev-libs/bar:0=` | `dev-libs/bar` |
| `python? ( dev-python/requests )` | `dev-python/requests` |
| `ssl? ( >=dev-libs/openssl-3 ) || ( dev-libs/mbedtls )` | `dev-libs/openssl`, `dev-libs/mbedtls` |
| `app-arch/bzip2[bzip2?]` | `app-arch/bzip2` |

**Implementation note:** A simple regex matching `[a-z][a-z0-9_-]+/[a-z][a-z0-9_-]+`
after stripping operators and slots is sufficient. More sophisticated dependency graph
parsing (e.g., handling virtual packages, slot atoms with `:=` binding, or cross-repo
atoms) is not required.

**Caching:**

- Version queries are cached in `$XDG_CACHE_HOME/larry/overlay-version-check-cache/`
- Cache expires after 6 hours
- Use `--fresh` to bypass cache entirely

**Package skipping:**

- `acct-group/*` and `acct-user/*` packages are automatically skipped (no upstream versions)
- Live (`-9999`) packages are elided from output unless `--verbose` is used

**Color output:**

- `OUTDATED` — red
- `UP-TO-DATE` — green (hidden by default)
- `AHEAD` — blue
- `NEWER-TAG` / `CHECK-TAG` / `HAS-RELEASE` — yellow
- `LIVE` / `UNKNOWN` — dimmed
- Source links (`GitHub:`, `PyPI:`, `npm:`) are hyperlinked using OSC 8 terminal links

---

## Architecture & Design Refinements

### Structured Version Type

**Specification:** The `UpstreamProvider::fetch_latest_version()` method must return a
structured `Version` type rather than a raw string. The `Version` type should support:

- **Parsing:** Parse strings into components (major, minor, patch, suffix, revision)
- **Comparison:** Implement `<`, `>`, `==` using Gentoo version sort semantics
- **Serialization:** Convert back to string representation
- **Normalization:** Apply prefix stripping (remove `v`, `V`, `bun-`, `rocm-`, `release-`, `^{...}`)

```text
struct Version {
    major: u64,
    minor: u64,
    patch: u64,
    suffix: Option<Suffix>,   // Alpha, Beta, Rc, Patch, None
    revision: Option<u64>,    // Gentoo -rN
}

enum Suffix {
    Alpha(u32),
    Beta(u32),
    Pre(u32),
    Rc(u32),
    Patch(u32),
    Release,  // Plain release (no suffix)
}
```

This enables reliable comparison without string manipulation tricks. Use an existing
SemVer or Gentoo-version parsing library in the target language when available.

### Remote Identifier Domain Model

**Specification:** Parse `<remote-id>` tags from `metadata.xml` into a strongly-typed
domain model:

```text
enum RemoteId {
    GitHub { owner: String, repo: String },
    PyPI { name: String },
    NPM { name: String },
    GitLab { host: String, path: String },
    Codeberg { owner: String, repo: String },
    SourceHut { host: String, path: String },
}
```

This type is passed to the `ProviderFactory` to construct the correct `UpstreamProvider`
instance. It is also used in `larry prepare` to determine the GitHub release tag
(`GitHub { owner, repo }`) and upload destination.

### CLI Design

**Specification:** The CLI should use a framework (e.g., Clap for Rust, Cobra for Go)
that supports hierarchical subcommands with shared global flags.

**Global flags:**

| Flag | Type | Purpose |
|------|------|---------|
| `--config` | Path | Path to config file (YAML/TOML/INI) |
| `--verbose, -v` | Count | Increase verbosity (0: silent, 1: info, 2: debug) |
| `--quiet, -q` | Flag | Suppress non-error output |
| `--cache-dir` | Path | Override `$XDG_CACHE_HOME/larry/` |
| `--distdir` | Path | Override DISTDIR |
| `--color` | Auto/Always/Never | Control ANSI color output |
| `--dry-run, -n` | Flag | Perform all operations except external mutations |
| `--jobs, -j` | Usize | Worker pool size for concurrent operations (default: 8) |

**Subcommands:**

```
larry <global-flags> <subcommand> [subcommand-args]
```

All subcommands share the global flag set. Subcommand-specific flags are documented
per-subcommand.

### End-to-End Dry-Run Guarantee

**Specification:** All `larry` operations should be dry-runnable end-to-end locally
without uploading to GitHub or making other externally-observable changes. The
`--dry-run` flag modifies behavior as follows:

| Operation | Normal Behavior | `--dry-run` Behavior |
|-----------|-----------------|----------------------|
| Distfile download | Download to `DISTDIR` | Download to `$XDG_CACHE_HOME/larry/dry-run/` (staging area) |
| Variable resolution | Resolve and use | Resolve and display |
| Tarball creation | Create in `DISTDIR` | Create in staging area, display size |
| GitHub upload | Push to release | Display what would be uploaded; skip upload |
| Ebuild modification | Write to disk | Display diff of changes; skip write |
| Manifest generation | Write `Manifest` file | Display what would be written; skip write |
| API queries | Fetch live data | Fetch live data (read-only, no mutation) |
| Cargo.lock download | Download from upstream | Download to staging area |

**Dry-run staging area:** Use `$XDG_CACHE_HOME/larry/dry-run/` or `~/.local/state/larry/dry-run/`
as a temporary workspace. All artifacts are discarded after the command exits.

**Data-fetching exception:** Read-only operations (API queries for version checking,
`Cargo.lock` downloads, distfile verification) **are** performed in dry-run mode
because they are necessary to validate the operation. Only write and upload operations
are skipped.

---

## Data Flow

```
┌─────────────┐     ┌──────────────┐     ┌─────────────────┐
│  User Input  │────>│  Command     │────>│  Resolve Ebuild │
│  (args)      │     │  Dispatcher  │     │  & Dependencies │
└─────────────┘     └──────────────┘     └────────┬────────┘
                                                   │
                    ┌──────────────────────────────┘
                    │
                    ▼
         ┌────────────────────────┐
         │  Type Detection /      │
         │  Source Resolution     │
         └────────┬───────────────┘
                  │
        ┌─────────┼──────────┬──────────┬────────────┐
        ▼         ▼          ▼          ▼            ▼
     npm       cargo       go       rpi-kernel   generic
        │         │          │          │            │
        ▼         ▼          ▼          ▼            ▼
   Fetch      Fetch       Vendor    Lookup     Manifest
   distfiles  crates      deps      kernel     generation
                  │          │        commit       │
                  ▼          ▼          │          ▼
               Update    Upload     Update       │
               CRATES    deps      COMMIT       │
                  │      to GH        │          │
                  ▼                   ▼          ▼
                            ┌─────────────────┐
                            │    Manifest     │
                            │    (pkgdev)     │
                            └────────┬────────┘
                                     │
                                     ▼
                              ┌─────────────┐
                              │   Complete  │
                              └─────────────┘
```

---

## Version Format

Gentoo version strings supported:

```
PV[-rREVISION]

Examples:
  1.2.3
  1.2.3-r1
  1.2.3_p1        (patch release)
  1.0_alpha1      (alpha)
  1.0_beta2       (beta)
  1.0_rc3         (release candidate)
  9999            (live/ebuild)
```

---

## Exit Codes

| Code | Meaning                                                     |
| ---- | ----------------------------------------------------------- |
| 0    | Success                                                     |
| 1    | Error (invalid arguments, missing files, failed operations) |

---

## Configuration

| Variable                                            | Default           | Description                                                              |
| --------------------------------------------------- | ----------------- | ------------------------------------------------------------------------ |
| `$XDG_CACHE_HOME/larry`                             | `~/.cache/larry/` | Cache directory for distfiles, version queries, and test artifacts       |
| `$XDG_CACHE_HOME/larry/distfiles`                   | —                 | DISTDIR for pre-fetched distfiles                                        |
| `$XDG_CACHE_HOME/larry/overlay-version-check-cache` | —                 | Upstream version query cache (6-hour TTL)                                |
| `$XDG_CACHE_HOME/larry/binpkgs`                     | —                 | Binary package cache for container testing                               |
| GitHub `origin` remote                              | Detected from git | Used to determine the repository for release uploads and version queries |

---

## Constraints

1. **No in-place modification of pushed ebuilds:** The git push hook enforces that ebuilds already on `origin` must not be edited; new revisions must be created instead.
2. **npm distfiles require upload:** npm dependency tarballs must be uploaded to a GitHub release tagged with the package name before `pkgdev manifest` can validate checksums.
3. **No worktree support for this repository:** The overlay repository should not be used as a worktree.
4. **Prerelease filtering:** By default, `larry upstream` and `larry bump` exclude prerelease versions from upstream queries.
5. **Recursive dependency checking:** `larry upstream` checks dependencies up to 3 levels deep; deeper dependencies are not scanned.
6. **Cache coherence:** The 6-hour cache expiration for version queries helps avoid stale data while limiting API rate limits.
7. **Dry-run mode for `larry prepare`:** `larry prepare --dry-run` performs all local operations (distfile download, variable resolution, tarball creation) and reports what would be uploaded or modified, but skips all external mutations (GitHub upload, ebuild modification, manifest generation). Use `--dry-run` to validate the pipeline before committing to remote operations.
8. **External state mutation:** `larry prepare` may create GitHub releases and upload assets that are **not** cleaned up on failure. Orphaned assets require manual removal.
9. **`<remote-id>` is the source of truth:** Both `larry prepare` and `larry upstream` must prefer `<remote-id>` tags in `metadata.xml` over SRC_URI/HOMEPAGE regex scraping for upstream identity resolution.
