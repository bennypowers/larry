package cli

import (
	"bytes"
	"testing"

	"bennypowers.dev/larry/internal/output"
	"bennypowers.dev/larry/internal/upstream"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// TestTUI_UpstreamTable — renders upstream results as colored table
// ---------------------------------------------------------------------------

func TestTUI_UpstreamTable(t *testing.T) {
	results := []*upstream.UpstreamResult{
		{Package: "dev-lisp/foo", Current: "1.0.0", Latest: "2.0.0", Status: upstream.StatusOutdated, Source: upstream.SourceInfo{Label: "GitHub:test/pkg", URL: "github.com/test/pkg"}},
		{Package: "dev-python/bar", Current: "2.0.0", Latest: "2.0.0", Status: upstream.StatusUpToDate, Source: upstream.SourceInfo{Label: "GitHub:test/pkg", URL: "github.com/test/pkg"}},
		{Package: "dev-ruby/baz", Current: "3.0.0", Latest: "", Status: upstream.StatusUnknown, Source: upstream.SourceInfo{}},
	}

	m := newUpstreamModel(results)
	view := m.View()

	// Should contain package names
	if !bytes.Contains([]byte(view), []byte("dev-lisp/foo")) {
		t.Error("table should contain 'dev-lisp/foo'")
	}
	if !bytes.Contains([]byte(view), []byte("dev-python/bar")) {
		t.Error("table should contain 'dev-python/bar'")
	}
	if !bytes.Contains([]byte(view), []byte("dev-ruby/baz")) {
		t.Error("table should contain 'dev-ruby/baz'")
	}

	// Should contain status markers
	if !bytes.Contains([]byte(view), []byte("OUTDATED")) {
		t.Error("table should contain 'OUTDATED'")
	}
	if !bytes.Contains([]byte(view), []byte("UP-TO-DATE")) {
		t.Error("table should contain 'UP-TO-DATE'")
	}
	if !bytes.Contains([]byte(view), []byte("UNKNOWN")) {
		t.Error("table should contain 'UNKNOWN'")
	}
}

// ---------------------------------------------------------------------------
// TestTUI_UpstreamProgress — spinner while fetching
// ---------------------------------------------------------------------------

func TestTUI_UpstreamProgress(t *testing.T) {
	m := newUpstreamModel(nil)
	m.state = fetchingState

	view := m.View()

	// Should contain spinner-like content (not empty)
	if len(view) == 0 {
		t.Error("progress view should not be empty")
	}

	// Should not contain a table
	if bytes.Contains([]byte(view), []byte("OUTDATED")) {
		t.Error("progress view should not contain table data")
	}
}

// ---------------------------------------------------------------------------
// TestTUI_ProvideSourceNames — displays provider names
// ---------------------------------------------------------------------------

func TestTUI_ProvideSourceNames(t *testing.T) {
	results := []*upstream.UpstreamResult{
		{Package: "dev-lisp/foo", Current: "1.0.0", Latest: "2.0.0", Status: upstream.StatusOutdated, Source: upstream.SourceInfo{Label: "GitHub:test/pkg", URL: "github.com/test/pkg"}},
		{Package: "dev-python/bar", Current: "2.0.0", Latest: "2.0.0", Status: upstream.StatusUpToDate, Source: upstream.SourceInfo{Label: "GitHub:test/pkg", URL: "github.com/test/pkg"}},
	}

	m := newUpstreamModel(results)
	view := m.View()

	// Should contain source labels
	if !bytes.Contains([]byte(view), []byte("GitHub:test/pkg")) {
		t.Error("table should contain source label")
	}
}

// ---------------------------------------------------------------------------
// TestTUI_NonTTY_Fallback — stdout not TTY → falls back to table output
// ---------------------------------------------------------------------------

func TestTUI_NonTTY_Fallback(t *testing.T) {
	results := []*upstream.UpstreamResult{
		{Package: "dev-lisp/foo", Current: "1.0.0", Latest: "2.0.0", Status: upstream.StatusOutdated, Source: upstream.SourceInfo{Label: "GitHub:test/pkg", URL: "github.com/test/pkg"}},
	}

	// When output is not TTY, table output should be used instead of TUI
	view := nonTTYTable(results)

	// Should contain package name
	if !bytes.Contains([]byte(view), []byte("dev-lisp/foo")) {
		t.Error("fallback table should contain 'dev-lisp/foo'")
	}
}

// ---------------------------------------------------------------------------
// TestTUI_ColorSupport — degrades in non-color terminals
// ---------------------------------------------------------------------------

func TestTUI_ColorSupport(t *testing.T) {
	results := []*upstream.UpstreamResult{
		{Package: "dev-lisp/foo", Current: "1.0.0", Latest: "2.0.0", Status: upstream.StatusOutdated, Source: upstream.SourceInfo{Label: "GitHub:test/pkg", URL: "github.com/test/pkg"}},
	}

	// Non-color model
	ch, _ := output.NewColorHelper("never")
	m := newUpstreamModelWithColors(results, ch)
	view := m.View()

	// Should still render without error
	if len(view) == 0 {
		t.Error("non-color view should not be empty")
	}

	// Color model
	ch2, _ := output.NewColorHelper("always")
	m2 := newUpstreamModelWithColors(results, ch2)
	view2 := m2.View()

	if len(view2) == 0 {
		t.Error("color view should not be empty")
	}
}

// ---------------------------------------------------------------------------
// TestTUI_Keybindings — q quit, j/k navigate
// ---------------------------------------------------------------------------

func TestTUI_Keybindings_Quit(t *testing.T) {
	results := []*upstream.UpstreamResult{
		{Package: "dev-lisp/foo", Current: "1.0.0", Latest: "2.0.0", Status: upstream.StatusOutdated, Source: upstream.SourceInfo{Label: "GitHub:test/pkg", URL: "github.com/test/pkg"}},
	}

	m := newUpstreamModel(results)
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if !m.quitting() {
		t.Error("Ctrl+C should quit")
	}

	_, msg := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if msg == nil {
		t.Error("should return message on quit")
	}
}

func TestTUI_Keybindings_Navigate(t *testing.T) {
	results := []*upstream.UpstreamResult{
		{Package: "dev-lisp/foo", Current: "1.0.0", Latest: "2.0.0", Status: upstream.StatusOutdated, Source: upstream.SourceInfo{Label: "GitHub:test/pkg", URL: "github.com/test/pkg"}},
		{Package: "dev-python/bar", Current: "2.0.0", Latest: "2.0.0", Status: upstream.StatusUpToDate, Source: upstream.SourceInfo{Label: "GitHub:test/pkg", URL: "github.com/test/pkg"}},
		{Package: "dev-ruby/baz", Current: "3.0.0", Latest: "", Status: upstream.StatusUnknown, Source: upstream.SourceInfo{}},
	}

	m := newUpstreamModel(results)
	_ = m.View()

	// Navigate down with j
	_, msg := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if msg != nil {
		t.Errorf("down navigation returned unexpected msg: %v", msg)
	}
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}

	// Navigate down again
	_, msg = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if msg != nil {
		t.Errorf("down navigation returned unexpected msg: %v", msg)
	}
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2", m.cursor)
	}

	// Navigate up with k
	_, msg = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if msg != nil {
		t.Errorf("up navigation returned unexpected msg: %v", msg)
	}
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}

	// Navigate up to boundary
	_, msg = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if msg != nil {
		t.Errorf("up navigation returned unexpected msg: %v", msg)
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (boundary)", m.cursor)
	}
}
