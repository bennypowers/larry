package upstream

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/lrstanley/go-nf/glyphs/dev"
	"github.com/muesli/termenv"
)

// Provider label constants — one definition, used by both resolveSourceInfo (producer)
// and SourceGlyph (consumer) so they cannot drift apart.
const (
	LabelGitHub    = "GitHub:"
	LabelGitLab    = "GitLab:"
	LabelCodeberg  = "Codeberg:"
	LabelSourceHut = "SourceHut:" // capital H
	LabelPyPI      = "PyPI:"
	LabelNPM       = "npm:"
)

// SourceInfo is computed once, in upstream.go, and carried on the result row.
// output.go renders it and NEVER re-derives a URL from a label.
type SourceInfo struct {
	Label string // "GitHub:owner/repo", "PyPI:name", "npm:@scope/name",
	//               "GitLab:gitlab.gnome.org/GNOME/geary", "Codeberg:owner/repo",
	//               "SourceHut:git.sr.ht/~tsdh/nirius"
	URL   string // canonical browse URL, already host-correct
	Glyph string // Nerd Font glyph, "" when unknown
}

// HyperlinksEnabled reports whether OSC-8 hyperlinks should be emitted.
// Same rule as RenderTable's colour detection (output.go:162-169).
func HyperlinksEnabled() bool {
	switch os.Getenv("LARRY_COLOR") {
	case "always":
		return true
	case "never":
		return false
	}
	return termenv.DefaultOutput().ColorProfile() != termenv.Ascii
}

// SourceGlyph returns the Nerd Font icon for a SourceInfo.
func SourceGlyph(info SourceInfo) string {
	switch {
	case strings.HasPrefix(info.Label, LabelGitHub):
		return string(dev.Github)
	case strings.HasPrefix(info.Label, LabelGitLab):
		return string(dev.Gitlab)
	case strings.HasPrefix(info.Label, LabelNPM):
		return string(dev.Npm)
	case strings.HasPrefix(info.Label, LabelPyPI):
		return "\ue73c" // dev-pypi (not in go-nf, hardcoded)
	case strings.HasPrefix(info.Label, LabelCodeberg):
		return "\uf330" // codeberg-icon (not in go-nf, hardcoded)
	case strings.HasPrefix(info.Label, LabelSourceHut):
		return "\ue702" // git icon as fallback for sourcehut
	default:
		return ""
	}
}

// SourceCell builds a source cell. hyperlinks==false means plain text only:
// no OSC-8 escape is emitted. This function reads no environment.
func SourceCell(r UpstreamResult, hyperlinks bool) string {
	switch r.Status {
	case StatusManual:
		// Vendor-only package: link to the upstream homepage when possible.
		if r.Source.URL != "" && hyperlinks {
			return renderCellHyperlink(r.Source.URL, "vendor")
		}
		return "vendor"
	case StatusDeprecated:
		// Show deprecated/renamed status — no glyph, no hyperlink.
		if r.Target != "" {
			return r.Source.Label + " → " + r.Target
		}
		return r.Source.Label
	}

	var glyph, shortLabel string
	if r.Source.Label != "" {
		glyph = SourceGlyph(r.Source)
		shortLabel = SourceLabelShort(r.Source)
		if glyph != "" && r.Source.URL != "" && hyperlinks {
			return renderCellHyperlink(r.Source.URL, glyph+" "+shortLabel)
		}
		if glyph != "" {
			return glyph + " " + shortLabel
		}
		return r.Source.Label
	}

	// Fallback: use r.Source.URL directly (deprecated path).
	if r.Source.URL != "" {
		if hyperlinks {
			return renderCellHyperlink(r.Source.URL, r.Source.URL)
		}
		return r.Source.URL
	}

	// No source info — show error if available.
	if r.Error != "" {
		return "unknown"
	}
	return ""
}

// SourceLabelShort strips the provider prefix from a SourceInfo.Label.
func SourceLabelShort(info SourceInfo) string {
	if idx := strings.Index(info.Label, ":"); idx != -1 {
		return info.Label[idx+1:]
	}
	return info.Label
}

// Status represents the upstream version comparison result.
type Status int

