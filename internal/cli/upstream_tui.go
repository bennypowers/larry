package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/upstream"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// ActionKind identifies an in-session action.
type ActionKind string

const (
	ActionBump    ActionKind = "bump"
	ActionPrepare ActionKind = "prepare"
	ActionTest    ActionKind = "test"
	ActionPrune   ActionKind = "prune"
)

// ActionFunc runs one action for a package, streaming its output line by line
// and, when the child reports it, structured progress.
type ActionFunc func(ctx context.Context, pkg string, onLine func(string), onProgress func(ActionProgress)) error

// TestOptions selects how the test action invokes `larry test`.
type TestOptions struct {
	Mode   string   // "compile", "merge", "default", or "phases"
	Phases []string // used when Mode == "phases"
	Use    string   // --use value
}

// ActionStepState is the state of one action checklist entry.
type ActionStepState int

const (
	ActionStepPending ActionStepState = iota
	ActionStepRunning
	ActionStepDone
	ActionStepFailed
)

// ActionProgress is a structured progress event from a child action.
type ActionProgress struct {
	Steps   []string        // first event: ordered checklist entries
	Step    string          // entry whose state changed
	State   ActionStepState // new state
	Message string
	Done    bool
	Failed  bool
	Err     error
}

// TestActionFunc runs the test action with the chosen options.
type TestActionFunc func(ctx context.Context, pkg string, opts TestOptions,
	onLine func(string), onProgress func(ActionProgress)) error

// UpstreamActions are the in-session actions available for the selected row.
// Nil callbacks disable the corresponding key.
type UpstreamActions struct {
	Bump    ActionFunc
	Prepare ActionFunc
	Test    TestActionFunc
}

func (a UpstreamActions) forKind(kind ActionKind) ActionFunc {
	switch kind {
	case ActionBump:
		return a.Bump
	case ActionPrepare:
		return a.Prepare
	default:
		return nil
	}
}

func (a UpstreamActions) enabled(kind ActionKind) bool {
	if kind == ActionTest {
		return a.Test != nil
	}
	return a.forKind(kind) != nil
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
	// Animate enables the elapsed-time ticker for running actions. Tests
	// leave it off so they can drive commands synchronously.
	Animate bool
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
	line     string
	progress *ActionProgress
	done     bool
	err      error
}

type actionEventMsg actionEvent
type actionClosedMsg struct{}

// actionTickMsg refreshes the elapsed clock and spinner while an action runs.
type actionTickMsg time.Time

type upstreamMode int

// actionStep is one checklist entry in the running-action view.
type actionStep struct {
	name  string
	state ActionStepState
}

const (
	modeBrowse upstreamMode = iota
	modeConfirm
	modeRunning
	modeTestOptions
)

