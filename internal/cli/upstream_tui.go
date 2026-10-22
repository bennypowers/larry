package cli

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"bennypowers.dev/larry/internal/upstream"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// UpstreamModelOptions configures the interactive upstream table.
type UpstreamModelOptions struct {
	Context context.Context
	Fetch   func(context.Context, func(upstream.Progress)) ([]upstream.UpstreamResult, error)
	ShowAll bool
	Theme   *Theme
	Overlay string
	Cancel  context.CancelFunc
}

type upstreamUIState int

const (
	upstreamFetching upstreamUIState = iota
	upstreamResults
	upstreamFailed
)

// upstreamProgressMsg carries one root-package progress event.
type upstreamProgressMsg upstream.Progress

// upstreamProgressClosedMsg means the progress channel closed.
type upstreamProgressClosedMsg struct{}

// upstreamDoneMsg carries the completed walk.
type upstreamDoneMsg struct {
	Results []upstream.UpstreamResult
	Err     error
}

// UpstreamModel is the Bubble Tea model for `larry upstream --tui`.
type UpstreamModel struct {
	ctx    context.Context
	cancel context.CancelFunc
	fetch  func(context.Context, func(upstream.Progress)) ([]upstream.UpstreamResult, error)

	showAll bool
	theme   *Theme
	overlay string

	state    upstreamUIState
	spinner  spinner.Model
	progress progress.Model
	table    table.Model
	viewport viewport.Model

	results []upstream.UpstreamResult
	visible []upstream.UpstreamResult
	err     error

	progressCh chan upstream.Progress
	closeOnce  sync.Once
	done       bool
	quit       bool

	width, height int
	showDetail    bool
	showHelp      bool

	checked int
	total   int
}

// NewUpstreamModel builds the interactive model. Fetch is optional for tests;
// the command supplies one that runs upstream.RunWithTargets.
func NewUpstreamModel(opts UpstreamModelOptions) *UpstreamModel {
	theme := opts.Theme
	if theme == nil {
		theme = NewTheme(ThemeOptions{})
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	fetch := opts.Fetch
	if fetch == nil {
		fetch = func(context.Context, func(upstream.Progress)) ([]upstream.UpstreamResult, error) {
			return nil, nil
		}
	}

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	if theme.Color {
		sp.Style = theme.Renderer.NewStyle().Foreground(lipgloss.Color("39"))
	} else {
		sp.Style = theme.Renderer.NewStyle()
	}
	pr := progress.New(progress.WithWidth(40))
	tbl := table.New(table.WithFocused(true), table.WithHeight(10))
	tbl.SetStyles(theme.TableStyles())
	vp := viewport.New(80, 6)

	return &UpstreamModel{
		ctx:        ctx,
		cancel:     opts.Cancel,
		fetch:      fetch,
		showAll:    opts.ShowAll,
		theme:      theme,
		overlay:    opts.Overlay,
		state:      upstreamFetching,
		spinner:    sp,
		progress:   pr,
		table:      tbl,
		viewport:   vp,
		progressCh: make(chan upstream.Progress, 128),
		width:      80,
		height:     24,
	}
}

// Init implements tea.Model.
func (m *UpstreamModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.waitForProgress(), m.fetchCmd())
}

func (m *UpstreamModel) waitForProgress() tea.Cmd {
	return func() tea.Msg {
		p, ok := <-m.progressCh
		if !ok {
			return upstreamProgressClosedMsg{}
		}
		return upstreamProgressMsg(p)
	}
}

func (m *UpstreamModel) fetchCmd() tea.Cmd {
	return func() tea.Msg {
		results, err := m.fetch(m.ctx, func(p upstream.Progress) {
			select {
			case m.progressCh <- p:
			case <-m.ctx.Done():
			}
		})
		m.closeOnce.Do(func() { close(m.progressCh) })
		return upstreamDoneMsg{Results: results, Err: err}
	}
}

// Update implements tea.Model.
func (m *UpstreamModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case upstreamProgressMsg:
		p := upstream.Progress(msg)
		m.checked = p.Done
		m.total = p.Total
		if m.width > 8 {
			m.progress.Width = m.width - 4
		}
		return m, m.waitForProgress()

	case upstreamProgressClosedMsg:
		return m, nil

	case upstreamDoneMsg:
		m.done = true
		if msg.Err != nil {
			m.err = msg.Err
			m.state = upstreamFailed
			return m, tea.Quit
		}
		results := msg.Results
		m.results = results
		m.applyFilter()
		m.state = upstreamResults
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitWithCancel()
			return m, tea.Quit
		case tea.KeyEnter:
			if m.state == upstreamResults && len(m.visible) > 0 {
				m.showDetail = !m.showDetail
				m.updateDetail()
			}
			return m, nil
		}
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'q':
				m.quitWithCancel()
				return m, tea.Quit
			case '?':
				m.showHelp = !m.showHelp
				return m, nil
			}
		}
		if m.state == upstreamResults {
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	return m, nil
}

func (m *UpstreamModel) quitWithCancel() {
	m.quit = true
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *UpstreamModel) applyFilter() {
	m.visible = m.visible[:0]
	for _, r := range m.results {
		if m.showAll || r.Status.IsSignificant() {
			m.visible = append(m.visible, r)
		}
	}
	m.buildTable()
}

