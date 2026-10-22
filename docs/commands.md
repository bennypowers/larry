# Commands Reference

## `larry upstream`

Check packages in a Gentoo overlay for upstream version updates.

### Synopsis

```bash
larry upstream [cat/pkg...] [overlay-path] [flags]
```

### Description

Walks the specified overlay directory and checks each ebuild against its upstream source.
Live versions (9999) are reported separately. Packages in `acct-group/*` and `acct-user/*`
are skipped.

Providers are auto-detected from `SRC_URI`:

| SRC_URI Prefix | Provider |
|----------------|----------|
| `mirror://pypi/` | PyPI |
| `mirror://npm/`, `registry.npmjs.org/` | npm |
| `mirror://github/` | GitHub |
| `mirror://gitlab/` | GitLab |
| `mirror://codeberg/` | Codeberg |
| `mirror://sourcehut/` | SourceHut |

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-f, --fresh` | `false` | Bypass cache and fetch fresh versions |
| `-p, --prereleases` | `false` | Include pre-release versions |
| `-v, --verbose` | 0 | Count: `-v` shows all statuses, `-vv` debug, `-vvv` trace |
| `--remote` | `origin` | Git remote name for upstream resolution |
| `--overlay` | `$LARRY_OVERLAY` | Overlay root path |
| `--output` | `text` | `text` or `json` (JSON always includes every status) |
| `--tui` | auto | Force the interactive table |
| `--no-tui` | `false` | Force plain table output |
| `-h, --help` | | Help for upstream |

### Examples

```bash
# Check the overlay resolved from the current git repository
larry upstream

# Check specific packages
larry upstream dev-python/requests dev-lisp/sbcl

# Include prereleases and show every status
larry upstream --prereleases -v

# Machine-readable output
larry upstream --output json | jq '.results[] | select(.status == "outdated")'

# Interactive TUI (also the default when stdin and stdout are terminals)
larry upstream --tui

# Force plain output in a terminal
larry upstream --no-tui

# Check a specific overlay
larry upstream /var/db/repos/my-overlay
```

### Interactive actions

When the table is interactive, the selected row supports:

| Key | Action |
|-----|--------|
| `b` | Bump the package (`larry bump <cat/pkg>`) |
| `p` | Prepare the package (`larry prepare <cat/pkg>`) |
| `t` | Run `larry test --compile <cat/pkg>` |
| `R` | Re-run the upstream check |
| `enter` | Show source URL, target, error, and depth |
| `?` | Toggle full key help |
| `q` / `Esc` / `Ctrl+C` | Quit |

Mutating actions ask for confirmation. Output streams into the view; a
successful action refreshes the table automatically.

### Output

Results are displayed as a colored table:

| Column | Description |
|--------|-------------|
| PACKAGE | Category/package name |
| CURRENT | Installed version |
| LATEST | Latest upstream version |
| STATUS | UP-TO-DATE (green), OUTDATED (red), LIVE (blue), UNKNOWN (yellow) |
| SOURCE | Upstream source provider |

### Per-package upstream rules

A package can override upstream resolution with `<cat>/<pkg>/.larry/config.yaml`:

```yaml
upstream:
  strategy: tags            # GitHub only: resolve from tags, not releases
  tag_match: '^release-(?P<V>\d+\.\d+)$'
  version_template: '${V}'
  ignore_tags: ['^nightly$']
  strip_prefixes: ['stable_']
  check: false              # vendor-only package: report MANUAL, skip lookup
```

`strategy` accepts `""`, `releases`, or `semver` (the default release-first
cascade) and `tags` (skip GitHub releases and resolve from remote tags, falling
back to dated tags). In `tags` mode, tags whose GitHub release is marked as a
prerelease are ignored unless `--prereleases` is passed. `tag_match` applies to
raw tag names and its named captures feed `version_template`.

`check: false` is for packages with no machine-readable upstream (vendor
binary drivers, firmware downloaded by hand). They report `MANUAL`, are hidden
from the default table, and link to `HOMEPAGE` with `-v`; `larry bump` refuses
auto-versioning for them.

## `larry bump`

Create a new versioned ebuild from the latest upstream version.

### Synopsis

```bash
larry bump [category/package] [flags]
```

### Description

Checks the upstream version for the specified package and creates a new ebuild
file with the updated version. Preserves the existing ebuild structure including
`SRC_URI`, `HOMEPAGE`, `LICENSE`, `DEPEND`, and `RDEPEND`.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-p, --prereleases` | `false` | Include pre-release versions in lookup |
| `--version` | | Specific version to bump to |
| `-r, --revision` | `false` | Bump revision (PVR) instead of version |
| `--dry-run` | `false` | Show what would be done |
| `--overlay` | `$LARRY_OVERLAY` | Overlay root path |
| `-h, --help` | | Help for bump |

### Examples

```bash
# Auto-detect latest version
larry bump dev-python/requests

# Bump to specific version
larry bump dev-python/requests --version 2.31.0

# Bump revision number
larry bump dev-python/requests --revision

# Dry run
larry bump dev-python/requests --dry-run
```

### Output

Creates a new ebuild file at `category/package/package-version.ebuild` in the overlay.

## `larry prepare`

