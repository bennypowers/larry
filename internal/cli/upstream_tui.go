package cli

import (
	"context"
	"fmt"
	"strings"

	"bennypowers.dev/larry/internal/upstream"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ActionKind identifies an in-session action.
type ActionKind string

const (
	ActionBump    ActionKind = "bump"
	ActionPrepare ActionKind = "prepare"
	ActionTest    ActionKind = "test"
)

// ActionFunc runs one action for a package, streaming its output line by line.
type ActionFunc func(ctx context.Context, pkg string, onLine func(string)) error

// UpstreamActions are the in-session actions available for the selected row.
// Nil callbacks disable the corresponding key.
type UpstreamActions struct {
	Bump    ActionFunc
	Prepare ActionFunc
	Test    ActionFunc
}

func (a UpstreamActions) forKind(kind ActionKind) ActionFunc {
	switch kind {
	case ActionBump:
		return a.Bump
	case ActionPrepare:
		return a.Prepare
	case ActionTest:
		return a.Test
	default:
		return nil
	}
}

// UpstreamModelOptions configures the interactive upstream table.
type UpstreamModelOptions struct {
	Context context.Context
	Fetch   func(context.Context, func(upstream.Progress)) ([]upstream.UpstreamResult, error)
	ShowAll bool
	Theme   *Theme
	Overlay string
	Cancel  context.CancelFunc
	Actions UpstreamActions
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

type actionEvent struct {
	line string
	done bool
	err  error
}

type actionEventMsg actionEvent
type actionClosedMsg struct{}

type upstreamMode int

const (
	modeBrowse upstreamMode = iota
	modeConfirm
	modeRunning
)

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

	// In-session actions.
	actions      UpstreamActions
	mode         upstreamMode
	pending      ActionKind
	actionCh     chan actionEvent
	actionCancel context.CancelFunc
	actionLines  []string
	actionErr    error
	actionPkg    string
	actionView   viewport.Model

	done bool
	quit bool

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
	av := viewport.New(80, 10)

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
		actionView: av,
		progressCh: make(chan upstream.Progress, 128),
		actions:    opts.Actions,
		mode:       modeBrowse,
		width:      80,
		height:     24,
	}
}

// Init implements tea.Model.
func (m *UpstreamModel) Init() tea.Cmd {
	return m.startFetch()
}

func (m *UpstreamModel) startFetch() tea.Cmd {
	m.state = upstreamFetching
	m.done = false
	m.err = nil
	m.results = nil
	m.visible = nil
	m.checked = 0
	m.total = 0
	m.progressCh = make(chan upstream.Progress, 128)
	m.mode = modeBrowse
	return tea.Batch(m.spinner.Tick, m.waitForProgress(m.progressCh), m.fetchCmd())
}

func (m *UpstreamModel) waitForProgress(ch chan upstream.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return upstreamProgressClosedMsg{}
		}
		return upstreamProgressMsg(p)
	}
}

func (m *UpstreamModel) fetchCmd() tea.Cmd {
	ch := m.progressCh
	return func() tea.Msg {
		results, err := m.fetch(m.ctx, func(p upstream.Progress) {
			select {
			case ch <- p:
			case <-m.ctx.Done():
			}
		})
		close(ch)
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
		return m, m.waitForProgress(m.progressCh)

	case upstreamProgressClosedMsg:
		return m, nil

	case actionEventMsg:
		ev := actionEvent(msg)
		if ev.line != "" {
			m.appendActionLine(ev.line)
			return m, m.waitAction(m.actionCh)
		}
		if ev.done {
			m.actionErr = ev.err
			m.mode = modeBrowse
			if ev.err == nil {
				return m, m.startFetch()
			}
		}
		return m, nil

	case actionClosedMsg:
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
		case tea.KeyCtrlC:
			m.quitWithCancel()
			return m, tea.Quit
		case tea.KeyEsc:
			switch m.mode {
			case modeConfirm:
				m.mode = modeBrowse
				return m, nil
			case modeRunning:
				if m.actionCancel != nil {
					m.actionCancel()
				}
				m.mode = modeBrowse
				return m, nil
			}
			m.quitWithCancel()
			return m, tea.Quit
		case tea.KeyEnter:
			switch m.mode {
			case modeConfirm:
				return m, m.startAction()
			case modeBrowse:
				if m.state == upstreamResults && len(m.visible) > 0 {
					m.showDetail = !m.showDetail
					m.updateDetail()
				}
			}
			return m, nil
		}
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'q':
				m.quitWithCancel()
				return m, tea.Quit
			case '?':
				if m.mode == modeBrowse {
					m.showHelp = !m.showHelp
				}
				return m, nil
			case 'y':
				if m.mode == modeConfirm {
					return m, m.startAction()
				}
			case 'n':
				if m.mode == modeConfirm {
					m.mode = modeBrowse
					return m, nil
				}
			case 'b', 'p', 't':
				if m.mode == modeBrowse && m.state == upstreamResults && len(m.visible) > 0 {
					var kind ActionKind
					switch msg.Runes[0] {
					case 'b':
						kind = ActionBump
					case 'p':
						kind = ActionPrepare
					case 't':
						kind = ActionTest
					}
					if m.actions.forKind(kind) != nil {
						m.pending = kind
						m.mode = modeConfirm
					}
					return m, nil
				}
			case 'R':
				if m.mode == modeBrowse && m.state == upstreamResults {
					return m, m.startFetch()
				}
			}
		}
		if m.state == upstreamResults && m.mode == modeBrowse {
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
	if m.actionCancel != nil {
		m.actionCancel()
	}
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *UpstreamModel) startAction() tea.Cmd {
	fn := m.actions.forKind(m.pending)
	if fn == nil || len(m.visible) == 0 {
		m.mode = modeBrowse
		return nil
	}
	pkg := m.visible[m.Selected()].Package
	m.actionPkg = pkg
	m.mode = modeRunning
	m.actionLines = nil
	m.actionErr = nil
	m.actionView.SetContent("")

	ctx, cancel := context.WithCancel(m.ctx)
	m.actionCancel = cancel
	ch := make(chan actionEvent, 128)
	m.actionCh = ch

	go func() {
		defer close(ch)
		err := fn(ctx, pkg, func(line string) {
			select {
			case ch <- actionEvent{line: line}:
			case <-ctx.Done():
			}
		})
		select {
		case ch <- actionEvent{done: true, err: err}:
		case <-ctx.Done():
		}
	}()
	return m.waitAction(ch)
}

