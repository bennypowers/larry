package cli

import (
	"os"
	"strings"

	"bennypowers.dev/larry/internal/upstream"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// ThemeOptions configures the shared TUI look. Colour and hyperlinks are
// explicit so --color is authoritative and lipgloss cannot independently
// decide to emit ANSI.
type ThemeOptions struct {
	Color      bool
	Hyperlinks bool
	Renderer   *lipgloss.Renderer // nil -> lipgloss.NewRenderer(os.Stdout)
}

// Theme holds renderer-bound styles shared by the upstream and prepare TUIs.
type Theme struct {
	Renderer   *lipgloss.Renderer
	Color      bool
	Hyperlinks bool
}

// NewTheme builds a theme pinned to the requested colour profile.
func NewTheme(opts ThemeOptions) *Theme {
	r := opts.Renderer
	if r == nil {
		r = lipgloss.NewRenderer(os.Stdout)
	}
	if opts.Color {
		r.SetColorProfile(termenv.TrueColor)
	} else {
		r.SetColorProfile(termenv.Ascii)
	}
	return &Theme{Renderer: r, Color: opts.Color, Hyperlinks: opts.Hyperlinks}
}

// StatusStyle returns the style for a status label.
func (t *Theme) StatusStyle(s upstream.Status) lipgloss.Style {
	if !t.Color {
		return t.Renderer.NewStyle()
	}
	return t.Renderer.NewStyle().Foreground(upstream.StatusColor(s))
}

// StatusLabel returns the coloured display label for a status.
func (t *Theme) StatusLabel(s upstream.Status) string {
	return t.StatusStyle(s).Render(s.String())
}

// TableStyles returns renderer-bound styles for bubbles/table.
func (t *Theme) TableStyles() table.Styles {
	styles := table.Styles{
		Header: t.Renderer.NewStyle().Bold(true).Padding(0, 1),
		Cell:   t.Renderer.NewStyle().Padding(0, 1),
		// Selection uses a background so it is visible in every theme.
		Selected: t.Renderer.NewStyle().Bold(true).Padding(0, 1),
	}
	if t.Color {
		styles.Header = styles.Header.Foreground(lipgloss.Color("240"))
		styles.Selected = styles.Selected.
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color("57"))
	}
	return styles
}

// HeaderStyle styles the screen title line.
func (t *Theme) HeaderStyle() lipgloss.Style {
	if !t.Color {
		return t.Renderer.NewStyle().Bold(true)
	}
	return t.Renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
}

// FooterStyle styles the key-help line.
func (t *Theme) FooterStyle() lipgloss.Style {
	if !t.Color {
		return t.Renderer.NewStyle()
	}
	return t.Renderer.NewStyle().Foreground(lipgloss.Color("240"))
}

// LogStyle styles the prepare log pane.
func (t *Theme) LogStyle() lipgloss.Style {
	return t.Renderer.NewStyle().Padding(0, 1)
}

// ErrorStyle styles error text.
func (t *Theme) ErrorStyle() lipgloss.Style {
	if !t.Color {
		return t.Renderer.NewStyle()
	}
	return t.Renderer.NewStyle().Foreground(lipgloss.Color("196"))
}

// PackageCell preserves dependency indentation.
func (t *Theme) PackageCell(r upstream.UpstreamResult) string {
	return strings.Repeat("  ", r.Depth) + r.Package
}

// SourceCell delegates to the shared upstream cell renderer, which correctly
// treats its first argument as visible text and its second as the URL.
func (t *Theme) SourceCell(r upstream.UpstreamResult) string {
	return upstream.SourceCell(r, t.Hyperlinks)
}
