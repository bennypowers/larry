# Polish Tasks (T23)

**What this covers:** Integration tests, documentation, packaging, shell completion.
**Why last:** Everything above must be green first.

---

### T23 — Integration tests, documentation, packaging

**Precondition:** T1-T22 complete

**Tests:**
- `TestIntegration_Upstream_Full` — end-to-end upstream check on a small test overlay
- `TestIntegration_Bump_Full` — full bump + prepare cycle
- `TestIntegration_Prepare_Generic` — end-to-end generic prepare
- `TestIntegration_Prepare_Npm` — end-to-end npm prepare (with mocked distfiles)
- `TestIntegration_Prepare_Go` — end-to-end go prepare (with mocked vendor)
- `TestIntegration_Import_Full` — import from test overlay + prepare
- `TestIntegration_DryRun_All` — dry-run for all commands succeeds
- `TestIntegration_ConfigFile` — config file controls all subcommands
- `TestIntegration_Cache` — cache persists across runs

**Documentation:**
1. `README.md` — overview, installation, quick start
2. `docs/commands.md` — per-subcommand reference with examples
3. `docs/contributing.md` — how to add new providers, types, phases
4. `docs/architecture.md` — high-level architecture diagram
5. Man pages in `docs/man/`

**Packaging:**
1. `Makefile install` target — installs to `/usr/local/bin/larry`
2. Shell completion: `cobra completion bash/zsh/fish`
3. Arch Linux PKGBUILD (optional, for overlay maintainers)

**Acceptance:** All integration tests pass. README complete. Shell completion works. Binary installable.
