package ebuild

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// keyLineRe matches lines like "KEY="value"" from ebuild info output.
	keyLineRe = regexp.MustCompile(`^([A-Z_]+)=(.*)$`)
	// inheritLineRe matches the "INHERITED" line specifically.
	inheritLineRe = regexp.MustCompile(`^INHERITED=(.+)$`)
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

	cmd.Run()

	info, _ := parseInfoOutput(path, out.String())

	// If ebuild info didn't produce useful fields, fall back to direct file parsing
	if info != nil && info.Category == "" && info.Pkg == "" {
		info, err := ParseEbuildFromFile(path)
		if err == nil {
			return info, nil
		}
	}

	return info, nil
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

	// Derive category from path
	info.Category = extractCategoryFromPath(path)

	// Extract PN from file or derive from filename
	if pn := extractVar(content, "PN"); pn != "" {
		info.Pkg = pn
	} else {
		info.Pkg = extractPkgName(path)
	}

	// Extract PV from file or derive from filename
	if pv := extractVar(content, "PV"); pv != "" {
		info.PVVal = pv
	} else {
		info.PVVal = extractVersionFromFilename(path)
	}

	// Extract P if present
	if p := extractVar(content, "P"); p != "" {
		info.PVal = p
	}

	// Extract INHERIT
	info.Inherit = extractInherit(content)

	return info, nil
}

// extractVar extracts a variable value from ebuild content.
// Only matches variables at the start of a line (after optional whitespace).
func extractVar(content, name string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, name+"=") {
			continue
		}
		valuePart := strings.TrimSpace(trimmed[len(name)+1:])
		if len(valuePart) >= 2 && (valuePart[0] == '"' || valuePart[0] == '\'') {
			quote := valuePart[0]
			end := strings.Index(valuePart[1:], string(quote))
			if end >= 0 {
				return valuePart[1 : 1+end]
			}
		}
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

// extractCategoryFromPath extracts the category from an ebuild file path.
// e.g., "/path/to/overlay/dev-python/pkg-1.0.ebuild" -> "dev-python"
func extractCategoryFromPath(path string) string {
	dir := filepath.Dir(path)
	return filepath.Base(dir)
}

// extractVersionFromFilename extracts version from ebuild filename.
// e.g., "pkg-1.2.3.ebuild" -> "1.2.3"
func extractVersionFromFilename(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".ebuild")
	if idx := strings.LastIndex(base, "-"); idx > 0 {
		return base[idx+1:]
	}
	return base
}

// extractPkgName extracts package name from ebuild filename.
// e.g., "pkg-1.2.3.ebuild" -> "pkg"
func extractPkgName(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".ebuild")
	if idx := strings.LastIndex(base, "-"); idx > 0 {
		return base[:idx]
	}
	return base
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
