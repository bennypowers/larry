package cli

import (
	"fmt"
	"os"
	"strings"

	"bennypowers.dev/larry/internal/output"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// prepareStep represents a step in the prepare pipeline
type prepareStep struct {
	Name  string
	State string // "pending", "running", "done", "error"
}

// prepareModel is the bubbletea model for prepare --tui
type prepareModel struct {
	steps  []prepareStep
	cursor int
	colors *output.ColorHelper
	width  int
	quit   bool
}

// newPrepareModel creates a new prepare TUI model
func newPrepareModel(steps []prepareStep) *prepareModel {
	ch, _ := output.NewColorHelper("never")
	if ch == nil {
		// Fallback: create a dummy color helper
		ch, _ = output.NewColorHelper("auto")
	}
	return &prepareModel{
		steps:  steps,
		colors: ch,
		width:  80,
	}
}

// NewPrepareModel exports the constructor for testing
var NewPrepareModel = newPrepareModel

// Quitting exports the quitting method for testing
func (m *prepareModel) Quitting() bool {
	return m.quit
}

// Init implements tea.Model
func (m *prepareModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (m *prepareModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			m.quit = true
			return m, tea.Quit
		case tea.KeyDown:
			if m.cursor < len(m.steps)-1 {
				m.cursor++
			}
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyRunes:
			for _, r := range msg.Runes {
				switch r {
				case 'j':
					if m.cursor < len(m.steps)-1 {
						m.cursor++
					}
				case 'k':
					if m.cursor > 0 {
						m.cursor--
					}
				}
			}
		}
	}
	return m, nil
}

// View implements tea.Model
func (m *prepareModel) View() string {
	var b strings.Builder

	// Header
	header := lipgloss.NewStyle().
		Bold(true).
		Width(m.width).
		Render("Prepare Pipeline")
	b.WriteString(header + "\n")
	b.WriteString(strings.Repeat("-", 60) + "\n")

	// Steps
	for i, step := range m.steps {
		var status string
		var prefix string

		switch step.State {
		case "done":
			status = m.colors.Green("done")
			prefix = "✓"
		case "running":
			status = m.colors.Yellow("running")
			prefix = "⟳"
		case "error":
			status = m.colors.Red("error")
			prefix = "✗"
		case "pending":
			status = m.colors.Dim("pending")
			prefix = "○"
		default:
			status = m.colors.Dim("unknown")
			prefix = "?"
		}

		row := lipgloss.NewStyle().
			Width(m.width).
			Render(fmt.Sprintf("  %s %s  %s", prefix, step.Name, status))

		// Highlight current step
		if i == m.cursor && step.State == "running" {
			row = lipgloss.NewStyle().
				Width(m.width).
				Bold(true).
				Render(fmt.Sprintf("  %s %s  %s", prefix, step.Name, status))
		}

		b.WriteString(row + "\n")
	}

	// Footer
	footer := lipgloss.NewStyle().
		Width(m.width).
		Bold(true).
		Render("↑/↓: navigate  Ctrl+C: quit")
	b.WriteString(footer)

	_ = os.Stdout
	return b.String()
}

// quitting returns true if the model should quit
func (m *prepareModel) quitting() bool {
	return m.quit
}

// UpdateSteps updates the step states
func (m *prepareModel) UpdateSteps(steps []prepareStep) {
	m.steps = steps
}

// SetRunning sets a step to running state
func (m *prepareModel) SetRunning(stepName string) {
	for i := range m.steps {
		if m.steps[i].Name == stepName {
			m.steps[i].State = "running"
			m.cursor = i
			break
		}
	}
}

// SetDone marks a step as done
func (m *prepareModel) SetDone(stepName string) {
	for i := range m.steps {
		if m.steps[i].Name == stepName {
			m.steps[i].State = "done"
			break
		}
	}
}

// SetError marks a step as error
func (m *prepareModel) SetError(stepName string, errMsg string) {
	for i := range m.steps {
		if m.steps[i].Name == stepName {
			m.steps[i].State = "error"
			break
		}
	}
}
