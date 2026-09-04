# `larry` — Implementation Plan (Index)

**Stack:** Go 1.22+ · cobra · viper · bubbletea · termenv
**Test discipline:** TDD at every stage — tests defined *before* implementation, green before merge.

## Module Structure

```
.
├── cmd/larry/
│   ├── main.go
│   ├── root.go
│   ├── bump.go
│   ├── prepare.go
│   ├── import.go
│   ├── test.go
│   └── upstream.go
├── internal/
│   ├── config/
│   │   └── config.go          # Viper config + Config struct
│   ├── version/
│   │   └── version.go         # Version type, parse, compare, normalize
│   ├── remoteid/
│   │   ├── remoteid.go        # RemoteId model + RemoteIdType
│   │   └── metadata.go        # metadata.xml XML parser
│   ├── provider/
│   │   ├── interface.go       # UpstreamProvider interface + ProviderMetadata
│   │   ├── registry.go        # ProviderRegistry (type → factory map)
│   │   └── factory.go         # ResolveProvider(RemoteId)
│   ├── providers/
│   │   ├── github.go          # GitHubProvider
│   │   ├── gitlab.go          # GitLabProvider
│   │   ├── pypi.go            # PyPIProvider
│   │   ├── npm.go             # NpmProvider
│   │   ├── codeberg.go        # CodebergProvider
│   │   └── sourcehut.go       # SourceHutProvider
│   ├── ratelimit/
│   │   ├── ratelimiter.go     # Per-host + global semaphore
│   │   └── retry.go           # Exponential backoff retry
│   ├── httpclient/
│   │   └── client.go          # Shared HTTP client with rate-limit helpers
│   ├── git/
│   │   └── git.go             # go-git repo access: remotes, tags, commits
│   ├── github/
│   │   └── github.go          # gh CLI wrapper: releases, upload, download (subprocess)
│   ├── ebuild/
│   │   ├── ebuild.go          # EbuildInfo struct
│   │   ├── portageq.go        # portageq wrapper
│   │   ├── info.go            # ebuild info parser
│   │   ├── srcuri.go          # SRC_URI resolution + mirror://npm/
│   │   └── detect.go          # Type detection
│   ├── prepare/
│   │   ├── prepare.go         # Entry point + dispatch
│   │   ├── generic.go         # Generic pipeline
│   │   ├── npm.go             # NPM pipeline
│   │   ├── cargo.go           # Cargo pipeline
│   │   ├── go.go              # Go pipeline
│   │   ├── rpi_kernel.go      # RPi kernel pipeline
│   │   ├── manifest.go        # pkgdev manifest runner
│   │   ├── rollback.go        # .bak backup/restore
│   │   └── staging.go         # Dry-run staging area
│   ├── bump/
│   │   └── bump.go            # Version/revision bump
│   ├── upstream/
│   │   ├── upstream.go        # Core upstream check
│   │   ├── cache.go           # Version query cache
│   │   ├── dependencies.go    # Recursive dep scanning
│   │   └── output.go          # Table renderer with colors
│   ├── import/
│   │   └── import.go          # Import pipeline
│   ├── test/
│   │   ├── test.go            # Entry point
│   │   ├── local.go           # Local mode
│   │   └── container.go       # Podman mode
│   ├── output/
│   │   ├── logger.go          # Structured logger
│   │   ├── color.go           # Color helpers (termenv)
│   │   └── progress.go        # ProgressBar + Spinner
│   └── cli/
│       ├── upstream_tui.go    # Bubbletea upstream TUI
│       └── prepare_tui.go     # Bubbletea prepare TUI
├── docs/
│   ├── commands.md
│   ├── contributing.md
│   ├── architecture.md
│   └── man/
├── .github/workflows/
│   └── ci.yml
├── Makefile
├── go.mod
├── go.sum
├── README.md
└── testdata/                  # Golden files, test fixtures
```

No external deps for: version parsing, metadata.xml parsing, rate limiting, HTTP client, cargo lock parsing.

## Overall Task List

| # | Phase | File | Status |
|---|-------|------|--------|
| 1 | Foundation | `01-PLAN-FOUNDATION.md` | ✅ |
| 2 | Core domain | `02-PLAN-CORE-DOMAIN.md` | ⬜ |
| 3 | HTTP/infra + providers | `03-PLAN-INFRA.md` | ⬜ |
| 3b | Provider implementations | `03-PLAN-INFRA.md` | ⬜ |
| 4 | Ebuild helper | `04-PLAN-EBUILD.md` | ⬜ |
| 5 | Upstream cmd | `05-PLAN-UPSTREAM.md` | ⬜ |
| 6 | Bump cmd (a) | `06-PLAN-BUMP.md` (T12a) | ⬜ |
| 6b | Bump cmd (b) | `06-PLAN-BUMP.md` (T12b) | ⬜ |
| 7 | Prepare cmd | `07-PLAN-PREPARE.md` | ⬜ |
| 8 | Import cmd | `08-PLAN-IMPORT.md` | ⬜ |
| 9 | Test cmd | `09-PLAN-TEST.md` | ⬜ |
| 10 | TUI | `10-PLAN-TUI.md` | ⬜ |
| 11 | Polish | `11-PLAN-POLISH.md` | ⬜ |

## Testing Strategy

| Level | Tool | Coverage |
|-------|------|----------|
| Unit | `go test` | Every `internal/` package, 80%+ coverage |
| Table-driven | `go test` tables | All parsing, comparison, detection logic |
| Mock HTTP | `net/http/httptest` | Provider API calls |
| Mock subprocess | `os/exec` stubs | `portageq`, `ebuild`, `pkgdev`, `gh`, `podman`, `npm`, `go` |
| Integration | `go test` with test overlay | Full command pipelines |
| Golden files | `testdata/` fixtures | Version parse/compare, metadata.xml parsing |

## Remaining Risks (Workstation-Verified)

All workstation-dependent risks are resolved — `portageq`, `ebuild`, `pkgdev`, `gh`, `podman`, `npm`, `go`, `git`, `pycargoebuild` are all installed and GPU passthrough devices are available. Only non-workstation risks remain:

| Risk | Impact | Mitigation |
|------|--------|------------|
| SourceHut version discovery | No conventional "latest release" REST endpoint | Research git-refs-via-protocol or GraphQL API; may need fallback to tag enumeration |
| GitHub rate limits in tests | Block provider tests | Use `gh` CLI which handles its own rate limiting; mock `os/exec` for tests |
