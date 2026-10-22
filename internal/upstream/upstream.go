package upstream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/larryconfig"
	"bennypowers.dev/larry/internal/logger"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/providers"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/version"
)

// PackageTarget represents a single package to check.
type PackageTarget struct {
	Atom     *atom.Atom
	Ebuild   *atom.ParsedEbuild
	LiveOnly bool
}

// discoverTargets walks the overlay and returns one target per package,
// using the same logic as the reference script:
//   - FindLatest returns the highest non-live ebuild (or ErrNoEbuilds if all are live).
//   - On ErrNoEbuilds, fall back to ListEbuilds and pick the live one.
//   - Skip acct-group/* and acct-user/*.
//   - Return results in ascending category/package order.
func discoverTargets(overlayRoot string) ([]*PackageTarget, error) {
	categories, err := os.ReadDir(overlayRoot)
	if err != nil {
		return nil, err
	}

	var targets []*PackageTarget
	for _, cat := range categories {
		if !cat.IsDir() {
			continue
		}
		catName := cat.Name()
		if catName == "acct-group" || catName == "acct-user" {
			continue
		}

		catPath := filepath.Join(overlayRoot, catName)
		pkgs, err := os.ReadDir(catPath)
		if err != nil {
			continue
		}

		for _, pkg := range pkgs {
			if !pkg.IsDir() {
				continue
			}
			pkgDir := filepath.Join(catPath, pkg.Name())

			// Try FindLatest first (returns non-live highest version)
			pe, err := atom.FindLatest(pkgDir)
			if err == nil {
				targets = append(targets, &PackageTarget{
					Atom: &atom.Atom{
						Category: pe.Category,
						PN:       pe.PN,
						PkgDir:   pkgDir,
					},
					Ebuild:   pe,
					LiveOnly: false,
				})
				continue
			}

			// On ErrNoEbuilds, fall back to live-only
			if errors.Is(err, atom.ErrNoEbuilds) {
				all, err := atom.ListEbuilds(pkgDir)
				if err != nil || len(all) == 0 {
					continue
				}
				// Take the last entry (ListEbuilds sorts live last)
				pe = all[len(all)-1]
				if !pe.Live {
					// All were live but FindLatest succeeded? shouldn't happen.
					continue
				}
				targets = append(targets, &PackageTarget{
					Atom: &atom.Atom{
						Category: pe.Category,
						PN:       pe.PN,
						PkgDir:   pkgDir,
					},
					Ebuild:   pe,
					LiveOnly: true,
				})
				continue
			}

			// Any other error: skip
		}
	}

	// Sort by category/package for deterministic output
	sort.Slice(targets, func(i, j int) bool {
		a := targets[i].Atom.Category + "/" + targets[i].Atom.PN
		b := targets[j].Atom.Category + "/" + targets[j].Atom.PN
		return a < b
	})

	return targets, nil
}

// ParseEbuildInfo parses an ebuild file and returns structured information.
// It uses the shared ebuild parser for accurate multi-line value extraction.
func ParseEbuildInfo(path string) (*ebuild.EbuildInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)
	info := &ebuild.EbuildInfo{}

	// Extract PV
	if pv := extractVar(content, "PV"); pv != "" {
		info.PVVal = pv
	} else {
		// Derive PV from filename via atom parser
		pe, err := atom.ParseEbuildPath(path)
		if err == nil {
			info.PVVal = pe.PV
		}
	}

	// Extract PN
	if pn := extractVar(content, "PN"); pn != "" {
		info.Pkg = pn
	} else {
		pe, err := atom.ParseEbuildPath(path)
		if err == nil {
			info.Pkg = pe.PN
		}
	}

	// Extract SRC_URI
	info.SRC_URI = extractVar(content, "SRC_URI")

	// Extract HOMEPAGE
	info.HOMEPAGE = extractVar(content, "HOMEPAGE")

	// Extract DEPEND / RDEPEND / BDEPEND (plan 20a: multi-line aware).
	info.DEPEND = extractVar(content, "DEPEND")
	info.RDEPEND = extractVar(content, "RDEPEND")
	info.BDEPEND = extractVar(content, "BDEPEND")

	// Expand shell variables in DEPEND/RDEPEND/BDEPEND using a minimal var map.
	// PN, PV, P are always available.  Also extract any custom variable
	// assignments (MY_P=..., MY_PN=...) from the raw ebuild content so that
	// e.g. RDEPEND="${DEPEND}\nbar" expands correctly when DEPEND was defined
	// earlier in the file.
	// Only expand the DEPEND/RDEPEND/BDEPEND fields themselves — not SRC_URI,
	// which has its own expansion path (ebuild.ExpandVariables + BuildVariableMap).
	varVars := ebuild.VariableMap{
		"PN": info.PN(),
		"PV": info.PV(),
		"P":  info.PN() + "-" + info.PV(),
	}
	// Collect all variable assignments from the full ebuild content so that
	// DEPEND, RDEPEND and BDEPEND can reference each other (e.g. RDEPEND="${DEPEND}").
	for _, m := range npmAssignRe.FindAllStringSubmatch(content, -1) {
		varVars[m[1]] = m[2]
	}
	if info.DEPEND != "" {
		info.DEPEND = ebuild.ExpandVariables(info.DEPEND, varVars)
	}
	if info.RDEPEND != "" {
		info.RDEPEND = ebuild.ExpandVariables(info.RDEPEND, varVars)
	}
	if info.BDEPEND != "" {
		info.BDEPEND = ebuild.ExpandVariables(info.BDEPEND, varVars)
	}

	// Extract INHERIT
	info.Inherit = ebuild.ParseInherit(content)

	// Store raw inherit line for metadata detection
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inherit ") {
			info.InheritRaw = line[len("inherit "):]
			break
		}
	}

	// Store raw content for metadata detection
	info.Raw = content

	// Derive category from directory
	info.Path = path
	pe, _ := atom.ParseEbuildPath(path)
	if pe != nil {
		info.Category = pe.Category
	}

	return info, nil
}

