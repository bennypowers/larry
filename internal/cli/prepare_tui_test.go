package cli

import (
	"bytes"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	stepPending = "pending"
	stepRunning = "running"
	stepDone    = "done"
	stepError   = "error"
)

// ---------------------------------------------------------------------------
// TestTUI_PrepareProgress — progress bar per pipeline step
// ---------------------------------------------------------------------------

func TestTUI_PrepareProgress(t *testing.T) {
	steps := []prepareStep{
		{Name: "Resolve upstream repo", State: stepDone},
		{Name: "Download Cargo.lock", State: stepDone},
		{Name: "Parse dependencies", State: stepRunning},
		{Name: "Update ebuild CRATES", State: stepPending},
	}

	m := newPrepareModel(steps)
	view := m.View()

	// Should contain step names
	for _, step := range steps {
		if !bytes.Contains([]byte(view), []byte(step.Name)) {
			t.Errorf("view should contain '%s'", step.Name)
		}
	}

	// Should contain progress indicators
	if !bytes.Contains([]byte(view), []byte(stepDone)) {
		t.Error("view should contain 'done'")
	}
	if !bytes.Contains([]byte(view), []byte(stepRunning)) {
		t.Error("view should contain 'running'")
	}
	if !bytes.Contains([]byte(view), []byte(stepPending)) {
		t.Error("view should contain 'pending'")
	}
}

// ---------------------------------------------------------------------------
// TestTUI_PrepareProgress_Done — all steps completed
// ---------------------------------------------------------------------------

func TestTUI_PrepareProgress_Done(t *testing.T) {
	steps := []prepareStep{
		{Name: "Download", State: stepDone},
		{Name: "Parse", State: stepDone},
		{Name: "Update ebuild", State: stepDone},
	}

	m := newPrepareModel(steps)
	view := m.View()

	// Should show completion message
	if !bytes.Contains([]byte(view), []byte(stepDone)) {
		t.Error("done view should contain 'done'")
	}

	// Should not show spinner
	if bytes.Contains([]byte(view), []byte("...")) {
		t.Error("done view should not contain spinner")
	}
}

// ---------------------------------------------------------------------------
// TestTUI_PrepareProgress_Error — error state
// ---------------------------------------------------------------------------

func TestTUI_PrepareProgress_Error(t *testing.T) {
	steps := []prepareStep{
		{Name: "Download", State: stepDone},
		{Name: "Parse", State: stepError},
		{Name: "Update", State: stepPending},
	}

	m := newPrepareModel(steps)
	view := m.View()

	// Should contain error indicator
	if !bytes.Contains([]byte(view), []byte(stepError)) {
		t.Error("error view should contain 'error'")
	}
}

// ---------------------------------------------------------------------------
// TestTUI_PrepareProgress_Keybindings
// ---------------------------------------------------------------------------

func TestTUI_PrepareProgress_Keybindings(t *testing.T) {
	steps := []prepareStep{
		{Name: "Step 1", State: stepDone},
		{Name: "Step 2", State: stepRunning},
	}

	m := newPrepareModel(steps)

	// Ctrl+C should quit
	_, msg := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m.quitting() {
		t.Error("Ctrl+C should quit")
	}
	if msg == nil {
		t.Error("should return message on quit")
	}
}

// ---------------------------------------------------------------------------
// TestTUI_PrepareProgress_InitialView
// ---------------------------------------------------------------------------

func TestTUI_PrepareProgress_InitialView(t *testing.T) {
	steps := []prepareStep{
		{Name: "Step 1", State: stepRunning},
		{Name: "Step 2", State: stepPending},
	}

	m := newPrepareModel(steps)
	view := m.View()

	// Initial view should show running step
	if !bytes.Contains([]byte(view), []byte("Step 1")) {
		t.Error("view should contain 'Step 1'")
	}
	if !bytes.Contains([]byte(view), []byte(stepRunning)) {
		t.Error("view should contain 'running'")
	}
}
