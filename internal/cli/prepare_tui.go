package cli

import (
	"context"
	"fmt"
	"strings"

	"bennypowers.dev/larry/internal/prepare"
	"bennypowers.dev/larry/internal/upstream"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PrepareModelOptions configures the interactive prepare progress view.
type PrepareModelOptions struct {
	Context context.Context
	Events  <-chan prepare.ProgressEvent
	Theme   *Theme
	Title   string
	Cancel  context.CancelFunc
}

type prepareStep struct {
	Name  string
	State string // pending, running, done, error
}

type prepareEventMsg prepare.ProgressEvent
type prepareEventsClosedMsg struct{}

// PrepareModel is the Bubble Tea model for `larry prepare --tui`.
type PrepareModel struct {
	ctx    context.Context
	cancel context.CancelFunc
	events <-chan prepare.ProgressEvent

	theme *Theme
	title string

	spinner  spinner.Model
	progress progress.Model
	table    table.Model
	viewport viewport.Model

	steps []prepareStep
	logs  []string

	done   bool
	closed bool
	quit   bool
	err    error

	processed int
	total     int

	width, height int
}

// NewPrepareModel builds the interactive prepare model.
func NewPrepareModel(opts PrepareModelOptions) *PrepareModel {
	theme := opts.Theme
	if theme == nil {
		theme = NewTheme(ThemeOptions{})
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	if theme.Color {
		sp.Style = theme.Renderer.NewStyle().Foreground(lipgloss.Color("39"))
	} else {
		sp.Style = theme.Renderer.NewStyle()
	}
	tbl := table.New(table.WithFocused(false), table.WithHeight(8))
	tbl.SetStyles(theme.TableStyles())
	vp := viewport.New(80, 8)

	return &PrepareModel{
		ctx:      ctx,
		cancel:   opts.Cancel,
		events:   opts.Events,
		theme:    theme,
		title:    opts.Title,
		spinner:  sp,
		progress: progress.New(progress.WithWidth(40)),
		table:    tbl,
		viewport: vp,
		width:    80,
		height:   24,
	}
}

// Init implements tea.Model.
func (m *PrepareModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.waitForEvent())
}

func (m *PrepareModel) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.events
		if !ok {
			return prepareEventsClosedMsg{}
		}
		return prepareEventMsg(ev)
	}
}

// Update implements tea.Model.
func (m *PrepareModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.done {
			return m, nil
		}
		return m, cmd

	case prepareEventMsg:
		m.handleEvent(prepare.ProgressEvent(msg))
		return m, m.waitForEvent()

	case prepareEventsClosedMsg:
		m.closed = true
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitWithCancel()
			return m, tea.Quit
		case tea.KeyPgUp, tea.KeyPgDown:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case tea.KeyHome:
			m.viewport.GotoTop()
			return m, nil
		case tea.KeyEnd:
			m.viewport.GotoBottom()
			return m, nil
		}
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'q':
				m.quitWithCancel()
				return m, tea.Quit
			case 'g':
				m.viewport.GotoTop()
			case 'G':
				m.viewport.GotoBottom()
			}
		}
		return m, nil
	}
	return m, nil
}

func (m *PrepareModel) quitWithCancel() {
	m.quit = true
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *PrepareModel) handleEvent(ev prepare.ProgressEvent) {
	if ev.Total > 0 {
		m.total = ev.Total
	}
	switch ev.Kind {
	case prepare.ProgressStart:
		// A new package begins; keep the existing log and step history.
	case prepare.ProgressStep:
		m.finishRunning()
		name := ev.Step
		if name == "" {
			name = "step"
			if ev.Package != "" {
				name = ev.Package
			}
		}
		m.setStepRunning(name)
		if ev.Done > 0 {
			m.processed = ev.Done
		}
		if ev.Message != "" && ev.Step != "" {
			m.appendLog(ev.Message)
		}
	case prepare.ProgressLog:
		prefix := ""
		if ev.Package != "" {
			prefix = ev.Package + ": "
		}
		m.appendLog(prefix + ev.Message)
	case prepare.ProgressDone:
		m.finishRunning()
		m.processed = ev.Done
		m.total = ev.Total
		m.done = true
	case prepare.ProgressError:
		m.markRunning("error")
		if ev.Err != nil {
			m.err = ev.Err
		} else if ev.Message != "" {
			m.err = fmt.Errorf("%s", ev.Message)
		}
		if ev.Message != "" {
			m.appendLog(ev.Message)
		}
		m.done = true
	}
	m.rebuildSteps()
}

