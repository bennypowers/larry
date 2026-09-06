# TUI (T22)

**Why:** Optional polish on top of working commands.
**Precondition:** T10 (upstream output is primary TUI target)
**Note:** TUI tests render colors/statuses — providers tested in T8b/T10.

**Tests:**
- `TestTUI_UpstreamTable` — renders upstream results as color table (OUTDATED=red, UP-TO-DATE=green, etc.)
- `TestTUI_UpstreamProgress` — spinner while fetching versions
- `TestTUI_PrepareProgress` — progress bar per pipeline step
- `TestTUI_Keybindings` — `q` quit, `j/k` navigate
- `TestTUI_ColorSupport` — degrades in non-color terminals
- `TestTUI_NonTTY_Fallback` — stdout not TTY → falls back to table output
- `TestTUI_ProvideSourceNames` — displays provider names (GitHub, PyPI, NPM, etc.)

**Implementation:**
1. `internal/cli/upstream_tui.go` — bubbletea model: spinner while fetching, colored table with OSC 8 hyperlinks in source column
2. `internal/cli/prepare_tui.go` — bubbletea model: progress bar per step, status text (downloaded, uploaded, etc.)
3. Trigger: `larry upstream --tui` or `larry prepare --tui`
4. Fallback: non-TTY or no `--tui` flag → table output (from T10)
5. Lipgloss layout, termenv colors

**Acceptance:** `--tui` shows animated UI. Non-TTY falls back to table. Keybindings work.
