package ebuild

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bennypowers.dev/larry/internal/atom"
)

// filesdirVersionVars are variables that re-evaluate on every version bump.
// They must not appear in ${FILESDIR} references: files under files/ are not
// renamed or carried over by bumps, so the reference stops resolving.
var filesdirVersionVars = []string{"${P}", "${PV}", "${PF}", "${PVR}"}

// FilesdirPin describes a ${FILESDIR} reference whose path contains a
// version variable such as ${PV} or ${P}.
type FilesdirPin struct {
	Line int    // 1-based line number in the ebuild
	Ref  string // path expression following ${FILESDIR}, e.g. ${PN}-${PV}-r1.initd
}

// FindFilesdirVersionPins scans raw ebuild content for ${FILESDIR}
// references whose path contains a version variable. Line-based and
// heuristic by design: it is a warning aid, not a parser.
func FindFilesdirVersionPins(raw string) []FilesdirPin {
	var pins []FilesdirPin
	for i, line := range strings.Split(raw, "\n") {
		for idx := strings.Index(line, "${FILESDIR}"); idx >= 0; {
			rest := line[idx+len("${FILESDIR}"):]
			ref := cutFilesdirRef(rest)
			if ref != "" {
				for _, v := range filesdirVersionVars {
					if strings.Contains(ref, v) {
						pins = append(pins, FilesdirPin{Line: i + 1, Ref: ref})
						break
					}
				}
			}
			next := strings.Index(rest, "${FILESDIR}")
			if next < 0 {
				break
			}
			idx = idx + len("${FILESDIR}") + next
		}
	}
	return pins
}

// cutFilesdirRef extracts the path expression that follows a ${FILESDIR}
// token: everything up to whitespace or a quote, with leading slashes and
// quotes stripped. Returns "" when nothing follows the token.
func cutFilesdirRef(rest string) string {
	rest = strings.TrimLeft(rest, "\"'/")
	if rest == "" {
		return ""
	}
	cut := len(rest)
	for i, r := range rest {
		if r == ' ' || r == '\t' || r == '"' || r == '\'' {
			cut = i
			break
		}
	}
	return rest[:cut]
}

// FilesdirWarning is a resolved version-pinned FILESDIR reference.
type FilesdirWarning struct {
	Line    int
	Name    string // expanded file name relative to files/
	Missing bool   // the expanded reference does not exist in files/
	Message string
}

// CheckFilesdirPins resolves the version-pinned ${FILESDIR} references in
// raw against the files/ directory next to the ebuild at ebuildPath and
// returns a warning per reference.
func CheckFilesdirPins(ebuildPath, raw string) []FilesdirWarning {
	pins := FindFilesdirVersionPins(raw)
	if len(pins) == 0 {
		return nil
	}

	pe, err := atom.ParseEbuildPath(ebuildPath)
	if err != nil {
		return nil
	}
	vars := BuildVariableMap(raw, pe)
	filesDir := filepath.Join(filepath.Dir(ebuildPath), "files")

	warnings := make([]FilesdirWarning, 0, len(pins))
	for _, pin := range pins {
		name := ExpandVariables(pin.Ref, vars)
		w := FilesdirWarning{Line: pin.Line, Name: name}
		if _, err := os.Stat(filepath.Join(filesDir, name)); err != nil {
			w.Missing = true
			w.Message = fmt.Sprintf(
				"files/%s does not exist — version bumps do not carry files/ entries over; copy it from the previous version or use an unversioned filename (line %d)",
				name, pin.Line)
		} else {
			w.Message = fmt.Sprintf(
				"version-pinned FILESDIR reference files/%s — consider an unversioned filename so future bumps keep working (line %d)",
				name, pin.Line)
		}
		warnings = append(warnings, w)
	}
	return warnings
}
