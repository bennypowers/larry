# `larry` — Implementation Plan (Index)

**Stack:** Go 1.22+ · cobra · viper · bubbletea · termenv
**Test discipline:** TDD — tests before implementation, green before merge.

## Module Structure

```
.
├── cmd/larry/              # CLI entry points
│   ├── main.go
│   ├── root.go
│   ├── bump.go
│   ├── prepare.go
│   ├── import.go
│   ├── test.go
│   └── upstream.go
├── internal/
│   ├── config/             # Viper config
│   ├── version/            # Version parse/compare
│   ├── remoteid/           # metadata.xml parser + RemoteId model
│   ├── provider/           # UpstreamProvider interface + registry
│   ├── providers/          # github, gitlab, pypi, npm, codeberg, sourcehut
│   ├── ratelimit/          # Token bucket + retry/backoff
│   ├── httpclient/         # Shared HTTP client
│   ├── git/                # go-git: remotes, tags, commits
│   ├── github/             # gh CLI wrapper (releases, upload, download)
│   ├── ebuild/             # portageq, info parser, SRC_URI, type detection
│   ├── prepare/            # Generic, npm, cargo, go, rpi-kernel pipelines
│   ├── bump/               # Version/revision bump
│   ├── upstream/           # Version check + cache + dep scanning + output
│   ├── importcmd/          # Import pipeline
│   ├── test/               # Local + podman test runners
│   ├── output/             # Logger, color, progress
│   └── cli/                # TUI models (bubbletea)
├── docs/                   # commands.md, contributing.md, architecture.md, man/
├── Containerfile.larry-test
├── Makefile
├── go.mod
├── go.sum
├── README.md
└── testdata/               # Golden files, fixtures
```

## Task List

| # | Task | File | Status |
|---|------|------|--------|
| 1 | Foundation (scaffold, config, logging, CLI) | `01-PLAN-FOUNDATION.md` | ✅ |
| 2 | Core domain (version, remoteid, provider registry) | `02-PLAN-CORE-DOMAIN.md` | ✅ |
| 3 | Infra: rate limiter, retry, HTTP client | `03-PLAN-INFRA.md#T7` | ✅ |
| 3b | go-git helper + gh CLI wrapper | `03-PLAN-INFRA.md#T8` | ✅ |
| 3c | Provider implementations (6 providers) | `03-PLAN-INFRA.md#T8b` | ✅ |
| 4 | Ebuild helper (portageq, info, SRC_URI, type detection) | `04-PLAN-EBUILD.md` | ✅ |
| 5 | Upstream command (providers + CLI + cache + deps + scanning) | `05-PLAN-UPSTREAM.md` | ✅ (T10, T11) |
| 6a | Bump: file creation (no prepare dep) | `06-PLAN-BUMP.md#T12a` | ✅ |
| 6b | Bump: invoke prepare on new ebuild | `06-PLAN-BUMP.md#T12b` | 🔴 |
| 7 | Prepare: generic + npm + cargo + go | `07-PLAN-PREPARE.md#T13-T16` | ✅ |
| 7b | Prepare: npm pipeline | `07-PLAN-PREPARE.md#T14` | 🔴 |
| 7c | Prepare: cargo pipeline | `07-PLAN-PREPARE.md#T15` | 🔴 |
| 7d | Prepare: go pipeline | `07-PLAN-PREPARE.md#T16` | 🔴 |
| 7e | Prepare: rpi-kernel pipeline | `07-PLAN-PREPARE.md#T17` | 🔴 |
| 7f | Prepare: vendored pipeline (mixed deps) | `07-PLAN-PREPARE.md#T18` | 🔴 |
| 7g | Prepare: dry-run/staging mode | `07-PLAN-PREPARE.md#T19` | 🔴 |
| 8 | Import command | `08-PLAN-IMPORT.md` | ✅ |
| 9 | Test: local mode | `09-PLAN-TEST.md#T20` | ✅ |
| 9b | Test: podman container mode + Containerfile | `09-PLAN-TEST.md#T21` | ✅ |
| 10 | TUI (bubbletea progress + results) | `10-PLAN-TUI.md` | 🔴 |
| 11 | Polish (integration tests, docs, packaging) | `11-PLAN-POLISH.md` | 🔴 |

## Key Dependencies

- T7: None
- T8: T3 (logger)
- T8b: T6, T7, T8
- T9: T3
- T10: T4–T8b (HTTP client `NewClientWithTransport()` factory needed for httptest)
- T11: T10
- T12a: T4, T9
- T12b: T12a, T13
- T13: T9
- T14: T13, T8, T8b
- T15: T8, T8b, T9
- T16: T8, T8b, T9
- T17: T8, T8b, T9
- T18: T13, T8, T8b (vendored pipeline)
- T19: T9 (ebuild parsing)
- T20: T9 (ebuild, portageq)
- T21: T20
- T21: T20
- T22: T10
- T23: T1–T22

## Testing Strategy

| Level | Tool | Coverage |
|-------|------|----------|
| Unit | `go test` | Every `internal/` package, 80%+ |
| Table-driven | `go test` tables | Parsing, comparison, detection |
| Mock HTTP | `net/http/httptest` | Provider API calls |
| Mock subprocess | `os/exec` stubs (`CmdRunner` interface) | `portageq`, `ebuild`, `pkgdev`, `gh`, `podman`, `npm`, `go` |
| Integration | `go test` + test overlay | Full command pipelines |
| Golden files | `testdata/` | Version parse/compare, metadata.xml |

## Known Risks (Resolved)

All workstation tools verified: `portageq`, `ebuild`, `pkgdev`, `gh`, `podman`, `npm`, `go`, `git`, `pycargoebuild`, GPU devices (`/dev/kfd`, `/dev/dri/card0`, `/dev/dri/renderD128`).

## Cross-Task Conventions

- **TUI stream isolation**: When `useTUI` is true, all subprocess stdout/stderr piped via `StdoutPipe()`/`StderrPipe()` — never inherit `os.Stdout`/`os.Stderr`. Read into buffer, display post-execution.
- **Rollback**: Every mutating pipeline backs up `.bak` before changes, restores on failure.
- **Dry-run**: Staging dir at `$XDG_CACHE_HOME/larry/dry-run/`. No external mutations. Exit 0 if all ops would succeed, exit 1 if any would fail.
- **Asset collision** (npm/go upload): `gh release view --json assets` → parse → SHA-256 compare → skip/match → `--clobber`/mismatch → `--clobber`.
- **`gh` CLI auth**: `GITHUB_TOKEN` passed through env to subprocesses. No `gh auth login` needed.

## External Tool Delegation

**Delegate to external tools:** `ebuild`, `pkgdev manifest`, `podman` (GPU passthrough), `npm`/`go` (language dep resolution), `pycargoebuild` (Cargo.lock → CRATES).

**Native (no subprocess):** `go-git` (remote resolution, tags), `net/http` (HTTP), `encoding/json` (JSON), stdlib (regex, archiving, walking).
