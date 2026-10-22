% LARRY-PREPARE(1)                 General Commands Manual                 %

## NAME

larry-prepare - Prepare an ebuild for commit (manifest, distfiles, etc.)

## SYNOPSIS

**larry prepare** [*category/package* | *ebuild-path*...] [*flags*]

## DESCRIPTION

Prepares an ebuild for commit by running the appropriate type-specific
pipeline. Downloads distfiles, resolves dependencies, generates a deps
tarball (if applicable), uploads to GitHub release, and generates the
overlay Manifest via `pkgdev manifest`.

Type detection is automatic based on ebuild content, but can be overridden
with the `--type` flag.

### Per-package Configuration (.larry/config.yaml)

If a `.larry/config.yaml` file exists in the ebuild's directory, the following
settings are applied:

| Key                     | Type      | Description                                      |
|-------------------------|-----------|--------------------------------------------------|
| `type_override`         | string    | Forces package type (overrides auto-detection)   |
| `vendor_dirs`           | map/array | Maps pkg names to vendored source subdirectories |
| `cleanup_orphaned_assets` | bool    | Removes orphaned GitHub assets on failure        |

### Custom Prepare Scripts

If `.larry/prepare.sh` exists in the ebuild's directory, it runs instead
of any built-in pipeline. The script receives these environment variables:

| Variable            | Description                              |
|---------------------|------------------------------------------|
| `EBUILD`            | Path to the ebuild                       |
| `PN`                | Package name                             |
| `PV`                | Package version                          |
| `P`                 | `${PN}-${PV}`                            |
| `CATEGORY`          | Ebuild category                          |
| `DISTDIR`           | Distfiles directory                      |
| `LARRY_PREPARE`     | Set to `1`                               |
| `LARRY_SANDBOX_WRITE` | Colon-separated write-allowed paths    |
| `LARRY_SANDBOX_READ`  | Colon-separated read-allowed paths     |

**Sandbox policy (userspace):**

| Constraint        | Policy                                     |
|-------------------|--------------------------------------------|
| **PATH**          | Restricted to system directories only      |
| **Environment**   | Dangerous vars stripped (GZIP, BZIP, …)    |
| **Write access**  | Ebuild directory + DISTDIR only            |
| **Temp dir**      | Isolated temp dir (cleaned up after)       |

**Sandbox limitations (not enforced):**

- No syscall-level file access control (unlike Portage's `ld.so` preload)
- No network access blocking
- No PID or mount namespace isolation
- No user/group switching

For full sandboxing, use `larry test --emerge` to run in a podman container,
or run `larry prepare` inside a container.

### Transaction Safety

- Ebuild edits are atomic: backup → modify → on success remove `.bak`, on
  failure restore from `.bak`
- GitHub uploads are **not** atomic — use `--cleanup` or
  `cleanup_orphaned_assets: true` in `.larry/config.yaml` to auto-remove orphaned assets
  on failure.

## FLAGS

**-t, --type** *type*
: Force package type (overrides auto-detection). Values: `npm`, `cargo`,
  `go`, `vendored`, `generic`. If omitted, type is auto-detected from the
  ebuild content.

**-a, --all**
: Manifest-only mode: run `pkgdev manifest` for every category/package in
  the overlay. No type-specific pipeline, and no positional arguments.

**--dry-run**
: Perform all local operations (downloads, variable resolution, tarball
  creation) but skip all external mutations (no GitHub upload, no ebuild
  modification, no manifest generation). Exits 0 if all operations would
  succeed, 1 if any would fail. Global `-n`.

**--tui**
: Stream live pipeline progress in an interactive view. Requires a
  terminal; falls back to plain output when piped.

**--cleanup**
: Remove orphaned GitHub release assets on prepare failure. Can also be
  set via `cleanup_orphaned_assets: true` in `.larry/config.yaml`.

**-h, --help**
: Help for prepare

## TYPE DETECTION

| Type       | Detection Criterion                          |
|------------|----------------------------------------------|
| `npm`      | Ebuild contains `mirror://npm/` in SRC_URI   |
| `cargo`    | Ebuild contains a `CRATES="..."` variable    |
| `go`       | Ebuild inherits `go-module`                  |
| `vendored` | SRC_URI contains `releases/download/${PN}/`  |
| `generic`  | Everything else                              |

## EXAMPLES

**Prepare a single ebuild**
: larry prepare dev-lisp/sbcl-unicode-dictionary/sbcl-unicode-dictionary-1.0.0.ebuild

**Prepare all ebuilds in a package directory**
: larry prepare dev-lisp/sbcl-unicode-dictionary

**Force type for an ebuild**
: larry prepare --type generic sys-apps/foo/foo.ebuild

**Dry run — preview without mutations**
: larry prepare --dry-run dev-lisp/sbcl-unicode-dictionary

**Prepare all ebuilds in a directory**
: larry prepare --all /var/db/repos/gentoo/dev-lisp

## SEE ALSO

**larry**(1), **larry-upstream**(1), **larry-bump**(1), **larry-import**(1), **larry-test**(1)

## AUTHOR

Larry is maintained by Gentoo maintainers.
