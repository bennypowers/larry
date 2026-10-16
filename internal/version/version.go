package version

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// SuffixType represents a version suffix.
type SuffixType int

const (
	_ SuffixType = iota
	Alpha          // _alpha1, _a1
	Beta           // _beta1, _b1
	Pre            // _pre1 (pre-release, before RC)
	Rc             // _rc1
	Release        // no suffix — the final release
	Patch          // _p1 (patch-level post-release)

	// Numeric ordering (suffixKey): alpha=1 < beta=2 < pre=3 < rc=4 < release=5 < p=6
	// This mirrors Gentoo/PkgVersionCompare semantics.
	// NOTE: suffix rank is compared BEFORE the revision, so 1.0_p1 > 1.0-r1.
)

func (s SuffixType) String() string {
	switch s {
	case Alpha:
		return "alpha"
	case Beta:
		return "beta"
	case Pre:
		return "pre"
	case Rc:
		return "rc"
	case Patch:
		return "patch"
	case Release:
		return "release"
	default:
		return "unknown"
	}
}

// Version represents a Gentoo package version.
// Components can be 1–5 elements: e.g. 1, 1.0, 1.2.3, 1.2.3.4, 2024.01.01
type Version struct {
	Components []uint64 // flexible: longest leading all-numeric dot-run (legacy field)
	base       string   // base version string for Format reconstruction
	Suffix     *Suffix
	Revision   uint64
	Live       bool // true for 9999 (or any component that is 9999)
	segments   []segment // authoritative for comparison
}

// segment is an atomic piece of a version base: either a numeric run or a string run.
type segment struct {
	num   uint64
	str   string
	isNum bool
}

// Suffix is a pre-release or patch-level marker.
type Suffix struct {
	Type   SuffixType
	Number uint32
}

var (
	// peeledRe strips ^{...} ANYWHERE in the string.
	peeledRe = regexp.MustCompile(`\^\{.*\}`)
	// vPrefixRe strips leading v/V or v./V. only when followed by a digit.
	vPrefixRe = regexp.MustCompile(`^[vV]\.?([0-9])`)
	// revRe matches -rN at the end of the string (anchored).
	revRe = regexp.MustCompile(`-r([0-9]+)$`)
	// dashSuffixRe rewrites dashed pre-release forms to underscore form.
	// Requires at least one digit after the suffix keyword (e.g., "1.0.0-beta.1" → "1.0.0_beta1")
	// Bare suffixes like "1.0-alpha" are left unchanged (not recognized as Gentoo suffixes).
	dashSuffixRe = regexp.MustCompile(`[-.](alpha|beta|pre|rc)\.?([0-9]+)$`)
	// bareSuffixRe normalizes bare PyPI-style pre-release suffixes (e.g., "3.3.1b3")
	// to underscore form so Parse can recognize them as prereleases.
	// Matches digit + single letter (a/b/c) + digits at end of string.
	bareSuffixRe = regexp.MustCompile(`([0-9])([abc])([0-9]+)$`)
)

// Normalize strips known prefix noise from a version string.
// Steps: TrimSpace → strip ^{.*} anywhere → strip bun-/rocm-/release- → strip v/V prefix → TrimSpace.
// Input: "v1.2.3", "bun-1.2.3", "rocm-1.2.3", "release-1.2.3", "v1.0^{}"
// Output: "1.2.3"
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	// Strip ^{...} wrapper — ANYWHERE in the string
	s = peeledRe.ReplaceAllString(s, "")
	// Strip known prefixes (rel- is NOT stripped — D7)
	prefixes := []string{"bun-", "rocm-", "release-"}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			s = strings.TrimPrefix(s, p)
			break
		}
	}
	// Strip leading v or V only when followed by a digit
	s = vPrefixRe.ReplaceAllString(s, "$1")
	return strings.TrimSpace(s)
}

