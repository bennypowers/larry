# Polish (T23)

**Why:** Everything above must be green first.
**Precondition:** T1–T22 complete
**Note:** Integration tests focus on full command lifecycle, not provider correctness (covered in T8b/T10/T14–T17).

### Status: ✅ **T23 COMPLETE** — 1 new package, 7 new docs, 8 integration tests

#### Tests: 8 integration tests passing
- ✅ `TestIntegration_Upstream_Full` — end-to-end upstream check on test overlay
- ✅ `TestIntegration_Bump_Full` — bump + prepare cycle
- ✅ `TestIntegration_Prepare_Generic` — end-to-end generic prepare
- ✅ `TestIntegration_Import_Full` — import from test overlay
- ✅ `TestIntegration_DryRun_All` — dry-run for all commands (3 subtests)
- ✅ `TestIntegration_ConfigFile` — config file controls subcommands
- ✅ `TestIntegration_Cache` — cache directory handling
- ✅ `TestIntegration_Version` — version output

#### Documentation:
1. ✅ `README.md` — overview, installation, quick start, commands table, providers, types, shell completion
2. ✅ `docs/commands.md` — per-subcommand reference with flags and examples
3. ✅ `docs/architecture.md` — high-level architecture diagram, key decisions, file flow
4. ✅ `docs/contributing.md` — how to add providers/types/phases, TDD workflow, coding standards
5. ✅ `docs/man/larry-upstream.1.md` — upstream man page
6. ✅ `docs/man/larry-bump.1.md` — bump man page

#### Packaging:
1. ✅ `Makefile install` → existing `make install` target
2. ✅ Shell completion documented in README and docs/commands.md
3. ✅ `go.mod` module: `bennypowers.dev/larry`

**Acceptance:** ✅ All 8 integration tests pass. README complete. Documentation created. Shell completion documented. Binary installable via `make install`.

## Implementation Details

### Integration Test Framework

Tests create temporary directories with minimal Gentoo overlay structures:
- `profiles/metadata/layout.conf` — overlay metadata
- `profiles/categories` — category list
- `dev-util/testpkg/` — test ebuild directory
- `distfiles/` — distfiles directory

The test framework:
1. Builds the `larry` binary into a temp directory
2. Creates a minimal overlay with test ebuilds
3. Runs CLI commands as subprocesses
4. Verifies output/errors
5. Cleans up temp directories

### Test Helper Functions

- `buildBinary(t, tmpDir)` — builds larry binary
- `createTestOverlay(t, base)` — creates minimal overlay structure
- `writeEbuild(t, path, content)` — writes ebuild file
- `larryRun(t, bin, overlay, args...)` — executes larry command
- `projectRoot()` — finds project root via `go.mod` walk-up
