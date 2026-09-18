package upstream

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/providers"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/version"
)

// CheckUpstream checks if an upstream update is available for a single ebuild.
// It uses the provider registry to resolve the correct provider for the ebuild's
// source type, fetches the latest version, and compares.
//
// Returns:
//   - *UpstreamResult with the comparison result
//   - bool indicating if the check was successful
//   - error if the check itself failed
func CheckUpstream(ei *ebuild.EbuildInfo, registry *provider.ProviderRegistry, allowPrereleases bool, cache *VersionCache) (*UpstreamResult, bool, error) {
	// Check skip rules
	if skip, isSkip := shouldSkip(ei.Category); isSkip {
		if skip {
			return nil, false, nil
		}
	}

	// Check for live version (9999)
	if isLiveVersion(ei.PVVal) {
		return &UpstreamResult{
			Package:     fmt.Sprintf("%s/%s", ei.Category, ei.Pkg),
			Current:     "",
			Latest:      "",
			Status:      StatusLive,
			SourceLabel: "",
		}, true, nil
	}

	// Detect package type from SRC_URI and HOMEPAGE
	providerType := detectProviderFromSRCURI(ei.SRC_URI, ei.HOMEPAGE)
	if providerType == "" {
		// Try to resolve from SRC_URI URL patterns
		providerType = string(providers.ResolveProviderFromURL(ei.SRC_URI))
	}

	if providerType == "" || providerType == "unknown" {
		return &UpstreamResult{
			Package: fmt.Sprintf("%s/%s", ei.Category, ei.Pkg),
			Current: ei.PVVal,
			Latest:  "",
			Status:  StatusUnknown,
			Error:   "no provider match",
		}, true, nil
	}

	// Build RemoteId from SRC_URI
	rid, err := resolveRemoteId(ei, providerType)
	if err != nil {
		return &UpstreamResult{
			Package: fmt.Sprintf("%s/%s", ei.Category, ei.Pkg),
			Current: ei.PVVal,
			Latest:  "",
			Status:  StatusUnknown,
			Error:   err.Error(),
		}, true, nil
	}

	// Check cache
	cacheKey := MakeKey(ei.Category, ei.Pkg, string(rid.Type), ei.PVVal)
	if cachedVer, ok := cache.Get(cacheKey); ok {
		cachedV, _ := version.Parse(cachedVer)
		currentV, _ := version.Parse(ei.PVVal)
		result := buildResult(ei, cachedV, currentV, rid.Type)
		result.Source = resolveSourceURL(rid)
		result.SourceLabel = resolveSourceLabel(rid)
		return result, true, nil
	}

	// Resolve provider from registry
	// Map the ebuild package type to a RemoteIdType
	ridType := mapTypeToRemoteIdType(providerType)
	p, err := registry.Resolve(remoteid.RemoteId{Type: ridType, Name: rid.Name, Owner: rid.Owner, Repo: rid.Repo})
	if err != nil {
		return &UpstreamResult{
			Package:     fmt.Sprintf("%s/%s", ei.Category, ei.Pkg),
			Current:     ei.PVVal,
			Latest:      "",
			Status:      StatusUnknown,
			SourceLabel: "unknown",
			Error:       fmt.Sprintf("no provider for %s", providerType),
		}, true, nil
	}

	// Fetch latest version
	latestV, err := p.FetchLatestVersion(*rid, allowPrereleases)
	if err != nil || latestV == nil {
		// Provider returned an error or nil (e.g., package not found, prerelease filtered)
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		} else {
			errMsg = "latestV is nil"
		}
		return &UpstreamResult{
			Package:     fmt.Sprintf("%s/%s", ei.Category, ei.Pkg),
			Current:     ei.PVVal,
			Latest:      "",
			Status:      StatusUnknown,
			SourceLabel: resolveSourceLabel(rid),
			Error:       errMsg,
		}, true, nil
	}

	// Store in cache
	cache.Set(cacheKey, latestV.Format())

	// Compare versions
	currentV, _ := version.Parse(ei.PVVal)
	result := buildResult(ei, latestV, currentV, rid.Type)
	result.Source = resolveSourceURL(rid)
	result.SourceLabel = resolveSourceLabel(rid)

	return result, true, nil
}