// UpstreamModel is the Bubble Tea model for `larry upstream --tui`.
type UpstreamModel struct {
	ctx    context.Context
	cancel context.CancelFunc
	fetch  func(context.Context, func(upstream.Progress)) ([]upstream.UpstreamResult, error)

	showAll bool
	theme   *Theme
	overlay string
	animate bool

	state    upstreamUIState
	spinner  spinner.Model
	progress progress.Model
	viewport viewport.Model
	cursor   int
	offset   int

	results []upstream.UpstreamResult
	visible []upstream.UpstreamResult

	// Early list: the ordered plan and its locally-known values.
	plan        []upstream.PlanEntry
	pendingRows map[string]bool
	widthsCache *[5]int
	err         error

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
	actionSteps  []actionStep
	actionDetail string
	actionError  string
	actionStart  time.Time
	lastActivity time.Time
	bumpOld      string
	bumpNew      string

	// Test options are remembered between runs. testInput edits one field of
	// testDraft while modeTestOptions is active.
	testOptions TestOptions
	testDraft   TestOptions
	testInput   string
	testField   int

	done bool
	quit bool

	// selectedPkg survives a refresh so a bump followed by test still targets
	// the same package even after it moves down the list.
	selectedPkg string

	width, height int
	showDetail    bool
	showHelp      bool
	autoprune     bool

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
	vp := viewport.New(80, 6)
	av := viewport.New(80, 10)

	return &UpstreamModel{
		ctx:         ctx,
		cancel:      opts.Cancel,
		fetch:       fetch,
		showAll:     opts.ShowAll,
		theme:       theme,
		overlay:     opts.Overlay,
		animate:     opts.Animate,
		state:       upstreamFetching,
		spinner:     sp,
		progress:    pr,
		viewport:    vp,
		actionView:  av,
		progressCh:  make(chan upstream.Progress, 128),
		actions:     opts.Actions,
		mode:        modeBrowse,
		testOptions: TestOptions{Mode: "compile"},
		width:       80,
		height:      24,
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
	m.plan = nil
	m.pendingRows = nil
	m.widthsCache = nil
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
		if m.done && m.mode != modeRunning {
			return m, nil
		}
		return m, cmd

	case upstreamProgressMsg:
		p := upstream.Progress(msg)
		m.checked = p.Done
		m.total = p.Total
		if len(p.Plan) > 0 {
			m.plan = p.Plan
			m.pendingRows = make(map[string]bool, len(p.Plan))
			m.visible = m.visible[:0]
			for _, entry := range p.Plan {
				m.visible = append(m.visible, upstream.UpstreamResult{
					Package: entry.Package,
					Current: entry.Current,
					Depth:   entry.Depth,
					Source:  entry.Source,
					Target:  entry.Target,
				})
				m.pendingRows[entry.Package] = true
			}
			widths := m.reservedWidths(p.Plan)
			m.widthsCache = &widths
			m.cursor = 0
			m.offset = 0
		}
		if p.Result != nil {
			for i := range m.visible {
				if m.visible[i].Package == p.Package {
					m.visible[i] = *p.Result
					delete(m.pendingRows, p.Package)
					break
				}
			}
		}
		if m.width > 8 {
			m.progress.Width = m.width - 4
		}
		return m, m.waitForProgress(m.progressCh)

	case upstreamProgressClosedMsg:
		return m, nil

	case actionEventMsg:
		ev := actionEvent(msg)
		if ev.progress != nil {
			m.applyActionProgress(*ev.progress)
			return m, m.waitAction(m.actionCh)
		}
		if ev.line != "" {
			m.appendActionLine(ev.line)
			return m, m.waitAction(m.actionCh)
		}
		if ev.done {
			m.actionErr = ev.err
			m.mode = modeBrowse
			if ev.err == nil {
				if m.pending == ActionBump && m.autoprune {
					if removed, err := m.pruneOlder(m.actionPkg); err == nil {
						for _, name := range removed {
							m.appendActionLine("autopruned " + name)
						}
					}
				}
				return m, m.startFetch()
			}
		}
		return m, nil

	case actionClosedMsg:
		return m, nil

	case actionTickMsg:
		if m.mode == modeRunning {
			if time.Since(m.lastActivity) >= 15*time.Second {
				m.appendActionLine(fmt.Sprintf(">>> still running (%s, no output)",
					time.Since(m.lastActivity).Round(time.Second)))
			}
			return m, m.actionTick()
		}
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
		if m.mode == modeTestOptions {
			return m.updateTestOptions(msg)
		}
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
					if m.actions.enabled(kind) {
						if kind == ActionTest {
							m.testDraft = m.testOptions
							m.testInput = ""
							m.testField = 0
							m.mode = modeTestOptions
						} else {
							m.pending = kind
							m.mode = modeConfirm
						}
					}
					return m, nil
				}
			case 'R':
				if m.mode == modeBrowse && m.state == upstreamResults {
					m.selectedPkg = m.selectedPackage()
					return m, m.startFetch()
				}
			case 'x':
				if m.mode == modeBrowse && m.state == upstreamResults &&
					m.showDetail && len(m.visible) > 0 {
					m.pending = ActionPrune
					m.mode = modeConfirm
				}
				return m, nil
			case 'A':
				if m.mode == modeBrowse && m.state == upstreamResults {
					m.autoprune = !m.autoprune
					m.updateDetail()
				}
				return m, nil
			}
		}
		if m.mode == modeBrowse && (m.state == upstreamResults || m.state == upstreamFetching) {
			m.navigate(msg.String())
		}
		return m, nil
	}

	return m, nil
}

// updateTestOptions handles the pre-test prompt: pick a mode, edit custom
// phases or USE flags, then enter to run.
func (m *UpstreamModel) updateTestOptions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = modeBrowse
		m.testField = 0
		m.testInput = ""
		return m, nil
	case tea.KeyEnter:
		m.commitTestInput()
		m.testOptions = m.testDraft
		m.testField = 0
		m.testInput = ""
		m.pending = ActionTest
		return m, m.startAction()
	case tea.KeyTab:
		m.commitTestInput()
		m.testField = (m.testField + 1) % 3
		switch m.testField {
		case 1:
			m.testInput = strings.Join(m.testDraft.Phases, " ")
		case 2:
			m.testInput = m.testDraft.Use
		default:
			m.testInput = ""
		}
		return m, nil
	case tea.KeyBackspace:
		if m.testField != 0 && m.testInput != "" {
			runes := []rune(m.testInput)
			m.testInput = string(runes[:len(runes)-1])
		}
		return m, nil
	}

	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return m, nil
	}
	r := msg.Runes[0]
	if m.testField != 0 {
		m.testInput += string(r)
		return m, nil
	}
	switch r {
	case 'c':
		m.testDraft.Mode = "compile"
	case 'm':
		m.testDraft.Mode = "merge"
	case 'd':
		m.testDraft.Mode = "default"
	case 'p':
		m.testField = 1
		m.testInput = strings.Join(m.testDraft.Phases, " ")
	case 'u':
		m.testField = 2
		m.testInput = m.testDraft.Use
	}
	return m, nil
}

