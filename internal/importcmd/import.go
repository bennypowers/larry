package importcmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"bennypowers.dev/larry/internal/remoteid"
)

// ImportConfig holds configuration for the import command.
type ImportConfig struct {
	SourceOverlay string // --from flag: explicit source overlay path
	TargetOverlay string // --overlay flag: target overlay path (default: empty = local)
	DryRun        bool   // skip write operations
	Force         bool   // overwrite even if target is newer
}

// ImportResult holds the result of an import operation.
type ImportResult struct {
	EbuildPath  string // path to imported ebuild in target
	MetaPath    string // path to metadata.xml in target
	SourcePath  string // path to source ebuild
	SourceMeta  *remoteid.Metadata
	Prepared    bool // whether prepare was called
	Warned      string // warning message (if any)
}

// ---------------------------------------------------------------------------
// findSourceOverlay — locate the source overlay
// ---------------------------------------------------------------------------

// findSourceOverlay returns the source overlay path.
// If cfg.SourceOverlay is set, use it directly.
// Otherwise, search /var/db/repos/ for an overlay containing the package.
func findSourceOverlay(catPkg string, cfg *ImportConfig) (string, error) {
	// Explicit --from takes precedence
	if cfg.SourceOverlay != "" {
		if _, err := os.Stat(cfg.SourceOverlay); err != nil {
			return "", fmt.Errorf("source overlay %s not found: %w", cfg.SourceOverlay, err)
		}
		return cfg.SourceOverlay, nil
	}

	// Auto-detect: search /var/db/repos/ for overlay containing package
	reposDir := "/var/db/repos"
	if _, err := os.Stat(reposDir); err != nil {
		return "", fmt.Errorf("no source overlay specified and %s not found: use --from to specify", reposDir)
	}

	entries, err := os.ReadDir(reposDir)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", reposDir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		overlayPath := filepath.Join(reposDir, entry.Name())
		catDir := findCatDirInOverlay(overlayPath, catPkg)
		if catDir != "" {
			return overlayPath, nil
		}
	}

	return "", fmt.Errorf("no source overlay found for %s in %s: use --from to specify", catPkg, reposDir)
}

// findCatDirInOverlay checks if the category/package directory exists in an overlay.
func findCatDirInOverlay(overlayPath, catPkg string) string {
	parts := strings.SplitN(catPkg, "/", 2)
	if len(parts) != 2 {
		return ""
	}
	catDir := filepath.Join(overlayPath, parts[0], parts[1])
	if _, err := os.Stat(catDir); err == nil {
		return catDir
	}
	return ""
}

// ---------------------------------------------------------------------------
// findEbuild — find the latest (or live) ebuild in a category directory
// ---------------------------------------------------------------------------

// findEbuild returns the path to the latest ebuild for the package in catDir.
// If liveOnly is true, only return live ebuilds (PV=9999 or PV=*_p*). Otherwise,
// skip live ebuilds and return the highest stable version.
func findEbuild(catDir string, catPkg string, liveOnly bool) (string, error) {
	parts := strings.SplitN(catPkg, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid catpkg %q: expected category/name", catPkg)
	}

	entries, err := os.ReadDir(catDir)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", catDir, err)
	}

	var ebuilds []ebInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, parts[1]+"-") || !strings.HasSuffix(name, ".ebuild") {
			continue
		}

		ei := extractEbuildInfo(name, parts[1])
		if ei == nil {
			continue
		}

		if liveOnly && !ei.isLive() {
			continue
		}
		if ei != nil {
			ebuilds = append(ebuilds, *ei)
		}
	}

	if len(ebuilds) == 0 {
		if liveOnly {
			return "", fmt.Errorf("no live ebuilds found for %s in %s", catPkg, catDir)
		}
		return "", fmt.Errorf("no ebuilds found for %s in %s", catPkg, catDir)
	}

	// Sort by version descending and return the highest
	sort.Slice(ebuilds, func(i, j int) bool {
		return compareVersions(ebuilds[i].pv, ebuilds[j].pv) > 0
	})

	return filepath.Join(catDir, ebuilds[0].name), nil
}

// ebInfo holds parsed info from an ebuild filename.
type ebInfo struct {
	name string // full filename, e.g. "foo-2.0.0.ebuild"
	pv   string // version part, e.g. "2.0.0"
}

