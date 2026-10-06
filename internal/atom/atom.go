// Package atom provides Gentoo atom parsing, ebuild-filename extraction, and
// latest-ebuild selection — replacing the five broken ad-hoc copies that existed
// across the codebase.
//
// # Key rules
//
// PN comes from the parent directory name, never from splitting the filename on hyphens.
// Revision suffixes are anchored: the regex -r([0-9]+)$ ensures we don't confuse
// "kornia-rs" with a revision.
package atom

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"bennypowers.dev/larry/internal/version"
)

// ParsedEbuild holds the parsed fields of a single ebuild filename.
type ParsedEbuild struct {
	Path     string // absolute path
	Category string // "dev-util"
	PN       string // from the PARENT DIRECTORY NAME
	PVR      string // "1.1.3-r2"
	PV       string // "1.1.3"
	Revision int    // 0 when absent
	P        string // "fish-lsp-1.1.3"
	PF       string // "fish-lsp-1.1.3-r2"
	Live     bool   // PV == "9999"
}

// Atom represents a Gentoo atom (category/pn optionally followed by a version slot).
type Atom struct {
	Category string
	PN       string
	Version  string // "" when the atom carried no version
	PkgDir   string // <overlay>/<category>/<pn>
}

// Sentinels.
var (
	ErrInvalidAtom      = errors.New("invalid atom")
	ErrCategoryNotFound = errors.New("category not found")
	ErrPackageNotFound  = errors.New("package not found")
	ErrNoEbuilds        = errors.New("no ebuilds found")
	ErrNotAnEbuild      = errors.New("not an ebuild of this package")
)

// revRe matches a revision suffix anchored at end of string: -r1, -r12, …
var revRe = regexp.MustCompile(`-r([0-9]+)$`)

// cmpPV orders two PV strings using version.Parse.  Unparseable PVs sort below
// every parseable one, ordered among themselves by strings.Compare.
// 9999 (live) sorts last among parseable versions.
//
// Replace the body with `return version.Compare(a, b)` once plan 14 lands.
func cmpPV(a, b string) int {
	va, errA := version.Parse(a)
	vb, errB := version.Parse(b)
	switch {
	case errA == nil && errB == nil:
		return va.Compare(vb)
	case errA == nil:
		return 1
	case errB == nil:
		return -1
	default:
		return strings.Compare(a, b)
	}
}

// StripRevision removes a trailing -rN revision suffix, if present.
// Only anchored -r\d+$ at a position > 0 is treated as a revision —
// "-r1" alone or "foo-rs" are unchanged.
func StripRevision(s string) string {
	loc := revRe.FindStringIndex(s)
	if loc == nil || loc[0] == 0 {
		return s
	}
	return revRe.ReplaceAllString(s, "")
}

// SplitRevision splits "1.0-r3" into ("1.0", 3).  When there is no revision
// suffix it returns (s, 0).
func SplitRevision(pvr string) (string, int) {
	matches := revRe.FindStringSubmatch(pvr)
	if matches != nil {
		rev, _ := strconv.Atoi(matches[1])
		return revRe.ReplaceAllString(pvr, ""), rev
	}
	return pvr, 0
}

// NextRevision returns the next revision string.
// "1.0" → "1.0-r1", "1.0-r3" → "1.0-r4".
func NextRevision(pvr string) string {
	base, rev := SplitRevision(pvr)
	return fmt.Sprintf("%s-r%d", base, rev+1)
}

// ParseEbuildPath parses an ebuild file path and returns a ParsedEbuild.
// PN comes from the parent directory, not from splitting the filename.
// Returns ErrNotAnEbuild when the basename is not <PN>-*.ebuild.
func ParseEbuildPath(path string) (*ParsedEbuild, error) {
	base := filepath.Base(path)
	dir := filepath.Dir(path)
	dir = filepath.Clean(dir)

	pn := filepath.Base(dir) // PN from parent directory

	if !strings.HasPrefix(base, pn+"-") || !strings.HasSuffix(base, ".ebuild") {
		return nil, fmt.Errorf("%w: %s", ErrNotAnEbuild, base)
	}

	cat := filepath.Base(filepath.Dir(dir))

	// Strip ".ebuild" then the PN prefix to get pvr
	pvr := strings.TrimPrefix(base, pn+"-")
	pvr = strings.TrimSuffix(pvr, ".ebuild")

	pv, rev := SplitRevision(pvr)
	p := pn + "-" + pv
	pf := pn + "-" + pvr

	if pv == "9999" {
		pf = pn + "-" + pv
	}

	return &ParsedEbuild{
		Path:     filepath.Clean(path),
		Category: cat,
		PN:       pn,
		PVR:      pvr,
		PV:       pv,
		Revision: rev,
		P:        p,
		PF:       pf,
		Live:     pv == "9999",
	}, nil
}