Download distfiles, generate dependency tarballs, and create manifests.

### Synopsis

```bash
larry prepare [category/package] [ebuild...] [flags]
```

### Description

Runs the appropriate pipeline based on the ebuild's `INHERIT` line:

| INHERIT | Pipeline |
|---------|----------|
| `cargo` | Download `Cargo.lock`, parse deps, update `CRATES` |
| `npm` | Download `package-lock.json`, update `NPM_DEPS` |
| `go-module` | `go mod vendor`, create vendor tarball |
| `vendor` | Download vendor tarball from SRC_URI |
| (none) | Generic: manifest only (no pipeline) |

#### Escape hatch: `.larry/prepare.sh`

Any package type can be handled by placing an executable `.larry/prepare.sh`
script in the ebuild's directory. The script runs with a sandboxed environment
(PATH restricted, dangerous vars stripped) and receives `EBUILD`, `PN`, `PV`,
`P`, `CATEGORY`, and `DISTDIR` as environment variables.

Example reference script: [rpi-kernel-prepare.sh](../docs/examples/rpi-kernel-prepare.sh)
for `sys-kernel/raspberrypi-kernel` ebuilds.

If a custom script is present, it runs **before** any built-in pipeline.
The framework automatically regenerates the Manifest after the script succeeds,
and rolls back the ebuild on any failure.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-a, --all` | `false` | Prepare all ebuilds in the directory |
| `-n, --dry-run` | `false` | Show what would be done |
| `-t, --type` | auto | Force type: npm, cargo, go, vendored, generic (rpi-kernel requires a custom `.larry/prepare.sh` script) |
| `--distdir` | `$LARRY_DISTDIR` | Distdir path |
| `--overlay` | `$LARRY_OVERLAY` | Overlay root path |
| `-h, --help` | | Help for prepare |

### Examples

```bash
# Prepare a specific package
larry prepare dev-python/requests

# Dry run
larry prepare dev-python/requests --dry-run

# Force type
larry prepare dev-python/requests --type npm

# Prepare all packages in a category
larry prepare dev-python/requests --all
```

## `larry import`

Import an ebuild from another overlay.

### Synopsis

```bash
larry import [category/package] [flags]
```

### Description

Copies the ebuild and `files/` directory from a source overlay to the local overlay.
Generates or merges `metadata.xml` (preserving upstream `<remote-id>` and adding
local maintainers).

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--from` | | Source overlay path (auto-detected from `--overlay` if not set) |
| `--dry-run` | `false` | Show what would be done |
| `--overlay` | | Local overlay path |
| `-h, --help` | | Help for import |

### Examples

```bash
# Import from Gentoo main tree
larry import --from /var/db/repos/gentoo dev-python/requests

# Import to a specific overlay
larry import --from /var/db/repos/gentoo --overlay /var/db/repos/my-overlay dev-python/requests

# Dry run
larry import --from /var/db/repos/gentoo dev-python/requests --dry-run
```

## `larry test`

Test-build an ebuild locally or in a container.

### Synopsis

```bash
larry test [category/package] [flags]
```

### Description

Runs ebuild phases (`clean`, `fetch`, `unpack`, `compile`, `install`, `qmerge`) 
either locally or in a Podman container with GPU passthrough.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-e, --emerge` | `false` | Use Podman container testing |
| `--distdir` | | Distdir path |
| `-c, --compile` | `false` | Run through compile |
| `-m, --merge` | `false` | Run through install |
| `--pretend` | `false` | Print the commands without executing (no shorthand) |
| `-p, --phase` | | Run a specific phase (repeatable) |
| `--image` | `larry-test` | Container image name |
| `--use` | | USE flags for the container build |
| `--portage-tmpdir` | | Override `PORTAGE_TMPDIR` |
| `--overlay` | `$LARRY_OVERLAY` | Overlay root path |
| `-h, --help` | | Help for test |

### Examples

```bash
# Local test
larry test dev-python/requests --compile

# Container test
larry test dev-python/requests --emerge

# Pretend mode
larry test dev-python/requests --emerge --pretend

# Specific phase
larry test dev-python/requests --phase fetch
```

## Global Flags

These flags apply to all subcommands:

| Flag | Default | Description |
|------|---------|-------------|
| `-v, --verbose` | 0 | Count: `-v` info, `-vv` debug, `-vvv` trace |
| `-q, --quiet` | `false` | Suppress informational output (errors still print) |
| `--color` | auto | Color mode: auto, always, never |
| `--dry-run` | `false` | Preview bump/import/prepare without changes |
| `--cache-dir` | | Cache directory |
| `--distdir` | | Distfile directory |
| `--overlay` | | Overlay root path |
| `--jobs` | 0 | Parallel worker count (currently single-threaded) |
| `--github-token` | | GitHub PAT (also exported as `GITHUB_TOKEN`) |
| `--remote` | origin | Git remote name |
| `--config` | | Config file path |

## Shell Completion

Generate completion scripts:

```bash
# Bash
larry completion bash > /etc/bash_completion.d/larry

# Zsh
larry completion zsh > ~/.zfunc/_larry

# Fish
larry completion fish > ~/.config/fish/completions/larry.fish
```
