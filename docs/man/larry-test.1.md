% LARRY-TEST(1)                      General Commands Manual                      %

## NAME

larry-test - Run ebuild test phases locally or in a podman container

## SYNOPSIS

**larry test** [*options*] [*category/package*]

## DESCRIPTION

Tests an ebuild by running Portage phases locally or in a clean podman
container with GPU passthrough. Accepts a category/package atom or a
direct ebuild path.

## FLAGS

**-c, --compile**
: Run through the compile phase (`clean fetch unpack compile`).

**-m, --merge**
: Full build and install to a temporary image (`clean fetch unpack compile
  install qmerge`).

**--pretend**
: Show what would be run without executing. Prints commands without
  invoking Portage. (No shorthand: `-p` is `--phase`.)

**-p, --phase** *phase*
: Run a specific ebuild phase (any valid `do_*` phase: `clean`, `fetch`,
  `unpack`, `prepare`, `configure`, `compile`, `install`, `test`,
  `package`, `preinst`, `postinst`, `prerm`, `postrm`). May be specified
  multiple times to run multiple phases in sequence.

**--image** *name*
: Container image name (default: `larry-test`).

**--use** *flags*
: USE flags for the container build, space or comma separated.

**--portage-tmpdir** *path*
: Override `PORTAGE_TMPDIR` for test runs.

**-e, --emerge**
: Run `emerge` in a clean podman container with GPU passthrough (requires
  `Containerfile.larry-test`).

**--distdir** *path*
: Distfiles directory for the test (default: `$LARRY_DISTDIR` or `PORTAGE_DISTDIR`).

**--overlay** *path*
: Overlay root path (default: `$LARRY_OVERLAY` or `/var/db/repos/gentoo`).

**-h, --help**
: Help for test

## MODES

### Local Mode (Default)

1. Resolves the target ebuild from the overlay tree
2. Disables network sandbox (`-network-sandbox`) to allow distfile downloads
3. Runs the specified Portage phase(s) via `ebuild <path> <phases...>`
4. Uses a temporary work directory under `/tmp/portage-test`

### Container Mode (`--emerge`)

1. Builds a podman image from the overlay's `Containerfile.larry-test`
2. Mounts the overlay, `DISTDIR`, and `binpkgs` directory into the container
3. Passes through GPU devices (`/dev/kfd`, `/dev/dri`, `--group-add video`)
4. Runs `emerge --verbose --autounmask=y` inside the container

## EXAMPLES

**Clean only (default)**
: larry test dev-lisp/sbcl-unicode-dictionary

**Compile phase**
: larry test --compile dev-lisp/sbcl-unicode-dictionary

**Full merge build**
: larry test --merge dev-lisp/sbcl-unicode-dictionary

**Specific phases**
: larry test --phase fetch --phase unpack dev-lisp/foo

**Dry run**
: larry test --pretend dev-lisp/foo

**Container mode (via --emerge)**
: larry test --emerge dev-lisp/foo

**Direct ebuild path**
: larry test /var/db/repos/gentoo/foo/foo-1.0.0.ebuild

## SEE ALSO

**larry**(1), **larry-upstream**(1), **larry-bump**(1), **larry-prepare**(1), **larry-import**(1)

## AUTHOR

Larry is maintained by Gentoo maintainers.
