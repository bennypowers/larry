package upstream

import (
	"regexp"
	"sort"
	"strings"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/provider"
)

// maxDepDepth is the maximum recursion depth for in-overlay dependency scanning.
// The root package is depth 0. A package at depth < 3 recurses; depth 3 prints but
// does not recurse. Maximum printed depth is 3.
const maxDepDepth = 3

// depAtomRe matches category/package-name tokens inside DEPEND/RDEPEND strings.
// It is the script's `[a-z0-9-]+/[a-z0-9_-]+` (L1316) widened with '+', '.' and
// uppercase-free name chars so that dev-libs/libstdc++ and dev-python/zope.interface
// are matched instead of truncated.
var depAtomRe = regexp.MustCompile(`[a-z0-9-]+/[a-z0-9_+.-]+`)

// depVerRe matches a trailing "-<version>" on a dep atom: a hyphen, then a digit,
// then anything that is not a path separator, to end of string.
//
// Known over-strip: a package whose name genuinely ends in -<digits>
// (e.g. media-fonts/font-adobe-100dpi) becomes media-fonts/font-adobe. That atom
// will not exist as an overlay directory, so the in-overlay filter drops it.
// Accept this; do not add a heuristic.
var depVerRe = regexp.MustCompile(`-[0-9][^/]*$`)

// extractAtoms extracts Gentoo package atoms from a dependency string.
// Widen the regex beyond the script, then strip operator prefixes, versions and slots.
// Output is deduplicated and sorted (matching the script's sort -u).
func extractAtoms(input string) []string {
	if input == "" {
		return nil
	}

	work := input

	// Remove use-flag conditionals: "python? ( ... )" → inner content
	useFlagRe := regexp.MustCompile(`\w+\?\s*\(([^)]+)\)`)
	work = useFlagRe.ReplaceAllString(work, "$1")

	// Remove disjunctions: "|| ( a b )" → "a b"
	disjunctionRe := regexp.MustCompile(`\|\|\s*\(([^)]+)\)`)
	work = disjunctionRe.ReplaceAllString(work, "$1")

	// Remove brace expansions: "pkg[flag]" → "pkg"
	braceRe := regexp.MustCompile(`\[[^\]]*\]`)
	work = braceRe.ReplaceAllString(work, "")

	// Remove sub-slot specifiers like ":0", ":abi_x86_64"
	slotRe := regexp.MustCompile(`:\S+`)
	work = slotRe.ReplaceAllString(work, "")

	// Find all category/package matches
	matches := depAtomRe.FindAllString(work, -1)

	// Deduplicate preserving sort order
	seen := make(map[string]bool)
	var atoms []string
	for _, raw := range matches {
		atom := strings.TrimLeft(raw, "<>=")
		atom = stripVersion(atom)
		if atom != "" && !seen[atom] {
			seen[atom] = true
			atoms = append(atoms, atom)
		}
	}

	sort.Strings(atoms)
	return atoms
}

// stripVersion removes the version suffix and slot from a package atom.
// First strips slot suffixes (:slot), then strips a trailing "-<version>"
// (a hyphen followed by a digit, then non-slash chars to end of string).
func stripVersion(atom string) string {
	// Strip slot suffixes first
	if idx := strings.Index(atom, ":"); idx >= 0 {
		atom = atom[:idx]
	}
	// Strip trailing version: -digit...
	return depVerRe.ReplaceAllString(atom, "")
}

// ScanDependencies returns a flat, pre-order slice of rows for every in-overlay
// dependency of eb, recursively. The parent's own row is NOT included. Depth is set
// on every row: the parent is parentDepth, its direct dependencies are parentDepth+1.
// visited is shared across the whole walk and is mutated; a package already present
// in it is skipped entirely (the script's CHECKED_PACKAGES).
func ScanDependencies(
	overlayRoot string,
	eb *atom.ParsedEbuild,
	registry *provider.ProviderRegistry,
	allowPrereleases bool,
	cache *VersionCache,
	parentDepth int,
	visited map[string]bool,
) []UpstreamResult {
	if parentDepth >= maxDepDepth {
		return nil
	}

	// Parse ebuild info to get DEPEND/RDEPEND
	ei, err := ParseEbuildInfo(eb.Path)
	if err != nil {
		return nil
	}

	// Collect atoms from both DEPEND and RDEPEND
	depAtoms := extractAtoms(ei.DEPEND)
	rdepAtoms := extractAtoms(ei.RDEPEND)

	// Deduplicate preserving order
	seen := make(map[string]bool)
	var unique []string
	for _, a := range append(depAtoms, rdepAtoms...) {
		if !seen[a] {
			seen[a] = true
			unique = append(unique, a)
		}
	}

	var results []UpstreamResult

	for _, atomStr := range unique {
		// Parse the dependency atom against the overlay.
		// Only recurse into packages that exist in the overlay.
		parsed, err := atom.ParseAtom(overlayRoot, atomStr)
		if err != nil {
			continue
		}

		atomKey := parsed.Category + "/" + parsed.PN
		if visited[atomKey] {
			continue
		}

		depth := parentDepth + 1

		// Find the actual ebuild for this dependency.
		// Try FindLatest first (highest non-live), then live fallback.
		pe, err := atom.FindLatest(parsed.PkgDir)
		if err != nil {
			if err == atom.ErrNoEbuilds {
				// Fall back to live ebuilds
				all, ferr := atom.ListEbuilds(parsed.PkgDir)
				if ferr != nil || len(all) == 0 {
					continue
				}
				pe = all[len(all)-1]
				if !pe.Live {
					continue
				}
			} else {
				continue
			}
		}

		// Mark as visited before recursing (prevents cycles)
		visited[atomKey] = true

		// Use resolveOne — the same function the top-level walk uses — so
		// the dep's real current version and real SRC_URI-derived provider
		// are used.
		row := resolveOne(overlayRoot, pe, registry, allowPrereleases, cache, depth)
		results = append(results, row)

		// Recurse into this dependency's own deps (pre-order)
		results = append(results, ScanDependencies(overlayRoot, pe, registry, allowPrereleases, cache, depth, visited)...)
	}

	return results
}
