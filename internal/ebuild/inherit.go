package ebuild

import (
	"strings"
)

// ParseInherit returns every eclass named by every `inherit` line in raw,
// joining backslash continuations, in source order, de-duplicated.
// Returns nil when no inherit lines are found.
func ParseInherit(raw string) []string {
	// 1. Join backslash continuations
	raw = strings.ReplaceAll(raw, "\\\n", " ")

	var result []string
	seen := make(map[string]bool)

	// 2. Scan each line
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		// Skip comments
		if strings.HasPrefix(line, "#") {
			continue
		}
		// Match inherit line
		if strings.HasPrefix(line, "inherit ") || line == "inherit" {
			rest := strings.TrimSpace(line[len("inherit"):])
			for _, name := range strings.Fields(rest) {
				if !seen[name] {
					seen[name] = true
					result = append(result, name)
				}
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}
