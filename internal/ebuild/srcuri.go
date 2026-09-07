package ebuild

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// assignRe extracts "VARNAME="value"" patterns from SRC_URI.
	assignRe = regexp.MustCompile(`([A-Z][A-Z0-9_]+)="([^"]*)"`)
	// singleQuoteAssignRe extracts 'VARNAME=value' patterns.
	singleQuoteAssignRe = regexp.MustCompile(`([A-Z][A-Z0-9_]+)='([^']*)'`)
)

// VariableMap holds variable names and their expanded values for SRC_URI resolution.
type VariableMap map[string]string

// ExpandVariables expands all ${VAR} and $VAR references in a string using the provided VariableMap.
// It handles nested variable references by iterating until no more replacements are possible.
// Supports: ${VAR}, ${VAR//pattern/replacement}, ${VAR//pattern}
func ExpandVariables(input string, vars VariableMap) string {
	if input == "" || vars == nil {
		return input
	}

	maxIter := 10
	for i := 0; i < maxIter; i++ {
		original := input
		var result strings.Builder
		remaining := input

		for len(remaining) > 0 {
			// Find the next ${ or $ reference
			braceIdx := strings.Index(remaining, "${")
			dollarIdx := strings.Index(remaining, "$")

			// Determine which comes first
			var pos int
			if braceIdx >= 0 && dollarIdx >= 0 {
				if braceIdx < dollarIdx {
					pos = braceIdx
				} else {
					pos = dollarIdx
				}
			} else if braceIdx >= 0 {
				pos = braceIdx
			} else {
				pos = dollarIdx
			}

			// If no reference found, write the rest and exit
			if pos < 0 {
				result.WriteString(remaining)
				break
			}

			// Write everything before the reference
			result.WriteString(remaining[:pos])

			if remaining[pos] == '$' && pos+1 < len(remaining) && remaining[pos+1] == '{' {
				// ${VAR} or ${VAR//...} form
				remaining = remaining[pos+2:]
				end := strings.Index(remaining, "}")
				if end < 0 {
					result.WriteString("${")
					result.WriteString(remaining)
					remaining = ""
					break
				}
				inner := remaining[:end]
				remaining = remaining[end+1:]

				varName, hasManip, sepIdx := extractVarName(inner)
				val, ok := vars[varName]
				if !ok {
					result.WriteString("${")
					result.WriteString(inner)
					result.WriteString("}")
					continue
				}

				if hasManip && sepIdx >= 0 {
					manip := inner[sepIdx:]
					val = applyBashManip(val, manip)
				}
				result.WriteString(val)
			} else {
				// $VAR form (no braces)
				remaining = remaining[pos+1:]
				// Extract variable name: [A-Za-z_][A-Za-z0-9_]*
				varEnd := 0
				for varEnd < len(remaining) && (remaining[varEnd] >= 'a' && remaining[varEnd] <= 'z' ||
					remaining[varEnd] >= 'A' && remaining[varEnd] <= 'Z' ||
					remaining[varEnd] >= '0' && remaining[varEnd] <= '9' ||
					remaining[varEnd] == '_') {
					varEnd++
				}
				if varEnd == 0 {
					// No valid var name after $, write as-is
					result.WriteString("$")
					continue
				}
				varName := remaining[:varEnd]
				remaining = remaining[varEnd:]

				val, ok := vars[varName]
				if !ok {
					result.WriteString("$")
					result.WriteString(varName)
					continue
				}
				result.WriteString(val)
			}
		}

		input = result.String()
		if input == original {
			break
		}
	}

	return input
}

// extractVarName extracts the variable name from ${VAR}, ${VAR//pattern}, or ${VAR="value"} syntax.
// Returns (varName, hasManipulation, separatorIndex).
func extractVarName(inner string) (varName string, hasManip bool, sepIdx int) {
	// Check for bash manipulation: ${VAR//pattern/replacement}
	if idx := strings.Index(inner, "//"); idx > 0 {
		return inner[:idx], true, idx
	}
	// Check for simple variable separator or assignment
	if idx := strings.Index(inner, "/"); idx > 0 {
		return inner[:idx], false, idx
	}
	if idx := strings.Index(inner, "="); idx > 0 {
		return inner[:idx], false, idx
	}
	// Simple var reference: ${VAR}
	return inner, false, -1
}

