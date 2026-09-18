# Architecture

## High-Level Design

```
┌─────────────────────────────────────────────────────────────────┐
│                         larry CLI                                │
│  upstream │ bump │ prepare │ import │ test │ --config │ --dry-run│
└────┬────────────────────────────────────────────────────────────┘
     │
     ├── cmd/larry/              ── Cobra command definitions
     │   ├── root.go             ── Root command, global flags, config
     │   ├── upstream.go         ── upstream subcommand
     │   ├── bump.go             ── bump subcommand
     │   ├── prepare.go          ── prepare subcommand
     │   ├── import.go           ── import subcommand
     │   ├── test.go             ── test subcommand
     │   └── main.go             ── Entry point
     │
     ├── internal/cli/           ── TUI models (bubbletea)
     │   ├── upstream_tui.go     ── upstream interactive UI
     │   └── prepare_tui.go      ── prepare progress UI
     │
     ├── internal/ebuild/        ── Ebuild parsing and info
     │   ├── info.go             ── Parse ebuild metadata
     │   ├── detect.go           ── Auto-detect package type
     │   ├── srcuri.go           ── SRC_URI expansion
     │   └── portageq.go         ── Portage variable resolution
     │
     ├── internal/upstream/      ── Version checking
     │   ├── upstream.go         ── Version comparison logic
     │   ├── output.go           ── Table rendering
     │   ├── cache.go            ── Version cache (JSON)
     │   └── dependencies.go     ── Atom extraction, DFS
     │
     ├── internal/bump/          ── Ebuild bumping
     │   └── bump.go             ── Create new versioned ebuild
     │
     ├── internal/prepare/       ── Prepare pipelines
     │   ├── prepare.go          ── Type dispatch, staging, rollback
     │   ├── cargo.go            ── Cargo pipeline
     │   ├── npm.go              ── npm pipeline
     │   ├── go.go               ── Go module pipeline
     │   ├── vendored.go         ── Vendored pipeline
     │   └── generic.go          ── Generic pipeline
     │
     ├── internal/importcmd/     ── Import command
     │   └── import.go           ── Copy ebuilds, merge metadata
     │
     ├── internal/test/          ── Test command
     │   ├── local.go            ── Local ebuild execution
     │   └── container.go        ── Podman container testing
     │
     ├── internal/provider/      ── Provider framework
     │   ├── interface.go        ── Provider interface
     │   ├── registry.go         ── Provider registry
     │   └── factory.go          ── Provider factory
     │
     ├── internal/providers/     ── Provider implementations
     │   ├── github.go           ── GitHub provider
     │   ├── gitlab.go           ── GitLab provider
     │   ├── pypi.go             ── PyPI provider
     │   ├── npm.go              ── npm provider
     │   ├── codeberg.go         ── Codeberg provider
     │   └── sourcehut.go        ── SourceHut provider
     │
     ├── internal/github/        ── GitHub operations
     │   └── github.go           ── Releases, tags, file download
     │
     ├── internal/git/           ── Native git operations
     │   └── git.go              ── go-git wrapper
     │
     ├── internal/config/        ── Configuration
     │   └── config.go           ── YAML config, defaults
     │
     ├── internal/output/        ── Output helpers
     │   └── color.go            ── Terminal color support
     │
     └── internal/remoteid/      ── Remote ID / metadata
         ├── remoteid.go         ── Remote ID resolution
         └── metadata.go         ── metadata.xml handling
```

## Key Design Decisions

### 1. No Hardcoded Package Logic

Package-specific logic is implemented as `files/prepare.sh` scripts rather than
hardcoded in Go. This follows the Unix philosophy and prevents unnecessary
`app-portage/larry` version bumps for package-specific changes.

### 2. Native Go Implementations

Where possible, larry uses native Go libraries instead of spawning subprocesses:

- `go-git` — remote resolution, tag enumeration (replaces `git` CLI)
- `net/http` — HTTP requests (replaces `curl`)
- `encoding/json` — JSON parsing (replaces `jq`)
- `regexp` — text processing (replaces `sed`/`grep`)

External tools are delegated via subprocess only when native implementation
is impractical or requires full Portage environment:

- `ebuild` — phase execution
- `pkgdev` — manifest generation
- `podman` — container testing

### 3. Provider Interface

All upstream version providers implement a common interface:

```go
type Provider interface {
    Name() string
    Resolve(ctx context.Context, client *http.Client, uri string, prereleases bool) (string, error)
}
```

This allows easy addition of new providers.

### 4. Prepare Pipelines

Each package type (cargo, npm, go, vendored, generic) has its own pipeline
implemented in a separate file. The `prepare.go` dispatcher selects the
appropriate pipeline based on ebuild analysis.

Pipelines follow a staging → execute → commit pattern with automatic
rollback on failure.

### 5. TDD Workflow

All code follows strict TDD:
1. Write failing test (red)
2. Implement minimum code to pass (green)
3. Refactor

Golden file fixtures live in `testdata/`.

### 6. Test Abstractions

- `CmdRunner` interface — testable subprocess execution
- `HTTPClient` injection — testable HTTP requests
- `StubRunner` — mock command execution for tests
- HTTP test servers — deterministic provider tests

### 7. Color Support

Color output is controlled via `--color` flag (auto/always/never)
and terminal detection via `termenv`. Non-TTY terminals default to no color.

## File Flow

### Upstream Check

```
ebuilds → detect type → resolve SRC_URI → provider.Resolve() → cache → table output
```

### Bump + Prepare

```
bump → create new ebuild → prepare pipeline (type-specific) → manifest
```

### Test

```
local mode: ebuild phase → subprocess execution
container mode: Podman build → bind mounts → ebuild phase → cleanup
```

## Dependencies

### Core
- `github.com/spf13/cobra` — CLI framework
- `github.com/spf13/viper` — Configuration
- `github.com/go-git/go-git/v5` — Native git operations

### TUI
- `github.com/charmbracelet/bubbletea` — Terminal UI
- `github.com/charmbracelet/lipgloss` — Terminal styling
- `github.com/muesli/termenv` — Terminal color detection

### Testing
- Standard library `testing` package
- `net/http/httptest` — HTTP test servers
- `os/exec` — Subprocess execution (wrapped via CmdRunner interface)