func (e *ebInfo) isLive() bool {
	return e.pv == "9999" || strings.Contains(e.pv, "_p")
}

// extractEbuildInfo parses PN-PV.ebuild into ebInfo.
func extractEbuildInfo(filename, pkgName string) *ebInfo {
	// Strip .ebuild suffix
	if !strings.HasSuffix(filename, ".ebuild") {
		return nil
	}
	base := strings.TrimSuffix(filename, ".ebuild")

	// Strip PN- prefix
	prefix := pkgName + "-"
	if !strings.HasPrefix(base, prefix) {
		return nil
	}
	pv := strings.TrimPrefix(base, prefix)

	// Must not be empty
	if pv == "" {
		return nil
	}

	return &ebInfo{
		name: filename,
		pv:   pv,
	}
}

// ---------------------------------------------------------------------------
// compareVersions — semantic-ish version comparison
// ---------------------------------------------------------------------------

// compareVersions returns >0 if a > b, <0 if a < b, 0 if equal.
// Handles dotted numeric versions (1.2.3) and ebuild-style (9999, _p20240101).
func compareVersions(a, b string) int {
	// Handle live versions: 9999 and *_p* are always latest
	if isLiveVersion(a) && !isLiveVersion(b) {
		return 1
	}
	if !isLiveVersion(a) && isLiveVersion(b) {
		return -1
	}
	if isLiveVersion(a) && isLiveVersion(b) {
		// Both live — compare the _p suffix or treat as equal
		aSuffix := extractLiveSuffix(a)
		bSuffix := extractLiveSuffix(b)
		if aSuffix != bSuffix {
			return strings.Compare(aSuffix, bSuffix)
		}
		return 0
	}

	// Normal version comparison
	aParts := splitVersion(a)
	bParts := splitVersion(b)

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		var aVal, bVal int
		if i < len(aParts) {
			aVal = parseIntOrZero(aParts[i])
		}
		if i < len(bParts) {
			bVal = parseIntOrZero(bParts[i])
		}
		if aVal != bVal {
			return aVal - bVal
		}
	}
	return 0
}

func isLiveVersion(v string) bool {
	return v == "9999" || strings.Contains(v, "_p")
}

func extractLiveSuffix(v string) string {
	if idx := strings.Index(v, "_p"); idx >= 0 {
		return v[idx:]
	}
	return v
}

func splitVersion(v string) []string {
	parts := strings.Split(v, ".")
	for i, p := range parts {
		// Strip any non-numeric suffixes
		num := ""
		for _, c := range p {
			if c >= '0' && c <= '9' {
				num += string(c)
			} else {
				break
			}
		}
		parts[i] = num
	}
	return parts
}

func parseIntOrZero(s string) int {
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
	}
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// isVersionNewer reports whether sourcePV is newer than targetPV.
func isVersionNewer(sourcePV, targetPV string) bool {
	return compareVersions(sourcePV, targetPV) > 0
}

// ---------------------------------------------------------------------------
// copyEbuild — copy ebuild file to target directory
// ---------------------------------------------------------------------------

// copyEbuild copies the ebuild file to the target directory.
func copyEbuild(srcPath, dstDir string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read ebuild: %w", err)
	}

	base := filepath.Base(srcPath)
	dstPath := filepath.Join(dstDir, base)
	return os.WriteFile(dstPath, data, 0644)
}

// ---------------------------------------------------------------------------
// copyFilesDir — copy files/ directory if present
// ---------------------------------------------------------------------------

// copyFilesDir copies the files/ directory from source to target.
func copyFilesDir(srcPkgDir, dstPkgDir string) error {
	srcFiles := filepath.Join(srcPkgDir, "files")
	if _, err := os.Stat(srcFiles); err != nil {
		return nil // no files/ directory — not an error
	}

	dstFiles := filepath.Join(dstPkgDir, "files")
	return copyRecursive(srcFiles, dstFiles)
}