// CheckUpstreamSimple is a simplified version for single-provider checks.
func CheckUpstreamSimple(ei *ebuild.EbuildInfo, p provider.UpstreamProvider, allowPrereleases bool) (*UpstreamResult, bool, error) {
	// Check skip rules
	if skip, isSkip := shouldSkip(ei.Category); isSkip {
		if skip {
			return nil, false, nil
		}
	}

	// Check for live version
	if isLiveVersion(ei.PVVal) {
		return &UpstreamResult{
			Package:     fmt.Sprintf("%s/%s", ei.Category, ei.Pkg),
			Current:     "",
			Latest:      "",
			Status:      StatusLive,
			SourceLabel: "",
		}, true, nil
	}

	// Fetch latest version
	latestV, err := p.FetchLatestVersion(remoteid.RemoteId{
		Type:  p.Type(),
		Name:  ei.Pkg,
		Owner: "",
		Repo:  "",
	}, allowPrereleases)
	if err != nil {
		return &UpstreamResult{
			Package: fmt.Sprintf("%s/%s", ei.Category, ei.Pkg),
			Current: ei.PVVal,
			Latest:  "",
			Status:  StatusUnknown,
			Error:   err.Error(),
		}, true, nil
	}

	// Compare versions
	currentV, _ := version.Parse(ei.PVVal)
	result := buildResult(ei, latestV, currentV, p.Type())

	return result, true, nil
}

// Run walks the overlay directory and checks all ebuilds for upstream updates.
// It returns results for all discoverable ebuilds, skipping acct-group/* and acct-user/*.
func Run(overlayDir string, allowPrereleases bool, fresh bool) ([]UpstreamResult, error) {
	// Create cache
	cacheDir := DefaultCachePath()
	cache := NewVersionCacheWithPath(cacheDir, 6*time.Hour)

	if fresh {
		_ = cache.Clear() // Bypass cache by clearing on each run
	}

	// Walk overlay directory
	results, err := walkAndCheck(overlayDir, nil, allowPrereleases, cache)
	if err != nil {
		return nil, err
	}

	return results, nil
}