// extractVar extracts a variable value from ebuild content.
// Only matches variables at the start of a line (after optional whitespace).
func extractVar(content, name string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, name+"=") {
			continue
		}
		valuePart := strings.TrimSpace(trimmed[len(name)+1:])
		if len(valuePart) >= 1 && (valuePart[0] == '"' || valuePart[0] == '\'') {
			quote := valuePart[0]
			end := strings.Index(valuePart[1:], string(quote))
			if end >= 0 {
				return valuePart[1 : 1+end]
			}
			// Multi-line value: collect lines after the opening-quote line until closing quote
			var parts []string
			parts = append(parts, valuePart[1:])
			for j := i + 1; j < len(lines); j++ {
				closeIdx := strings.Index(lines[j], string(quote))
				if closeIdx >= 0 {
					parts = append(parts, lines[j][:closeIdx])
					return strings.Join(parts, "\n")
				}
				parts = append(parts, lines[j])
			}
			return strings.Join(parts, "\n")
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

// expandVar expands shell variable patterns in PYPI_PN/MY_PN values.
// Handles patterns like ${PN//-/_} (replace all dashes with underscores),
// ${PN/-/_} (replace first dash with underscore), and ${PN} (literal).
func expandVar(raw, pn string) string {
	switch raw {
	case "${PN//-/_}", "$PN//-/_":
		return strings.ReplaceAll(pn, "-", "_")
	case "${PN/-/_}", "$PN/-/_":
		return strings.Replace(pn, "-", "_", 1)
	case "${PN}", "$PN":
		return pn
	}
	// Return raw value as-is if no known pattern
	return raw
}

// shouldSkip checks if a category should be skipped.
func shouldSkip(category string) bool {
	return category == "acct-group" || category == "acct-user"
}

// detectProviderFromSRCURI detects the provider type from SRC_URI and HOMEPAGE.
// It handles both mirror:// URIs and direct URLs (https://github.com/... etc).
func detectProviderFromSRCURI(srcURI string, homepage string) string {
	// Check SRC_URI first, then HOMEPAGE
	for _, text := range []string{srcURI, homepage} {
		switch {
		case strings.Contains(text, "mirror://pypi/"):
			return "pypi"
		case strings.Contains(text, "files.pythonhosted.org/"):
			return "pypi"
		case strings.Contains(text, "mirror://npm/"), strings.Contains(text, "registry.npmjs.org/"):
			return "npm"
		case strings.Contains(text, "mirror://github/") || strings.Contains(text, "mirror://github.com/"):
			return "github"
		case strings.Contains(text, "mirror://gitlab/") || strings.Contains(text, "mirror://gitlab.com/"):
			return "gitlab"
		case strings.Contains(text, "mirror://codeberg/"):
			return "codeberg"
		case strings.Contains(text, "mirror://sourcehut/"):
			return "sourcehut"
		case strings.Contains(text, "github.com/"):
			return "github"
		case regexp.MustCompile(`https?://gitlab\.[A-Za-z0-9.-]+/`).MatchString(text):
			return "gitlab"
		case strings.Contains(text, "codeberg.org/"):
			return "codeberg"
		case strings.Contains(text, "sr.ht") || strings.Contains(text, "sourcehut"):
			return "sourcehut"
		}
	}
	return ""
}

// detectProviderFromMetadata detects the provider type from ebuild metadata
// (PYPI_* variables, inherit directives). This is used as a fallback when
// SRC_URI/HOMEPAGE detection is ambiguous (e.g., PyPI packages hosted on GitHub).
// inherit is the parsed inherit list (works for both file parsing and ebuild info).
// content is the raw ebuild file content (may be empty for ebuild info output).
func detectProviderFromMetadata(inherit []string, content string) string {
	// Check for PYPI_* variables (indicates PyPI package)
	if content != "" {
		pypiVars := []string{"PYPI_SOURCE_URI", "PYPI_PN", "PYPI_P", "PYPI_S"}
		for _, v := range pypiVars {
			if strings.Contains(content, v+"=") {
				return "pypi"
			}
		}
		// Check for NPM_* variables (indicates npm package)
		npmVars := []string{"NPM_PN", "NPM_P", "NPM_PKGNAME"}
		for _, v := range npmVars {
			if strings.Contains(content, v+"=") {
				return "npm"
			}
		}
		// Check for Go-related variables (indicates Go module)
		goVars := []string{"GO_MODULE_PATH", "GO_PROJECT"}
		for _, v := range goVars {
			if strings.Contains(content, v+"=") {
				return "github"
			}
		}
	}
	// Check inherit for PyPI-specific eclasses (NOT general Python eclasses like
	// python-single-r1 or distutils-r1, which are used for local/Codeberg/GitHub
	// Python packages too). Only "pypi" eclass specifically indicates PyPI source.
	pypiEclasses := []string{"pypi"}
	for _, eclass := range inherit {
		for _, pe := range pypiEclasses {
			if eclass == pe {
				return "pypi"
			}
		}
	}
	return ""
}

// ResolveProviderType resolves the final provider type, preferring metadata.xml
// <remote-id> over URL heuristics. When ebuildPath is non-empty, the function
// first looks for metadata.xml in the ebuild's directory tree and returns the
// type from the first <remote-id> entry whose type is one of the known forge
// types (github, gitlab, pypi, npm, codeberg, sourcehut).  When no usable
// remote-id is found the function falls back to URL scanning and eclass
// heuristics.
func ResolveProviderType(ebuildPath, srcURI, homepage string, inherit []string, content string) string {
	// 1. metadata.xml <remote-id> wins, UNLESS inherit says pypi or npm
	// (the remote-id may point to a GitHub mirror or issue tracker while
	// the actual source tarball comes from PyPI/npm).
	var metaType string
	if ebuildPath != "" {
		if rids, err := remoteid.ResolveRemoteId(ebuildPath); err == nil && len(rids) > 0 {
			for _, rid := range rids {
				t := string(rid.Type)
				if t == "github" || t == "gitlab" || t == "pypi" || t == "npm" || t == "codeberg" || t == "sourcehut" {
					metaType = t
					break
				}
			}
		}
	}
	// 2. URL detection.
	provider := detectProviderFromSRCURI(srcURI, homepage)
	if provider == "" {
		// No URL match — fall back to metadata detection.
		if metaType == "" {
			metaType = detectProviderFromMetadata(inherit, content)
		}
		return metaType
	}
	// 3. pypi/npm inherit always wins over github/gitlab/codeberg metadata
	// or URL (the source tarball comes from PyPI/npm, not from the forge).
	if metaType == "pypi" || metaType == "npm" {
		return metaType
	}
	// 4. If URL says GitHub/npm but metadata says PyPI, prefer PyPI.
	if provider == "github" || provider == "gitlab" || provider == "codeberg" {
		meta := detectProviderFromMetadata(inherit, content)
		if meta == "pypi" || meta == "npm" {
			return meta
		}
	}
	return provider
}

// mapTypeToRemoteIdType maps an ebuild package type to a RemoteIdType.
func mapTypeToRemoteIdType(pkgType string) remoteid.RemoteIdType {
	switch pkgType {
	case "pypi":
		return remoteid.TypePyPI
	case "npm":
		return remoteid.TypeNPM
	case "github":
		return remoteid.TypeGitHub
	case "gitlab":
		return remoteid.TypeGitLab
	case "codeberg":
		return remoteid.TypeCodeberg
	case "sourcehut":
		return remoteid.TypeSourceHut
	default:
		return remoteid.TypeUnknown
	}
}

// ResolveRemoteId constructs a RemoteId from ebuild info and resolved provider type.
// Handles all provider-specific extraction: GitHub owner/repo, PyPI PN expansion,
// NPM scoped packages, etc.
//
// When ebuildPath is non-empty and metadata.xml contains a <remote-id> matching
// providerType, that entry is returned directly.  Otherwise URL heuristics
// (SRC_URI / HOMEPAGE) are used.
func ResolveRemoteId(ei *ebuild.EbuildInfo, providerType string) (*remoteid.RemoteId, error) {
	// 1. Prefer metadata.xml <remote-id> when it matches providerType.
	if ei.Path != "" {
		if rids, err := remoteid.ResolveRemoteId(ei.Path); err == nil && len(rids) > 0 {
			for _, rid := range rids {
				if string(rid.Type) == providerType {
					return rid, nil
				}
			}
		}
	}

	switch providerType {
	case "pypi":
		// Use PYPI_PN if present, otherwise MY_PN, otherwise Gentoo PN
		pypiName := ei.Pkg
		if raw := extractVar(ei.Raw, "PYPI_PN"); raw != "" {
			pypiName = expandVar(raw, ei.Pkg)
		} else if raw := extractVar(ei.Raw, "MY_PN"); raw != "" {
			pypiName = expandVar(raw, ei.Pkg)
		}
		return &remoteid.RemoteId{
			Type: remoteid.TypePyPI,
			Name: pypiName,
		}, nil
	case "npm":
		// Extract npm package name from SRC_URI mirror URL if available
		name := extractNpmNameFromSRCURI(ei.SRC_URI, ei.Raw, ei.Pkg)
		if name == "" {
			// Fall back to MY_NPM_NAME, NPM_PN, or Gentoo PN
			if pn := extractVar(ei.Raw, "MY_NPM_NAME"); pn != "" {
				name = pn
			} else if pn := extractVar(ei.Raw, "NPM_PN"); pn != "" {
				name = pn
			} else {
				name = ei.Pkg
			}
		}
		return &remoteid.RemoteId{
			Type: remoteid.TypeNPM,
			Name: name,
		}, nil
	case "github":
		if r := remoteid.ExtractFromURLs(remoteid.TypeGitHub, ei.SRC_URI, ei.HOMEPAGE); r != nil {
			return r, nil
		}
		return nil, nil
	case "gitlab":
		if r := remoteid.ExtractFromURLs(remoteid.TypeGitLab, ei.SRC_URI, ei.HOMEPAGE); r != nil {
			return r, nil
		}
		return nil, nil
	case "codeberg":
		if r := remoteid.ExtractFromURLs(remoteid.TypeCodeberg, ei.SRC_URI, ei.HOMEPAGE); r != nil {
			return r, nil
		}
		return nil, nil
	case "sourcehut":
		if r := remoteid.ExtractFromURLs(remoteid.TypeSourceHut, ei.SRC_URI, ei.HOMEPAGE); r != nil {
			return r, nil
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown provider type: %s", providerType)
	}
}

// resolveSourceLabel builds a short source label like "GitHub:owner/repo" or "PyPI:name".
// Uses the Label* constants so the producer and glyph matcher cannot drift apart.
func resolveSourceLabel(rid *remoteid.RemoteId) string {
	switch rid.Type {
	case remoteid.TypePyPI:
		return LabelPyPI + rid.Name
	case remoteid.TypeNPM:
		return LabelNPM + rid.Name
	case remoteid.TypeGitHub:
		if rid.Owner != "" && rid.Repo != "" {
			return LabelGitHub + rid.Owner + "/" + rid.Repo
		}
		return LabelGitHub
	case remoteid.TypeGitLab:
		if rid.Host != "" && rid.Path != "" {
			return LabelGitLab + rid.Host + "/" + rid.Path
		}
		return LabelGitLab
	case remoteid.TypeCodeberg:
		if rid.Owner != "" && rid.Repo != "" {
			return LabelCodeberg + rid.Owner + "/" + rid.Repo
		}
		return LabelCodeberg
	case remoteid.TypeSourceHut:
		if rid.Host != "" && rid.Path != "" {
			return LabelSourceHut + rid.Host + "/" + rid.Path
		}
		return LabelSourceHut
	default:
		return "unknown"
	}
}

// resolveSourceURL builds a source URL from a RemoteId.
func resolveSourceURL(rid *remoteid.RemoteId) string {
	switch rid.Type {
	case remoteid.TypePyPI:
		return fmt.Sprintf("https://pypi.org/project/%s/", rid.Name)
	case remoteid.TypeNPM:
		return fmt.Sprintf("https://www.npmjs.com/package/%s", rid.Name)
	case remoteid.TypeGitHub:
		return fmt.Sprintf("https://github.com/%s/%s", rid.Owner, rid.Repo)
	case remoteid.TypeGitLab:
		return fmt.Sprintf("https://%s/%s", rid.Host, rid.Path)
	case remoteid.TypeCodeberg:
		return fmt.Sprintf("https://codeberg.org/%s/%s", rid.Owner, rid.Repo)
	case remoteid.TypeSourceHut:
		return fmt.Sprintf("https://%s/%s", rid.Host, rid.Path)
	default:
		return ""
	}
}

// resolveSourceInfo computes SourceInfo (label, URL, glyph) from a RemoteId.
func resolveSourceInfo(rid *remoteid.RemoteId) SourceInfo {
	info := SourceInfo{Label: resolveSourceLabel(rid), URL: resolveSourceURL(rid)}
	// Resolve the glyph — SourceGlyph in output.go uses the label constants.
	info.Glyph = SourceGlyph(info)
	return info
}

// buildResult builds an UpstreamResult from version comparison.
// isVersionLike mirrors the script's `string match -qr '^\d+[\ .\d]*'` (L1129).
var versionLikeRe = regexp.MustCompile(`^[0-9][0-9.]*`)

// isVersionLike reports whether s looks like a version number (starts with a digit
// optionally followed by more digits and dots).
func isVersionLike(s string) bool { return versionLikeRe.MatchString(s) }

// deriveTagStatus implements the script's `fetch_result == 2` branch.
// currentPV is the ebuild's PV. tag is the (optionally template-expanded) tag string.
// tagDate is an optional YYYYMMDD date from the commit or tag name.
// Returns the status and the Latest cell text.
//
// Both tag dates are "YYYYMMDD" strings, so lexicographic comparison is correct
// and no strconv conversion is needed.
func deriveTagStatus(currentPV string, tag string, tagDate string) (Status, string) {
	// Strip leading v/V only when followed by a digit (script L1471: ^[vV](?=\d)).
	tagName := tag
	if len(tagName) > 1 && (tagName[0] == 'v' || tagName[0] == 'V') && tagName[1] >= '0' && tagName[1] <= '9' {
		tagName = tagName[1:]
	}
	latest := tagName

	curDate, curOK := version.ExtractDate(currentPV)

	// Both sides version-like: compare dates if available, fall back to version compare.
	if isVersionLike(currentPV) && isVersionLike(tagName) {
		if curOK && tagDate != "" {
			// YYYYMMDD comparison is lexicographic.
			switch {
			case tagDate > curDate:
				return StatusNewerTag, latest
			case tagDate == curDate:
				return StatusUpToDate, latest
			default:
				return StatusAhead, latest
			}
		}
		// No dates available — fall back to version comparison.
		switch version.Compare(currentPV, tagName) {
		case -1:
			return StatusOutdated, latest
		case 0:
			return StatusUpToDate, latest
		default:
			return StatusAhead, latest
		}
	}

	// At least one side is not version-like → CHECK-TAG.
	return StatusCheckTag, latest
}

func buildResult(ei *ebuild.EbuildInfo, latestV, currentV *version.Version, ridType remoteid.RemoteIdType) *UpstreamResult {
	cmp := latestV.Compare(currentV)
	var status Status

	switch cmp {
	case 1:
		status = StatusOutdated // upstream is newer
	case 0:
		status = StatusUpToDate
	case -1:
		status = StatusAhead // current is newer
	}

	return &UpstreamResult{
		Package: fmt.Sprintf("%s/%s", ei.Category, ei.Pkg),
		Current: ei.PVVal,
		Latest:  latestV.Format(),
		Status:  status,
	}
}

// resolveOne turns one package's ebuild into exactly one UpstreamResult row.
// It is the single source of truth for provider lookup, caching, error rows and
// status derivation, used by both the top-level walk (depth 0) and the dependency scanner.
//
// The deprecated-package branch stays in the caller (walkAndCheckTargets) because it
// is keyed off the deprecated map which the dependency scanner does not consult.
//
// Depth is the recursion depth: the root package is 0, its direct dependencies are 1, etc.
func resolveOne(
	overlayRoot string,
	eb *atom.ParsedEbuild,
	registry *provider.ProviderRegistry,
	allowPrereleases bool,
	cache *VersionCache,
	depth int,
) UpstreamResult {
	einfo, err := ParseEbuildInfo(eb.Path)
	if err != nil {
		return UpstreamResult{
			Package: eb.Category + "/" + eb.PN,
			Current: eb.PVR,
			Latest:  "",
			Status:  StatusUnknown,
			Error:   err.Error(),
			Depth:   depth,
		}
	}

	// Load per-package .larry/config.yaml upstream rules. Unconditionally set
	// ResolveOptions on any GitHubProvider so that state does not leak
	// between packages during a multi-package walk.
	var resOpts *larryconfig.ResolveOptions
	var cfgErr error
	if eb.Path != "" {
		cfg, err := larryconfig.LoadConfig(filepath.Dir(eb.Path))
		if err != nil {
			cfgErr = err
		} else if cfg.Upstream != nil {
			opts, err := cfg.Upstream.Compile(einfo.PVVal)
			if err != nil {
				cfgErr = fmt.Errorf("invalid %s: %w", larryconfig.ConfigFile, err)
			} else {
				resOpts = opts
			}
		}
	}
	if cfgErr != nil {
		return UpstreamResult{
			Package: eb.Category + "/" + eb.PN,
			Current: eb.PVR,
			Latest:  "",
			Status:  StatusUnknown,
			Error:   cfgErr.Error(),
			Depth:   depth,
		}
	}

	// Vendor-only packages opt out of upstream checks. Report MANUAL rather
	// than UNKNOWN, and link to HOMEPAGE as the manual source.
	if resOpts != nil && !resOpts.Check {
		return UpstreamResult{
			Package: einfo.Category + "/" + einfo.Pkg,
			Current: eb.PVR,
			Latest:  "",
			Status:  StatusManual,
			Source:  SourceInfo{Label: "vendor", URL: einfo.HOMEPAGE},
			Depth:   depth,
		}
	}

	// Live-only handling
	if eb.Live {
		providerType := ResolveProviderType(einfo.Path, einfo.SRC_URI, einfo.HOMEPAGE, einfo.Inherit, einfo.Raw)
		if providerType == "" {
			providerType = string(providers.ResolveProviderFromURL(einfo.SRC_URI))
		}
		logger.Tracef("upstream %s/%s (live): provider=%s", einfo.Category, einfo.Pkg, providerType)
		if providerType != "" && providerType != "unknown" {
			rid, err := ResolveRemoteId(einfo, providerType)
			if err == nil && rid != nil {
				logger.Tracef("upstream %s/%s: remote-id=%s", einfo.Category, einfo.Pkg, rid.String())
				cacheKey := MakeKey(einfo.Category, einfo.Pkg, string(rid.Type), einfo.PVVal)
				var latestV *version.Version

				if cachedVer, ok := cache.Get(cacheKey); ok {
					latestV, _ = version.Parse(cachedVer)
				} else if registry != nil {
					ridType := mapTypeToRemoteIdType(providerType)
					p, err := registry.Resolve(remoteid.RemoteId{Type: ridType, Name: rid.Name, Owner: rid.Owner, Repo: rid.Repo})
					if err == nil {
						// Wire per-package ResolveOptions onto any GitHubProvider.
						if ghProv, ok := p.(*providers.GitHubProvider); ok {
							ghProv.ResolveOptions = resOpts
						}
						latestV, err = p.FetchLatestVersion(*rid, allowPrereleases)
						if err == nil && latestV != nil {
							cache.Set(cacheKey, latestV.Format())
						}
					}
				}

				if latestV != nil {
					return UpstreamResult{
						Package: einfo.Category + "/" + einfo.Pkg,
						Current: "9999 (live)",
						Latest:  latestV.Format(),
						Status:  StatusHasRelease,
						Source:  resolveSourceInfo(rid),
						Depth:   depth,
					}
				}
			}
		}
		// No upstream release found
		return UpstreamResult{
			Package: einfo.Category + "/" + einfo.Pkg,
			Current: "9999 (live)",
			Latest:  "",
			Status:  StatusLive,
			Depth:   depth,
		}
	}

	// Normal (versioned) handling
	providerType := ResolveProviderType(einfo.Path, einfo.SRC_URI, einfo.HOMEPAGE, einfo.Inherit, einfo.Raw)
	if providerType == "" {
		providerType = string(providers.ResolveProviderFromURL(einfo.SRC_URI))
	}
	logger.Tracef("upstream %s/%s: provider=%s", einfo.Category, einfo.Pkg, providerType)

	if providerType == "" || providerType == "unknown" {
		return UpstreamResult{
			Package: einfo.Category + "/" + einfo.Pkg,
			Current: eb.PVR,
			Latest:  "",
			Status:  StatusUnknown,
			Error:   "no provider match",
			Depth:   depth,
		}
	}

	rid, err := ResolveRemoteId(einfo, providerType)
	if err != nil || rid == nil {
		return UpstreamResult{
			Package: einfo.Category + "/" + einfo.Pkg,
			Current: eb.PVR,
			Latest:  "",
			Status:  StatusUnknown,
			Error:   "no remote-id found",
			Depth:   depth,
		}
	}
	logger.Tracef("upstream %s/%s: remote-id=%s", einfo.Category, einfo.Pkg, rid.String())

	cacheKey := MakeKey(einfo.Category, einfo.Pkg, string(rid.Type), einfo.PVVal)
	var latestV *version.Version
	var sourceURL, sourceLabel string

	if cachedVer, ok := cache.Get(cacheKey); ok {
		latestV, _ = version.Parse(cachedVer)
		sourceURL = resolveSourceURL(rid)
		sourceLabel = resolveSourceLabel(rid)
	} else if registry != nil {
		ridType := mapTypeToRemoteIdType(providerType)
		p, err := registry.Resolve(remoteid.RemoteId{Type: ridType, Name: rid.Name, Owner: rid.Owner, Repo: rid.Repo})
		if err != nil {
			return UpstreamResult{
				Package: einfo.Category + "/" + einfo.Pkg,
				Current: eb.PVR,
				Latest:  "",
				Status:  StatusUnknown,
				Error:   fmt.Sprintf("no provider for %s", providerType),
				Depth:   depth,
			}
		}

		// Wire per-package ResolveOptions onto any GitHubProvider before
		// resolution so that ignore_tags / strip_prefixes apply. UNCONDITIONAL
		// set (even if resOpts is nil) to prevent state leaking from a
		// previously resolved package.
		if ghProv, ok := p.(*providers.GitHubProvider); ok {
			ghProv.ResolveOptions = resOpts
		}

		// Try ResolverWithTags first (e.g. GitHubProvider — plan 17b),
		// fall back to plain FetchLatestVersion for other providers.
		if rt, ok := p.(provider.ResolverWithTags); ok {
			res, rerr := rt.FetchResolution(*rid, allowPrereleases)
			if rerr == nil && res != nil && res.ByDate {
				// Apply tag_match captures + version_template for date-based
				// resolution (tier 4), the only path that bypasses tryParseVersion.
				tagForStatus := res.Tag
				if resOpts != nil && resOpts.TagMatchRe != nil {
					tagCaps := larryconfig.ExtractCaptures(resOpts.TagMatchRe, res.Tag)
					allVars := make(map[string]string)
					for k, v := range resOpts.PVCaptures {
						allVars[k] = v
					}
					for k, v := range tagCaps {
						allVars[k] = v
					}
					if resOpts.VersionTemplate != "" {
						tagForStatus = larryconfig.ExpandTemplate(resOpts.VersionTemplate, allVars)
					}
				}
				status, latest := deriveTagStatus(einfo.PVVal, tagForStatus, res.TagDate)
				return UpstreamResult{
					Package: einfo.Category + "/" + einfo.Pkg,
					Current: einfo.PVVal,
					Latest:  latest,
					Status:  status,
					Source:  resolveSourceInfo(rid),
					Depth:   depth,
				}
			}
			if rerr == nil && res != nil && res.Version != "" {
				latestV, err = version.Parse(res.Version)
			} else {
				latestV, err = p.FetchLatestVersion(*rid, allowPrereleases)
			}
		} else {
			latestV, err = p.FetchLatestVersion(*rid, allowPrereleases)
		}
		if err != nil {
			// Provider resolved but version fetch failed — show source, not "unknown".
			return UpstreamResult{
				Package: einfo.Category + "/" + einfo.Pkg,
				Current: eb.PVR,
				Latest:  "",
				Status:  StatusUnknown,
				Source:  resolveSourceInfo(rid),
				Error:   err.Error(),
				Depth:   depth,
			}
		}

		cache.Set(cacheKey, latestV.Format())
		sourceURL = resolveSourceURL(rid)
		sourceLabel = resolveSourceLabel(rid)
	} else {
		latestV, sourceURL = resolveVersionFromSRCURI(einfo, providerType, rid)
		sourceLabel = resolveSourceLabel(rid)
	}

	if latestV == nil {
		return UpstreamResult{
			Package: einfo.Category + "/" + einfo.Pkg,
			Current: eb.PVR,
			Latest:  "",
			Status:  StatusUnknown,
			Error:   "could not resolve upstream version",
			Depth:   depth,
		}
	}

	currentV, _ := version.Parse(einfo.PVVal)
	result := buildResult(einfo, latestV, currentV, rid.Type)
	result.Source = SourceInfo{Label: sourceLabel, URL: sourceURL, Glyph: SourceGlyph(SourceInfo{Label: sourceLabel})}
	result.Depth = depth
	return *result
}

// resolveVersionFromSRCURI tries to resolve a version directly from SRC_URI
// when no registry is available.
func resolveVersionFromSRCURI(ei *ebuild.EbuildInfo, providerType string, rid *remoteid.RemoteId) (*version.Version, string) {
	switch providerType {
	case "pypi":
		// Can't resolve version from SRC_URI alone without HTTP
		return nil, ""
	case "npm":
		return nil, ""
	default:
		return nil, ""
	}
}

// Progress reports root-package progress. Dependency rows are part of the
// work for a root but do not increment Done or Total.
type Progress struct {
	Package string
	Done    int
	Total   int
}

// RunOptions configures an upstream walk.
type RunOptions struct {
	Context          context.Context
	OverlayDir       string
	Registry         *provider.ProviderRegistry
	Targets          []*atom.Atom
	AllowPrereleases bool
	Fresh            bool
	CacheDir         string         // empty -> DefaultCachePath()
	Progress         func(Progress) // nil-safe
}

// RunWithTargets walks the overlay and checks specific targets (or all if nil).
// Deprecated and renamed packages from profiles/package.deprecated and profiles/updates/* are skipped.
func RunWithTargets(opts RunOptions) ([]UpstreamResult, error) {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = DefaultCachePath()
	}
	cache := NewVersionCacheWithPath(cacheDir, 6*time.Hour)

	if opts.Fresh {
		_ = cache.Clear()
	}

	// Load deprecated/renamed package list
	deprecated := LoadDeprecatedPackages(opts.OverlayDir)

	return walkAndCheckTargets(ctx, opts, deprecated, cache)
}

// walkAndCheckTargets processes a list of PackageTarget and returns results.
// Deprecated and renamed packages are skipped.
// When TargetAtoms is non-nil, only those atoms are checked.
func walkAndCheckTargets(ctx context.Context, opts RunOptions, deprecated map[string]DeprecatedInfo, cache *VersionCache) ([]UpstreamResult, error) {
	registry := opts.Registry
	allowPrereleases := opts.AllowPrereleases
	targetAtoms := opts.Targets

	targets, err := discoverTargets(opts.OverlayDir)
	if err != nil {
		return nil, err
	}

	// Build a set of allowed atoms for filtering
	var allowed map[string]bool
	if targetAtoms != nil {
		allowed = make(map[string]bool)
		for _, a := range targetAtoms {
			allowed[a.Category+"/"+a.PN] = true
		}
	}

	// Select roots first so progress totals describe the requested set, not
	// every package in the overlay.
	roots := make([]*PackageTarget, 0, len(targets))
	for _, target := range targets {
		key := target.Atom.Category + "/" + target.Atom.PN
		if allowed != nil && !allowed[key] {
			continue
		}
		roots = append(roots, target)
	}

	total := len(roots)
	emit := func(p Progress) {
		if opts.Progress != nil {
			opts.Progress(p)
		}
	}

	var results []UpstreamResult
	// Single visited map shared across the whole walk (script's CHECKED_PACKAGES).
	// A package already emitted at any depth is never emitted again.
	visited := make(map[string]bool)

	for i, target := range roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		atomKey := target.Atom.Category + "/" + target.Atom.PN
		emit(Progress{Package: atomKey, Done: i, Total: total})

		// Skip deprecated/renamed packages
		if dep, ok := deprecated[atomKey]; ok {
			results = append(results, UpstreamResult{
				Package: atomKey,
				Current: target.Ebuild.PVR,
				Latest:  "",
				Status:  StatusDeprecated,
				Source:  SourceInfo{Label: dep.Source},
				Target:  dep.Target,
			})
			visited[atomKey] = true
			emit(Progress{Package: atomKey, Done: i + 1, Total: total})
			continue
		}

		// Skip if already emitted as a dependency of an earlier package.
		if visited[atomKey] {
			emit(Progress{Package: atomKey, Done: i + 1, Total: total})
			continue
		}

		row := resolveOne(opts.OverlayDir, target.Ebuild, registry, allowPrereleases, cache, 0)
		results = append(results, row)
		visited[atomKey] = true

		// Scan in-overlay dependencies (pre-order, depth 0→3, single visited map).
		results = append(results, ScanDependencies(opts.OverlayDir, target.Ebuild, registry, allowPrereleases, cache, 0, visited)...)
		emit(Progress{Package: atomKey, Done: i + 1, Total: total})
	}

	return results, nil
}