// copyRecursive copies a directory tree.
func copyRecursive(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}

	if err := os.MkdirAll(dst, 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyRecursive(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Metadata operations
// ---------------------------------------------------------------------------

// createMetadata creates a new metadata.xml with the given maintainer and remote-ids.
func createMetadata(path string, maintainerEmail, maintainerName string, remotes []*remoteid.RemoteId) error {
	m := &remoteid.Metadata{
		Maintainers: []remoteid.Maintainer{
			{Email: maintainerEmail, Name: maintainerName},
		},
	}
	m.Remotes = remotes
	return writeMetadata(path, m)
}

// mergeMetadata merges new maintainer + remote-ids into an existing metadata.xml.
// Existing maintainers are preserved; the new maintainer is added.
// Existing remote-ids are preserved; the new remote-ids are added (duplicates skipped).
func mergeMetadata(path string, maintainerEmail, maintainerName string, remotes []*remoteid.RemoteId) error {
	// Read existing metadata
	var existing *remoteid.Metadata
	if data, err := os.ReadFile(path); err == nil {
		if parsed, err := remoteid.ParseMetadata(data); err == nil {
			existing = parsed
		}
	}

	m := &remoteid.Metadata{
		Maintainers: []remoteid.Maintainer{
			{Email: maintainerEmail, Name: maintainerName},
		},
	}
	addedRemote := map[string]bool{}
	if existing != nil {
		// Add existing maintainers (skip duplicates)
		added := map[string]bool{maintainerEmail: true}
		for _, maint := range existing.Maintainers {
			if !added[maint.Email] {
				m.Maintainers = append(m.Maintainers, maint)
				added[maint.Email] = true
			}
		}
		// Add existing remote-ids (skip duplicates)
		for _, r := range existing.Remotes {
			key := r.String()
			if !addedRemote[key] {
				m.Remotes = append(m.Remotes, r)
				addedRemote[key] = true
			}
		}
	}
	// Add new remote-ids (skip duplicates)
	for _, r := range remotes {
		key := r.String()
		if !addedRemote[key] {
			m.Remotes = append(m.Remotes, r)
		}
	}

	return writeMetadata(path, m)
}

// writeMetadata marshals metadata to XML and writes it.
func writeMetadata(path string, m *remoteid.Metadata) error {
	// Build XML manually for proper formatting
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<pkg-metadata>
`)

	// Write maintainers
	for _, maint := range m.Maintainers {
		buf.WriteString(fmt.Sprintf(`  <maintainer>
    <email>%s</email>
    <name>%s</name>
  </maintainer>
`, escXML(maint.Email), escXML(maint.Name)))
	}

	// Write remote-ids
	for _, r := range m.Remotes {
		buf.WriteString(fmt.Sprintf(`  <remote-id type="%s">%s</remote-id>
`, r.Type, escXML(rString(r))))
	}

	buf.WriteString(`</pkg-metadata>
`)

	return os.WriteFile(path, buf.Bytes(), 0644)
}

func escXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

func rString(r *remoteid.RemoteId) string {
	switch r.Type {
	case remoteid.TypeGitHub, remoteid.TypeCodeberg:
		return r.Owner + "/" + r.Repo
	case remoteid.TypePyPI, remoteid.TypeNPM:
		return r.Name
	case remoteid.TypeGitLab, remoteid.TypeSourceHut:
		return r.Host + ":" + r.Path
	default:
		return r.String()
	}
}

// ---------------------------------------------------------------------------
// autoDetectOverlay — find which overlay contains a package
// ---------------------------------------------------------------------------

// autoDetectOverlay searches a list of overlay paths for the given catpkg.
func autoDetectOverlay(overlays []string, catPkg string) string {
	for _, overlay := range overlays {
		if findCatDirInOverlay(overlay, catPkg) != "" {
			return overlay
		}
	}
	return ""
}

// autoDetectOverlayDirs returns a list of overlay directories from /var/db/repos/.
func autoDetectOverlayDirs() []string {
	var overlays []string
	reposDir := "/var/db/repos"
	if _, err := os.Stat(reposDir); err != nil {
		return overlays
	}

	entries, err := os.ReadDir(reposDir)
	if err != nil {
		return overlays
	}

	for _, entry := range entries {
		if entry.IsDir() {
			overlays = append(overlays, filepath.Join(reposDir, entry.Name()))
		}
	}

	return overlays
}

// ---------------------------------------------------------------------------
// Import — main entry point
// ---------------------------------------------------------------------------

// Import performs the full import workflow for a catpkg from source to target overlay.
// It locates the source overlay, finds the latest ebuild, copies it along with files/,
// creates/merges metadata.xml, and optionally calls the prepare pipeline.
func Import(catPkg string, cfg *ImportConfig) (*ImportResult, error) {
	if cfg == nil {
		cfg = &ImportConfig{}
	}

	// 1. Find source overlay
	sourceOverlay, err := findSourceOverlay(catPkg, cfg)
	if err != nil {
		return nil, fmt.Errorf("find source overlay: %w", err)
	}

	// 2. Locate source ebuild
	srcCatDir := findCatDirInOverlay(sourceOverlay, catPkg)
	if srcCatDir == "" {
		return nil, fmt.Errorf("package %s not found in overlay %s", catPkg, sourceOverlay)
	}

	srcEbuildPath, err := findEbuild(srcCatDir, catPkg, false)
	if err != nil {
		return nil, fmt.Errorf("find ebuild: %w", err)
	}

	// 3. Parse source metadata
	srcMetaPath := filepath.Join(filepath.Dir(srcEbuildPath), "metadata.xml")
	var srcMeta *remoteid.Metadata
	if meta, err := remoteid.ParseMetadataFile(srcMetaPath); err == nil {
		srcMeta = meta
	}

	// 4. Determine target directory
	targetOverlay := cfg.TargetOverlay
	if targetOverlay == "" {
		// Use current working directory as local overlay
		targetOverlay, _ = os.Getwd()
	}

	targetCatDir := filepath.Join(targetOverlay, filepath.Dir(srcEbuildPath))
	targetPkgDir := filepath.Join(targetCatDir, filepath.Base(srcEbuildPath))

	if err := os.MkdirAll(targetPkgDir, 0755); err != nil {
		return nil, fmt.Errorf("create target dir: %w", err)
	}

	// 5. Check version comparison (source vs target)
	targetEbuild := filepath.Join(targetPkgDir, filepath.Base(srcEbuildPath))
	if cfg.DryRun {
		return &ImportResult{
			SourcePath:  srcEbuildPath,
			EbuildPath:  targetEbuild,
			SourceMeta:  srcMeta,
		}, nil
	}

	// 6. Copy ebuild
	if err := copyEbuild(srcEbuildPath, targetPkgDir); err != nil {
		return nil, fmt.Errorf("copy ebuild: %w", err)
	}

	// 7. Copy files/ directory
	if err := copyFilesDir(filepath.Dir(srcEbuildPath), targetPkgDir); err != nil {
		return nil, fmt.Errorf("copy files/: %w", err)
	}

	// 8. Create or merge metadata.xml
	metaPath := filepath.Join(targetPkgDir, "metadata.xml")
	if _, err := os.Stat(metaPath); err == nil {
		// Target metadata exists — merge
		if err := mergeMetadata(metaPath, "", "", srcMeta.GetRemoteIds()); err != nil {
			return nil, fmt.Errorf("merge metadata: %w", err)
		}
	} else {
		// Target metadata doesn't exist — create
		remoteIds := []*remoteid.RemoteId{}
		if srcMeta != nil {
			remoteIds = srcMeta.GetRemoteIds()
		}
		if err := createMetadata(metaPath, "", "", remoteIds); err != nil {
			return nil, fmt.Errorf("create metadata: %w", err)
		}
	}

	// 9. Determine result paths
	result := &ImportResult{
		SourcePath:  srcEbuildPath,
		EbuildPath:  filepath.Join(targetPkgDir, filepath.Base(srcEbuildPath)),
		MetaPath:    metaPath,
		SourceMeta:  srcMeta,
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// PortageQ integration — find overlays via portageq
// ---------------------------------------------------------------------------

// findOverlaysViaPortageq uses portageq to find all available overlays.
func findOverlaysViaPortageq() ([]string, error) {
	cmd := exec.Command("portageq", "portage_trees", "/")
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("portageq portage_trees: %w", err)
	}

	var overlays []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), " ") {
		line = strings.TrimSpace(line)
		if line != "" {
			overlays = append(overlays, line)
		}
	}
	return overlays, nil
}
