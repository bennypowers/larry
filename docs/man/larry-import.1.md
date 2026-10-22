% LARRY-IMPORT(1)                    General Commands Manual                    %

## NAME

larry-import - Import an ebuild from another overlay

## SYNOPSIS

**larry import** [*category/package*] [*flags*]

## DESCRIPTION

Imports the latest stable ebuild from a source overlay into the local
overlay. Searches the source overlay (default: `/var/db/repos/`, or
`--from` to specify) for the latest stable ebuild, copies it along with
the `files/` directory and `metadata.xml` (merging maintainers and
preserving upstream `<remote-id>` tags) into the local overlay at the
same category/package path.

After importing, run `larry prepare` on the imported ebuild to run the
appropriate type-specific pipeline.

## FLAGS

**--from** *path|repo*
: Source overlay path or repository name. If omitted, searches all overlays
  in `/var/db/repos/` (excluding the local overlay).

**--dry-run**
: Preview the import without making changes. Shows source and target paths
  without copying any files.

**--force**
: Overwrite even if the target already has a newer or equal version.

**-h, --help**
: Help for import

## OUTPUT FILES

| File                    | Description                                |
|-------------------------|--------------------------------------------|
| `${cat}/${pkg}/${pkg}-${PV}.ebuild` | Imported ebuild file             |
| `${cat}/${pkg}/metadata.xml`          | New metadata with merged maintainers |
| `${cat}/${pkg}/files/`                | Copied from source if present       |

## EXAMPLES

**Auto-detect source overlay**
: larry import dev-lisp/sbcl-unicode-dictionary

**Explicit source overlay**
: larry import --from /var/db/repos/guru dev-lisp/foo

**Preview import**
: larry import --dry-run dev-lisp/foo

## SEE ALSO

**larry**(1), **larry-upstream**(1), **larry-bump**(1), **larry-prepare**(1), **larry-test**(1)

## AUTHOR

Larry is maintained by Gentoo maintainers.
