package upstream

import (
	"context"
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

// checkNode is one package in the resolved graph. Discovery is local and
// sequential; resolution is concurrent. Deprecated nodes carry their rename
// metadata and skip provider work.
type checkNode struct {
	Package    string
	Ebuild     *atom.ParsedEbuild
	Depth      int
	Deprecated *DeprecatedInfo
}

// ScanDependencies returns a flat, pre-order slice of rows for every in-overlay
// dependency of eb, recursively. The parent's own row is NOT included. Depth is set
// on every row: the parent is parentDepth, its direct dependencies are parentDepth+1.
// visited is shared across the whole walk and is mutated; a package already present
// in it is skipped entirely (the script's CHECKED_PACKAGES).
//
// It uses the same discovery + resolution path as RunWithTargets, with one worker.
func ScanDependencies(
	overlayRoot string,
	eb *atom.ParsedEbuild,
	registry *provider.ProviderRegistry,
	allowPrereleases bool,
	cache *VersionCache,
	parentDepth int,
	visited map[string]bool,
) []UpstreamResult {
	nodes := discoverDependencyNodes(overlayRoot, eb, parentDepth, visited)
	results, _ := resolveNodes(context.Background(), overlayRoot, nodes, registry, allowPrereleases, cache, 1, nil)
	return results
}

// discoverDependencyNodes walks DEPEND/RDEPEND in pre-order, appending one
// node per in-overlay package and mutating visited. No provider work happens
// here, so the graph is deterministic regardless of worker count.
func discoverDependencyNodes(overlayRoot string, eb *atom.ParsedEbuild, parentDepth int, visited map[string]bool) []checkNode {
	var nodes []checkNode
	appendDependencyNodes(overlayRoot, eb, parentDepth, visited, &nodes)
	return nodes
}

func appendDependencyNodes(overlayRoot string, eb *atom.ParsedEbuild, parentDepth int, visited map[string]bool, out *[]checkNode) {
	if parentDepth >= maxDepDepth {
		return
	}

	// Parse ebuild info to get DEPEND/RDEPEND
	ei, err := ParseEbuildInfo(eb.Path)
	if err != nil {
		return
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

	for _, atomStr := range unique {
		// Parse the dependency atom against the overlay.
		// Only recurse into packages that exist in the overlay.
		parsed, err := atom.ParseAtom(overlayRoot, atomStr)
		if err != nil {
			continue
		}

		// Skip system account categories — the script never recurses
		// into them from a dependency scan.
		if shouldSkip(parsed.Category) {
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

		// Same resolution function as the root walk, deferred to the
		// concurrent phase.
		*out = append(*out, checkNode{Package: atomKey, Ebuild: pe, Depth: depth})

		// Recurse into this dependency's own deps (pre-order)
		appendDependencyNodes(overlayRoot, pe, depth, visited, out)
	}
}
