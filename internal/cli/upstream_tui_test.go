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

func TestUpstreamModel_FetchingShowsPlanAndSpinners(t *testing.T) {
	m, _ := newModelForTest(t, true)
	m.Update(upstreamProgressMsg(upstream.Progress{
		Total: 2,
		Plan: []upstream.PlanEntry{
			{
				Package: "app-shells/alpha", Current: "1.0.0",
				Source: upstream.SourceInfo{Label: "GitHub:acme/alpha", URL: "https://github.com/acme/alpha"},
			},
			{Package: "app-shells/dep", Current: "2.0.0", Depth: 1},
		},
	}))
	view := m.View()
	for _, want := range []string{"Checking upstream packages", "0/2", "PACKAGE", "app-shells/alpha", "1.0.0", "  app-shells/dep"} {
		if !strings.Contains(view, want) {
			t.Errorf("fetching view missing %q:\n%s", want, view)
		}
	}
	if lines := strings.Split(view, "\n"); len(lines) != 30 {
		t.Errorf("fetching view has %d lines, want 30", len(lines))
	}

	m.Update(upstreamProgressMsg(upstream.Progress{
		Package: "app-shells/alpha",
		Done:    1,
		Total:   2,
		Result: &upstream.UpstreamResult{
			Package: "app-shells/alpha", Current: "1.0.0", Latest: "2.0.0",
			Status: upstream.StatusOutdated,
		},
	}))
	view = m.View()
	if !strings.Contains(view, "OUTDATED") || !strings.Contains(view, "1/2") {
		t.Errorf("live row missing after result:\n%s", view)
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

func TestUpstreamModel_ActionableRowsSortFirst(t *testing.T) {
	results := []upstream.UpstreamResult{
		{Package: "app/uptodate", Current: "1.0.0", Latest: "1.0.0", Status: upstream.StatusUpToDate},
		{Package: "app/outdated", Current: "1.0.0", Latest: "2.0.0", Status: upstream.StatusOutdated},
		{Package: "app/live", Current: "9999 (live)", Status: upstream.StatusLive},
		{Package: "app/newer", Current: "3.0.0", Latest: "3.1.0", Status: upstream.StatusNewerTag},
	}
	m, _ := newModelForTest(t, true)
	m.Update(upstreamDoneMsg{Results: results})

	want := []string{"app/outdated", "app/newer", "app/uptodate", "app/live"}
	if len(m.visible) != len(want) {
		t.Fatalf("visible rows = %d, want %d", len(m.visible), len(want))
	}
	for i, pkg := range want {
		if m.visible[i].Package != pkg {
			t.Errorf("visible[%d] = %q, want %q", i, m.visible[i].Package, pkg)
		}
	}
	if got := m.visible[m.Selected()].Package; got != "app/outdated" {
		t.Errorf("cursor starts on %q, want the first actionable row", got)
	}
}

func TestUpstreamModel_RefreshKeepsSelectedPackage(t *testing.T) {
	first := []upstream.UpstreamResult{
		{Package: "app/alpha", Current: "1.0.0", Latest: "2.0.0", Status: upstream.StatusOutdated},
		{Package: "app/beta", Current: "1.0.0", Latest: "2.0.0", Status: upstream.StatusOutdated},
	}
	m, _ := newModelForTest(t, true)
	m.Update(upstreamDoneMsg{Results: first})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.selectedPackage(); got != "app/beta" {
		t.Fatalf("cursor on %q, want app/beta", got)
	}

	// Refresh; beta is now up to date and sorts below the remaining actionable
	// row, but the cursor must follow the package, not the index.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	m.Update(upstreamDoneMsg{Results: []upstream.UpstreamResult{
		{Package: "app/beta", Current: "2.0.0", Latest: "2.0.0", Status: upstream.StatusUpToDate},
		{Package: "app/alpha", Current: "1.0.0", Latest: "2.0.0", Status: upstream.StatusOutdated},
	}})
	if got := m.selectedPackage(); got != "app/beta" {
		t.Errorf("after refresh cursor on %q, want app/beta", got)
	}
}

func TestUpstreamModel_ColumnsFitContent(t *testing.T) {
	results := []upstream.UpstreamResult{{
		Package: "dev-util/alpha", Current: "0.11.0", Latest: "0.11.3",
		Status: upstream.StatusOutdated,
		Source: upstream.SourceInfo{Label: "GitHub:bennypowers/a-very-long-repository-name"},
	}}
	m, _ := newModelForTest(t, true)
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
	m.Update(upstreamDoneMsg{Results: results})
	view := m.View()
	if !strings.Contains(view, "OUTDATED") {
		t.Errorf("status column truncated:\n%s", view)
	}
	if strings.Contains(view, "bennypowers/a-very-long-repository-name") {
		t.Errorf("source should truncate before other columns:\n%s", view)
	}
	if !strings.Contains(view, "…") {
		t.Errorf("source was not truncated with an ellipsis:\n%s", view)
	}
}