// commitTestInput copies the active field's buffer into the draft options.
func (m *UpstreamModel) commitTestInput() {
	switch m.testField {
	case 1:
		if phases := strings.Fields(m.testInput); len(phases) > 0 {
			m.testDraft.Mode = "phases"
			m.testDraft.Phases = phases
		}
	case 2:
		m.testDraft.Use = strings.TrimSpace(m.testInput)
	}
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
	if m.pending == ActionPrune {
		if len(m.visible) == 0 {
			m.mode = modeBrowse
			return nil
		}
		pkg := m.visible[m.Selected()].Package
		m.selectedPkg = pkg
		m.actionPkg = pkg
		m.actionLines = nil
		m.actionErr = nil
		removed, err := m.pruneOlder(pkg)
		for _, name := range removed {
			m.appendActionLine("pruned " + name)
		}
		if err != nil {
			m.actionErr = err
			m.appendActionLine("!!! prune failed: " + err.Error())
		} else if len(removed) == 0 {
			m.appendActionLine("nothing to prune: only the newest ebuild remains")
		}
		m.mode = modeBrowse
		m.updateDetail()
		return nil
	}
	var fn ActionFunc
	if m.pending != ActionTest {
		fn = m.actions.forKind(m.pending)
	}
	if (fn == nil && m.pending != ActionTest) || len(m.visible) == 0 {
		m.mode = modeBrowse
		return nil
	}
	testFn := m.actions.Test
	if m.pending == ActionTest && testFn == nil {
		m.mode = modeBrowse
		return nil
	}
	pkg := m.visible[m.Selected()].Package
	m.selectedPkg = pkg
	m.actionPkg = pkg
	m.mode = modeRunning
	m.actionLines = nil
	m.actionErr = nil
	m.actionSteps = nil
	m.actionDetail = ""
	m.actionError = ""
	m.bumpOld = ""
	m.bumpNew = ""
	m.actionStart = time.Now()
	m.lastActivity = m.actionStart
	m.actionView.SetContent("")

	ctx, cancel := context.WithCancel(m.ctx)
	m.actionCancel = cancel
	ch := make(chan actionEvent, 128)
	m.actionCh = ch

	line := func(line string) {
		select {
		case ch <- actionEvent{line: line}:
		case <-ctx.Done():
		}
	}
	progress := func(p ActionProgress) {
		select {
		case ch <- actionEvent{progress: &p}:
		case <-ctx.Done():
		}
	}

	go func() {
		defer close(ch)
		var err error
		if m.pending == ActionTest {
			err = testFn(ctx, pkg, m.testOptions, line, progress)
		} else {
			err = fn(ctx, pkg, line, progress)
		}
		select {
		case ch <- actionEvent{done: true, err: err}:
		case <-ctx.Done():
		}
	}()
	if m.animate {
		return tea.Batch(m.waitAction(ch), m.spinner.Tick, m.actionTick())
	}
	return m.waitAction(ch)
}

// actionTick schedules the next elapsed-time refresh.
func (m *UpstreamModel) actionTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return actionTickMsg(t)
	})
}

// applyActionProgress folds one structured event into the checklist. Steps
// named by the child are appended on first sight, so commands that discover
// their steps as they go do not need an up-front list.
func (m *UpstreamModel) applyActionProgress(p ActionProgress) {
	m.lastActivity = time.Now()
	if p.Message != "" && p.Step == "" {
		m.actionDetail = p.Message
	}
	if len(p.Steps) > 0 {
		m.actionSteps = make([]actionStep, 0, len(p.Steps))
		for _, name := range p.Steps {
			m.actionSteps = append(m.actionSteps, actionStep{name: name})
		}
		m.advancePending()
	}
	if p.Step != "" {
		for i := range m.actionSteps {
			if m.actionSteps[i].name == p.Step {
				m.actionSteps[i].state = p.State
				m.advancePending()
				return
			}
		}
		m.actionSteps = append(m.actionSteps, actionStep{name: p.Step, state: p.State})
		m.advancePending()
		return
	}
	if p.Done || p.Failed {
		state := ActionStepDone
		if p.Failed {
			state = ActionStepFailed
		}
		for i := range m.actionSteps {
			if m.actionSteps[i].state == ActionStepPending || m.actionSteps[i].state == ActionStepRunning {
				m.actionSteps[i].state = state
			}
		}
	}
}

