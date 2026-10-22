% LARRY-BUMP(1)                    General Commands Manual                    %

## NAME

larry-bump - Create a new versioned ebuild from the latest

## SYNOPSIS

**larry bump** [*category/package*] [*flags*]

## DESCRIPTION

Checks the upstream version for the specified package and creates a new
ebuild file with the updated version. Preserves existing ebuild structure
including SRC_URI, HOMEPAGE, LICENSE, DEPEND, and RDEPEND.

## FLAGS

**-p, --prereleases**
: Include pre-release versions in lookup

**--version** *version*
: Specific version to bump to

**-r, --revision**
: Bump revision (PVR) instead of version

**-t, --type** *type*
: Force package type (npm, cargo, go, vendored, generic).

**--dry-run**
: Show what would be done without making changes (global `-n`).

**--overlay** *path*
: Overlay root path (default: $LARRY_OVERLAY)

**-h, --help**
: Help for bump

## OUTPUT

Creates a new ebuild file at *category/package/package-version.ebuild*

## EXAMPLES

**Auto-detect latest version**
: larry bump dev-python/requests

**Bump to specific version**
: larry bump dev-python/requests --version 2.31.0

**Bump revision**
: larry bump dev-python/requests --revision

**Dry run**
: larry bump dev-python/requests --dry-run

## SEE ALSO

**larry**(1), **larry-upstream**(1), **larry-prepare**(1), **larry-import**(1), **larry-test**(1)

## AUTHOR

Larry is maintained by Gentoo maintainers.