// applyBashManip applies bash-style string manipulation: ${VAR//pattern/replacement}
// e.g., ${PV//./_} replaces all dots with underscores
func applyBashManip(value, manip string) string {
	// Strip the leading // for substitution
	if !strings.HasPrefix(manip, "//") {
		return value
	}

	// After stripping "//$", parse pattern/replacement from the rest.
	// In bash, ${var//pattern/repl}: after the operator "//",
	// pattern extends from the start up to (but not including) the first "/"
	// that is NOT at position 0 (position 0 is part of the pattern).
	// The first "/" at position 0 could be the pattern itself (e.g., pattern = "/").
	// The separator between pattern and replacement is the next "/" after position 0.
	sub := manip[2:] // everything after the "//$" operator

	// Find the separator: first "/" in sub[1:] + 1
	idx := strings.Index(sub[1:], "/")
	var pattern, replacement string
	if idx >= 0 {
		sepPos := idx + 1
		pattern = sub[:sepPos]
		replacement = sub[sepPos+1:]
	} else {
		// No replacement part, just remove pattern
		pattern = sub
		replacement = ""
	}

	return strings.ReplaceAll(value, pattern, replacement)
}

// BuildVariableMap builds a VariableMap from SRC_URI lines, extracting all variable
// assignments (${VAR="value"} or ${VAR}=value) and core variables (PN, PV, P).
func BuildVariableMap(info *EbuildInfo) VariableMap {
	vars := make(VariableMap)

	// Always set core Portage variables
	vars["PN"] = info.PN()
	vars["PV"] = info.PV()
	if info.PVal != "" {
		vars["P"] = info.PVal
	} else {
		vars["P"] = info.PN() + "-" + info.PV()
	}

	// Parse SRC_URI for custom variable assignments like MY_P="${PN}-0.75.3"
	srcURI := info.SRC_URI

	matches := assignRe.FindAllStringSubmatch(srcURI, -1)
	for _, m := range matches {
		vars[m[1]] = m[2]
	}

	matches2 := singleQuoteAssignRe.FindAllStringSubmatch(srcURI, -1)
	for _, m := range matches2 {
		vars[m[1]] = m[2]
	}

	return vars
}

// ResolveSRCURI resolves a SRC_URI string, expanding variables and rewriting mirror:// schemes.
func ResolveSRCURI(uri string) ([]string, error) {
	return ResolveSRCURIWithVars(uri, nil)
}

// ResolveSRCURIWithVars resolves a SRC_URI string with a custom variable map.
// It handles mirror://npm/, mirror://pypi/, and other mirror schemes.
// Pure variable assignments (MY_P="...", etc.) are stripped before URI resolution.
func ResolveSRCURIWithVars(uri string, vars VariableMap) ([]string, error) {
	if uri == "" {
		return nil, nil
	}

	// Pre-process: strip leading "SRC_URI=" prefix if present.
	stripped := strings.TrimSpace(uri)
	if strings.HasPrefix(stripped, "SRC_URI=") {
		stripped = strings.TrimLeft(stripped[8:], " \t=")
		uri = stripped
	}

	rawEntries := splitSRCURI(uri)

	resolved := make([]string, 0, len(rawEntries))
	for _, raw := range rawEntries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		// Skip pure variable assignment entries like MY_P="..." or MY_PN="${PV}"
		if isPureAssignment(entry) {
			continue
		}

		// Also strip "SRC_URI=" prefix from individual entries
		// (handles multi-line SRC_URI where assignments and URIs are on same line)
		strippedEntry := strings.TrimSpace(entry)
		if strings.HasPrefix(strippedEntry, "SRC_URI=") {
			strippedEntry = strings.TrimLeft(strippedEntry[8:], " \t=")
			// Strip surrounding quotes
			strippedEntry = strings.TrimSpace(strippedEntry)
			if len(strippedEntry) >= 2 && strippedEntry[0] == '"' && strippedEntry[len(strippedEntry)-1] == '"' {
				strippedEntry = strippedEntry[1 : len(strippedEntry)-1]
			}
			entry = strippedEntry
		}

		if vars != nil {
			entry = ExpandVariables(entry, vars)
		}

		resolvedEntry, err := resolveMirror(entry)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, resolvedEntry)
	}

	return resolved, nil
}