// npmAssignRe mirrors the script's `grep -E '^[A-Z_]+="[^"]*"'`, widened to
// allow digits in the variable name (MY_NPM_2 etc).
var npmAssignRe = regexp.MustCompile(`(?m)^([A-Z][A-Z0-9_]*)="([^"]*)"$`)

// npmVarMap builds a variable map from the raw ebuild content by scanning
// every `^[A-Z_]+="…"` assignment — matching the script's behaviour.
// Core vars are added last so they always win.
//
// TODO(plan 20): replace with the shared whole-ebuild variable map from
// internal/ebuild.ExpandVariables + ebuild.BuildVariableMap.
func npmVarMap(ei *ebuild.EbuildInfo) ebuild.VariableMap {
	vars := make(ebuild.VariableMap)
	for _, m := range npmAssignRe.FindAllStringSubmatch(ei.Raw, -1) {
		vars[m[1]] = m[2]
	}
	// Core vars last so they always win, matching the script's ordering.
	vars["PN"] = ei.Pkg
	vars["PV"] = ei.PVVal
	vars["P"] = ei.Pkg + "-" + ei.PVVal
	return vars
}

// extractNpmNameFromSRCURI extracts the npm package name from SRC_URI,
// expanding every `^[A-Z_]+="…"` assignment in the raw ebuild (the script
// scans all variable declarations, not just the ebuild's SRC_URI field).
//
// Algorithm: take the first line containing an npm registry URL (the legacy
// mirror://npm/ scheme or a direct registry.npmjs.org URL), expand variables,
// strip everything up to and including the registry prefix, then drop
// everything from /-/ onward.
//
// Scoped URL with no /-/: we deliberately keep only the @scope/name prefix
// rather than the script's output which includes the full path
// (@acme/widget → @acme/widget, not @acme/widget/widget-1.0.0.tgz).
//
// e.g., "mirror://npm/@scope/pkg/-/pkg-1.0.tgz" -> "@scope/pkg"
// e.g., "mirror://npm/pkg/-/pkg-1.0.tgz" -> "pkg"
// e.g., "https://registry.npmjs.org/@scope/pkg/-/pkg-1.0.tgz" -> "@scope/pkg"
// e.g., "mirror://npm/${MY_SCOPE}/${PN}/-/${P}.tgz" -> "@scope/pkg"
func extractNpmNameFromSRCURI(srcURI, fullContent, pn string) string {
	lines := strings.Split(fullContent, "\n")
	for _, line := range lines {
		if !strings.Contains(line, "mirror://npm/") && !strings.Contains(line, "registry.npmjs.org/") {
			continue
		}
		// Expand variables using npmVarMap + the shared expander
		vars := npmVarMap(&ebuild.EbuildInfo{Raw: fullContent, Pkg: pn})
		path := ebuild.ExpandVariables(line, vars)

		// Strip everything up to and including the registry prefix
		// (mirror://npm/ or a direct registry.npmjs.org/ URL).
		for _, prefix := range []string{"mirror://npm/", "https://registry.npmjs.org/", "http://registry.npmjs.org/"} {
			if idx := strings.Index(path, prefix); idx >= 0 {
				path = path[idx+len(prefix):]
				break
			}
		}
		if strings.Contains(path, "mirror://npm/") || strings.Contains(path, "registry.npmjs.org/") {
			continue
		}

		// The npm mirror URL format is: mirror://npm/<package-name>/-/<filename>
		// Extract everything before the first "-/" (the package name)
		sepIdx := strings.Index(path, "/-/")
		if sepIdx < 0 {
			// Scoped URL with no /-/: extract package name.
			// This intentionally diverges from the script which would return
			// the full path (@acme/widget/widget-1.0.0.tgz).
			parts := strings.Split(path, "/")
			if len(parts) >= 2 && strings.HasPrefix(parts[0], "@") {
				// Scoped: @scope/name
				return parts[0] + "/" + parts[1]
			}
			// Unscoped: just the first segment before the next /
			return parts[0]
		}

		name := path[:sepIdx]

		// Handle scoped packages: strip @scope/ prefix, return just name
		// (the scope is part of the name for scoped packages)
		return name
	}
	return ""
}
