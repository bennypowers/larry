package cli

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// spinnerFrames are the frames for the loading spinner animation.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// AnimateSpinner runs fn() while printing a spinner animation.
// Returns fn's string result.
// If quiet is true, the spinner is suppressed.
func AnimateSpinner(msg string, quiet bool, fn func() string) string {
	if quiet {
		out := fn()
		fmt.Print(out)
		return out
	}

	// Check if stdout is a TTY
	if !isTTYStdout() {
		out := fn()
		fmt.Print(out)
		return out
	}

	// Run fn in a goroutine so spinner can run concurrently
	ch := make(chan string, 1)
	go func() {
		ch <- fn()
	}()

	s := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			frame := spinnerFrames[s%len(spinnerFrames)]
			s++
			fmt.Printf("\r%s %s  ", frame, msg)
		case out := <-ch:
			// Move to a new line and print the result
			fmt.Println()
			fmt.Print(out)
			return out
		}
	}
}

// isTTYStdout checks if stdout is a TTY using golang.org/x/term.
func isTTYStdout() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// ---- bubbletea spinner model for TUI mode ----

// spinnerTickMsg is a message fired by the spinner tick command.
type spinnerTickMsg struct{}

// spinnerModel renders a spinning indicator.
type spinnerModel struct {
	spinner *Spinner
	message string
	width   int
	quit    bool
}

// Spinner is a simple loading spinner for use in bubbletea models.
type Spinner struct {
	frames   []string
	frame    int
	interval time.Duration
}

// NewSpinner creates a new Spinner with default settings.
func NewSpinner() *Spinner {
	return &Spinner{
		frames:   spinnerFrames,
		interval: 100 * time.Millisecond,
	}
}

// Tick returns the next frame and a tea.Cmd that fires after the interval.
func (s *Spinner) Tick() tea.Cmd {
	return tea.Tick(s.interval, func(t time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// Next advances to the next frame and returns it.
func (s *Spinner) Next() string {
	frame := s.frames[s.frame]
	s.frame = (s.frame + 1) % len(s.frames)
	return frame
}

// NewSpinnerModel creates a new spinner model for bubbletea.
func NewSpinnerModel(msg string) *spinnerModel {
	return &spinnerModel{
		spinner: NewSpinner(),
		message: msg,
		width:   80,
	}
}

func (m *spinnerModel) Init() tea.Cmd {
	return m.spinner.Tick()
}

func (m *spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.(tea.WindowSizeMsg).Width
	case tea.KeyMsg:
		if msg.(tea.KeyMsg).Type == tea.KeyCtrlC {
			m.quit = true
			return m, tea.Quit
		}
	case spinnerTickMsg:
		return m, m.spinner.Tick()
	}
	return m, nil
}

func (m *spinnerModel) View() string {
	if m.quit {
		return ""
	}
	return lipgloss.NewStyle().
		Width(m.width).
		Bold(true).
		Render(m.spinner.Next() + "  " + m.message) + "\n"
}

// FetchingStateView renders the spinner view for the upstream TUI.
var FetchingStateView = func() string {
	// Use a fixed frame for the TUI fetching state
	return lipgloss.NewStyle().
		Bold(true).
		Render("Checking upstream versions...\n")
}
