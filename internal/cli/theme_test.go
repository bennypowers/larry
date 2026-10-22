package cli

import (
	"bytes"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/upstream"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

// TestTheme_SelectedRowDoesNotShiftColumns guards the bubbles table quirk
// where Selected is rendered around the joined row: padding on that style
// indents the cursor row relative to every other row.
func TestTheme_SelectedRowDoesNotShiftColumns(t *testing.T) {
	theme, _ := themeForTest(false, false)
	tbl := table.New()
	tbl.SetStyles(theme.TableStyles())
	tbl.SetColumns([]table.Column{
		{Title: "PACKAGE", Width: 20},
		{Title: "STATUS", Width: 10},
	})
	tbl.SetRows([]table.Row{
		{"dev-util/alpha", "OUTDATED"},
		{"dev-util/beta", "UP-TO-DATE"},
	})
	tbl.SetCursor(1)

	var alphaLine, betaLine string
	for _, line := range strings.Split(tbl.View(), "\n") {
		switch {
		case strings.Contains(line, "dev-util/alpha"):
			alphaLine = line
		case strings.Contains(line, "dev-util/beta"):
			betaLine = line
		}
	}
	if alphaLine == "" || betaLine == "" {
		t.Fatalf("table view missing rows:\n%s", tbl.View())
	}
	alphaPad := len(alphaLine) - len(strings.TrimLeft(alphaLine, " "))
	betaPad := len(betaLine) - len(strings.TrimLeft(betaLine, " "))
	if alphaPad != betaPad {
		t.Errorf("selected row indented by %d, unselected by %d:\n%q\n%q",
			betaPad, alphaPad, alphaLine, betaLine)
	}
}

func themeForTest(color, hyperlinks bool) (*Theme, *bytes.Buffer) {
	var buf bytes.Buffer
	return NewTheme(ThemeOptions{
		Color:      color,
		Hyperlinks: hyperlinks,
		Renderer:   lipgloss.NewRenderer(&buf),
	}), &buf
}

func TestTheme_ColorDisabledNoANSI(t *testing.T) {
	theme, _ := themeForTest(false, false)
	rendered := strings.Join([]string{
		theme.StatusLabel(upstream.StatusOutdated),
		theme.PackageCell(upstream.UpstreamResult{Package: "dev-util/foo", Depth: 2}),
		theme.SourceCell(upstream.UpstreamResult{Source: upstream.SourceInfo{Label: "GitHub:a/b", URL: "https://github.com/a/b"}}),
		theme.HeaderStyle().Render("header"),
		theme.FooterStyle().Render("footer"),
		theme.TableStyles().Header.Render("col"),
	}, "")
	if strings.Contains(rendered, "\x1b[") {
		t.Errorf("colour-disabled theme emitted ANSI: %q", rendered)
	}
}

func TestTheme_ColorEnabledHasANSI(t *testing.T) {
	theme, _ := themeForTest(true, false)
	if got := theme.StatusLabel(upstream.StatusOutdated); !strings.Contains(got, "\x1b[") {
		t.Errorf("colour-enabled status label has no ANSI: %q", got)
	}
}

func TestTheme_StatusLabelText(t *testing.T) {
	theme, _ := themeForTest(false, false)
	if got := theme.StatusLabel(upstream.StatusHasRelease); !strings.Contains(got, "HAS-RELEASE") {
		t.Errorf("StatusLabel = %q, want HAS-RELEASE", got)
	}
}

func TestTheme_PackageCellPreservesDepth(t *testing.T) {
	theme, _ := themeForTest(false, false)
	if got := theme.PackageCell(upstream.UpstreamResult{Package: "dev-util/foo", Depth: 0}); got != "dev-util/foo" {
		t.Errorf("depth 0 = %q", got)
	}
	if got := theme.PackageCell(upstream.UpstreamResult{Package: "dev-util/foo", Depth: 2}); got != "    dev-util/foo" {
		t.Errorf("depth 2 = %q, want four leading spaces", got)
	}
}

func TestTheme_SourceCellUsesURLAsHyperlinkTarget(t *testing.T) {
	theme, _ := themeForTest(false, true)
	cell := theme.SourceCell(upstream.UpstreamResult{
		Status: upstream.StatusOutdated,
		Source: upstream.SourceInfo{Label: "GitHub:acme/foo", URL: "https://github.com/acme/foo"},
	})
	want := "\033]8;;https://github.com/acme/foo\033\\"
	if !strings.Contains(cell, want) {
		t.Errorf("source cell = %q, want hyperlink target %q", cell, want)
	}
	if !strings.Contains(cell, "acme/foo") {
		t.Errorf("source cell = %q, want the visible label", cell)
	}
}
