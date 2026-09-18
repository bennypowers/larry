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

// Status represents the upstream version comparison result.
type Status int

const (
	StatusOutdated Status = iota // Current version is behind upstream
	StatusUpToDate               // Current version matches latest upstream
	StatusAhead                  // Current version is newer than latest upstream
	StatusNewerTag               // A newer tag exists but current is not outdated
	StatusLive                   // Live version (9999)
	StatusUnknown                // Could not determine upstream status
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
	case StatusUnknown:
		return "UNKNOWN"
	default:
		return "UNKNOWN"
	}
}

// IsSignificant returns true for statuses that indicate a notable state.
func (s Status) IsSignificant() bool {
	return s == StatusOutdated || s == StatusAhead
}

// SourceGlyph returns the Nerd Font icon for a source label.
func SourceGlyph(sourceLabel string) string {
	switch {
	case strings.HasPrefix(sourceLabel, "GitHub:"):
		return string(dev.Github)
	case strings.HasPrefix(sourceLabel, "GitLab:"):
		return string(dev.Gitlab)
	case strings.HasPrefix(sourceLabel, "npm:"):
		return string(dev.Npm)
	case strings.HasPrefix(sourceLabel, "PyPI:"):
		return "\ue73c" // dev-pypi (not in go-nf, hardcoded)
	case strings.HasPrefix(sourceLabel, "Codeberg:"):
		return "\uf330" // codeberg-icon (not in go-nf, hardcoded)
	case strings.HasPrefix(sourceLabel, "SourceHut:"):
		return "\ue702" // git icon as fallback for sourcehut
	default:
		return ""
	}
}

// SourceURL extracts the source URL from a SourceLabel.
func SourceURL(sourceLabel string) string {
	switch {
	case strings.HasPrefix(sourceLabel, "GitHub:"):
		return "https://github.com/" + sourceLabel[7:]
	case strings.HasPrefix(sourceLabel, "GitLab:"):
		return "https://gitlab.com/" + sourceLabel[7:]
	case strings.HasPrefix(sourceLabel, "PyPI:"):
		return "https://pypi.org/project/" + sourceLabel[5:]
	case strings.HasPrefix(sourceLabel, "npm:"):
		return "https://www.npmjs.com/package/" + sourceLabel[4:]
	case strings.HasPrefix(sourceLabel, "Codeberg:"):
		return "https://codeberg.org/" + sourceLabel[9:]
	case strings.HasPrefix(sourceLabel, "SourceHut:"):
		return "https://git.sr.ht/" + sourceLabel[10:]
	}
	return ""
}

// SourceLabelShort strips the provider prefix from a SourceLabel (e.g. "GitHub:owner/repo" -> "owner/repo").
func SourceLabelShort(sourceLabel string) string {
	if idx := strings.Index(sourceLabel, ":"); idx != -1 {
		return sourceLabel[idx+1:]
	}
	return sourceLabel
}

// SourceCell builds a source cell with glyph + hyperlink.
func SourceCell(r UpstreamResult) string {
	var sourceCell string

	switch {
	case r.SourceLabel != "":
		glyph := SourceGlyph(r.SourceLabel)
		url := SourceURL(r.SourceLabel)
		short := SourceLabelShort(r.SourceLabel)
		if glyph != "" && url != "" {
			sourceCell = renderCellHyperlink(url, glyph+" "+short)
		} else if glyph != "" {
			sourceCell = glyph + " " + short
		} else {
			sourceCell = r.SourceLabel
		}
	case r.Source != "":
		sourceCell = renderCellHyperlink(r.Source, r.Source)
	case r.Error != "":
		sourceCell = "unknown"
	}
	return sourceCell
}

// UpstreamResult holds the result of an upstream version check.
type UpstreamResult struct {
	Package     string
	Current     string
	Latest      string
	Status      Status
	Source      string
	SourceLabel string
	Error       string
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

// RenderTable renders upstream check results as a formatted table.
func RenderTable(w io.Writer, results []UpstreamResult) error {
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
			case row == 0: // header
				return lipgloss.NewStyle().Bold(true).PaddingLeft(1).PaddingRight(1)
			case col == 3 && colorProfile != termenv.Ascii: // status column
				rIdx := row - 1
				var r UpstreamResult
				if rIdx >= 0 && rIdx < len(results) {
					r = results[rIdx]
				}
				var color lipgloss.Color
				switch r.Status {
				case StatusOutdated:
					color = lipgloss.Color("204") // orange-red
				case StatusAhead:
					color = lipgloss.Color("63") // blue
				case StatusNewerTag:
					color = lipgloss.Color("220") // yellow
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

	// Data rows
	for _, r := range results {
		t.Row(r.Package, r.Current, r.Latest, r.Status.String(), SourceCell(r))
	}

	fmt.Fprint(w, t.Render())
	return nil
}

