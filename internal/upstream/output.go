package upstream

import (
	"fmt"
	"os"
)

// RenderTable renders upstream check results as a table.
func RenderTable(w *os.File, results []UpstreamResult) error {
	for _, r := range results {
		fmt.Fprintf(w, "%s: %s -> %s\n", r.Name, r.OldVersion, r.NewVersion)
	}
	return nil
}

// UpstreamResult holds the result of an upstream check.
type UpstreamResult struct {
	Name       string
	OldVersion string
	NewVersion string
}
