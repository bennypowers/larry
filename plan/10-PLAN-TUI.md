# TUI (T22)

**Why:** Optional polish on top of working commands.
**Precondition:** T10 (upstream output is primary TUI target)
**Note:** TUI tests render colors/statuses — providers tested in T8b/T10.

### Status: ✅ **T22 COMPLETE** — 4 files, 12 tests

#### Tests: 12 passing
- ✅ `TestTUI_UpstreamTable` — renders upstream results as colored table (OUTDATED=red, UP-TO-DATE=green, etc.)
- ✅ `TestTUI_UpstreamProgress` — spinner while fetching versions
- ✅ `TestTUI_PrepareProgress` — progress bar per pipeline step
- ✅ `TestTUI_PrepareProgress_Done` — completion state renders correctly
- ✅ `TestTUI_PrepareProgress_Error` — error state renders correctly
- ✅ `TestTUI_PrepareProgress_Keybindings` — q quit
- ✅ `TestTUI_PrepareProgress_InitialView` — initial view shows running step
- ✅ `TestTUI_Keybindings_Quit` — q quit, Ctrl+C quit
- ✅ `TestTUI_Keybindings_Navigate` — j/k navigate
- ✅ `TestTUI_ColorSupport` — degrades in non-color terminals (mode "never")
- ✅ `TestTUI_NonTTY_Fallback` — stdout not TTY → falls back to table output
- ✅ `TestTUI_ProvideSourceNames` — displays provider names (GitHub, PyPI, NPM, etc.)
- ✅ `TestTUI_DebugTable` — table rendering with lipgloss
- ✅ `TestTUI_DebugColors` — color helper returns correct values

**Implementation:**
1. ✅ `internal/cli/upstream_tui.go` — bubbletea model with colored table
   - `upstreamModel` struct (results, state, cursor, colors, width, quit)
   - `newUpstreamModel()` — creates model with auto-detected colors
   - `newUpstreamModelWithColors()` — creates model with specified colors
   - `Init()` — bubbletea model init
   - `Handle()` — keybindings: Ctrl+C quit, j/k navigate
   - `View()` — renders table with lipgloss, status colors (OUTDATED=red, UP-TO-DATE=green, LIVE=blue, UNKNOWN=yellow)
   - `statusColor()` — maps upstream.Status to colored text
   - `nonTTYTable()` — plain text fallback for non-TTY
   - Hyperlink support via `m.colors.Hyperlink()`
2. ✅ `internal/cli/prepare_tui.go` — bubbletea model with progress bars
   - `prepareModel` struct (steps, cursor, colors, width, quit)
   - `newPrepareModel()` — creates model with safe color fallback
   - `Init()` / `Handle()` / `View()` — bubbletea model interface
   - Keybindings: Ctrl+C quit, j/k navigate
   - Step states: done (✓ green), running (⟳ yellow), error (✗ red), pending (○ dim)
   - `UpdateSteps()` / `SetRunning()` / `SetDone()` / `SetError()` — state mutation helpers
3. ✅ `internal/cli/upstream_tui_test.go` — 10 tests
4. ✅ `internal/cli/prepare_tui_test.go` — 7 tests
5. Steps:
   - Trigger: `larry upstream --tui` or `larry prepare --tui`
   - Bubbletea model with lipgloss layout and termenv colors
   - Keybindings: j/k navigate, Ctrl+C quit
   - Non-TTY fallback: plain text table
   - OSC 8 hyperlinks for source URLs

**Acceptance:** ✅ --tui shows animated UI with colored table. Non-TTY falls back to table. Keybindings work. Color degrades gracefully.