const (
	StatusOutdated   Status = iota // Current version is behind upstream
	StatusUpToDate                 // Current version matches latest upstream
	StatusAhead                    // Current version is newer than latest upstream
	StatusNewerTag                 // A newer tag exists but current is not outdated
	StatusLive                     // Live version (9999)
	StatusDeprecated               // Package is deprecated/renamed
	StatusUnknown                  // Could not determine upstream status
	StatusHasRelease               // Live-only ebuild, but upstream has a release
	StatusCheckTag                 // Only a non-version-like tag was found; needs a human
	StatusManual                   // Package opts out of checks (vendor-only binary)
)

// String returns the display string for the status.
func (s Status) String() string {
	switch s {
	case StatusOutdated:
		return "OUTDATED"
	case StatusUpToDate:
		return "UP-TO-DATE"
	case StatusAhead:
		return "AHEAD"
	case StatusNewerTag:
		return "NEWER-TAG"
	case StatusLive:
		return "LIVE"
	case StatusDeprecated:
		return "DEPRECATED"
	case StatusUnknown:
		return "UNKNOWN"
	case StatusHasRelease:
		return "HAS-RELEASE"
	case StatusCheckTag:
		return "CHECK-TAG"
	case StatusManual:
		return "MANUAL"
	default:
		return "UNKNOWN"
	}
}

// IsSignificant returns true for statuses that should be shown by default
// (i.e., everything except UP-TO-DATE, LIVE, DEPRECATED, HAS-RELEASE, and MANUAL).
// Use -v to show DEPRECATED and MANUAL packages.
func (s Status) IsSignificant() bool {
	return s != StatusUpToDate && s != StatusLive && s != StatusDeprecated && s != StatusHasRelease && s != StatusManual
}

// UpstreamResult holds the result of an upstream version check.
type UpstreamResult struct {
	Package string
	Current string
	Latest  string
	Status  Status
	Source  SourceInfo // label, URL, glyph — precomputed in upstream.go
	Target  string     // target package atom (for deprecated/renamed)
	Error   string
	Depth   int // recursion depth (0=root, 1=direct dep, 2=grand-dep, 3=great-grand-dep)
}

// String returns a human-readable string representation.
func (r UpstreamResult) String() string {
	return fmt.Sprintf("%s (%s)", r.Package, r.Status.String())
}

// renderCellHyperlink wraps text in an OSC 8 hyperlink.
func renderCellHyperlink(url, text string) string {
	if url == "" || text == "" {
		return text
	}
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, text)
}

// RenderTable renders upstream check results as a formatted table. hyperlinks is
// passed straight through to SourceCell; it does not affect the status-column
// colouring, which keeps its own detection below.
func RenderTable(w io.Writer, results []UpstreamResult, hyperlinks bool) error {
	// Detect color support
	colorProfile := termenv.Ascii
	if colorEnv := os.Getenv("LARRY_COLOR"); colorEnv == "always" {
		colorProfile = termenv.TrueColor
	} else if colorEnv != "never" {
		output := termenv.DefaultOutput()
		colorProfile = output.ColorProfile()
	}

	// Create table with rounded borders
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("240"))).
		StyleFunc(func(row, col int) lipgloss.Style {
			switch {
			case row == table.HeaderRow: // header row
				return lipgloss.NewStyle().Bold(true).PaddingLeft(1).PaddingRight(1)
			case col == 3 && colorProfile != termenv.Ascii: // status column
				var r UpstreamResult
				if row >= 0 && row < len(results) {
					r = results[row]
				}
				var color lipgloss.Color
				switch r.Status {
				case StatusOutdated:
					color = lipgloss.Color("208") // bright orange
				case StatusUpToDate:
					color = lipgloss.Color("42") // green
				case StatusAhead:
					color = lipgloss.Color("32") // cyan
				case StatusNewerTag:
					color = lipgloss.Color("220") // yellow
				case StatusCheckTag:
					color = lipgloss.Color("220") // yellow
				case StatusLive:
					color = lipgloss.Color("39") // blue
				default:
					color = lipgloss.Color("249") // dim
				}
				return lipgloss.NewStyle().Foreground(color).PaddingLeft(1).PaddingRight(1)
			default:
				return lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
			}
		})

	// Headers
	t.Headers("Package", "Current", "Latest", "Status", "Source")

	// Data rows — indent Package cell by 2 spaces per depth level (plan 18).
	// Plan 19 owns the Source cell; do not touch it here.
	for _, r := range results {
		pkg := strings.Repeat("  ", r.Depth) + r.Package
		t.Row(pkg, r.Current, r.Latest, r.Status.String(), SourceCell(r, hyperlinks))
	}

	_, err := fmt.Fprint(w, t.Render())
	return err
}
