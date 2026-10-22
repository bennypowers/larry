package cli

import (
	"fmt"
	"os"
	"strings"

	"bennypowers.dev/larry/internal/output"
	"bennypowers.dev/larry/internal/upstream"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// UpstreamState represents the TUI state for upstream command
type upstreamState int

const (
	fetchingState upstreamState = iota
	resultState
)

// upstreamModel is the bubbletea model for upstream --tui
type upstreamModel struct {
	results []*upstream.UpstreamResult
	state   upstreamState
	cursor  int
	colors  *output.ColorHelper
	width   int
	quit    bool
}

// newUpstreamModel creates a new upstream TUI model
func newUpstreamModel(results []*upstream.UpstreamResult) *upstreamModel {
	ch, _ := output.NewColorHelper("never")
	if ch == nil {
		ch, _ = output.NewColorHelper("auto")
	}
	return newUpstreamModelWithColors(results, ch)
}

// NewUpstreamModel exports the upstream model constructor for CLI
var NewUpstreamModel = newUpstreamModel

// newUpstreamModelWithColors creates a model with explicit colors
func newUpstreamModelWithColors(results []*upstream.UpstreamResult, ch *output.ColorHelper) *upstreamModel {
	return &upstreamModel{
		results: results,
		state:   resultState,
		colors:  ch,
		width:   80,
	}
}

// NewUpstreamModelWithColors exports the constructor for testing
var NewUpstreamModelWithColors = newUpstreamModelWithColors

// Quitting exports the quitting method for testing
func (m *upstreamModel) Quitting() bool {
	return m.quit
}

// Init implements tea.Model
func (m *upstreamModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (m *upstreamModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.cursor < len(m.results)-1 {
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
					if m.cursor < len(m.results)-1 {
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
func (m *upstreamModel) View() string {
	switch m.state {
	case fetchingState:
		return m.renderFetching()
	case resultState:
		return m.renderResults()
	}
	return "unknown state"
}

// quitting returns true if the model should quit
func (m *upstreamModel) quitting() bool {
	return m.quit
}

func (m *upstreamModel) renderFetching() string {
	return "Checking upstream versions..."
}

func (m *upstreamModel) renderResults() string {
	if len(m.results) == 0 {
		return m.colors.Dim("No results to display.")
	}

	// Build table header
	header := lipgloss.NewStyle().
		Bold(true).
		Width(m.width).
		Render("PACKAGE", "CURRENT", "LATEST", "STATUS", "SOURCE")

	// Build table rows
	var rows []string
	for i, r := range m.results {
		status := m.statusColor(r.Status)
		source := r.Source.Label

		// Create hyperlink for source
		if r.Source.URL != "" && m.colors.Enabled() {
			source = m.colors.Hyperlink(r.Source.URL, source)
		}

		// Build row text — indent Package cell by 2 spaces per depth level (plan 18).
		rowText := fmt.Sprintf("%-30s %-12s %-12s %-12s %s",
			strings.TrimSpace(strings.Repeat("  ", r.Depth)+r.Package),
			strings.TrimSpace(r.Current),
			strings.TrimSpace(r.Latest),
			status,
			source)

		// Highlight selected row
		var row string
		if i == m.cursor {
			row = lipgloss.NewStyle().
				Width(m.width).
				Bold(true).
				Render(rowText)
		} else {
			row = lipgloss.NewStyle().
				Width(m.width).
				Render(rowText)
		}

		rows = append(rows, row)
	}

	// Build footer
	footer := lipgloss.NewStyle().
		Width(m.width).
		Bold(true).
		Render("↑/↓: navigate  Ctrl+C: quit")

	return header + "\n" + strings.Join(rows, "\n") + "\n" + footer
}

func (m *upstreamModel) statusColor(s upstream.Status) string {
	switch s {
	case upstream.StatusOutdated:
		return m.colors.Red("OUTDATED")
	case upstream.StatusUpToDate:
		return m.colors.Green("UP-TO-DATE")
	case upstream.StatusAhead:
		return m.colors.Cyan("AHEAD")
	case upstream.StatusNewerTag:
		return m.colors.Yellow("NEWER-TAG")
	case upstream.StatusCheckTag:
		return m.colors.Yellow("CHECK-TAG")
	case upstream.StatusLive:
		return m.colors.Blue("LIVE")
	case upstream.StatusHasRelease:
		return m.colors.Blue("HAS-RELEASE")
	case upstream.StatusUnknown:
		return m.colors.Yellow("UNKNOWN")
	case upstream.StatusManual:
		return m.colors.Dim("MANUAL")
	default:
		return m.colors.Dim("UNKNOWN")
	}
}

// nonTTYTable renders a simple table for non-TTY output
func nonTTYTable(results []*upstream.UpstreamResult) string {
	if len(results) == 0 {
		return "No results to display."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%-30s %-12s %-12s %-12s %s\n", "PACKAGE", "CURRENT", "LATEST", "STATUS", "SOURCE")
	b.WriteString(strings.Repeat("-", 80) + "\n")

	for _, r := range results {
		status := r.Status.String()
		// Indent Package cell by 2 spaces per depth level (plan 18).
		pkg := strings.Repeat("  ", r.Depth) + r.Package
		fmt.Fprintf(&b, "%-30s %-12s %-12s %-12s %s\n",
			pkg, r.Current, r.Latest, status, r.Source.Label)
	}

	_ = os.Stdout
	return b.String()
}