func TestUpstreamModel_FooterSitsOnLastLine(t *testing.T) {
	m, _ := newModelForTest(t, true)
	m.Update(upstreamDoneMsg{Results: sampleResults()})
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 30 {
		t.Errorf("view has %d lines, want 30 (terminal height):\n%s", len(lines), m.View())
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "q: quit") {
		t.Errorf("last line = %q, want the key help footer", last)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // open the detail pane
	lines = strings.Split(m.View(), "\n")
	if len(lines) != 30 {
		t.Errorf("with detail, view has %d lines, want 30", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "q: quit") {
		t.Errorf("with detail, last line = %q, want the footer", lines[len(lines)-1])
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
	m := actionModel(t, UpstreamActions{Bump: func(context.Context, string, func(string), func(ActionProgress)) error {
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
	m := actionModel(t, UpstreamActions{Bump: func(_ context.Context, pkg string, onLine func(string), _ func(ActionProgress)) error {
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
	m := actionModel(t, UpstreamActions{Test: func(context.Context, string, TestOptions,
		func(string), func(ActionProgress)) error {
		return errors.New("test failed")
	}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drainCmd(t, m, cmd)
	if m.ActionErr() == nil || !strings.Contains(m.ActionErr().Error(), "test failed") {
		t.Errorf("ActionErr() = %v", m.ActionErr())
	}
	if !strings.Contains(m.View(), "test failed") {
		t.Errorf("view does not show the action error:\n%s", m.View())
	}
}

func TestUpstreamModel_TestOptionsPrompt(t *testing.T) {
	var got TestOptions
	m := actionModel(t, UpstreamActions{Test: func(_ context.Context, _ string, opts TestOptions,
		_ func(string), _ func(ActionProgress)) error {
		got = opts
		return nil
	}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	view := m.View()
	for _, want := range []string{"Test dev-util/alpha", "compile", "phases", "use"} {
		if !strings.Contains(view, want) {
			t.Errorf("test options view missing %q:\n%s", want, view)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	for _, r := range "fetch unpack" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drainCmd(t, m, cmd)
	if got.Mode != "phases" {
		t.Errorf("Mode = %q, want phases", got.Mode)
	}
	if strings.Join(got.Phases, " ") != "fetch unpack" {
		t.Errorf("Phases = %v, want fetch unpack", got.Phases)
	}
}

func TestUpstreamModel_ProgressAppendsUnknownSteps(t *testing.T) {
	m, _ := newModelForTest(t, true)
	m.applyActionProgress(ActionProgress{Step: "prepare", State: ActionStepRunning})
	m.applyActionProgress(ActionProgress{Step: "manifest", State: ActionStepRunning})
	m.applyActionProgress(ActionProgress{Done: true})

	steps := m.ActionSteps()
	if len(steps) != 2 {
		t.Fatalf("steps = %+v, want prepare and manifest", steps)
	}
	for _, step := range steps {
		if step.state != ActionStepDone {
			t.Errorf("%s state = %v, want done on overall completion", step.name, step.state)
		}
	}
}

func TestUpstreamModel_RunningActionKeepsTableVisible(t *testing.T) {
	release := make(chan struct{})
	m := actionModel(t, UpstreamActions{Bump: func(_ context.Context, _ string,
		_ func(string), _ func(ActionProgress)) error {
		<-release
		return nil
	}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	defer func() {
		close(release)
		_ = cmd
	}()

	m.applyActionProgress(ActionProgress{Steps: []string{"bump", "prepare", "commit"}})
	m.applyActionProgress(ActionProgress{Step: "bump", State: ActionStepDone})
	m.applyActionProgress(ActionProgress{Message: "manifest"})

	view := m.View()
	for _, want := range []string{"PACKAGE", "dev-util/alpha", "✓ bump", "○ prepare", "manifest", "────", "esc: cancel action"} {
		if !strings.Contains(view, want) {
			t.Errorf("running view missing %q:\n%s", want, view)
		}
	}
	if lines := strings.Split(view, "\n"); len(lines) != 30 {
		t.Errorf("running view has %d lines, want 30", len(lines))
	}
}

func TestUpstreamModel_TestProgressChecklist(t *testing.T) {
	m := actionModel(t, UpstreamActions{Test: func(_ context.Context, _ string, _ TestOptions,
		_ func(string), onProgress func(ActionProgress)) error {
		onProgress(ActionProgress{Steps: []string{"podman", "image", "emerge"}})
		onProgress(ActionProgress{Step: "podman", State: ActionStepRunning})
		onProgress(ActionProgress{Step: "podman", State: ActionStepDone})
		return nil
	}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drainCmd(t, m, cmd)

	steps := m.ActionSteps()
	if len(steps) != 3 {
		t.Fatalf("steps = %v, want 3 entries", steps)
	}
	if steps[0].name != "podman" || steps[0].state != ActionStepDone {
		t.Errorf("first step = %+v, want podman done", steps[0])
	}
	if steps[1].state != ActionStepPending || steps[2].state != ActionStepPending {
		t.Errorf("unreported steps should stay pending: %+v", steps)
	}
}
