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
: Bump the selected package (`larry bump`), with a bump/prepare/commit checklist.

**p**
: Prepare the selected package (`larry prepare`), with a live step checklist.

**t**
: Choose test mode, custom phases, or USE flags, then run the test pipeline.

**R**
: Re-run the upstream check.

**enter**
: Toggle the detail pane: source URL, target, error, depth, and every ebuild
  version present in the overlay.

**x**
: Prune older ebuilds, keeping the newest and any live (9999) ebuilds.

**A**
: Toggle autoprune for the session: prune after a successful bump.

**?**
: Toggle full key help.

**q**, **Esc**, **Ctrl+C**
: Quit. Esc in an action cancels the child process.

## ACTIONS

Mutating actions ask for confirmation. Their stdout and stderr stream into
the view. A successful action refreshes the table; a failure stays visible.
Actions execute the larry binary as a child, so flags, logging, and exit
codes match the standalone commands.

The bump action shows checkpoints for bump, prepare, and commit. The prepare
action appends pipeline steps as they start. The test action opens a prompt:
**c** compile, **m** merge, **d** default, **p** custom phases, **u** USE
flags, **tab** next field, **enter** run; the choice is remembered.

The package list appears before all checks finish: CURRENT and SOURCE render
immediately, while LATEST and STATUS spin until each row resolves. Overall
progress lives in the header and footer.

Running actions keep the table on screen and use a pane below it for the
checklist, elapsed time, and output tail. The pane surfaces the latest error
line, shows a no-output timer when the child stalls, and redacts the overlay's
absolute path from child output.

## EXAMPLES

**Open the session for the overlay resolved from git**
: larry manage

**Check one package**
: larry manage dev-util/cem

**Fresh check with prereleases**
: larry manage --fresh --prereleases

## SEE ALSO

**larry**(1), **larry-upstream**(1), **larry-bump**(1), **larry-prepare**(1), **larry-test**(1)
