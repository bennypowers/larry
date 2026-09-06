# Polish (T23)

**Why:** Everything above must be green first.
**Precondition:** T1–T22 complete
**Note:** Integration tests focus on full command lifecycle, not provider correctness (covered in T8b/T10/T14–T17).

**Tests:**
- `TestIntegration_Upstream_Full` — end-to-end upstream check on small test overlay
- `TestIntegration_Bump_Full` — bump + prepare cycle
- `TestIntegration_Prepare_Generic` — end-to-end generic prepare
- `TestIntegration_Prepare_Npm` — end-to-end npm prepare (mocked distfiles)
- `TestIntegration_Prepare_Go` — end-to-end go prepare (mocked vendor)
- `TestIntegration_Import_Full` — import from test overlay + prepare
- `TestIntegration_DryRun_All` — dry-run for all commands succeeds
- `TestIntegration_ConfigFile` — config file controls all subcommands
- `TestIntegration_Cache` — cache persists across runs

**Documentation:**
1. `README.md` — overview, installation, quick start
2. `docs/commands.md` — per-subcommand reference with examples
3. `docs/contributing.md` — how to add providers/types/phases
4. `docs/architecture.md` — high-level architecture diagram
5. Man pages in `docs/man/`

**Packaging:**
1. `Makefile install` → `/usr/local/bin/larry`
2. Shell completion: `cobra completion bash/zsh/fish`
3. Arch PKGBUILD (optional)

**Acceptance:** All integration tests pass. README complete. Shell completion works. Binary installable.