// advancePending marks the next pending step running when the current one
// finishes. The child may be slow to announce the next step (or the event may
// be lost), so the checklist must never imply that nothing is happening.
func (m *UpstreamModel) advancePending() {
	for _, step := range m.actionSteps {
		if step.state == ActionStepRunning {
			return
		}
	}
	for i := range m.actionSteps {
		if m.actionSteps[i].state == ActionStepPending {
			m.actionSteps[i].state = ActionStepRunning
			return
		}
	}
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
	m.lastActivity = time.Now()
	if isActionErrorLine(line) {
		m.actionError = line
	}
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "Old version:"):
		m.bumpOld = strings.TrimSpace(strings.TrimPrefix(trimmed, "Old version:"))
		return
	case strings.HasPrefix(trimmed, "New version:"):
		m.bumpNew = strings.TrimSpace(strings.TrimPrefix(trimmed, "New version:"))
		if m.bumpOld != "" {
			m.actionDetail = m.bumpOld + " → " + m.bumpNew
		}
		return
	case strings.HasPrefix(trimmed, "Bumped:"), strings.HasPrefix(trimmed, "Package:"):
		// Folded into the pane's structured summary; do not duplicate here.
		return
	}
	m.actionLines = append(m.actionLines, line)
	m.actionView.SetContent(strings.Join(m.actionLines, "\n"))
	m.actionView.GotoBottom()
}

// isActionErrorLine recognises the logger and CLI error prefixes used by
// child commands.
func isActionErrorLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(trimmed, "!!!") ||
		strings.HasPrefix(trimmed, "larry:") ||
		strings.HasPrefix(lower, "error:")
}

// ActionLines returns output captured from the last action.
func (m *UpstreamModel) ActionLines() []string { return m.actionLines }

// ActionSteps returns the checklist from the last action.
func (m *UpstreamModel) ActionSteps() []actionStep { return m.actionSteps }

// ActionErr returns the last action's error, if any.
func (m *UpstreamModel) ActionErr() error { return m.actionErr }

func (m *UpstreamModel) applyFilter() {
	m.visible = m.visible[:0]
	if !m.showAll {
		for _, r := range m.results {
			if r.Status.IsSignificant() {
				m.visible = append(m.visible, r)
			}
		}
	} else {
		// Actionable rows first, then the rest. Both passes preserve the
		// deterministic discovery order, so rows never shuffle between checks.
		for _, r := range m.results {
			if r.Status.IsSignificant() {
				m.visible = append(m.visible, r)
			}
		}
		for _, r := range m.results {
			if !r.Status.IsSignificant() {
				m.visible = append(m.visible, r)
			}
		}
	}
	m.restoreSelection()
	m.resize()
}

// restoreSelection puts the cursor back on the package that survived a
// refresh, falling back to the first row.
func (m *UpstreamModel) restoreSelection() {
	m.cursor = 0
	m.offset = 0
	if m.selectedPkg != "" {
		for i, r := range m.visible {
			if r.Package == m.selectedPkg {
				m.cursor = i
				break
			}
		}
	}
	m.ensureCursorVisible()
	if m.showDetail {
		m.updateDetail()
	}
}