// ParseAtom parses a Gentoo atom string and resolves it against the overlay.
// Validates the shape, then walks hyphens longest-first to find the package directory.
func ParseAtom(overlay, atom string) (*Atom, error) {
	atom = strings.TrimSpace(atom)
	if atom == "" {
		return nil, fmt.Errorf("%w: empty", ErrInvalidAtom)
	}

	// Validate shape: cat/pn[-version]
	parts := strings.SplitN(atom, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("%w: %q", ErrInvalidAtom, atom)
	}

	cat := parts[0]
	rest := parts[1]

	// Validate characters: cat must be [a-zA-Z0-9_-]+, rest [a-zA-Z0-9_.+-]+
	if !isValidAtomPart(cat) {
		return nil, fmt.Errorf("%w: bad category %q", ErrInvalidAtom, cat)
	}

	// Separate version from pn
	// Walk backwards, looking for a version suffix.
	// Versions start with a digit.  Hyphen before a digit = version boundary.
	versionStr := ""
	pn := rest

	for i := len(rest) - 1; i > 0; i-- {
		if rest[i] == '-' && i+1 < len(rest) && rest[i+1] >= '0' && rest[i+1] <= '9' {
			versionStr = rest[i+1:]
			pn = rest[:i]
			break
		}
	}

	if !isValidAtomPart(pn) {
		return nil, fmt.Errorf("%w: bad package name %q", ErrInvalidAtom, pn)
	}

	// Stats the category directory
	catDir := filepath.Join(overlay, cat)
	if _, err := os.Stat(catDir); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrCategoryNotFound, catDir)
		}
		return nil, fmt.Errorf("stat category: %w", err)
	}

	// Try exact directory match first
	pkgDir := filepath.Join(catDir, pn)
	if _, err := os.Stat(pkgDir); err == nil {
		return &Atom{
			Category: cat,
			PN:       pn,
			Version:  versionStr,
			PkgDir:   pkgDir,
		}, nil
	}

	// Walk hyphens backwards, longest-first (script L600–624)
	for i := len(pn); i > 0; i-- {
		if pn[i-1] == '-' {
			candidate := pn[:i]
			candidateDir := filepath.Join(catDir, candidate)
			if _, err := os.Stat(candidateDir); err == nil {
				return &Atom{
					Category: cat,
					PN:       candidate,
					Version:  versionStr,
					PkgDir:   candidateDir,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("%w: %s/%s", ErrPackageNotFound, cat, pn)
}

func isValidAtomPart(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// isEbuild returns true if name looks like an ebuild file (ends in .ebuild).
func isEbuild(name string) bool {
	return strings.HasSuffix(name, ".ebuild")
}

// ListEbuilds reads a package directory and returns all parsed ebuilds,
// sorted ascending by PV with revision tiebreaks, live last, unparseable
// just before the first parseable one.
func ListEbuilds(dir string) ([]*ParsedEbuild, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory: %w", err)
	}

	var ebuilds []*ParsedEbuild
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !isEbuild(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		pe, err := ParseEbuildPath(path)
		if err != nil {
			// Skip files that are not ebuilds of this package (ErrNotAnEbuild).
			// But NEVER skip a structurally valid ebuild whose PV fails version.Parse.
			if errors.Is(err, ErrNotAnEbuild) {
				continue
			}
			// ParseEbuildPath succeeded structurally — keep it even if PV is weird
			// (the caller may want to see it).
			ebuilds = append(ebuilds, pe)
		} else {
			ebuilds = append(ebuilds, pe)
		}
	}

	// Sort ascending: unparseable PVs first (by PVR string), parseable by cmpPV,
	// revisions tiebreak within same PV, live (9999) last.
	sort.SliceStable(ebuilds, func(i, j int) bool {
		a, b := ebuilds[i], ebuilds[j]

		// Live versions sort last
		if a.Live && !b.Live {
			return false
		}
		if !a.Live && b.Live {
			return true
		}

		cmp := cmpPV(a.PV, b.PV)
		if cmp != 0 {
			return cmp < 0
		}

		// Same PV — higher revision sorts after lower
		if a.Revision != b.Revision {
			return a.Revision < b.Revision
		}

		// Stable sort key — by full PVR string
		return strings.Compare(a.PVR, b.PVR) < 0
	})

	return ebuilds, nil
}

// FindLatest returns the highest-versioned non-live ebuild in dir, or ErrNoEbuilds.
// If all ebuilds are live (9999), ErrNoEbuilds is returned (script L631).
func FindLatest(dir string) (*ParsedEbuild, error) {
	ebuilds, err := ListEbuilds(dir)
	if err != nil {
		return nil, err
	}

	// Find highest non-live
	var best *ParsedEbuild
	for _, pe := range ebuilds {
		if pe.Live {
			continue
		}
		if best == nil {
			best = pe
		} else {
			cmp := cmpPV(best.PV, pe.PV)
			if cmp < 0 {
				best = pe
			} else if cmp == 0 && best.Revision < pe.Revision {
				best = pe
			}
		}
	}

	if best == nil {
		return nil, ErrNoEbuilds
	}
	return best, nil
}

// FindForAtom resolves an ebuild for a parsed atom.  Exact version match first,
// then FindLatest, then live as a last resort (script L643–664).
func FindForAtom(a *Atom) (*ParsedEbuild, error) {
	ebuilds, err := ListEbuilds(a.PkgDir)
	if err != nil {
		return nil, err
	}

	// Exact version match
	if a.Version != "" {
		for _, pe := range ebuilds {
			if pe.PV == a.Version || pe.PVR == a.Version {
				return pe, nil
			}
		}
		// Also try with PN prefix for the search
		// (the atom might specify "dev-util/javy-8.1.1" which maps to PN "javy")
		for _, pe := range ebuilds {
			if pe.PV == a.Version || pe.PVR == a.Version {
				return pe, nil
			}
		}
		return nil, fmt.Errorf("%w: %s", ErrPackageNotFound, a.Version)
	}

	// Find latest non-live
	if best, err := FindLatest(a.PkgDir); err == nil {
		return best, nil
	}

	// Last resort: live
	for _, pe := range ebuilds {
		if pe.Live {
			return pe, nil
		}
	}

	return nil, ErrNoEbuilds
}