func (m *UpstreamModel) waitAction(ch chan actionEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return actionClosedMsg{}
		}
		return actionEventMsg(ev)
	}
}

func (m *UpstreamModel) appendActionLine(line string) {
	m.actionLines = append(m.actionLines, line)
	m.actionView.SetContent(strings.Join(m.actionLines, "\n"))
	m.actionView.GotoBottom()
}

// ActionLines returns output captured from the last action.
func (m *UpstreamModel) ActionLines() []string { return m.actionLines }

// ActionErr returns the last action's error, if any.
func (m *UpstreamModel) ActionErr() error { return m.actionErr }

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
	m.actionView.Width = m.width
	m.actionView.Height = 10
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
	if m.mode == modeRunning {
		return m.actionRunningView()
	}
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

func (m *UpstreamModel) actionRunningView() string {
	var b strings.Builder
	b.WriteString(m.theme.HeaderStyle().Render(
		fmt.Sprintf("%s %s", m.pending, m.actionPkg)))
	b.WriteString("\n\n")
	if len(m.actionLines) == 0 {
		b.WriteString(m.spinner.View())
		b.WriteString(" running")
	} else {
		b.WriteString(m.theme.LogStyle().Render(m.actionView.View()))
	}
	b.WriteString("\n")
	b.WriteString(m.theme.FooterStyle().Render("esc: cancel action  q: quit"))
	return b.String()
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
	if m.actionErr != nil {
		b.WriteString(m.theme.ErrorStyle().Render("last action failed: " + m.actionErr.Error()))
		b.WriteString("\n")
	}
	if len(m.actionLines) > 0 {
		b.WriteString(m.theme.FooterStyle().Render("last action output:"))
		b.WriteString("\n")
		b.WriteString(m.theme.LogStyle().Render(m.actionView.View()))
		b.WriteString("\n")
	}
	if m.mode == modeConfirm {
		b.WriteString(m.theme.ErrorStyle().Render(
			fmt.Sprintf("Run %s for %s? (y/N)", m.pending, m.selectedPackage())))
		b.WriteString("\n")
	}
	b.WriteString(m.footer())
	return b.String()
}

func (m *UpstreamModel) selectedPackage() string {
	if len(m.visible) == 0 {
		return ""
	}
	return m.visible[m.Selected()].Package
}

func (m *UpstreamModel) footer() string {
	short := "↑/↓: move  enter: details  b: bump  p: prepare  t: test  R: refresh  q: quit"
	if m.showHelp {
		short = "↑/↓ or j/k: move  pgup/pgdn or b/f: page  g/G: first/last  enter: details  " +
			"b: bump  p: prepare  t: test  R: refresh  ?: help  q: quit"
	}
	if m.mode == modeConfirm {
		short = "y: confirm  n/esc: cancel"
	}
	return m.theme.FooterStyle().Render(short)
}

// Quitting reports whether the model has requested exit.
func (m *UpstreamModel) Quitting() bool { return m.quit }

// Selected returns the table cursor index.
func (m *UpstreamModel) Selected() int { return m.table.Cursor() }

// Err returns the walk error, if any.
func (m *UpstreamModel) Err() error { return m.err }