func (m *UpstreamModel) buildTable() {
	rows := make([]table.Row, 0, len(m.visible))
	for _, r := range m.visible {
		rows = append(rows, table.Row{
			m.theme.PackageCell(r),
			r.Current,
			r.Latest,
			m.theme.StatusLabel(r.Status),
			m.theme.SourceCell(r),
		})
	}
	m.table.SetRows(rows)
	m.table.SetCursor(0)
	m.resize()
}

func (m *UpstreamModel) resize() {
	sourceWidth := m.width - 70
	if sourceWidth < 12 {
		sourceWidth = 12
	}
	m.table.SetColumns([]table.Column{
		{Title: "PACKAGE", Width: 30},
		{Title: "CURRENT", Width: 12},
		{Title: "LATEST", Width: 12},
		{Title: "STATUS", Width: 13},
		{Title: "SOURCE", Width: sourceWidth},
	})
	m.table.SetWidth(m.width)
	height := m.height - 6
	if height < 1 {
		height = 1
	}
	if m.showDetail {
		height -= 6
	}
	if height < 1 {
		height = 1
	}
	m.table.SetHeight(height)
	m.viewport.Width = m.width
	m.viewport.Height = 6
	m.progress.Width = m.width - 4
}

func (m *UpstreamModel) updateDetail() {
	if len(m.visible) == 0 {
		m.viewport.SetContent("")
		return
	}
	r := m.visible[m.Selected()]
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", r.Package)
	fmt.Fprintf(&b, "Current: %s    Latest: %s\n", r.Current, r.Latest)
	fmt.Fprintf(&b, "Status:  %s\n", r.Status.String())
	if r.Source.URL != "" {
		fmt.Fprintf(&b, "Source:  %s\n", r.Source.URL)
	} else if r.Source.Label != "" {
		fmt.Fprintf(&b, "Source:  %s\n", r.Source.Label)
	}
	if r.Target != "" {
		fmt.Fprintf(&b, "Target:  %s\n", r.Target)
	}
	if r.Error != "" {
		fmt.Fprintf(&b, "Error:   %s\n", r.Error)
	}
	fmt.Fprintf(&b, "Depth:   %d\n", r.Depth)
	m.viewport.SetContent(b.String())
}

// View implements tea.Model.
func (m *UpstreamModel) View() string {
	switch m.state {
	case upstreamFetching:
		var b strings.Builder
		b.WriteString(m.theme.HeaderStyle().Render("Checking upstream packages"))
		b.WriteString("\n\n")
		b.WriteString(m.spinner.View())
		b.WriteString(" checking")
		if m.total > 0 {
			fmt.Fprintf(&b, " %d/%d", m.checked, m.total)
		}
		b.WriteString("\n")
		b.WriteString(m.progressView())
		b.WriteString("\n\n")
		b.WriteString(m.theme.FooterStyle().Render("q: quit"))
		return b.String()

	case upstreamFailed:
		return fmt.Sprintf("%s\n\n%s", m.theme.ErrorStyle().Render("upstream check failed: "+errText(m.err)),
			m.theme.FooterStyle().Render("q: quit"))

	default:
		return m.resultsView()
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (m *UpstreamModel) progressView() string {
	percent := 0.0
	if m.total > 0 {
		percent = float64(m.checked) / float64(m.total)
	}
	if percent > 1 {
		percent = 1
	}
	if m.theme.Color {
		return m.progress.ViewAs(percent)
	}
	filled := int(percent * 20)
	return fmt.Sprintf("[%s%s] %d/%d",
		strings.Repeat("#", filled), strings.Repeat("-", 20-filled), m.checked, m.total)
}

func (m *UpstreamModel) resultsView() string {
	var b strings.Builder
	title := "Upstream"
	if m.overlay != "" {
		title += " — " + m.overlay
	}
	b.WriteString(m.theme.HeaderStyle().Render(title))
	fmt.Fprintf(&b, "  (%d shown, %d checked)", len(m.visible), len(m.results))
	b.WriteString("\n")

	if len(m.visible) == 0 {
		b.WriteString("\nNo results to display.")
		if !m.showAll {
			b.WriteString(" Use -v to include up-to-date, live, deprecated, and manual packages.")
		}
		b.WriteString("\n\n")
	} else {
		b.WriteString(m.table.View())
		b.WriteString("\n")
		if m.showDetail {
			b.WriteString(m.theme.LogStyle().Render(m.viewport.View()))
			b.WriteString("\n")
		}
	}
	b.WriteString(m.footer())
	return b.String()
}

func (m *UpstreamModel) footer() string {
	short := "↑/↓: move  enter: details  q: quit"
	if m.showHelp {
		short = "↑/↓ or j/k: move  pgup/pgdn or b/f: page  g/G: first/last  enter: details  ?: help  q: quit"
	}
	return m.theme.FooterStyle().Render(short)
}

// Quitting reports whether the model has requested exit.
func (m *UpstreamModel) Quitting() bool { return m.quit }

// Selected returns the table cursor index.
func (m *UpstreamModel) Selected() int { return m.table.Cursor() }

// Err returns the walk error, if any.
func (m *UpstreamModel) Err() error { return m.err }
