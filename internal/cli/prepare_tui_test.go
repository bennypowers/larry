package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/prepare"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func newPrepareModelForTest(t *testing.T) *PrepareModel {
	t.Helper()
	var buf bytes.Buffer
	theme := NewTheme(ThemeOptions{Renderer: lipgloss.NewRenderer(&buf)})
	m := NewPrepareModel(PrepareModelOptions{
		Context: context.Background(),
		Events:  make(chan prepare.ProgressEvent, 8),
		Theme:   theme,
		Title:   "dev-util/foo",
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func sendPrepareEvent(m *PrepareModel, ev prepare.ProgressEvent) {
	m.Update(prepareEventMsg(ev))
}

func TestPrepareModel_ProgressBar(t *testing.T) {
	m := newPrepareModelForTest(t)
	sendPrepareEvent(m, prepare.ProgressEvent{Kind: prepare.ProgressStart, Total: 3})
	sendPrepareEvent(m, prepare.ProgressEvent{Kind: prepare.ProgressStep, Step: "download", Done: 1, Total: 3})
	if view := m.View(); !strings.Contains(view, "1/3") {
		t.Errorf("view missing 1/3:\n%s", view)
	}
}

func TestPrepareModel_StepTable(t *testing.T) {
	m := newPrepareModelForTest(t)
	sendPrepareEvent(m, prepare.ProgressEvent{Kind: prepare.ProgressStep, Step: "download", Done: 0, Total: 2})
	sendPrepareEvent(m, prepare.ProgressEvent{Kind: prepare.ProgressStep, Step: "manifest", Done: 1, Total: 2})
	view := m.View()
	for _, want := range []string{"download", "manifest", "done", "running"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestPrepareModel_LogViewport(t *testing.T) {
	m := newPrepareModelForTest(t)
	for _, line := range []string{"first line", "second line", "third line"} {
		sendPrepareEvent(m, prepare.ProgressEvent{Kind: prepare.ProgressLog, Message: line})
	}
	if view := m.View(); !strings.Contains(view, "third line") {
		t.Errorf("log view missing last line:\n%s", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	_ = m.View()
}

func TestPrepareModel_ErrorEvent(t *testing.T) {
	m := newPrepareModelForTest(t)
	sendPrepareEvent(m, prepare.ProgressEvent{
		Kind:    prepare.ProgressError,
		Message: "manifest failed",
		Err:     errors.New("manifest failed"),
	})
	if m.Err() == nil || !strings.Contains(m.Err().Error(), "manifest failed") {
		t.Errorf("Err() = %v", m.Err())
	}
	if !strings.Contains(m.View(), "manifest failed") {
		t.Errorf("view does not show the error:\n%s", m.View())
	}
}

func TestPrepareModel_ChannelClose(t *testing.T) {
	m := newPrepareModelForTest(t)
	m.Update(prepareEventsClosedMsg{})
	_ = m.View()
	if m.Quitting() {
		t.Error("channel close should not quit")
	}
}

func TestPrepareModel_Keybindings_Quit(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyEsc},
		{Type: tea.KeyCtrlC},
	} {
		m := newPrepareModelForTest(t)
		m.Update(key)
		if !m.Quitting() {
			t.Errorf("%v did not quit", key)
		}
	}
}

func TestPrepareModel_DoneStopsSpinner(t *testing.T) {
	m := newPrepareModelForTest(t)
	sendPrepareEvent(m, prepare.ProgressEvent{Kind: prepare.ProgressDone, Done: 2, Total: 2})
	if !m.Done() {
		t.Error("Done() = false after ProgressDone")
	}
	if view := m.View(); !strings.Contains(view, "2/2") {
		t.Errorf("completed view missing 2/2:\n%s", view)
	}
}

func TestPrepareModel_ColorDisabledNoANSI(t *testing.T) {
	m := newPrepareModelForTest(t)
	sendPrepareEvent(m, prepare.ProgressEvent{Kind: prepare.ProgressStep, Step: "download", Done: 1, Total: 2})
	sendPrepareEvent(m, prepare.ProgressEvent{Kind: prepare.ProgressLog, Message: "line"})
	if view := m.View(); strings.Contains(view, "\x1b[") {
		t.Errorf("colour-disabled prepare model emitted ANSI:\n%q", view)
	}
}

func TestPrepareModel_InitReturnsCommand(t *testing.T) {
	m := newPrepareModelForTest(t)
	if m.Init() == nil {
		t.Error("Init() = nil, want a batch command")
	}
}
