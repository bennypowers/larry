package upstream

import (
	"fmt"
	"regexp"
	"strings"

	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/providers"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/version"
)

// DepNode represents a node in the dependency tree.
type DepNode struct {
	Atom     string          // e.g., "dev-lang/python"
	Version  string          // current version
	Status   Status  // dependency status
	Level    int             // depth in tree (0 = root)
	Children []DepNode       // sub-dependencies
}

// ScanDependencies scans an ebuild's dependencies and builds a dependency tree.
// It recursively resolves dependencies up to maxDepth levels, using visited set
// to prevent infinite loops on circular references.
func ScanDependencies(ei *ebuild.EbuildInfo, registry *provider.ProviderRegistry, allowPrereleases bool) []DepNode {
	return scanDependencies(ei, 3, registry, allowPrereleases)
}

func scanDependencies(ei *ebuild.EbuildInfo, maxDepth int, registry *provider.ProviderRegistry, allowPrereleases bool) []DepNode {
	if maxDepth <= 0 {
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

	visited := map[string]bool{
		fmt.Sprintf("%s/%s", ei.Category, ei.Pkg): true,
	}

	var nodes []DepNode
	for _, atom := range unique {
		node := buildDepNode(atom, ei.Category, ei.Pkg, registry, 1, maxDepth, allowPrereleases, visited)
		nodes = append(nodes, node)
	}

	return nodes
}

func buildDepNode(atom, ebuildCat, ebuildPkg string, registry *provider.ProviderRegistry,
	level, maxDepth int, allowPrereleases bool, visited map[string]bool,
) DepNode {
	node := DepNode{
		Atom:    atom,
		Version: "",
		Status:  StatusUnknown,
		Level:   level,
	}

	key := atom
	if visited[key] {
		return node
	}
	visited[key] = true

	depEbuild := resolveDepEbuild(atom)
	if depEbuild == nil {
		return node
	}

	node.Version = depEbuild.PVVal

	if isLiveVersion(depEbuild.PVVal) {
		node.Status = StatusLive
		return node
	}

	if registry != nil {
		providerType := detectProviderFromSRCURI(depEbuild.SRC_URI)
		if providerType == "" {
			providerType = string(providers.ResolveProviderFromURL(depEbuild.SRC_URI))
		}

		if providerType == "" || providerType == "unknown" {
			node.Status = StatusUnknown
		} else {
			rid, err := buildRemoteIdForProvider(depEbuild, providerType)
			if err != nil {
				node.Status = StatusUnknown
			} else {
				p, err := registry.Resolve(*rid)
				if err != nil {
					node.Status = StatusUnknown
				} else {
					latestV, err := p.FetchLatestVersion(*rid, allowPrereleases)
					if err != nil {
						node.Status = StatusUnknown
					} else {
						node.Status = compareVersions(depEbuild.PVVal, latestV)
					}
				}
			}
		}
	}

	// Recurse for sub-dependencies
	if level < maxDepth {
		depVisited := make(map[string]bool)
		for k, v := range visited {
			depVisited[k] = v
		}
		node.Children = scanDependencies(depEbuild, maxDepth, registry, allowPrereleases)
	}

	return node
}

// extractAtoms extracts Gentoo package atoms from a dependency string.
// Strips operators (>=, <=, =), version specs, slots, use flags, and disjunctions.
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

	// Split by whitespace and extract atoms
	tokens := strings.Fields(work)

	// Atom pattern: category/package[-version] — captures the full match
	// We strip the version part manually afterwards
	// Note: package name after / must be at least 1 char ([a-z][a-z0-9_.-]*)
	atomRe := regexp.MustCompile(`([a-z][a-z0-9_-]+/[a-z][a-z0-9_.-]*)`)

	var atoms []string
	for _, token := range tokens {
		matches := atomRe.FindAllStringSubmatch(token, -1)
		for _, match := range matches {
			if len(match) >= 2 {
				atom := strings.TrimLeft(match[1], "<>=")
				// Strip version: cut at first hyphen followed by a digit
				atom = stripVersion(atom)
				atoms = append(atoms, atom)
			}
		}
	}

	return atoms
}

