package upstream

import (
	"fmt"
	"io"
	"os"
	"strings"

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

// Color returns the ANSI color code for this status.
func (s Status) Color() string {
	switch s {
	case StatusOutdated:
		return "\033[31m" // red
	case StatusUpToDate:
		return "\033[32m" // green
	case StatusAhead:
		return "\033[34m" // blue
	case StatusNewerTag:
		return "\033[33m" // yellow
	default:
		return "\033[2m" // dim/gray
	}
}

// UpstreamResult holds the result of an upstream version check.
type UpstreamResult struct {
	Package string // "category/pkgname"
	Current string // Current version in overlay
	Latest  string // Latest upstream version
	Status  Status
	Source  string // Source URL (for hyperlink)
	Error   string // Error message if any
}

// String returns a human-readable string representation.
func (r UpstreamResult) String() string {
	var sb strings.Builder
	sb.WriteString(r.Package)
	sb.WriteString(" (")
	sb.WriteString(r.Status.String())
	sb.WriteString(")")
	if r.Latest != "" && r.Latest != r.Current {
		sb.WriteString(" -> ")
		sb.WriteString(r.Latest)
	}
	return sb.String()
}

// renderHyperlink creates an OSC 8 hyperlink for the source URL.
func renderHyperlink(url, text string) string {
	if url == "" || text == "" {
		return text
	}
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, text)
}

// RenderTable renders upstream check results as a formatted table.
func RenderTable(w io.Writer, results []UpstreamResult) error {
	// Detect color support
	forceColor := false
	if colorEnv := os.Getenv("LARRY_COLOR"); colorEnv == "always" {
		forceColor = true
	} else if colorEnv != "never" {
		output := termenv.DefaultOutput()
		forceColor = output.ColorProfile() != termenv.Ascii
	}

	// Render header
	if forceColor {
		fmt.Fprintf(w, "\033[1m%-30s %-12s %-12s %-15s %s\033[0m\n",
			"PACKAGE", "CURRENT", "LATEST", "STATUS", "SOURCE")
		fmt.Fprintln(w, strings.Repeat("─", 80))
	} else {
		fmt.Fprintf(w, "%-30s %-12s %-12s %-15s %s\n",
			"PACKAGE", "CURRENT", "LATEST", "STATUS", "SOURCE")
		fmt.Fprintln(w, strings.Repeat("─", 80))
	}

	// Render rows
	for _, r := range results {
		statusStr := r.Status.String()

		// Build source cell
		sourceCell := ""
		if r.Source != "" {
			sourceCell = renderHyperlink(r.Source, r.Source[:minInt(len(r.Source), 30)])
		} else if r.Error != "" {
			sourceCell = r.Error
		}

		if forceColor && r.Status.IsSignificant() {
			fmt.Fprintf(w, "%-30s %-12s %-12s %s%s\033[0m %s\n",
				r.Package, r.Current, r.Latest,
				r.Status.Color(), statusStr,
				sourceCell)
		} else {
			fmt.Fprintf(w, "%-30s %-12s %-12s %-15s %s\n",
				r.Package, r.Current, r.Latest, statusStr, sourceCell)
		}
	}

	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
