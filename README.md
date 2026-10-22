# larry

> Gentoo overlay upstream tooling

Larry helps Gentoo maintainers check for upstream version updates, prepare ebuild bumps,
and manage the ebuild lifecycle with provider-specific pipelines.

![Larry the Cow](https://wiki.gentoo.org/images/thumb/b/b8/Larry-nefarius-v2.svg/600px-Larry-nefarius-v2.svg.png)

## Commands

| Command | Description |
|---------|-------------|
| `larry manage` | Interactive session: check, bump, prepare, test |
| `larry upstream` | Check packages for upstream version updates |
| `larry bump` | Create a new versioned ebuild from the latest |
| `larry prepare` | Download distfiles, generate deps tarballs, upload, manifest |
| `larry import` | Import an ebuild from another overlay |
| `larry test` | Test-build an ebuild locally or in a podman container |

## Installation

```bash
make install    # installs to $GOPATH/bin or /usr/local/bin
```

Or build from source:

```bash
go build -o larry ./cmd/larry
```

## Quick Start

### Check upstream versions

```bash
# Check the overlay resolved from the current git repository
larry upstream

# Check specific packages
larry upstream dev-python/requests dev-lisp/sbcl

# Include pre-release versions and show every status
larry upstream --prereleases -v

# Machine-readable output
larry upstream --output json | jq '.results[] | select(.status == "outdated")'

# Interactive session: check, bump, prepare, and test in one view
larry manage

# In the session: b=bump, p=prepare, t=test, R=refresh, enter=details
larry manage dev-util/cem

# Check a specific overlay
larry upstream /path/to/my-overlay
```

### Manage packages interactively

Bare `larry` opens `larry manage` when both stdin and stdout are terminals, and
prints help otherwise. `larry upstream` never starts a TUI, so piped and CI
runs stay plain.

Checks run concurrently (`--jobs`, 0 = auto). GitHub and SourceHut repositories
are batch-resolved in as few API calls as possible; other providers use the
worker pool.

### Bump a package

```bash
# Auto-detect latest version and create new ebuild
larry bump dev-python/requests

# Bump to a specific version
larry bump dev-python/requests --version 2.31.0

# Dry run to see what would happen
larry bump dev-python/requests --dry-run

# Bump revision instead of version
larry bump dev-python/requests --revision
```

### Prepare an ebuild

```bash
# Download distfiles and generate dependency tarballs
larry prepare dev-python/requests

# Dry run
larry prepare dev-python/requests --dry-run

# Force a specific package type
larry prepare dev-python/requests --type npm
```

### Import from another overlay

```bash
larry import --from /var/db/repos/gentoo dev-python/requests
```

### Test an ebuild

```bash
# Local testing
larry test dev-python/requests --compile

# Container-based testing (requires podman)
larry test dev-python/requests --emerge

# Pretend mode
larry test dev-python/requests --pretend
```

## Custom prepare scripts

For packages with non-standard prepare logic, drop a `.larry/prepare.sh` script
into the package directory. Larry executes it instead of any built-in pipeline.

```bash
# sys-kernel/raspberrypi-kernel/.larry/prepare.sh
#!/bin/sh
date=$(echo "$PV" | grep -oP '_p\K\d{8}$')
date=$(echo $date | sed 's/^\(\.{4}\)\(\.{2}\)\(\.{2}\)$/\1-\2-\3/')
commit=$(curl -s "api.github.com/repos/raspberrypi/firmware/releases" \
         | jq -r ".[] | select(.created_at | startswith(\"$date\")) | .body" \
         | grep -oP '(?:commit|hash)\s*[:=]\s*\K[0-9a-f]{8,40}' | head -1)
sed -i "s/COMMIT=\"\([^\"]*\)\"/COMMIT=\"$commit\"/" "$EBUILD"
```

**Environment variables:** `EBUILD`, `PN`, `PV`, `P`, `CATEGORY`, `DISTDIR`, `LARRY_PREPARE=1`

**Working directory:** the ebuild's parent directory

**Dry-run:** detected but script is NOT executed.

## Configuration

Larry reads configuration from `$XDG_CONFIG_HOME/larry/config.yaml`:

```yaml
color: auto        # auto, always, never
dry_run: false     # default dry-run mode
overlay:           # default overlay path
verbose: 0         # 0=results, 1=info, 2=debug, 3=trace
cache_dir:         # version query cache directory
jobs: 0            # upstream check workers (0 = auto-detect)
github_token:      # GitHub PAT for API rate limits
remote: origin     # git remote for upstream resolution
```

Command-line flags override config values. Use `--config` to specify a custom path.

## Provider Support

Larry auto-detects upstream providers from `SRC_URI`:

| URI Prefix | Provider | Resolution |
|------------|----------|------------|
| `mirror://pypi/` | PyPI | `pypi.org` JSON API |
| `mirror://npm/` | npm | `registry.npmjs.org` |
| `mirror://github/` | GitHub | GitHub API + tags |
| `mirror://gitlab/` | GitLab | GitLab API |
| `mirror://codeberg/` | Codeberg | Codeberg API |
| `mirror://sourcehut/` | SourceHut | SourceHut API |

## Ebuild Types

| Type | INHERIT | Pipeline |
|------|---------|----------|
| `cargo` | `cargo` | Download `Cargo.lock`, parse deps, update `CRATES` |
| `npm` | `npm` | Download `package-lock.json`, update `NPM_DEPS` |
| `go` | `go-module` | `go mod vendor`, create vendor tarball |
| `vendored` | `vendor` | Download vendor tarball from SRC_URI |
| `generic` | (none) | Download from `SRC_URI`, manifest |

## Dependencies

| Tool | Purpose |
|------|---------|
| `ebuild` (Portage) | Phase execution, manifest generation |
| `pkgdev` (Portage contrib) | `pkgdev manifest` |
| `gh` (GitHub CLI) | GitHub API auth + releases |
| `podman` | Container testing with GPU passthrough |
| `npm` | npm dependency resolution |
| `go` | Go module vendoring |
| `git` | Remote resolution, tag operations |
| `pycargoebuild` | Cargo lockfile → CRATES parsing |

## Shell Completion

Generate shell completion scripts:

```bash
# Bash
larry completion bash > /usr/share/bash-completion/completions/larry

# Zsh
larry completion zsh > /usr/share/zsh/site-functions/_larry

# Fish
larry completion fish > ~/.config/fish/completions/larry.fish
```

## Documentation

- [Commands Reference](docs/commands.md)
- [Architecture](docs/architecture.md)
- [Contributing](docs/contributing.md)
- [Man Pages](docs/man/)
