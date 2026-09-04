# TUI Tasks (T22)

**What this covers:** Bubbletea progress spinner + result table for `upstream` and `prepare`.
**Why last (functional):** Optional polish on top of working commands.

---

### T22 — Bubbletea TUI for progress + results

**Precondition:** T10 complete (upstream output is the primary TUI target)

**Tests:**
- `TestTUI_UpstreamTable` — renders upstream results as color table in TUI
- `TestTUI_UpstreamProgress` — spinner while checking versions
- `TestTUI_PrepareProgress` — progress bar for prepare pipeline steps
- `TestTUI_Keybindings` — `q` to quit, `j/k` to navigate
- `TestTUI_ColorSupport` — gracefully degrades in non-color terminals
- `TestTUI_NonTTY_Fallback` — when stdout is not a TTY, falls back to table output

**Implementation:**
1. `internal/cli/upstream_tui.go` — bubbletea model for `larry upstream`:
   - Spinner while fetching versions
   - Animated table with color-coded status
   - OSC 8 hyperlinks in source column
2. `internal/cli/prepare_tui.go` — bubbletea model for `larry prepare`:
   - Progress bar per pipeline step
   - Status text per step (downloaded, uploaded, etc.)
3. Trigger: `larry upstream --tui` or `larry prepare --tui` flag
4. Fallback: if stdout is not a TTY or `--tui` not set, use table output (from T10)
5. Lipgloss for layout, termenv for colors

**Acceptance:** `larry upstream --tui` shows animated TUI. Non-TTY terminals fall back to table. All keybindings work.