func (m *PrepareModel) finishRunning() {
	m.markRunning("done")
}

func (m *PrepareModel) markRunning(state string) {
	for i := range m.steps {
		if m.steps[i].State == "running" {
			m.steps[i].State = state
		}
	}
}

func (m *PrepareModel) setStepRunning(name string) {
	for i := range m.steps {
		if m.steps[i].Name == name {
			m.steps[i].State = "running"
			return
		}
	}
	m.steps = append(m.steps, prepareStep{Name: name, State: "running"})
}

func (m *PrepareModel) appendLog(line string) {
	if line == "" {
		return
	}
	m.logs = append(m.logs, line)
	m.viewport.SetContent(strings.Join(m.logs, "\n"))
	m.viewport.GotoBottom()
}

func (m *PrepareModel) rebuildSteps() {
	rows := make([]table.Row, 0, len(m.steps))
	for _, s := range m.steps {
		rows = append(rows, table.Row{s.Name, s.State})
	}
	m.table.SetRows(rows)
	m.resize()
}

func (m *PrepareModel) resize() {
	m.table.SetColumns([]table.Column{
		{Title: "STEP", Width: 40},
		{Title: "STATE", Width: 10},
	})
	m.table.SetWidth(m.width)
	stepHeight := len(m.steps) + 1 // header row
	if stepHeight > 9 {
		stepHeight = 9
	}
	if stepHeight < 1 {
		stepHeight = 1
	}
	m.table.SetHeight(stepHeight)
	m.viewport.Width = m.width
	logHeight := m.height - stepHeight - 8
	if logHeight < 3 {
		logHeight = 3
	}
	m.viewport.Height = logHeight
	m.progress.Width = m.width - 4
}

// View implements tea.Model.
func (m *PrepareModel) View() string {
	var b strings.Builder
	title := m.title
	if title == "" {
		title = "Prepare"
	}
	b.WriteString(m.theme.HeaderStyle().Render(title))
	b.WriteString("\n")
	b.WriteString(m.progressView())
	b.WriteString("\n")

	if len(m.steps) > 0 {
		b.WriteString(m.table.View())
		b.WriteString("\n")
	}

	if m.done && m.err == nil {
		b.WriteString(m.theme.StatusLabel(upstream.StatusUpToDate))
		b.WriteString(" complete\n")
	}
	if m.err != nil {
		b.WriteString(m.theme.ErrorStyle().Render("failed: " + m.err.Error()))
		b.WriteString("\n")
	}

	if len(m.logs) > 0 {
		b.WriteString(m.theme.LogStyle().Render(m.viewport.View()))
		b.WriteString("\n")
	}

	if !m.done {
		b.WriteString(m.spinner.View())
		b.WriteString(" working\n")
	}
	b.WriteString(m.theme.FooterStyle().Render("q: quit  pgup/pgdn or g/G: scroll log"))
	return b.String()
}

func (m *PrepareModel) progressView() string {
	percent := 0.0
	if m.total > 0 {
		percent = float64(m.processed) / float64(m.total)
	}
	if percent > 1 {
		percent = 1
	}
	if m.theme.Color && m.total > 0 {
		return m.progress.ViewAs(percent)
	}
	if m.total > 0 {
		filled := int(percent * 20)
		return fmt.Sprintf("[%s%s] %d/%d",
			strings.Repeat("#", filled), strings.Repeat("-", 20-filled), m.processed, m.total)
	}
	return "[working]"
}

// Quitting reports whether the model has requested exit.
func (m *PrepareModel) Quitting() bool { return m.quit }

// Done reports whether the pipeline completed or failed.
func (m *PrepareModel) Done() bool { return m.done }

// Err returns the pipeline error, if any.
func (m *PrepareModel) Err() error { return m.err }
