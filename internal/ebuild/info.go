package ebuild

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
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

	if err := cmd.Run(); err != nil {
		// ebuild info may exit non-zero if there are eclass issues; still try to parse output
		if strings.Contains(stderr.String(), "not found") || strings.Contains(stderr.String(), "not installed") {
			return nil, fmt.Errorf("ebuild not found in PATH; is Portage installed?")
		}
		// Return partial info if we got some output, or wrap the error
		if out.Len() > 0 {
			return parseInfoOutput(path, out.String())
		}
		return nil, fmt.Errorf("ebuild info failed: %s", stderr.String())
	}

	return parseInfoOutput(path, out.String())
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