// Parse parses a version string into a Version struct.
// Handles: 1.2.3, 1.2.3-r1, 1.2.3_p1, 1.0_alpha1, 1.0_beta2, 1.0_rc3, 9999,
//          1.2.3a, 2.0.0-rc1, 1.0.0-beta.1, 1.2.3.4.5.6, 20250410, 1.0_pre
func Parse(s string) (*Version, error) {
	// Step 1: Normalize
	s = Normalize(s)
	if s == "" {
		return nil, fmt.Errorf("invalid version string: %q", s)
	}

	// Step 2: Gate — must start with a digit
	if len(s) == 0 || s[0] < '0' || s[0] > '9' {
		return nil, fmt.Errorf("invalid version string: %q", s)
	}

	// Step 3: Revision — anchored -rN at end
	revision := uint64(0)
	if m := revRe.FindStringSubmatch(s); m != nil {
		var err error
		revision, err = strconv.ParseUint(m[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid revision in version: %q", s)
		}
		s = s[:len(s)-len(m[0])]
	}

	// Step 3.5: Normalize bare PyPI-style pre-release suffixes.
	// Converts "3.3.1b3" → "3.3.1_b3" so parseSuffix can recognize it.
	s = bareSuffixRe.ReplaceAllString(s, "_${1}_${2}$3")

	// Step 4: Dashed pre-release rewrite — one substitution
	s = dashSuffixRe.ReplaceAllString(s, "_$1$2")

	// Step 5: Suffix — parse _ suffix
	var suffix *Suffix
	if idx := strings.LastIndex(s, "_"); idx > 0 {
		suffixPart := s[idx+1:]
		base := s[:idx]

		suffixType, suffixNum, ok := parseSuffix(suffixPart)
		if ok {
			suffix = &Suffix{Type: suffixType, Number: suffixNum}
			s = base
		}
	}

	// Step 6: Segments — split base into numeric/non-digit runs (dropping ., -, _)
	segs := parseSegments(s)

	// Step 7: Components — longest leading all-numeric dot-run (legacy)
	// Store base for Format reconstruction
	components := parseComponents(s)
	base := s

	v := &Version{
		Components: components,
		base:       base,
		Suffix:     suffix,
		Revision:   revision,
		segments:   segs,
	}

	// Step 8: Live version check — any component is 9999
	for _, c := range v.Components {
		if c == 9999 {
			v.Live = true
			break
		}
	}

	return v, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// parseSuffix parses a suffix string like "alpha1", "a2", "beta3", "rc1", "p1", "pre4"
// Returns (SuffixType, Number, success).
// Tries markers in order: alpha, beta, pre, rc, a, b, p.
// If the numeric tail is non-empty, it must be all digits; if empty, number defaults to 0.
func parseSuffix(s string) (SuffixType, uint32, bool) {
	switch {
	case strings.HasPrefix(s, "alpha"):
		if len(s) >= 5 {
			tail := s[5:]
			if tail == "" || isDigits(tail) {
				n, _ := strconv.ParseUint(tail, 10, 32)
				return Alpha, uint32(n), true
			}
		}
	case strings.HasPrefix(s, "a") && len(s) >= 1:
		tail := s[1:]
		if tail == "" || isDigits(tail) {
			n, _ := strconv.ParseUint(tail, 10, 32)
			return Alpha, uint32(n), true
		}
	case strings.HasPrefix(s, "beta"):
		if len(s) >= 4 {
			tail := s[4:]
			if tail == "" || isDigits(tail) {
				n, _ := strconv.ParseUint(tail, 10, 32)
				return Beta, uint32(n), true
			}
		}
	case strings.HasPrefix(s, "b") && len(s) >= 1:
		tail := s[1:]
		if tail == "" || isDigits(tail) {
			n, _ := strconv.ParseUint(tail, 10, 32)
			return Beta, uint32(n), true
		}
	case strings.HasPrefix(s, "pre") && len(s) >= 3:
		tail := s[3:]
		if tail == "" || isDigits(tail) {
			n, _ := strconv.ParseUint(tail, 10, 32)
			return Pre, uint32(n), true
		}
	case strings.HasPrefix(s, "rc") && len(s) >= 2:
		tail := s[2:]
		if tail == "" || isDigits(tail) {
			n, _ := strconv.ParseUint(tail, 10, 32)
			return Rc, uint32(n), true
		}
	case strings.HasPrefix(s, "p") && len(s) >= 1:
		tail := s[1:]
		if tail == "" || isDigits(tail) {
			n, _ := strconv.ParseUint(tail, 10, 32)
			return Patch, uint32(n), true
		}
	}
	return 0, 0, false
}

func parseComponents(s string) []uint64 {
	parts := strings.Split(s, ".")
	// Collect longest leading all-numeric dot-run
	var components []uint64
	for _, p := range parts {
		if p == "" || !isDigits(p) {
			break
		}
		c, _ := strconv.ParseUint(p, 10, 64)
		components = append(components, c)
	}
	if len(components) == 0 {
		// No dot-separated numeric components; check if the whole string is numeric
		if isDigits(s) {
			c, _ := strconv.ParseUint(s, 10, 64)
			return []uint64{c}
		}
		return nil
	}
	return components
}

// parseSegments splits the base string into maximal runs of ASCII digits and non-digit
// characters, dropping ., -, and _ (they are boundaries, not segments).
func parseSegments(s string) []segment {
	var segs []segment
	var buf strings.Builder
	for _, c := range s {
		if c == '.' || c == '-' || c == '_' {
			// Flush current run
			if buf.Len() > 0 {
				segs = flushBuf(&buf, &segs)
			}
			continue
		}
		buf.WriteRune(c)
	}
	if buf.Len() > 0 {
		segs = flushBuf(&buf, &segs)
	}
	return segs
}

func flushBuf(buf *strings.Builder, segs *[]segment) []segment {
	content := buf.String()
	buf.Reset()
	if isDigits(content) {
		n, _ := strconv.ParseUint(content, 10, 64)
		*segs = append(*segs, segment{num: n, isNum: true})
	} else {
		*segs = append(*segs, segment{str: content, isNum: false})
	}
	return *segs
}

// Format returns the version as a string.
func (v *Version) Format() string {
	var sb strings.Builder
	sb.WriteString(v.base)
	if v.Suffix != nil {
		sb.WriteString("_")
		switch v.Suffix.Type {
		case Alpha:
			sb.WriteString("alpha")
		case Beta:
			sb.WriteString("beta")
		case Pre:
			sb.WriteString("pre")
		case Rc:
			sb.WriteString("rc")
		case Patch:
			sb.WriteString("p")
		}
		sb.WriteString(strconv.FormatUint(uint64(v.Suffix.Number), 10))
	}
	if v.Revision > 0 {
		sb.WriteString("-r")
		sb.WriteString(strconv.FormatUint(v.Revision, 10))
	}
	return sb.String()
}

// Compare returns -1, 0, or 1 if v < other, v == other, or v > other.
// Implements Gentoo PkgVersionCompare semantics.
func (v *Version) Compare(other *Version) int {
	// nil handling
	if v == nil && other == nil {
		return 0
	}
	if v == nil {
		return -1
	}
	if other == nil {
		return 1
	}

	// Live versions (9999) beat non-Live
	if v.Live && !other.Live {
		return 1
	}
	if !v.Live && other.Live {
		return -1
	}

	// Compare segments (authoritative).
	// Drop trailing zero-valued numeric segments from both sides first.
	aSegs := dropTrailingZeros(v.segments)
	bSegs := dropTrailingZeros(other.segments)

	minLen := len(aSegs)
	if len(bSegs) < minLen {
		minLen = len(bSegs)
	}

	for i := 0; i < minLen; i++ {
		cmp := compareSegment(aSegs[i], bSegs[i])
		if cmp != 0 {
			return cmp
		}
	}

	// All shared positions equal → longer slice wins
	if len(aSegs) != len(bSegs) {
		if len(aSegs) < len(bSegs) {
			return -1
		}
		return 1
	}

	// Segments equal — compare suffixes
	sv := suffixKey(v.Suffix)
	so := suffixKey(other.Suffix)
	if sv < so {
		return -1
	}
	if sv > so {
		return 1
	}

	// Same suffix type — compare suffix number
	if v.Suffix != nil && other.Suffix != nil {
		if v.Suffix.Number < other.Suffix.Number {
			return -1
		}
		if v.Suffix.Number > other.Suffix.Number {
			return 1
		}
	}

	// Compare revisions
	if v.Revision < other.Revision {
		return -1
	}
	if v.Revision > other.Revision {
		return 1
	}

	return 0
}

// compareSegment compares two segments pairwise:
// num vs num → numeric; str vs str → strings.Compare; num vs str → numeric below alphabetic.
func compareSegment(a, b segment) int {
	if a.isNum && b.isNum {
		if a.num < b.num {
			return -1
		}
		if a.num > b.num {
			return 1
		}
		return 0
	}
	if a.isNum && !b.isNum {
		// numeric sorts below alphabetic
		return -1
	}
	if !a.isNum && b.isNum {
		return 1
	}
	// Both strings
	return strings.Compare(a.str, b.str)
}

// dropTrailingZeros drops trailing numeric segments whose value is 0.
func dropTrailingZeros(segs []segment) []segment {
	result := make([]segment, len(segs))
	copy(result, segs)
	for len(result) > 0 {
		last := result[len(result)-1]
		if last.isNum && last.num == 0 {
			result = result[:len(result)-1]
		} else {
			break
		}
	}
	return result
}

// suffixKey returns the numeric sort key for a suffix.
// alpha=1, beta=2, pre=3, rc=4, release=5 (no suffix), patch=6
func suffixKey(s *Suffix) int {
	if s == nil {
		return 5 // release (no suffix) beats pre-releases
	}
	switch s.Type {
	case Alpha:
		return 1
	case Beta:
		return 2
	case Pre:
		return 3
	case Rc:
		return 4
	case Release:
		return 5
	case Patch:
		return 6
	default:
		return 0
	}
}

// String returns the version as a string (alias for Format).
func (v *Version) String() string {
	return v.Format()
}

// IsPrerelease reports whether v is a pre-release version.
// True for alpha, beta, pre, and rc suffixes.
// Patch (_p), revisions (-rN), and live versions (9999) are NOT pre-releases.
func (v *Version) IsPrerelease() bool {
	if v == nil || v.Suffix == nil {
		return false
	}
	return v.Suffix.Type == Alpha || v.Suffix.Type == Beta || v.Suffix.Type == Pre || v.Suffix.Type == Rc
}

// ExtractDate finds a YYYYMMDD date (exactly 8 digits starting with "20") in s.
// Returns the 8-digit string and true if found; ("", false) otherwise.
func ExtractDate(s string) (string, bool) {
	m := regexp.MustCompile(`(20\d{6})`).FindStringSubmatch(s)
	if len(m) < 2 {
		return "", false
	}
	return m[1], true
}

// Compare parses a and b and compares them.
// Unparseable input sorts BELOW any parseable version; two unparseable values
// fall back to strings.Compare, so the result is always a total order.
func Compare(a, b string) int {
	va, errA := Parse(a)
	vb, errB := Parse(b)

	if errA != nil && errB != nil {
		// Both unparseable → strings.Compare (total order guaranteed)
		return strings.Compare(a, b)
	}
	if errA != nil {
		// a unparseable → sorts below b
		return -1
	}
	if errB != nil {
		// b unparseable → a sorts above
		return 1
	}
	return va.Compare(vb)
}

// Less is Compare(a, b), for slices.SortFunc / sort.Slice.
func Less(a, b string) int {
	return Compare(a, b)
}