// navigate moves the cursor. Keeping this in the model (rather than a table
// component) lets cells carry ANSI colour without breaking width math.
func (m *UpstreamModel) navigate(key string) {
	if len(m.visible) == 0 {
		return
	}
	page := m.listHeight()
	switch key {
	case "up", "k":
		m.cursor--
	case "down", "j":
		m.cursor++
	case "pgup", "b":
		m.cursor -= page
	case "pgdown", "f":
		m.cursor += page
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = len(m.visible) - 1
	default:
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	m.ensureCursorVisible()
	if m.showDetail {
		m.updateDetail()
	}
}

// ensureCursorVisible scrolls the window so the cursor row stays on screen.
func (m *UpstreamModel) ensureCursorVisible() {
	height := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+height {
		m.offset = m.cursor - height + 1
	}
	maxOffset := len(m.visible) - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// listHeight is the number of table rows that fit between the title, header,
// detail pane, action pane, and footer at the current terminal size.
func (m *UpstreamModel) listHeight() int {
	height := m.height - 3
	if m.mode == modeRunning {
		height -= m.actionPaneHeight()
	} else {
		if m.showDetail {
			height -= 7
		}
		if m.actionErr != nil {
			height--
		}
		if len(m.actionLines) > 0 {
			height -= 11
			if m.bumpNew != "" {
				height--
			}
		}
		if m.mode == modeConfirm {
			height--
		}
	}
	if height < 1 {
		height = 1
	}
	return height
}

// actionPaneHeight is the fixed height of the running-action pane, which sits
// below the table rather than replacing it.
func (m *UpstreamModel) actionPaneHeight() int {
	height := m.height / 3
	if height < 7 {
		height = 7
	}
	if height > 14 {
		height = 14
	}
	return height
}

func (m *UpstreamModel) resize() {
	m.viewport.Width = m.width
	m.viewport.Height = 6
	m.actionView.Width = m.width
	m.actionView.Height = 10
	m.progress.Width = m.width - 4
	m.ensureCursorVisible()
}

var upstreamColumnTitles = [5]string{"PACKAGE", "CURRENT", "LATEST", "STATUS", "SOURCE"}

// rowValues returns the plain text of each cell. ANSI is added at render time
// so column widths are measured on visible text, never on escape sequences.
func (m *UpstreamModel) rowValues(r upstream.UpstreamResult) [5]string {
	return [5]string{
		m.theme.PackageCell(r),
		r.Current,
		r.Latest,
		r.Status.String(),
		upstream.SourceText(r),
	}
}

// columnWidths sizes columns to their content, then gives SOURCE whatever is
// left. SOURCE is the first column to truncate; only when it hits its minimum
// do PACKAGE, STATUS, CURRENT, and LATEST give up space.
func (m *UpstreamModel) columnWidths() [5]int {
	if m.widthsCache != nil {
		return *m.widthsCache
	}
	rows := make([][5]string, 0, len(m.visible))
	for _, r := range m.visible {
		rows = append(rows, m.rowValues(r))
	}
	return m.columnWidthsFor(rows)
}

// reservedWidths sizes columns from the plan alone, reserving room for the
// status labels and typical version strings so the layout does not jump when
// results arrive.
func (m *UpstreamModel) reservedWidths(plan []upstream.PlanEntry) [5]int {
	const (
		latestReserve = 12
		statusReserve = 11 // "HAS-RELEASE"
	)
	rows := make([][5]string, 0, len(plan))
	for _, entry := range plan {
		rows = append(rows, [5]string{
			m.theme.PackageCell(upstream.UpstreamResult{Package: entry.Package, Depth: entry.Depth}),
			entry.Current,
			strings.Repeat(" ", latestReserve),
			strings.Repeat(" ", statusReserve),
			upstream.SourceText(upstream.UpstreamResult{Source: entry.Source}),
		})
	}
	return m.columnWidthsFor(rows)
}

func (m *UpstreamModel) columnWidthsFor(rows [][5]string) [5]int {
	natural := [5]int{}
	for i, title := range upstreamColumnTitles {
		natural[i] = runewidth.StringWidth(title)
	}
	for _, values := range rows {
		for i, v := range values {
			natural[i] = max(natural[i], runewidth.StringWidth(v))
		}
	}

	widths := natural
	// One space of padding on each side of every cell.
	avail := m.width - 2*len(upstreamColumnTitles)
	if avail < len(upstreamColumnTitles) {
		avail = len(upstreamColumnTitles)
	}

	sumFixed := func() int {
		return widths[0] + widths[1] + widths[2] + widths[3]
	}

	const minSource = 6
	if source := avail - sumFixed(); source < minSource {
		deficit := minSource - source

		// PACKAGE keeps at least 12 columns before it yields.
		shrink := min(deficit, max(0, widths[0]-12))
		widths[0] -= shrink
		deficit -= shrink

		// Then trim STATUS and the two version columns back to their headers.
		for _, i := range []int{3, 1, 2} {
			header := runewidth.StringWidth(upstreamColumnTitles[i])
			shrink = min(deficit, max(0, widths[i]-header))
			widths[i] -= shrink
			deficit -= shrink
		}
		if deficit > 0 {
			shrink = min(deficit, max(0, widths[0]-4))
			widths[0] -= shrink
		}
	}
	widths[4] = max(avail-sumFixed(), 1)

	if total := sumFixed() + widths[4]; total > avail {
		widths[4] -= total - avail
		if widths[4] < 1 {
			widths[4] = 1
		}
	}
	return widths
}

// renderTable draws the fixed-width header plus the visible slice of rows. It
// pads short pages so the footer stays on the last line of the screen.
func (m *UpstreamModel) renderTable() string {
	widths := m.columnWidths()
	lines := make([]string, 0, m.listHeight()+1)

	var header strings.Builder
	for i, title := range upstreamColumnTitles {
		header.WriteString(renderPaddedCell(title, widths[i], m.theme.TableHeaderCell(), "", m.theme))
	}
	lines = append(lines, header.String())

	height := m.listHeight()
	end := min(m.offset+height, len(m.visible))
	for i := m.offset; i < end; i++ {
		lines = append(lines, m.renderRow(m.visible[i], i == m.cursor, widths))
	}
	blank := strings.Repeat(" ", m.width)
	for len(lines) < height+1 {
		lines = append(lines, blank)
	}
	return strings.Join(lines, "\n")
}

func (m *UpstreamModel) renderRow(r upstream.UpstreamResult, selected bool, widths [5]int) string {
	pending := m.state == upstreamFetching && m.pendingRows[r.Package]
	values := m.rowValues(r)
	if pending {
		values[2] = m.spinnerGlyph()
		values[3] = m.spinnerGlyph()
	}
	var b strings.Builder
	for i, value := range values {
		style := m.theme.TableCell()
		switch {
		case selected:
			style = m.theme.TableSelectedCell()
		case i == 3 && !pending:
			style = m.theme.StatusStyle(r.Status)
		}
		url := ""
		if i == 4 && !pending && r.Status != upstream.StatusDeprecated {
			url = r.Source.URL
		}
		b.WriteString(renderPaddedCell(value, widths[i], style, url, m.theme))
	}
	return b.String()
}

// spinnerGlyph returns the current spinner frame without ANSI, so it can be
// measured and padded like any other cell.
func (m *UpstreamModel) spinnerGlyph() string {
	return ansi.Strip(m.spinner.View())
}

// renderPaddedCell truncates on visible width, pads inside the style, then
// wraps only the text in a hyperlink, so background and link stay aligned.
func renderPaddedCell(text string, width int, style lipgloss.Style, url string, theme *Theme) string {
	text = truncateToWidth(text, width)
	padding := width - runewidth.StringWidth(text)
	if padding < 0 {
		padding = 0
	}
	if url != "" {
		text = theme.Link(url, text)
	}
	return style.Render(" " + text + strings.Repeat(" ", padding) + " ")
}

// truncateToWidth shortens plain text to width cells, ending with an ellipsis.
func truncateToWidth(text string, width int) string {
	if runewidth.StringWidth(text) <= width {
		return text
	}
	if width <= 1 {
		return "…"
	}
	var b strings.Builder
	used := 0
	for _, r := range text {
		rw := runewidth.RuneWidth(r)
		if used+rw > width-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	b.WriteString("…")
	return b.String()
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

	if ebuilds := m.overlayEbuilds(r.Package); len(ebuilds) > 0 {
		versions := make([]string, 0, len(ebuilds))
		for _, eb := range ebuilds {
			versions = append(versions, eb.PVR)
		}
		fmt.Fprintf(&b, "In overlay: %s\n", strings.Join(versions, "  "))
		auto := "off"
		if m.autoprune {
			auto = "on"
		}
		fmt.Fprintf(&b, "Prune: x remove %d older ebuild(s)   A: autoprune %s\n",
			m.prunableCount(ebuilds), auto)
	}
	m.viewport.SetContent(b.String())
}

// overlayEbuilds lists the package's ebuilds in the overlay, oldest first.
func (m *UpstreamModel) overlayEbuilds(pkg string) []*atom.ParsedEbuild {
	if m.overlay == "" || pkg == "" {
		return nil
	}
	ebuilds, err := atom.ListEbuilds(filepath.Join(m.overlay, pkg))
	if err != nil {
		return nil
	}
	return ebuilds
}

// prunableCount is how many ebuilds x would remove: every non-live ebuild
// except the newest.
func (m *UpstreamModel) prunableCount(ebuilds []*atom.ParsedEbuild) int {
	newest := -1
	for i, eb := range ebuilds {
		if !eb.Live {
			newest = i
		}
	}
	count := 0
	for i, eb := range ebuilds {
		if !eb.Live && i != newest {
			count++
		}
	}
	return count
}

// pruneOlder removes every non-live ebuild except the newest, returning the
// removed paths. Live ebuilds are always kept.
func (m *UpstreamModel) pruneOlder(pkg string) ([]string, error) {
	ebuilds := m.overlayEbuilds(pkg)
	newest := -1
	for i, eb := range ebuilds {
		if !eb.Live {
			newest = i
		}
	}
	var removed []string
	var firstErr error
	for i, eb := range ebuilds {
		if eb.Live || i == newest {
			continue
		}
		if err := os.Remove(eb.Path); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed = append(removed, filepath.Base(eb.Path))
	}
	return removed, firstErr
}

// View implements tea.Model.
func (m *UpstreamModel) View() string {
	if m.mode == modeTestOptions {
		return m.testOptionsView()
	}
	if m.mode == modeRunning {
		return m.actionRunningView()
	}
	switch m.state {
	case upstreamFetching:
		if len(m.plan) == 0 {
			var b strings.Builder
			b.WriteString(m.theme.HeaderStyle().Render("Checking upstream packages"))
			if m.total > 0 {
				fmt.Fprintf(&b, "  %d/%d", m.checked, m.total)
			}
			b.WriteString("\n\n")
			b.WriteString(m.spinner.View())
			b.WriteString(" checking")
			b.WriteString("\n\n")
			b.WriteString(m.theme.FooterStyle().Render("q: quit"))
			return b.String()
		}
		var b strings.Builder
		b.WriteString(m.theme.HeaderStyle().Render("Checking upstream packages"))
		fmt.Fprintf(&b, "  %d/%d", m.checked, m.total)
		b.WriteString("\n")
		b.WriteString(m.renderTable())
		b.WriteString("\n")
		m.progress.Width = max(m.width-16, 10)
		b.WriteString(m.progressView())
		b.WriteString("  ")
		b.WriteString(m.theme.FooterStyle().Render("q: quit"))
		return b.String()

	case upstreamFailed:
		return fmt.Sprintf("%s\n\n%s", m.theme.ErrorStyle().Render("upstream check failed: "+errText(m.err)),
			m.theme.FooterStyle().Render("q: quit"))

	default:
		return m.resultsView()
	}
}

// resultsHeader is the title line shared by the results view and the running
// action view.
func (m *UpstreamModel) resultsHeader() string {
	title := "Upstream"
	if m.overlay != "" {
		title += " — " + m.overlay
	}
	return m.theme.HeaderStyle().Render(title) +
		fmt.Sprintf("  (%d shown, %d checked)", len(m.visible), len(m.results))
}

// actionRunningView keeps the table visible and puts the action in a pane
// below it, rather than taking over the screen.
func (m *UpstreamModel) actionRunningView() string {
	var b strings.Builder
	b.WriteString(m.resultsHeader())
	b.WriteString("\n")
	b.WriteString(m.renderTable())
	b.WriteString("\n")
	b.WriteString(m.actionPaneView())
	b.WriteString("\n")
	b.WriteString(m.theme.FooterStyle().Render("esc: cancel action  q: quit"))
	return b.String()
}

// actionPaneView renders exactly actionPaneHeight lines: a title rule, the
// checklist, and the tail of the child's output.
// rule renders a full-width rule with a left label, used for the detail and
// action panes.
func (m *UpstreamModel) rule(label string) string {
	plain := "─ " + label
	fill := m.width - runewidth.StringWidth(plain) - 1
	if fill < 0 {
		fill = 0
	}
	return m.theme.HeaderStyle().Render(plain + " " + strings.Repeat("─", fill))
}

func (m *UpstreamModel) actionPaneView() string {
	paneHeight := m.actionPaneHeight()

	label := fmt.Sprintf("%s %s", m.pending, m.actionPkg)
	if !m.actionStart.IsZero() {
		label += " · " + time.Since(m.actionStart).Round(time.Second).String()
	}
	if !m.lastActivity.IsZero() && time.Since(m.lastActivity) > 10*time.Second {
		label += " · stalled"
	}
	label = truncateToWidth(label, max(m.width-4, 1))
	lines := make([]string, 0, paneHeight)
	lines = append(lines, m.rule(label))

	var steps strings.Builder
	for i, step := range m.actionSteps {
		if i > 0 {
			steps.WriteString("   ")
		}
		glyph := stepGlyph(step.state)
		if step.state == ActionStepRunning && m.animate {
			glyph = m.spinnerGlyph()
		}
		fmt.Fprintf(&steps, "%s %s", glyph, step.name)
	}
	lines = append(lines, m.colorizeStepGlyphs(truncateToWidth(steps.String(), max(m.width-1, 1))))

	logHeight := paneHeight - 3
	lines = append(lines, m.actionDetailStrip())
	if m.actionError != "" {
		lines = append(lines, m.theme.ErrorStyle().Render(
			truncateToWidth(m.actionError, max(m.width-1, 1))))
		logHeight--
	}
	if logHeight < 1 {
		logHeight = 1
	}

	m.actionView.Width = max(m.width-2, 1)
	m.actionView.Height = logHeight
	if len(m.actionLines) == 0 {
		lines = append(lines, m.spinner.View()+" running")
	} else {
		for _, line := range strings.Split(m.actionView.View(), "\n") {
			lines = append(lines, m.theme.LogStyle().Render(line))
		}
	}
	for len(lines) < paneHeight {
		lines = append(lines, "")
	}
	return strings.Join(lines[:paneHeight], "\n")
}

// actionDetailStrip is the package summary shared with the details pane:
// version transition (when bumping) and the ebuilds present in the overlay.
func (m *UpstreamModel) actionDetailStrip() string {
	plain := ""
	switch {
	case m.bumpOld != "" && m.bumpNew != "":
		plain = m.bumpOld + " → " + m.bumpNew
	case len(m.visible) > 0:
		r := m.visible[m.Selected()]
		plain = "current " + r.Current
		if r.Latest != "" {
			plain += " · latest " + r.Latest
		}
	}
	if plain == "" {
		return ""
	}
	plain = truncateToWidth(plain, max(m.width-2, 1))
	out := plain
	if m.theme.Color && m.bumpOld != "" && m.bumpNew != "" {
		renderer := m.theme.Renderer
		out = renderer.NewStyle().Foreground(lipgloss.Color("208")).Render(m.bumpOld) +
			renderer.NewStyle().Foreground(lipgloss.Color("240")).Render(" → ") +
			renderer.NewStyle().Foreground(lipgloss.Color("42")).Bold(true).Render(m.bumpNew)
	}
	if m.bumpNew == "" && m.actionDetail != "" {
		out += "   " + m.actionDetail
	}

	pkg := m.actionPkg
	if pkg == "" {
		pkg = m.selectedPackage()
	}
	ebuilds := m.overlayEbuilds(pkg)
	if len(ebuilds) == 0 {
		return out
	}
	versions := make([]string, 0, len(ebuilds))
	for _, eb := range ebuilds {
		versions = append(versions, eb.PVR)
	}
	suffix := "   overlay: " + strings.Join(versions, "  ")
	remaining := m.width - 1 - runewidth.StringWidth(plain)
	return out + truncateToWidth(suffix, max(remaining, 0))
}

// colorizeStepGlyphs tints checklist markers after the line has been measured
// and truncated, so ANSI never affects column width.
func (m *UpstreamModel) colorizeStepGlyphs(s string) string {
	if !m.theme.Color {
		return s
	}
	renderer := m.theme.Renderer
	replacer := strings.NewReplacer(
		stepGlyph(ActionStepDone), renderer.NewStyle().Foreground(lipgloss.Color("42")).Render(stepGlyph(ActionStepDone)),
		stepGlyph(ActionStepFailed), renderer.NewStyle().Foreground(lipgloss.Color("196")).Render(stepGlyph(ActionStepFailed)),
		stepGlyph(ActionStepPending), renderer.NewStyle().Foreground(lipgloss.Color("240")).Render(stepGlyph(ActionStepPending)),
	)
	return replacer.Replace(s)
}

// stepGlyph is the checklist marker for one step state.
func stepGlyph(state ActionStepState) string {
	switch state {
	case ActionStepRunning:
		return "▸"
	case ActionStepDone:
		return "✓"
	case ActionStepFailed:
		return "✗"
	default:
		return "○"
	}
}

// testOptionsView is the pre-test prompt. Mode keys apply immediately; p and u
// focus an editable line for custom phases or USE flags.
func (m *UpstreamModel) testOptionsView() string {
	var b strings.Builder
	b.WriteString(m.theme.HeaderStyle().Render("Test " + m.selectedPackage()))
	b.WriteString("\n\n")

	fmt.Fprintf(&b, " mode   %s\n", testModeLabel(m.testDraft.Mode, m.testDraft.Phases))
	fmt.Fprintf(&b, " phases %s\n", m.displayTestField(1, strings.Join(m.testDraft.Phases, " ")))
	fmt.Fprintf(&b, " use    %s\n", m.displayTestField(2, m.testDraft.Use))
	b.WriteString("\n")
	b.WriteString(m.theme.FooterStyle().Render(
		"c: compile  m: merge  d: default  p: phases  u: use  tab: next field  enter: run  esc: cancel"))
	return b.String()
}

// displayTestField shows the draft value, or the live input buffer when the
// field is focused.
func (m *UpstreamModel) displayTestField(field int, value string) string {
	if m.testField != field {
		if value == "" {
			return "(none)"
		}
		return value
	}
	prompt := value
	if field == 1 || field == 2 {
		prompt = m.testInput
	}
	return prompt + "█"
}

func testModeLabel(mode string, phases []string) string {
	switch mode {
	case "merge":
		return "merge (clean fetch unpack compile install)"
	case "default":
		return "default (clean fetch unpack)"
	case "phases":
		if len(phases) > 0 {
			return "custom: " + strings.Join(phases, " ")
		}
		return "custom (set phases)"
	default:
		return "compile (clean fetch unpack compile)"
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
	b.WriteString(m.resultsHeader())
	b.WriteString("\n")

	if len(m.visible) == 0 {
		b.WriteString("\nNo results to display.")
		if !m.showAll {
			b.WriteString(" Use -v to include up-to-date, live, deprecated, and manual packages.")
		}
		b.WriteString("\n\n")
	} else {
		b.WriteString(m.renderTable())
		b.WriteString("\n")
		if m.showDetail {
			b.WriteString(m.rule("details"))
			b.WriteString("\n")
			b.WriteString(m.theme.LogStyle().Render(m.viewport.View()))
			b.WriteString("\n")
		}
	}
	if m.actionErr != nil {
		b.WriteString(m.theme.ErrorStyle().Render("last action failed: " + m.actionErr.Error()))
		b.WriteString("\n")
	}
	if len(m.actionLines) > 0 {
		if m.bumpNew != "" {
			b.WriteString(m.bumpSummary())
			b.WriteString("\n")
		}
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

// bumpSummary renders the package's version transition.
func (m *UpstreamModel) bumpSummary() string {
	if !m.theme.Color {
		return fmt.Sprintf("%s  %s → %s", m.actionPkg, m.bumpOld, m.bumpNew)
	}
	renderer := m.theme.Renderer
	oldStyle := renderer.NewStyle().Foreground(lipgloss.Color("208"))
	newStyle := renderer.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	arrow := renderer.NewStyle().Foreground(lipgloss.Color("240")).Render("→")
	return fmt.Sprintf("%s  %s %s %s", m.actionPkg,
		oldStyle.Render(m.bumpOld), arrow, newStyle.Render(m.bumpNew))
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
			"b: bump  p: prepare  t: test  R: refresh  x: prune older  A: autoprune  ?: help  q: quit"
	}
	if m.mode == modeConfirm {
		short = "y: confirm  n/esc: cancel"
	}
	return m.theme.FooterStyle().Render(short)
}

// Quitting reports whether the model has requested exit.
func (m *UpstreamModel) Quitting() bool { return m.quit }

// Selected returns the table cursor index.
func (m *UpstreamModel) Selected() int { return m.cursor }

// Err returns the walk error, if any.
func (m *UpstreamModel) Err() error { return m.err }
