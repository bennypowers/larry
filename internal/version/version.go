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
	Rc             // _rc1, _pre1
	Patch          // _p1 (patch-level pre-release)
	Release        // no suffix — the final release
	R1             // -r1 (revision)

	// Numeric ordering: alpha < beta < rc < patch < release < revision
	// This mirrors Gentoo/PkgVersionCompare semantics.
)

func (s SuffixType) String() string {
	switch s {
	case Alpha:
		return "alpha"
	case Beta:
		return "beta"
	case Rc:
		return "rc"
	case Patch:
		return "patch"
	case Release:
		return "release"
	case R1:
		return "r"
	default:
		return "unknown"
	}
}

// Version represents a Gentoo package version.
// Components can be 1–5 elements: e.g. 1, 1.0, 1.2.3, 1.2.3.4, 2024.01.01
type Version struct {
	Components []uint64 // flexible: 1, 2, 3, 4, or 5 components
	Suffix     *Suffix
	Revision   uint64
	Live       bool // true for 9999 (or any component that is 9999)
}

// Suffix is a pre-release or patch-level marker.
type Suffix struct {
	Type   SuffixType
	Number uint32
}

var (
	// normalizeRe strips known prefix noise and ^{...} wrappers.
	normalizeRe = regexp.MustCompile(`^\^{(.+?)\}$`)
)

// Normalize strips known prefix noise from a version string.
// Input: "v1.2.3", "bun-1.2.3", "rocm-1.2.3", "release-1.2.3", "^{1.2.3}"
// Output: "1.2.3"
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	// Strip ^{...} wrapper
	s = normalizeRe.ReplaceAllString(s, `$1`)
	// Strip known prefixes
	prefixes := []string{"bun-", "rocm-", "release-", "rel-"}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			s = strings.TrimPrefix(s, p)
			break
		}
	}
	// Strip leading v or V
	if len(s) > 0 && (s[0] == 'v' || s[0] == 'V') {
		s = s[1:]
	}
	return strings.TrimSpace(s)
}

