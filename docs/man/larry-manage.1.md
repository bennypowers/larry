% LARRY-MANAGE(1)                   General Commands Manual                  %

## NAME

larry-manage - Interactive session for checking, bumping, preparing, and testing

## SYNOPSIS

**larry manage** [*cat/pkg*...] [*overlay-path*] [*flags*]

## DESCRIPTION

Opens an interactive upstream table for the overlay and runs actions on the
selected package. Every package is listed; rows that need attention sort above
up-to-date, live, deprecated, and manual rows. Discovery order is preserved
inside each group, and the cursor follows its package across refreshes, so a
bump followed by a test targets the same package.

Requires a terminal; use **larry-upstream**(1) for piped or scripted use.

## FLAGS

**-f, --fresh**
: Bypass the cache and fetch fresh versions.

**-p, --prereleases**
: Include pre-release versions in checks.

**--remote** *remote*
: Git remote name used to resolve the overlay's GitHub repository.

**--overlay** *path*
: Overlay root path.

**-h, --help**
: Help for manage.

## KEYS

**b**
: Bump the selected package (`larry bump`).

**p**
: Prepare the selected package (`larry prepare`).

**t**
: Run the test pipeline (`larry test --compile`).

**R**
: Re-run the upstream check.

**enter**
: Show source URL, target, error, and depth.

**?**
: Toggle full key help.

**q**, **Esc**, **Ctrl+C**
: Quit. Esc in an action cancels the child process.

## ACTIONS

Mutating actions ask for confirmation. Their stdout and stderr stream into
the view. A successful action refreshes the table; a failure stays visible.
Actions execute the larry binary as a child, so flags, logging, and exit
codes match the standalone commands.

## EXAMPLES

**Open the session for the overlay resolved from git**
: larry manage

**Check one package**
: larry manage dev-util/cem

**Fresh check with prereleases**
: larry manage --fresh --prereleases

## SEE ALSO

**larry**(1), **larry-upstream**(1), **larry-bump**(1), **larry-prepare**(1), **larry-test**(1)