// stripVersion removes the version suffix and slot from a package atom.
// e.g., "dev-lang/python-3.9" -> "dev-lang/python"
//       "dev-java/icedtea-web" -> "dev-java/icedtea-web" (no version)
//       "dev-libs/glib-2:0" -> "dev-libs/glib-2" (strip :0 slot only)
// Version-like patterns: -1.0, -3.9, -9999, -100
// Package suffixes: glib-2, icedtea-web (NOT stripped)
func stripVersion(atom string) string {
	// First, strip slot suffixes (:slot or :abi_x86_64)
	if idx := strings.Index(atom, ":"); idx >= 0 {
		atom = atom[:idx]
	}

	// Strip version suffix: hyphen followed by digits (possibly with dots)
	// e.g., "-3.9", "-1.0.0", "-9999", "-100"
	// But NOT single-digit suffixes that are part of the package name
	// e.g., "glib-2" stays as "glib-2"
	for i := len(atom) - 1; i >= 0; i-- {
		if atom[i] == '-' && i+1 < len(atom) {
			rest := atom[i+1:]
			// Check if rest looks like a version (digits, possibly with dots)
			looksLikeVersion := true
			dotCount := 0
			for _, c := range rest {
				if c >= '0' && c <= '9' {
					continue
				} else if c == '.' {
					dotCount++
					continue
				}
				looksLikeVersion = false
				break
			}
			// Only strip if it looks like a version (has digits) and
			// has at least one dot OR is 2+ digits
			if looksLikeVersion && (dotCount > 0 || len(rest) >= 2) {
				return atom[:i]
			}
		}
	}
	return atom
}

// resolveDepEbuild returns a minimal ebuild for a given atom.
func resolveDepEbuild(atom string) *ebuild.EbuildInfo {
	parts := strings.SplitN(atom, "/", 2)
	if len(parts) != 2 {
		return nil
	}
	return &ebuild.EbuildInfo{
		Category: parts[0],
		Pkg:      parts[1],
		PVVal:    "1.0.0",
		PVal:     parts[1] + "-1.0.0",
		DEPEND:   "",
		RDEPEND:  "",
	}
}

// compareVersions compares a current version string against a parsed version.
func compareVersions(current string, latest *version.Version) Status {
	if latest == nil {
		return StatusUnknown
	}
	currentV, err := version.Parse(current)
	if err != nil {
		return StatusUnknown
	}
	switch currentV.Compare(latest) {
	case -1:
		return StatusOutdated
	case 1:
		return StatusAhead
	default:
		return StatusUpToDate
	}
}

// buildRemoteIdForProvider constructs a RemoteId for a given provider type.
func buildRemoteIdForProvider(ei *ebuild.EbuildInfo, providerType string) (*remoteid.RemoteId, error) {
	ridType, err := providerTypeToRemoteId(providerType)
	if err != nil {
		return nil, err
	}
	return &remoteid.RemoteId{
		Type: ridType,
		Name: ei.Pkg,
	}, nil
}

// providerTypeToRemoteId maps a string provider type to RemoteIdType.
func providerTypeToRemoteId(pt string) (remoteid.RemoteIdType, error) {
	switch pt {
	case "github":
		return remoteid.TypeGitHub, nil
	case "gitlab":
		return remoteid.TypeGitLab, nil
	case "pypi":
		return remoteid.TypePyPI, nil
	case "npm":
		return remoteid.TypeNPM, nil
	case "codeberg":
		return remoteid.TypeCodeberg, nil
	case "sourcehut":
		return remoteid.TypeSourceHut, nil
	default:
		return "", fmt.Errorf("unknown provider type: %s", pt)
	}
}

// MergeDepStatuses merges the statuses of a parent with its dependencies.
// If any dependency is outdated, the parent is marked as outdated.
func MergeDepStatuses(parent Status, deps []UpstreamResult) Status {
	if parent != StatusUpToDate {
		return parent
	}
	for _, dep := range deps {
		if dep.Status == StatusOutdated {
			return StatusOutdated
		}
		if dep.Status == StatusAhead {
			return StatusAhead
		}
	}
	return parent
}