// Parse parses a version string into a Version struct.
// Handles: 1.2.3, 1.2.3-r1, 1.2.3_p1, 1.0_alpha1, 1.0_beta2, 1.0_rc3, 9999
func Parse(s string) (*Version, error) {
	s = Normalize(s)
	if s == "" {
		return nil, fmt.Errorf("invalid version string: %q", s)
	}

	// Extract revision first: -rN (last occurrence)
	revision := uint64(0)
	if idx := strings.LastIndex(s, "-r"); idx > 0 {
		revStr := s[idx+2:]
		if revStr != "" && isDigits(revStr) {
			rev, err := strconv.ParseUint(revStr, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid revision in version: %q", s)
			}
			revision = rev
			s = s[:idx]
		}
	}

	// Extract suffix: _alphaN, _aN, _betaN, _bN, _rcN, _preN, _pN
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

	// Parse components from the base
	components := parseComponents(s)
	if len(components) == 0 {
		return nil, fmt.Errorf("invalid version string: %q", s)
	}

	v := &Version{
		Components: components,
		Suffix:     suffix,
		Revision:   revision,
	}

	// Live version check: any component is 9999
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

// parseSuffix parses a suffix string like "alpha1", "a2", "beta3", "rc1", "p1"
// Returns (SuffixType, Number, success).
func parseSuffix(s string) (SuffixType, uint32, bool) {
	switch {
	case strings.HasPrefix(s, "alpha") && len(s) > 5:
		n, _ := strconv.ParseUint(s[5:], 10, 32)
		return Alpha, uint32(n), true
	case strings.HasPrefix(s, "a") && len(s) > 1:
		n, _ := strconv.ParseUint(s[1:], 10, 32)
		return Alpha, uint32(n), true
	case strings.HasPrefix(s, "beta") && len(s) > 4:
		n, _ := strconv.ParseUint(s[4:], 10, 32)
		return Beta, uint32(n), true
	case strings.HasPrefix(s, "b") && len(s) > 1:
		n, _ := strconv.ParseUint(s[1:], 10, 32)
		return Beta, uint32(n), true
	case strings.HasPrefix(s, "rc") && len(s) > 2:
		n, _ := strconv.ParseUint(s[2:], 10, 32)
		return Rc, uint32(n), true
	case strings.HasPrefix(s, "pre") && len(s) > 3:
		n, _ := strconv.ParseUint(s[3:], 10, 32)
		return Rc, uint32(n), true
	case strings.HasPrefix(s, "p") && len(s) > 1:
		n, _ := strconv.ParseUint(s[1:], 10, 32)
		return Patch, uint32(n), true
	}
	return 0, 0, false
}

func parseComponents(s string) []uint64 {
	parts := strings.Split(s, ".")
	// Max 5 components
	if len(parts) > 5 || len(parts) == 0 {
		return nil
	}
	// Validate each part is a non-empty digit string
	for _, p := range parts {
		if p == "" || !isDigits(p) {
			return nil
		}
	}
	components := make([]uint64, len(parts))
	for i, p := range parts {
		components[i], _ = strconv.ParseUint(p, 10, 64)
	}
	return components
}

// Format returns the version as a string.
func (v *Version) Format() string {
	var sb strings.Builder
	for i, c := range v.Components {
		if i > 0 {
			sb.WriteString(".")
		}
		sb.WriteString(strconv.FormatUint(c, 10))
	}
	if v.Suffix != nil {
		sb.WriteString("_")
		switch v.Suffix.Type {
		case Alpha:
			sb.WriteString("alpha")
		case Beta:
			sb.WriteString("beta")
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
	if v == nil && other == nil {
		return 0
	}
	if v == nil {
		return -1
	}
	if other == nil {
		return 1
	}

	// Live versions (9999) are greater than any finite version
	if v.Live && !other.Live {
		return 1
	}
	if !v.Live && other.Live {
		return -1
	}

	// Compare components
	maxLen := len(v.Components)
	if len(other.Components) > maxLen {
		maxLen = len(other.Components)
	}
	for i := 0; i < maxLen; i++ {
		aVal := uint64(0)
		bVal := uint64(0)
		if i < len(v.Components) {
			aVal = v.Components[i]
		}
		if i < len(other.Components) {
			bVal = other.Components[i]
		}
		if aVal < bVal {
			return -1
		}
		if aVal > bVal {
			return 1
		}
	}

	// Components equal — compare suffixes
	// Order: alpha < beta < rc < patch < release < revision
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

// suffixKey returns the numeric sort key for a suffix.
// Alpha=1, Beta=2, Rc=3, Patch=4, Release=5 (no suffix), Revision=6
func suffixKey(s *Suffix) int {
	if s == nil {
		return 5 // release (no suffix) beats pre-releases
	}
	switch s.Type {
	case Alpha:
		return 1
	case Beta:
		return 2
	case Rc:
		return 3
	case Patch:
		return 4
	case Release:
		return 5
	case R1:
		return 6 // revision beats release
	default:
		return 0
	}
}

// String returns the version as a string (alias for Format).
func (v *Version) String() string {
	return v.Format()
}

// IsPrerelease reports whether v is a pre-release version.
// Pre-releases are versions with alpha, beta, rc, or patch (_p) suffixes.
// Revisions (-rN) are NOT pre-releases — they are post-release.
// Live versions (9999) are NOT pre-releases.
func (v *Version) IsPrerelease() bool {
	if v == nil || v.Suffix == nil {
		return false
	}
	// alpha, beta, rc, and patch (_p) are pre-releases
	// release and revision are NOT pre-releases
	return v.Suffix.Type == Alpha || v.Suffix.Type == Beta || v.Suffix.Type == Rc || v.Suffix.Type == Patch
}
