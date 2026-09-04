# `larry` — Agent Development Guide

This file documents the project structure, development workflow, and constraints for coding agents.

## Prerequisites (Workstation-Verified)

All required tools are installed and verified on the build workstation: `ebuild`, `pkgdev`, `gh`, `podman`, `npm`, `go`, `git`, `pycargoebuild`, GPU devices (`/dev/kfd`, `/dev/dri/card0`, `/dev/dri/renderD128`)

> Git operations use `go-git` (`github.com/go-git/go-git/v5`) — no `git` CLI subprocess. `gh` CLI is available but GitHub auth is handled natively via `GITHUB_TOKEN`.

> `pycargoebuild` is the preferred Cargo.lock parser.

## Project Structure

```
plan/PLAN.md    — Master plan index, module layout, task list
plan/01-PLAN-FOUNDATION.md — T1-T3: scaffold, config, logging/color, CLI
plan/02-PLAN-CORE-DOMAIN.md  — T4-T6: Version, RemoteId, provider registry
plan/03-PLAN-INFRA.md        - T7-T8: rate limiter, retry, go-git, gh CLI wrapper
plan/03-PLAN-INFRA.md (T8b)   - Provider implementations (github, gitlab, pypi, npm, codeberg, sourcehut)
plan/04-PLAN-EBUILD.md       — T9: portageq wrapper, variable resolution, type detection
plan/05-PLAN-UPSTREAM.md     — T10-T11: upstream command, cache, dep scanning
plan/06-PLAN-BUMP.md         - T12a: bump file creation; T12b: bump + prepare invocation
plan/07-PLAN-PREPARE.md      — T13-T18: prepare command (all 5 type pipelines)
plan/08-PLAN-IMPORT.md       — T19: import command
plan/09-PLAN-TEST.md         — T20-T21: test command (local + podman)
plan/10-PLAN-TUI.md          — T22: bubbletea TUI
plan/11-PLAN-POLISH.md       — T23: integration tests, docs, packaging
plan/SPEC.md                 — Full specification
README.md               — End-user: commands, dependencies
AGENTS.md               — This file
```

Each plan file is scoped to a single coding agent session. Execute tasks in numbered order.

## Build & Test

```bash
make test          # unit tests (go test ./...)
make build         # compile binary
make install       # install to /usr/local/bin/larry
make lint          # go vet + staticcheck if available
```

## TDD Discipline

Every task requires tests defined *before* implementation. Each plan file lists the specific tests to write. Golden file fixtures live in `testdata/`.

## Task Execution Order

| Phase         | Tasks   | File                     |
| ------------- | ------- | ------------------------ |
| Foundation    | T1–T3   | `01-PLAN-FOUNDATION.md`  |
| Core domain   | T4–T6   | `02-PLAN-CORE-DOMAIN.md` |
| HTTP/infra    | T7–T8   | `03-PLAN-INFRA.md`       |
| Provider impl | T8b     | `03-PLAN-INFRA.md`       |
| Ebuild helper | T9      | `04-PLAN-EBUILD.md`      |
| Upstream cmd  | T10–T11 | `05-PLAN-UPSTREAM.md`    |
| Bump cmd (a)  | T12a    | `06-PLAN-BUMP.md`        |
| Bump cmd (b)  | T12b    | `06-PLAN-BUMP.md`        |
| Prepare cmd   | T13–T18 | `07-PLAN-PREPARE.md`     |
| Import cmd    | T19     | `08-PLAN-IMPORT.md`      |
| Test cmd      | T20–T21 | `09-PLAN-TEST.md`        |
| TUI           | T22     | `10-PLAN-TUI.md`         |
| Polish        | T23     | `11-PLAN-POLISH.md`      |

## External Tool Delegation Policy

**Delegate to external tools:**
- `ebuild` — full Portage environment required
- `pkgdev manifest` — deep Portage digest integration
- `podman` — GPU passthrough, cgroup management
- `npm`, `go` — language-specific dependency resolution
- `pycargoebuild` — preferred Cargo.lock → CRATES parser
- `portageq` — variable resolution within Portage environment

**Native implementations (no subprocess):**
- `go-git` — remote resolution, tag enumeration, commit lookup (replaces `git` CLI)
- `net/http` — HTTP requests (not curl)
- `encoding/json` — JSON parsing (not jq)
- stdlib regex/string ops — text processing (not sed/grep/awk)
- `archive/tar`, `compress/gzip` — archiving (not tar)
- `os.MkdirTemp` — temp directories (not mktemp)
- `filepath.Walk` — file finding (not find)
- native comparison — version sorting (not sort -V)

## Key Go Dependencies

| Package                              | Purpose                  |
| ------------------------------------ | ------------------------ |
| `github.com/spf13/cobra`             | CLI framework            |
| `github.com/spf13/viper`             | Configuration            |
| `github.com/charmbracelet/bubbletea` | TUI                      |
| `github.com/charmbracelet/lipgloss`  | TUI styling              |
| `github.com/muesli/termenv`          | Terminal color detection |
| `github.com/go-git/go-git/v5`        | Native git operations    |

