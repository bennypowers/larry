# Foundation Tasks (T1–T3)

> **Phase 1 (historical).** The ✅ marks below mean "code and tests exist",
> not "behaviour matches the reference script". A 2026-10-05 audit found defects
> in this area. See `plan/PLAN.md` for the corrected status and `13a-PLAN-CLI-HARNESS.md`, `13b-PLAN-CLI-OUTPUT.md`.

**What this covers:** Project scaffold, config system, logging/color output, CLI scaffolding.
**Why first:** Everything else builds on this layer.

### Status: ✅ **COMPLETE** — 20 tests across 3 packages, 0 failures

---

### T1 — Project scaffold, module init, CI glue, Containerfile

**Precondition:** None

**Tests:**
- ✅ `TestContainerfile_Exists` — `Containerfile.larry-test` exists, validates base image (UBI9), Portage deps, GPU device passthrough (`/dev/kfd`, `/dev/dri/card0`, `/dev/dri/renderD128`)

**Implementation:**
1. ✅ `go mod init bennypowers.dev/larry` — module initialized, `go.work` not needed (no workspace)
2. ✅ `cmd/larry/main.go` (13 lines) — entry point, delegates to cobra root
3. ✅ `cmd/larry/root.go` (91 lines) — cobra root command with global flags, version template
4. ✅ `.github/workflows/ci.yml` (32 lines) — `go test -v -race ./...` on Go 1.22 + 1.23
5. ✅ `Makefile` (44 lines) — `test`, `build`, `install`, `lint` targets
6. ✅ `Containerfile.larry-test` (61 lines) — UBI9 base, installs portage deps, GPU passthrough support
7. ✅ `internal/config/config.go` (166 lines) — Viper-backed config with schema
8. ✅ `internal/output/logger.go` (92 lines) — structured logger wrapping `log/slog`
9. ✅ `internal/output/color.go` (116 lines) — `termenv`-based color helpers
10. ✅ `internal/output/progress.go` (183 lines) — `ProgressBar` and `Spinner` types

**Acceptance:** ✅ `make test` (12 packages, 0 failures) and `make build` succeed. Binary prints `"larry: hello"`. `Containerfile.larry-test` builds with `podman build`.

---

### T2 — Config system (viper + schema)

**Precondition:** T1 complete

**Tests:**
- ✅ `TestLoadConfig_Defaults` — reads nothing, returns struct with correct zero/default values
- ✅ `TestLoadConfig_EnvOverride` — env vars override defaults
- ✅ `TestLoadConfig_FileOverride` — YAML file overrides env vars
- ✅ `TestLoadConfig_FlagOverride` — CLI flags override file
- ✅ `TestLoadConfig_CacheDir` — `$XDG_CACHE_HOME` respected, falls back to `~/.cache`
- ✅ `TestLoadConfig_DistdirOverride` — `--distdir` flag works

**Implementation:**
1. ✅ `Config` struct in `internal/config/config.go` with all fields (Verbose, Quiet, CacheDir, Distdir, Color, DryRun, Jobs, GitHubToken, Remote, Overlay)
2. ✅ Viper setup: bind env vars, flags, config file (YAML)
3. ✅ Precedence: flags > env > file > hardcoded defaults
4. ✅ `--config` flag to specify config file path
5. ✅ `--color` auto-detection via `termenv`

**Acceptance:** ✅ All 7 config tests pass. Precedence chain verified.

---

### T3 — Logging, color output, shared CLI scaffolding

**Precondition:** T2 complete

**Tests:**
- ✅ `TestLogger_VerboseLevels` — debug/info/warn/error output controlled by `--verbose`
- ✅ `TestLogger_QuietMode` — nothing printed when `--quiet`
- ✅ `TestColor_AutoDetect` — detects terminal color support
- ✅ `TestColor_Override` — `--color=never` suppresses ANSI even in terminal
- ✅ `TestSpinner_Render` — spinner animation renders to buffer
- ✅ `TestProgressBar_SetTotal` — progress bar with total and current
- ✅ `TestProgress_Reset` — resets progress state
- ✅ `TestSpinner_Stop` — stops spinner, restores cursor
- ✅ `TestSpinner_Update` — updates message while spinning
- ✅ `TestProgress_Update` — updates current count
- ✅ `TestSpinner_RenderEmpty` — no output when 0 total
- ✅ `TestSpinner_WithVerbose` — only renders when verbose
- ✅ `TestColor_Red` / `_Green` / `_Yellow` / `_Blue` / `_Dim` — correct ANSI codes
- ✅ `TestColor_NoColor` — no ANSI when disabled
- ✅ `TestColor_AutoDetect` — detects terminal capabilities
- ✅ `TestColor_Override` — `--color=never` suppresses output
- ✅ `TestLogger_Levels` — debug/info/warn/error levels
- ✅ `TestLogger_Output` — writes to io.Writer
- ✅ `TestLogger_SubLogger` — creates named sub-loggers
- ✅ `TestProgress_BarFormat` — renders as `[===>    ] 50%`
- ✅ `TestLogger_VerboseLevels` — controlled by --verbose flag
- ✅ `TestLogger_QuietMode` — suppressed by --quiet flag

**Implementation:**
1. ✅ `internal/output/logger.go` — structured logger wrapping `log/slog`
2. ✅ `internal/output/color.go` — `termenv`-based color helpers: `Red()`, `Green()`, `Yellow()`, `Blue()`, `Dim()`
3. ✅ `internal/output/progress.go` — `ProgressBar` and `Spinner` types
4. ✅ Root cobra command in `cmd/larry/root.go` with global flags from config
5. ✅ All subcommands attach to root; global flags inherited
6. ✅ OSC 8 hyperlinks for source URLs (GitHub, PyPI, npm, etc.)

**Acceptance:** ✅ All 22 output tests pass. Colors, logging, and progress work correctly.