// isPureAssignment checks if an entry is a bare variable assignment like MY_P="value"
// but NOT a Portage variable line like DEPEND="...", SRC_URI="...", etc.
func isPureAssignment(entry string) bool {
	if !strings.Contains(entry, "=") {
		return false
	}
	if len(entry) == 0 || entry[0] < 'A' || entry[0] > 'Z' {
		return false
	}
	// Find the first = sign
	for i, c := range entry {
		if c == '=' {
			if i+1 >= len(entry) {
				return false
			}
			next := entry[i+1]
			if next != '"' && next != '\'' {
				return false
			}
			name := entry[:i]
			// Exclude known Portage variable names
			switch name {
			case "SRC_URI", "DEPEND", "RDEPEND", "PDEPEND", "BDEPEND",
				"HOMEPAGE", "LICENSE", "SLOT", "KEYWORDS", "IUSE",
				"CONFIG_CHECK", "REQUIRED_USE", "RESTRICT", "PROPERTIES",
				"PATCHES", "EGIT_REPO_URI", "EGIT_BRANCH", "EGIT_COMMIT",
				"EAPI", "PYTHON_COMPAT", "RUBY_TARGETS":
				return false // Portage variable, not a pure assignment
			}
			// Pure assignments: MY_P, MY_PN, etc. (contain underscore or short name)
			return true
		}
	}
	return false
}

// splitSRCURI splits a SRC_URI string into individual URI entries.
func splitSRCURI(uri string) []string {
	uri = strings.ReplaceAll(uri, "\\\n", " ")
	uri = strings.ReplaceAll(uri, "\\\\n", " ")
	return strings.Fields(uri)
}

// resolveMirror resolves a mirror:// URI entry.
func resolveMirror(uri string) (string, error) {
	if !strings.HasPrefix(uri, "mirror://") {
		return uri, nil
	}

	rest := strings.TrimPrefix(uri, "mirror://")

	if strings.HasPrefix(rest, "npm/") {
		return "https://registry.npmjs.org/" + strings.TrimPrefix(rest, "npm/"), nil
	}
	if strings.HasPrefix(rest, "pypi/") {
		return "https://files.pythonhosted.org/packages/" + strings.TrimPrefix(rest, "pypi/"), nil
	}
	if strings.HasPrefix(rest, "sourceforge/") {
		return "https://downloads.sourceforge.net/project/" + strings.TrimPrefix(rest, "sourceforge/"), nil
	}
	if strings.HasPrefix(rest, "kernel/") {
		return "https://kernel.org/" + strings.TrimPrefix(rest, "kernel/"), nil
	}

	return rest, nil
}

// SRCURIVars expands a SRC_URI value and returns the expanded string.
func SRCURIVars(uri string, vars VariableMap) (string, error) {
	entries, err := ResolveSRCURIWithVars(uri, vars)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", nil
	}
	return strings.Join(entries, " "), nil
}

// ResolveNpmDistfile resolves a mirror://npm/ URI to the actual npm registry URL.
func ResolveNpmDistfile(uri string, vars VariableMap) (string, error) {
	if !strings.Contains(uri, "mirror://npm/") {
		return "", fmt.Errorf("not an npm mirror URI: %s", uri)
	}

	expanded := ExpandVariables(uri, vars)

	if strings.HasPrefix(expanded, "mirror://npm/") {
		return "https://registry.npmjs.org/" + strings.TrimPrefix(expanded, "mirror://npm/"), nil
	}

	if strings.HasPrefix(expanded, "https://registry.npmjs.org/") {
		return expanded, nil
	}

	parts := strings.SplitN(expanded, "/", 2)
	if len(parts) >= 2 {
		return "https://registry.npmjs.org/" + parts[1], nil
	}

	return "", fmt.Errorf("cannot resolve npm URI: %s", uri)
}
