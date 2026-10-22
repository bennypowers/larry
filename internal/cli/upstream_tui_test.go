package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/upstream"

	tea "github.com/charmbracelet/bubbletea"
)

func newModelForTest(t *testing.T, showAll bool) (*UpstreamModel, *bytes.Buffer) {
	t.Helper()
	theme, _ := themeForTest(false, false)
	m := NewUpstreamModel(UpstreamModelOptions{
		Context: context.Background(),
		Fetch: func(context.Context, func(upstream.Progress)) ([]upstream.UpstreamResult, error) {
			return nil, nil
		},
		ShowAll: showAll,
		Theme:   theme,
		Overlay: "/tmp/overlay",
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m, nil
}

func sampleResults() []upstream.UpstreamResult {
	return []upstream.UpstreamResult{
		{
			Package: "dev-util/alpha", Current: "1.0.0", Latest: "1.1.0",
			Status: upstream.StatusOutdated,
			Source: upstream.SourceInfo{Label: "GitHub:acme/alpha", URL: "https://github.com/acme/alpha"},
		},
		{
			Package: "dev-util/dep", Current: "2.0.0", Latest: "2.0.0",
			Status: upstream.StatusUpToDate, Depth: 1,
			Source: upstream.SourceInfo{Label: "GitHub:acme/dep", URL: "https://github.com/acme/dep"},
		},
		{
			Package: "dev-util/broken", Current: "3.0.0",
			Status: upstream.StatusUnknown,
			Source: upstream.SourceInfo{},
			Error:  "no provider match",
		},
	}
}

func TestUpstreamModel_InitialViewShowsSpinner(t *testing.T) {
	m, _ := newModelForTest(t, false)
	view := m.View()
	if !strings.Contains(view, "Checking upstream") {
		t.Errorf("initial view = %q, want a checking message", view)
	}
}

func TestUpstreamModel_InitReturnsCommand(t *testing.T) {
	m, _ := newModelForTest(t, false)
	if m.Init() == nil {
		t.Error("Init() = nil, want a batch command")
	}
}

func TestUpstreamModel_DoneRendersTable(t *testing.T) {
	m, _ := newModelForTest(t, true)
	m.Update(upstreamDoneMsg{Results: sampleResults()})
	view := m.View()
	for _, want := range []string{"dev-util/alpha", "dev-util/dep", "OUTDATED", "UP-TO-DATE", "  dev-util/dep"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestUpstreamModel_ShowAllFilter(t *testing.T) {
	m, _ := newModelForTest(t, false)
	m.Update(upstreamDoneMsg{Results: sampleResults()})
	view := m.View()
	if strings.Contains(view, "dev-util/dep") {
		t.Errorf("default view showed an UP-TO-DATE row:\n%s", view)
	}
	if !strings.Contains(view, "dev-util/alpha") {
		t.Errorf("default view hid the significant row:\n%s", view)
	}

	mAll, _ := newModelForTest(t, true)
	mAll.Update(upstreamDoneMsg{Results: sampleResults()})
	if !strings.Contains(mAll.View(), "dev-util/dep") {
		t.Errorf("ShowAll view hid the UP-TO-DATE row:\n%s", mAll.View())
	}
}

func TestUpstreamModel_DoneError(t *testing.T) {
	m, _ := newModelForTest(t, false)
	m.Update(upstreamDoneMsg{Err: errors.New("walk exploded")})
	if m.Err() == nil || !strings.Contains(m.Err().Error(), "walk exploded") {
		t.Errorf("Err() = %v", m.Err())
	}
	if !strings.Contains(m.View(), "walk exploded") {
		t.Errorf("view does not show the error:\n%s", m.View())
	}
}

func TestUpstreamModel_Keybindings_Quit(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyEsc},
		{Type: tea.KeyCtrlC},
	} {
		m, _ := newModelForTest(t, false)
		m.Update(key)
		if !m.Quitting() {
			t.Errorf("%v did not quit", key)
		}
	}
}

func TestUpstreamModel_Keybindings_Navigate(t *testing.T) {
	m, _ := newModelForTest(t, true)
	m.Update(upstreamDoneMsg{Results: sampleResults()})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.Selected(); got != 1 {
		t.Errorf("after down Selected() = %d, want 1", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got := m.Selected(); got != 2 {
		t.Errorf("after j Selected() = %d, want 2", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.Selected(); got != 1 {
		t.Errorf("after up Selected() = %d, want 1", got)
	}
}

func TestUpstreamModel_ScrollToBottom(t *testing.T) {
	m, _ := newModelForTest(t, true)
	m.Update(upstreamDoneMsg{Results: sampleResults()})
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if got, want := m.Selected(), len(sampleResults())-1; got != want {
		t.Errorf("after end Selected() = %d, want %d", got, want)
	}
}

func TestUpstreamModel_DetailToggle(t *testing.T) {
	m, _ := newModelForTest(t, true)
	m.Update(upstreamDoneMsg{Results: sampleResults()})
	if strings.Contains(m.View(), "https://github.com/acme/alpha") {
		t.Fatal("detail visible before toggling")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := m.View()
	if !strings.Contains(view, "https://github.com/acme/alpha") {
		t.Errorf("detail toggle did not show the source URL:\n%s", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if strings.Contains(m.View(), "https://github.com/acme/alpha") {
		t.Errorf("detail did not hide on second toggle:\n%s", m.View())
	}
}

func TestUpstreamModel_ProgressMessage(t *testing.T) {
	m, _ := newModelForTest(t, false)
	m.Update(upstreamProgressMsg(upstream.Progress{Package: "dev-util/alpha", Done: 1, Total: 3}))
	if !strings.Contains(m.View(), "1/3") {
		t.Errorf("view = %q, want checked 1/3", m.View())
	}
}

func TestUpstreamModel_WindowResize(t *testing.T) {
	m, _ := newModelForTest(t, true)
	m.Update(upstreamDoneMsg{Results: sampleResults()})
	for _, size := range []tea.WindowSizeMsg{{Width: 120, Height: 40}, {Width: 20, Height: 6}} {
		m.Update(size)
		_ = m.View()
	}
}

func TestUpstreamModel_ColorDisabledNoANSI(t *testing.T) {
	m, _ := newModelForTest(t, true)
	m.Update(upstreamDoneMsg{Results: sampleResults()})
	if view := m.View(); strings.Contains(view, "\x1b[") {
		t.Errorf("colour-disabled model emitted ANSI:\n%q", view)
	}
}

func actionModel(t *testing.T, actions UpstreamActions) *UpstreamModel {
	t.Helper()
	theme, _ := themeForTest(false, false)
	m := NewUpstreamModel(UpstreamModelOptions{
		Context: context.Background(),
		Fetch: func(context.Context, func(upstream.Progress)) ([]upstream.UpstreamResult, error) {
			return nil, nil
		},
		ShowAll: true,
		Theme:   theme,
		Overlay: "/tmp/overlay",
		Actions: actions,
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(upstreamDoneMsg{Results: sampleResults()})
	return m
}

func drainCmd(t *testing.T, m *UpstreamModel, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return
		}
		if _, batch := msg.(tea.BatchMsg); batch {
			return // batch commands are not executed by direct model tests
		}
		_, next := m.Update(msg)
		cmd = next
	}
}

func TestUpstreamModel_ActionConfirmation(t *testing.T) {
	called := false
	m := actionModel(t, UpstreamActions{Bump: func(context.Context, string, func(string)) error {
		called = true
		return nil
	}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if view := m.View(); !strings.Contains(view, "Run bump") {
		t.Errorf("view missing confirmation prompt:\n%s", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if called {
		t.Error("action ran after declining confirmation")
	}
	if strings.Contains(m.View(), "Run bump") {
		t.Error("confirmation prompt stayed after declining")
	}
}

func TestUpstreamModel_RunsAction(t *testing.T) {
	var calledPkg string
	var lines []string
	m := actionModel(t, UpstreamActions{Bump: func(_ context.Context, pkg string, onLine func(string)) error {
		calledPkg = pkg
		onLine("bumped " + pkg)
		onLine("committed")
		return nil
	}})

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	drainCmd(t, m, cmd)

	if calledPkg != "dev-util/alpha" {
		t.Errorf("action ran for %q, want dev-util/alpha", calledPkg)
	}
	lines = m.ActionLines()
	if len(lines) != 2 || !strings.Contains(lines[0], "bumped dev-util/alpha") {
		t.Errorf("action lines = %v", lines)
	}
	// A successful action refreshes the table.
	if !strings.Contains(m.View(), "Checking upstream") {
		t.Errorf("view after successful action:\n%s", m.View())
	}
}

func TestUpstreamModel_ActionError(t *testing.T) {
	m := actionModel(t, UpstreamActions{Test: func(context.Context, string, func(string)) error {
		return errors.New("test failed")
	}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	drainCmd(t, m, cmd)
	if m.ActionErr() == nil || !strings.Contains(m.ActionErr().Error(), "test failed") {
		t.Errorf("ActionErr() = %v", m.ActionErr())
	}
	if !strings.Contains(m.View(), "test failed") {
		t.Errorf("view does not show the action error:\n%s", m.View())
	}
}