// walkAndCheck recursively walks the overlay directory and checks ebuilds.
func walkAndCheck(dir string, registry *provider.ProviderRegistry, allowPrereleases bool, cache *VersionCache) ([]UpstreamResult, error) {
	var results []UpstreamResult

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		entryPath := filepath.Join(dir, entry.Name())

		// Check if this is a category directory (contains ebuild files)
		ebuildFiles, err := findEbuilds(entryPath)
		if err != nil {
			continue
		}

		if len(ebuildFiles) > 0 {
			// This is a category directory
			category := entry.Name()

			// Check skip rules
			if skip, isSkip := shouldSkip(category); isSkip && skip {
				continue
			}

			for _, ebuildPath := range ebuildFiles {
				einfo, err := parseEbuildInfo(ebuildPath)
				if err != nil {
					continue
				}

				// Check live version
				if isLiveVersion(einfo.PVVal) {
					results = append(results, UpstreamResult{
						Package: fmt.Sprintf("%s/%s", einfo.Category, einfo.Pkg),
						Current: "",
						Latest:  "",
						Status:  StatusLive,
						SourceLabel: "",
					})
					continue
				}

				// Detect provider from SRC_URI and HOMEPAGE
				providerType := detectProviderFromSRCURI(einfo.SRC_URI, einfo.HOMEPAGE)
				if providerType == "" {
					providerType = string(providers.ResolveProviderFromURL(einfo.SRC_URI))
				}

				if providerType == "" || providerType == "unknown" {
					results = append(results, UpstreamResult{
						Package:     fmt.Sprintf("%s/%s", einfo.Category, einfo.Pkg),
						Current:     einfo.PVVal,
						Latest:      "",
						Status:      StatusUnknown,
						SourceLabel: "unknown",
						Error:       "no provider match",
					})
					continue
				}

				// Build RemoteId
				rid, err := resolveRemoteId(einfo, providerType)
				if err != nil {
					results = append(results, UpstreamResult{
						Package:     fmt.Sprintf("%s/%s", einfo.Category, einfo.Pkg),
						Current:     einfo.PVVal,
						Latest:      "",
						Status:      StatusUnknown,
						SourceLabel: "unknown",
						Error:       err.Error(),
					})
					continue
				}

				// Check cache
				cacheKey := MakeKey(einfo.Category, einfo.Pkg, string(rid.Type), einfo.PVVal)
				var latestV *version.Version
				var sourceURL string
				var sourceLabel string

				if cachedVer, ok := cache.Get(cacheKey); ok {
					latestV, _ = version.Parse(cachedVer)
					sourceURL = resolveSourceURL(rid)
					sourceLabel = resolveSourceLabel(rid)
				} else if registry != nil {
					// Resolve provider from registry
					ridType := mapTypeToRemoteIdType(providerType)
					p, err := registry.Resolve(remoteid.RemoteId{Type: ridType, Name: rid.Name, Owner: rid.Owner, Repo: rid.Repo})
					if err != nil {
						results = append(results, UpstreamResult{
							Package:     fmt.Sprintf("%s/%s", einfo.Category, einfo.Pkg),
							Current:     einfo.PVVal,
							Latest:      "",
							Status:      StatusUnknown,
							SourceLabel: "unknown",
							Error:       fmt.Sprintf("no provider for %s", providerType),
						})
						continue
					}

					latestV, err = p.FetchLatestVersion(*rid, allowPrereleases)
					if err != nil {
						results = append(results, UpstreamResult{
							Package:     fmt.Sprintf("%s/%s", einfo.Category, einfo.Pkg),
							Current:     einfo.PVVal,
							Latest:      "",
							Status:      StatusUnknown,
							SourceLabel: resolveSourceLabel(rid),
							Error:       err.Error(),
						})
						continue
					}

					// Cache the result
					cache.Set(cacheKey, latestV.Format())
					sourceURL = resolveSourceURL(rid)
					sourceLabel = resolveSourceLabel(rid)
				} else {
					// No registry — try to parse provider-specific logic
					latestV, sourceURL = resolveVersionFromSRCURI(einfo, providerType, rid)
					sourceLabel = resolveSourceLabel(rid)
				}

				if latestV == nil {
					results = append(results, UpstreamResult{
						Package:     fmt.Sprintf("%s/%s", einfo.Category, einfo.Pkg),
						Current:     einfo.PVVal,
						Latest:      "",
						Status:      StatusUnknown,
						SourceLabel: sourceLabel,
						Error:   "could not resolve upstream version",
					})
					continue
				}

				currentV, _ := version.Parse(einfo.PVVal)
				result := buildResult(einfo, latestV, currentV, rid.Type)
				result.Source = sourceURL
				result.SourceLabel = sourceLabel
				results = append(results, *result)
			}
		} else {
			// Not a category directory — recurse
			subResults, err := walkAndCheck(entryPath, registry, allowPrereleases, cache)
			if err == nil {
				results = append(results, subResults...)
			}
		}
	}

	return results, nil
}

// findEbuilds finds all .ebuild files in a directory, returning only the
// latest version of each package name. This avoids checking every historical
// ebuild, which would be slow and produce duplicate results.
func findEbuilds(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	// Group by package name (filename without version), keeping latest version
	type namedVersion struct {
		name string
		ver  *version.Version
		path string
	}
	best := make(map[string]*namedVersion) // pkgName -> best version

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".ebuild") {
			pv := strings.TrimSuffix(entry.Name(), ".ebuild")
			// Extract package name: everything before the last "-<version>"
			idx := strings.LastIndex(pv, "-")
			if idx <= 0 {
				continue
			}
			pkgName := pv[:idx]
			pvStr := pv[idx+1:]

			parsed, err := version.Parse(pvStr)
			if err != nil {
				continue
			}

			if cur, ok := best[pkgName]; ok {
				if parsed.Compare(cur.ver) > 0 {
					best[pkgName] = &namedVersion{name: pkgName, ver: parsed, path: filepath.Join(dir, entry.Name())}
				}
			} else {
				best[pkgName] = &namedVersion{name: pkgName, ver: parsed, path: filepath.Join(dir, entry.Name())}
			}
		}
	}

	// Collect the best (latest) ebuild for each package
	var ebuilds []string
	for _, nv := range best {
		ebuilds = append(ebuilds, nv.path)
	}
	return ebuilds, nil
}

