package ebuild

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"bennypowers.dev/larry/internal/atom"
)

var (
	// keyLineRe matches lines like "KEY="value"" from ebuild info output.
	keyLineRe = regexp.MustCompile(`^([A-Z_]+)=(.*)$`)
)

// ParseEbuildInfo runs `ebuild <path> info` and parses the output into EbuildInfo.
func ParseEbuildInfo(path string) (*EbuildInfo, error) {
	if path == "" {
		return nil, fmt.Errorf("ebuild path is empty")
	}

	// Check file exists before invoking ebuild
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("ebuild file does not exist: %w", err)
	}

	cmd := exec.Command("ebuild", path, "info")
	var out, stderr strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// ebuild info may exit non-zero; fall through to file-based parsing.
		_ = err
	}

	info, _ := parseInfoOutput(path, out.String())

	// Always fall back to direct file parsing to get accurate SRC_URI, HOMEPAGE,
	// INHERIT, and Raw content. ebuid info often produces truncated/malformed
	// SRC_URI for multi-line URIs (e.g., SRC_URI="\"\"\").
	fileInfo, err := ParseEbuildFromFile(path)
	if err != nil {
		return info, nil
	}

	// Merge: ebuild info fields take priority for PN/PV, but file parsing
	// provides accurate SRC_URI, HOMEPAGE, INHERIT, and Raw content.
	if info != nil && (info.Category != "" || info.Pkg != "") {
		// Use ebuild info Category/PV if valid
		if fileInfo.Category == "" {
			fileInfo.Category = info.Category
		}
		if fileInfo.PVVal == "" {
			fileInfo.PVVal = info.PVVal
		}
	}

	return fileInfo, nil
}

// parseInfoOutput parses the text output of `ebuild <path> info` into EbuildInfo.
func parseInfoOutput(path string, raw string) (*EbuildInfo, error) {
	info := &EbuildInfo{
		Path: path,
	}

	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Match KEY=value pattern
		matches := keyLineRe.FindStringSubmatch(trimmed)
		if matches == nil {
			continue
		}

		key := matches[1]
		value := matches[2]

		switch key {
		case "CATEGORY":
			info.Category = unquote(value)
		case "PN":
			info.Pkg = unquote(value)
		case "PV":
			info.PVVal = unquote(value)
		case "P":
			info.PVal = unquote(value)
		case "SRC_URI":
			info.SRC_URI = unquote(value)
		case "DEPEND":
			info.DEPEND = unquote(value)
		case "RDEPEND":
			info.RDEPEND = unquote(value)
		case "HOMEPAGE":
			info.HOMEPAGE = unquote(value)
		case "INHERITED":
			info.Inherit = parseInherited(value)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning ebuild info output: %w", err)
	}

	return info, nil
}

// unquote removes surrounding quotes (double or single) from a string.
// It also trims whitespace from the inner content.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if s[0] == '"' && s[len(s)-1] == '"' {
			return strings.TrimSpace(s[1 : len(s)-1])
		}
		// Single quote: byte value 39
		if s[0] == byte(39) && s[len(s)-1] == byte(39) {
			return strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	return s
}

// parseInherited splits the INHERITED value into a slice of eclass names.
// Values are unquoted first.
func parseInherited(raw string) []string {
	// Unquote first
	unquoted := unquote(raw)
	names := strings.Fields(unquoted)
	if len(names) == 0 {
		return nil
	}
	return names
}

// MustParseEbuildInfo calls ParseEbuildInfo and panics on error.
// Useful in tests where a valid ebuild path is guaranteed.
func MustParseEbuildInfo(path string) *EbuildInfo {
	info, err := ParseEbuildInfo(path)
	if err != nil {
		panic(fmt.Sprintf("MustParseEbuildInfo(%q): %v", path, err))
	}
	return info
}

// ParseEbuildFromFile parses an ebuild file directly, without invoking ebuild info.
// This is used as a fallback when ebuild info doesn't produce useful output.
func ParseEbuildFromFile(path string) (*EbuildInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ebuild file: %w", err)
	}
	content := string(data)

	info := &EbuildInfo{
		Path:     path,
		SRC_URI:  extractVar(content, "SRC_URI"),
		HOMEPAGE: extractVar(content, "HOMEPAGE"),
	}

	// Derive category and PN from path
	pe, err := atom.ParseEbuildPath(path)
	if err == nil {
		info.Category = pe.Category
	} else {
		// Fallback: category is parent of parent directory
		info.Category = filepath.Base(filepath.Dir(path))
	}

	// Extract PN from file or derive from filename
	if pn := extractVar(content, "PN"); pn != "" {
		info.Pkg = pn
	} else if pe != nil {
		info.Pkg = pe.PN
	}

	// Extract PV from file or derive from filename
	if pv := extractVar(content, "PV"); pv != "" {
		info.PVVal = pv
	} else if pe != nil {
		info.PVVal = pe.PV
	}

	// Extract P if present
	if p := extractVar(content, "P"); p != "" {
		info.PVal = p
	}

	// Extract INHERIT
	info.Inherit = extractInherit(content)
	info.InheritRaw = extractInheritRaw(content)
	info.Raw = content

	return info, nil
}

// extractVar extracts a variable value from ebuild content.
// Only matches variables at the start of a line (after optional whitespace).
// Handles both single-line and multi-line quoted values (e.g., SRC_URI="...").
func extractVar(content, name string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, name+"=") {
			continue
		}
		valuePart := strings.TrimSpace(trimmed[len(name)+1:])

		// Check if value starts with a quote
		if len(valuePart) >= 1 && (valuePart[0] == '"' || valuePart[0] == '\'') {
			quote := valuePart[0]
			// Check if the closing quote is on the same line (after the opening quote)
			end := strings.Index(valuePart[1:], string(quote))
			if end >= 0 {
				// Single-line value
				return valuePart[1 : 1+end]
			}
			// Multi-line value: collect lines until closing quote
			var parts []string
			// Take remainder of first line (after opening quote)
			parts = append(parts, valuePart[1:])
			for j := i + 1; j < len(lines); j++ {
				l := lines[j]
				closeIdx := strings.Index(l, string(quote))
				if closeIdx >= 0 {
					parts = append(parts, l[:closeIdx])
					return strings.Join(parts, "\n")
				}
				parts = append(parts, l)
			}
			// No closing quote found, return what we have
			return strings.Join(parts, "\n")
		}

		// Unquoted value: take until whitespace
		end := strings.IndexAny(valuePart, " \t\n")
		if end > 0 {
			return valuePart[:end]
		}
		if len(valuePart) > 0 {
			return valuePart
		}
	}
	return ""
}

// extractInherit extracts the inherit line from ebuild content.
func extractInherit(content string) []string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inherit ") {
			rest := strings.TrimSpace(line[len("inherit "):])
			return strings.Fields(rest)
		}
	}
	return nil
}

// extractInheritRaw extracts the raw inherit line content (after "inherit ").
func extractInheritRaw(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inherit ") {
			return strings.TrimSpace(line[len("inherit "):])
		}
	}
	return ""
}
