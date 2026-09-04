# Foundation Tasks (T1–T3)

**What this covers:** Project scaffold, config system, logging/color output, CLI scaffolding.
**Why first:** Everything else builds on this layer.

---

### T1 — Project scaffold, module init, CI glue, Containerfile

**Precondition:** None

**Tests:**
- `go test ./...` compiles and passes (empty suite)
- `TestContainerfile_Exists` — `Containerfile.larry-test` exists and is valid (base image, Portage installed, GPU libs available)

**Precondition:** None

**Tests:**
- `go test ./...` compiles and passes (empty suite)

**Implementation:**
1. `go mod init github.com/<org>/larry`
2. Create `cmd/larry/main.go` — stub that prints `"larry: hello"`
3. Create `internal/config/config.go` — stub
4. Create `.github/workflows/ci.yml` — `go test` + `go vet` on Go 1.22/1.23. Note: the Makefile's `lint` target (which adds `staticcheck if available`) is **not** wired into CI — only `go test` + `go vet` are gated. If `staticcheck` enforcement is desired, it must be explicitly added to `ci.yml`.
5. Create `Makefile` with targets: `test`, `build`, `install`, `lint`
6. Create `Containerfile.larry-test` — minimal Gentoo-based image for `larry test --container`:
   - Base: `registry.access.redhat.com/ubi9/ubi-minimal` or `ghcr.io/gentoo/stage3-amd64` (whichever is available)
   - Installs: `portage`, `sys-apps/baselayout`, `app-portage/egencache`, `app-portage/pym`, `sys-apps/util-linux` (for /dev), GPU libs (`sys-apps/pciutils`, `media-libs/mesa` runtime)
   - Creates non-root user, sets `EPREFIX` correctly
   - Copies `/usr/sbin/emerge` and `portageq` binaries into PATH
   - Tagged as `larry-test` in T21
7. Create `go.work` if workspace mode is desired
6. Create `go.work` if workspace mode is desired

**Acceptance:** `make test` and `make build` succeed. Binary prints `"larry: hello"`. `Containerfile.larry-test` builds with `podman build -f Containerfile.larry-test -t larry-test`.

---

### T2 — Config system (viper + schema)

**Precondition:** T1 complete

**Tests:**
- `TestLoadConfig_Defaults` — reads nothing, returns struct with correct zero/default values
- `TestLoadConfig_EnvOverride` — env vars override defaults
- `TestLoadConfig_FileOverride` — YAML file overrides env vars
- `TestLoadConfig_FlagOverride` — CLI flags override file
- `TestLoadConfig_CacheDir` — `$XDG_CACHE_HOME` respected, falls back to `~/.cache`
- `TestLoadConfig_DistdirOverride` — `--distdir` flag works

**Implementation:**
1. Define `Config` struct in `internal/config/config.go`:
   ```go
   type Config struct {
       Verbose       int
       Quiet         bool
       CacheDir      string
       Distdir       string
       Color         string   // "auto", "always", "never"
       DryRun        bool
       Jobs          int
       GitHubToken   string
       Remote        string   // GitHub remote name (default "origin")
       Overlay       string   // overlay root path
   }
   ```
2. Viper setup: bind env vars, flags, config file (YAML)
3. Precedence: flags > env > file > hardcoded defaults
4. `--config` flag to specify config file path
5. `--color` auto-detection via `termenv`

**Acceptance:** `larry --verbose --cache-dir=/tmp/larry --color=always --dry-run` sets config correctly. Config file with `verbose: true, color: always` is merged.

---

### T3 — Logging, color output, shared CLI scaffolding

**Precondition:** T2 complete

**Tests:**
- `TestLogger_VerboseLevels` — debug/info/warn/error output controlled by `--verbose`
- `TestLogger_QuietMode` — nothing printed when `--quiet`
- `TestColor_AutoDetect` — detects terminal color support
- `TestColor_Override` — `--color=never` suppresses ANSI even in terminal

**Implementation:**
1. `internal/output/logger.go` — structured logger wrapping `log/slog`
2. `internal/output/color.go` — `termenv`-based color helpers: `Red()`, `Green()`, `Yellow()`, `Blue()`, `Dim()`
3. `internal/output/progress.go` — `ProgressBar` and `Spinner` types (reusable by bubbletea later)
4. Root cobra command in `cmd/larry/root.go` with global flags from config
5. All subcommands attach to root; global flags inherited
6. OSC 8 hyperlinks for source URLs (GitHub, PyPI, npm, etc.)

**Acceptance:** `larry --verbose` shows info+debug output. `larry --quiet` suppresses all stdout. Colors respect `--color` flag.