// parseEbuildInfo parses an ebuild file into EbuildInfo.
// This is a simplified parser that extracts key fields.
func parseEbuildInfo(path string) (*ebuild.EbuildInfo, error) {
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
		// Derive PV from filename
		info.PVVal = extractVersionFromFilename(path)
	}

	// Extract PN
	if pn := extractVar(content, "PN"); pn != "" {
		info.Pkg = pn
	} else {
		info.Pkg = extractPkgName(path)
	}

	// Extract SRC_URI
	info.SRC_URI = extractVar(content, "SRC_URI")

	// Extract HOMEPAGE
	info.HOMEPAGE = extractVar(content, "HOMEPAGE")

	// Extract INHERIT
	info.Inherit = extractInherit(content)

	// Derive category from directory
	info.Path = path
	info.Category = extractCategoryFromPath(path)

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

// extractVersionFromFilename extracts version from ebuild filename.
// e.g., "pkg-1.2.3.ebuild" -> "1.2.3"
func extractVersionFromFilename(path string) string {
	base := filepath.Base(path)
	// Remove .ebuild suffix
	base = strings.TrimSuffix(base, ".ebuild")
	// Remove PN prefix: "pkg-" from "pkg-1.2.3"
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

// extractCategoryFromPath extracts category from path.
// e.g., "/overlay/dev-python/requests/pkg-1.0.ebuild" -> "dev-python"
// Strategy: extract PN from filename, then find the directory that matches PN.
// That directory is the package directory; its parent is the category.
func extractCategoryFromPath(path string) string {
	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) < 3 {
		return ""
	}

	// Extract PN from filename
	filename := parts[len(parts)-1]
	filename = strings.TrimSuffix(filename, ".ebuild")
	if idx := strings.LastIndex(filename, "-"); idx > 0 {
		filename = filename[:idx]
	}

	// Find the package directory (matches PN)
	for i := len(parts) - 2; i >= 0; i-- {
		if parts[i] == filename {
			// This is the package directory; category is its parent
			if i > 0 {
				return parts[i-1]
			}
		}
	}

	// Fallback: find the first directory that looks like a Gentoo category
	// (starts with letter, contains hyphens, or is a known category prefix)
	knownPrefixes := []string{"dev-", "www-", "app-", "sys-", "net-", "media-", "sci-", "mail-", "net-", "x11-", "acct-", "virtual-", "profiles"}
	for i := len(parts) - 2; i >= 1; i-- {
		part := parts[i]
		if len(part) > 0 && part[0] >= 'a' && part[0] <= 'z' {
			// Check if it looks like a Gentoo category
			for _, prefix := range knownPrefixes {
				if strings.HasPrefix(part, prefix) {
					return part
				}
			}
		}
	}

	// Last resort: second-to-last directory component
	if len(parts) >= 3 {
		return parts[len(parts)-2]
	}
	return ""
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



// shouldSkip checks if a category should be skipped.
// Returns (skip, isSkipRule).
func shouldSkip(category string) (bool, bool) {
	if category == "acct-group" || category == "acct-user" {
		return true, true
	}
	return false, false
}

// isLiveVersion checks if the version string represents a live version (9999).
func isLiveVersion(pv string) bool {
	return pv == "9999" || strings.HasSuffix(pv, "-9999")
}

// detectProviderFromSRCURI detects the provider type from SRC_URI and HOMEPAGE.
// It handles both mirror:// URIs and direct URLs (https://github.com/... etc).
func detectProviderFromSRCURI(srcURI string, homepage string) string {
	// Check SRC_URI first, then HOMEPAGE
	for _, text := range []string{srcURI, homepage} {
		switch {
		case strings.Contains(text, "mirror://pypi/"):
			return "pypi"
		case strings.Contains(text, "mirror://npm/"):
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
		case strings.Contains(text, "gitlab.com/") || strings.Contains(text, "gitlab."):
			return "gitlab"
		case strings.Contains(text, "codeberg.org/"):
			return "codeberg"
		case strings.Contains(text, "sr.ht") || strings.Contains(text, "sourcehut"):
			return "sourcehut"
		}
	}
	return ""
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

// resolveRemoteId extracts a RemoteId from ebuild SRC_URI/HOMEPAGE.
func resolveRemoteId(ei *ebuild.EbuildInfo, providerType string) (*remoteid.RemoteId, error) {
	switch providerType {
	case "pypi":
		return &remoteid.RemoteId{
			Type: remoteid.TypePyPI,
			Name: ei.Pkg,
		}, nil
	case "npm":
		return &remoteid.RemoteId{
			Type: remoteid.TypeNPM,
			Name: ei.Pkg,
		}, nil
	case "github":
		owner, repo := extractRepoFromURLs(ei.SRC_URI, ei.HOMEPAGE, "github.com")
		return &remoteid.RemoteId{
			Type:  remoteid.TypeGitHub,
			Owner: owner,
			Repo:  repo,
		}, nil
	case "gitlab":
		host, path := extractRepoFromURLs(ei.SRC_URI, ei.HOMEPAGE, "gitlab")
		return &remoteid.RemoteId{
			Type: remoteid.TypeGitLab,
			Host: host,
			Path: "/" + path,
		}, nil
	case "codeberg":
		owner, repo := extractRepoFromURLs(ei.SRC_URI, ei.HOMEPAGE, "codeberg.org")
		return &remoteid.RemoteId{
			Type:  remoteid.TypeCodeberg,
			Owner: owner,
			Repo:  repo,
		}, nil
	case "sourcehut":
		host, path := extractRepoFromURLs(ei.SRC_URI, ei.HOMEPAGE, "sr.ht")
		return &remoteid.RemoteId{
			Type: remoteid.TypeSourceHut,
			Host: host,
			Path: "/" + path,
		}, nil
	default:
		return nil, fmt.Errorf("unknown provider type: %s", providerType)
	}
}

// extractRepoFromURLs extracts owner/repo (or host/path) from SRC_URI and HOMEPAGE.
// It looks for URLs matching the given domain pattern.
func extractRepoFromURLs(srcURI, homepage, domain string) (string, string) {
	for _, text := range []string{srcURI, homepage} {
		owner, repo := extractOwnerRepo(text, domain)
		if owner != "" && repo != "" {
			return owner, repo
		}
	}
	return "", ""
}

// extractOwnerRepo extracts owner/repo from text containing URLs with the given domain.
func extractOwnerRepo(text, domain string) (string, string) {
	// Look for URLs like https://domain.com/owner/repo or mirror://domain/owner/repo
	// Pattern: domain followed by /owner/repo (exactly 2 path segments)
	pattern := domain + "/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)"
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(text)
	if len(matches) >= 3 {
		return matches[1], matches[2]
	}
	return "", ""
}

// resolveSourceLabel builds a short source label like "GitHub:owner/repo" or "PyPI:name".
func resolveSourceLabel(rid *remoteid.RemoteId) string {
	switch rid.Type {
	case remoteid.TypePyPI:
		return "PyPI:" + rid.Name
	case remoteid.TypeNPM:
		return "npm:" + rid.Name
	case remoteid.TypeGitHub:
		if rid.Owner != "" && rid.Repo != "" {
			return "GitHub:" + rid.Owner + "/" + rid.Repo
		}
		return "GitHub:"
	case remoteid.TypeGitLab:
		if rid.Host != "" && rid.Path != "" {
			return "GitLab:" + rid.Host + rid.Path
		}
		return "GitLab:"
	case remoteid.TypeCodeberg:
		if rid.Owner != "" && rid.Repo != "" {
			return "Codeberg:" + rid.Owner + "/" + rid.Repo
		}
		return "Codeberg:"
	case remoteid.TypeSourceHut:
		if rid.Host != "" && rid.Path != "" {
			return "Sourcehut:" + rid.Host + rid.Path
		}
		return "Sourcehut:"
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
		return fmt.Sprintf("https://%s%s", rid.Host, rid.Path)
	case remoteid.TypeCodeberg:
		return fmt.Sprintf("https://codeberg.org/%s/%s", rid.Owner, rid.Repo)
	case remoteid.TypeSourceHut:
		return fmt.Sprintf("https://%s%s", rid.Host, rid.Path)
	default:
		return ""
	}
}

// buildResult builds an UpstreamResult from version comparison.
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

// RunWithCategories walks the overlay and checks specific categories (or all if empty).
func RunWithCategories(overlayDir string, registry *provider.ProviderRegistry, categories []string, allowPrereleases bool, fresh bool) ([]UpstreamResult, error) {
	cacheDir := DefaultCachePath()
	cache := NewVersionCacheWithPath(cacheDir, 6*time.Hour)

	if fresh {
		_ = cache.Clear()
	}

	return walkAndCheckCategories(overlayDir, registry, categories, allowPrereleases, cache)
}

// walkAndCheckCategories walks the overlay and checks only specified categories.
func walkAndCheckCategories(dir string, registry *provider.ProviderRegistry, categories []string, allowPrereleases bool, cache *VersionCache) ([]UpstreamResult, error) {
	var results []UpstreamResult

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		category := entry.Name()

		// If specific categories are requested, skip others
		if len(categories) > 0 {
			found := false
			for _, c := range categories {
				if c == category {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Check skip rules
		if skip, isSkip := shouldSkip(category); isSkip && skip {
			continue
		}

		categoryPath := filepath.Join(dir, category)
		ebuildFiles, err := findEbuilds(categoryPath)
		if err != nil {
			continue
		}

		if len(ebuildFiles) > 0 {
			// Ebuilds found directly in this directory
			for _, ebuildPath := range ebuildFiles {
			einfo, err := parseEbuildInfo(ebuildPath)
			if err != nil {
				continue
			}

			// Skip live versions
			if isLiveVersion(einfo.PVVal) {
				results = append(results, UpstreamResult{
					Package: fmt.Sprintf("%s/%s", einfo.Category, einfo.Pkg),
					Current: "",
					Latest:  "",
					Status:  StatusLive,
					SourceLabel: "",
				})
				continue
			}

			// Detect provider from SRC_URI and HOMEPAGE
			providerType := detectProviderFromSRCURI(einfo.SRC_URI, einfo.HOMEPAGE)
			if providerType == "" {
				providerType = string(providers.ResolveProviderFromURL(einfo.SRC_URI))
			}

			if providerType == "" || providerType == "unknown" {
				results = append(results, UpstreamResult{
					Package: fmt.Sprintf("%s/%s", einfo.Category, einfo.Pkg),
					Current: einfo.PVVal,
					Latest:  "",
					Status:  StatusUnknown,
					Error:   "no provider match",
				})
				continue
			}

			// Build RemoteId
			rid, err := resolveRemoteId(einfo, providerType)
			if err != nil {
				results = append(results, UpstreamResult{
					Package: fmt.Sprintf("%s/%s", einfo.Category, einfo.Pkg),
					Current: einfo.PVVal,
					Latest:  "",
					Status:  StatusUnknown,
					Error:   err.Error(),
				})
				continue
			}

			// Check cache
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
					results = append(results, UpstreamResult{
						Package: fmt.Sprintf("%s/%s", einfo.Category, einfo.Pkg),
						Current: einfo.PVVal,
						Latest:  "",
						Status:  StatusUnknown,
						Error:   fmt.Sprintf("no provider for %s", providerType),
					})
					continue
				}

				latestV, err = p.FetchLatestVersion(*rid, allowPrereleases)
				if err != nil {
					results = append(results, UpstreamResult{
						Package: fmt.Sprintf("%s/%s", einfo.Category, einfo.Pkg),
						Current: einfo.PVVal,
						Latest:  "",
						Status:  StatusUnknown,
						Error:   err.Error(),
					})
					continue
				}

				cache.Set(cacheKey, latestV.Format())
				sourceURL = resolveSourceURL(rid)
				sourceLabel = resolveSourceLabel(rid)
			} else {
				latestV, sourceURL = resolveVersionFromSRCURI(einfo, providerType, rid)
				sourceLabel = resolveSourceLabel(rid)
			}

			if latestV == nil {
				results = append(results, UpstreamResult{
					Package: fmt.Sprintf("%s/%s", einfo.Category, einfo.Pkg),
					Current: einfo.PVVal,
					Latest:  "",
					Status:  StatusUnknown,
					Error:   "could not resolve upstream version",
				})
				continue
			}

			currentV, _ := version.Parse(einfo.PVVal)
			result := buildResult(einfo, latestV, currentV, rid.Type)
			result.Source = sourceURL
			result.SourceLabel = sourceLabel
			results = append(results, *result)
		}
		} else {
			// No ebuilds found directly — recurse into subdirectories
			subResults, err := walkAndCheckCategories(categoryPath, registry, nil, allowPrereleases, cache)
			if err == nil {
				results = append(results, subResults...)
			}
		}
	}

	return results, nil
}
