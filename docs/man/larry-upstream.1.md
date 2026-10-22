% LARRY-UPSTREAM(1)                General Commands Manual               %

## NAME

larry-upstream - Check packages for upstream version updates

## SYNOPSIS

**larry upstream** [*cat/pkg*...] [*overlay-path*] [*flags*]

## DESCRIPTION

Checks packages in a Gentoo overlay for upstream version updates. Walks
the specified overlay directory and compares each ebuild's version against
its upstream source.

## FLAGS

**-f, --fresh**
: Bypass cache and fetch fresh versions

**-p, --prereleases**
: Include pre-release versions in checks

**-v, --verbose**
: Count flag. `-v` shows every status (including up-to-date, live,
  deprecated, and manual), `-vv` adds debug diagnostics, `-vvv` adds trace
  (provider resolution, cache hits, HTTP requests, subprocess argv).

**--remote** *remote*
: Git remote name for upstream resolution (default: origin)

**--overlay** *path*
: Overlay root path (default: $LARRY_OVERLAY or /var/db/repos/gentoo)

**--output** *format*
: `text` (default) or `json`. JSON always contains every result, ignores
  the significance filter and `-v`, and includes source URL, target, error,
  and depth.

**--tui**
: Interactive table with details (requires a TTY; incompatible with
  `--output json`).

**-h, --help**
: Help for upstream

## PROVIDERS

Providers are auto-detected from SRC_URI:

- **mirror://pypi/** → PyPI
- **mirror://npm/** → npm
- **mirror://github/** → GitHub
- **mirror://gitlab/** → GitLab
- **mirror://codeberg/** → Codeberg
- **mirror://sourcehut/** → SourceHut

## DEPENDENCY SCANNING

`larry upstream` recursively checks upstream dependencies up to 3 levels
deep. Dependency atoms are extracted from `DEPEND=` and `RDEPEND=` lines.
Dependencies are always scanned; `-v` controls whether insignificant rows
(including LIVE dependencies) are displayed.

## OUTPUT

Results are displayed as a colored table with columns: PACKAGE, CURRENT,
LATEST, STATUS, SOURCE.

## EXAMPLES

**Check all categories**
: larry upstream

**Check specific categories**
: larry upstream dev-python dev-lisp

**Include pre-releases**
: larry upstream --prereleases

**Interactive TUI**
: larry upstream --tui

## SEE ALSO

**larry**(1), **larry-bump**(1), **larry-prepare**(1), **larry-import**(1), **larry-test**(1)

## AUTHOR

Larry is maintained by Gentoo maintainers.
