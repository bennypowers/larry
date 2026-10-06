package importcmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/version"
	"bennypowers.dev/larry/internal/remoteid"
)

// ImportConfig holds configuration for the import command.
type ImportConfig struct {
	SourceOverlay string // --from flag: raw value (path, repo name, or empty)
	TargetOverlay string // --overlay flag: target overlay path (default: empty = local)
	DryRun        bool   // skip write operations
	Force         bool   // overwrite even if target is newer or equal
	ReposRoot     string // base dir for repo scan; defaults to /var/db/repos

	// MaintainerName and MaintainerEmail are required for non-dry-run imports.
	MaintainerName  string
	MaintainerEmail string

	// Prepare runs the prepare pipeline on the freshly imported ebuild.
	// Nil means no prepare step.
	Prepare func(ebuildPath string) error

	// Log is used for provenance and progress lines.
	// When nil, logging is a no-op.
	Log *ImportLogger
}

// ImportResult holds the result of an import operation.
type ImportResult struct {
	EbuildPath string // path to imported ebuild in target
	MetaPath   string // path to metadata.xml in target
	SourcePath string // path to source ebuild
	RepoName   string // source repo name for provenance
	Prepared   bool   // whether prepare was called
	Warned     string // warning message (if any)
}

// ---------------------------------------------------------------------------
// resolveSourceOverlay — locate the source overlay
// ---------------------------------------------------------------------------

// resolveSourceOverlay returns the overlay root holding catPkg and the repo name
// (the overlay directory's base name) for the provenance line.
// Resolution order:
//  1. --from <path> that os.Stat's as a directory → use it;
//  2. --from <name> that does not stat → /var/db/repos/<name>; if that does not stat, error;
//  3. no --from → scan /var/db/repos/*, skipping any entry whose Clean equals targetOverlay,
//     first match containing <category>/<package> wins;
//  4. nothing found → error naming --from.
func resolveSourceOverlay(from, targetOverlay, catPkg, reposRoot string) (overlayPath, repoName string, err error) {
	if reposRoot == "" {
		reposRoot = "/var/db/repos"
	}
	targetClean := filepath.Clean(targetOverlay)

	// Explicit --from: path or repo name
	if from != "" {
		if _, err := os.Stat(from); err == nil {
			// --from is a directory path
			return from, filepath.Base(from), nil
		}
		// --from might be a repo name; try /var/db/repos/<name>
		candidate := filepath.Join(reposRoot, from)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, from, nil
		}
		return "", "", fmt.Errorf("source not found: %s", from)
	}

	// Auto-detect: scan /var/db/repos/*
	entries, err := os.ReadDir(reposRoot)
	if err != nil {
		return "", "", fmt.Errorf("no source overlay specified and %s not found: use --from to specify", reposRoot)
	}

	parts := strings.SplitN(catPkg, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid catpkg %q: expected category/name", catPkg)
	}
	catPkgPath := filepath.Join(parts[0], parts[1])

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		overlayPath := filepath.Join(reposRoot, entry.Name())
		// Skip own overlay
		if filepath.Clean(overlayPath) == targetClean {
			continue
		}
		if _, err := os.Stat(filepath.Join(overlayPath, catPkgPath)); err == nil {
			return overlayPath, entry.Name(), nil
		}
	}

	return "", "", fmt.Errorf("no source overlay found for %s: use --from to specify", catPkg)
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
// ImportLogger — lightweight logger for import provenance lines
// ---------------------------------------------------------------------------

// ImportLogger provides Msg, Warn, and Error methods matching the script's
// provenance-line output style. Zero value is a no-op.
type ImportLogger struct {
	mu   sync.Mutex
	out  io.Writer
	err  io.Writer
	msgs []string // captured for testing
}

// Msg writes an import progress/provenance line to out.
func (l *ImportLogger) Msg(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.mu.Lock()
	l.msgs = append(l.msgs, msg)
	l.mu.Unlock()
	if l.out == nil {
		return
	}
	fmt.Fprintln(l.out, msg)
}

// Warn writes a warning line to err.
func (l *ImportLogger) Warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.mu.Lock()
	l.msgs = append(l.msgs, msg)
	l.mu.Unlock()
	if l.err == nil {
		return
	}
	fmt.Fprintln(l.err, msg)
}

// Error writes an error line to err.
func (l *ImportLogger) Error(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.mu.Lock()
	l.msgs = append(l.msgs, msg)
	l.mu.Unlock()
	if l.err == nil {
		return
	}
	fmt.Fprintln(l.err, msg)
}

// SetOut sets the writer for Msg.
func (l *ImportLogger) SetOut(w io.Writer) { l.out = w }

// SetErr sets the writer for Warn/Error.
func (l *ImportLogger) SetErr(w io.Writer) { l.err = w }

// Messages returns a copy of captured messages (for testing).
func (l *ImportLogger) Messages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.msgs))
	copy(out, l.msgs)
	return out
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

// writeMetadata marshals metadata to XML and writes it with exact Gentoo format:
// DOCTYPE, <pkgmetadata> root (no hyphen), tab indents, type="person" on maintainer,
// <upstream> only when there are remote-ids, single trailing newline.
func writeMetadata(path string, m *remoteid.Metadata) error {
	var buf bytes.Buffer
	buf.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	buf.WriteString("<!DOCTYPE pkgmetadata SYSTEM \"https://www.gentoo.org/dtd/metadata.dtd\">\n")
	buf.WriteString("<pkgmetadata>\n")

	// Write maintainers (tab-indented, type="person")
	for _, maint := range m.Maintainers {
		buf.WriteString("\t<maintainer type=\"person\">\n")
		buf.WriteString(fmt.Sprintf("\t\t<email>%s</email>\n", escXML(maint.Email)))
		buf.WriteString(fmt.Sprintf("\t\t<name>%s</name>\n", escXML(maint.Name)))
		buf.WriteString("\t</maintainer>\n")
	}

	// Write <upstream> block ONLY when there are remote-ids.
	if len(m.Remotes) > 0 {
		buf.WriteString("\t<upstream>\n")
		for _, r := range m.Remotes {
			buf.WriteString(fmt.Sprintf("\t\t<remote-id type=\"%s\">%s</remote-id>\n", r.Type, escXML(rString(r))))
		}
		buf.WriteString("\t</upstream>\n")
	}

	buf.WriteString("</pkgmetadata>\n")

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
// Import — main entry point
// ---------------------------------------------------------------------------

// Import performs the full import workflow for a catpkg from source to target overlay.
// It locates the source overlay, finds the latest ebuild, copies it along with files/,
// creates/merges metadata.xml, and optionally calls the prepare pipeline.
func Import(catPkg string, cfg *ImportConfig) (*ImportResult, error) {
	if cfg == nil {
		cfg = &ImportConfig{}
	}

	log := cfg.Log
	if log == nil {
		log = &ImportLogger{}
	}

	// 0. Validate maintainer config (before any writes)
	if cfg.MaintainerName == "" || cfg.MaintainerEmail == "" {
		return nil, fmt.Errorf("maintainer not configured: set maintainer.name and maintainer.email in the config file, or LARRY_MAINTAINER_NAME / LARRY_MAINTAINER_EMAIL")
	}

	// 1. Resolve source overlay
	sourceOverlay, repoName, err := resolveSourceOverlay(
		cfg.SourceOverlay,
		cfg.TargetOverlay,
		catPkg,
		cfg.ReposRoot,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve source overlay: %w", err)
	}

	// 2. Locate source package directory
	srcPkgDir := filepath.Join(sourceOverlay, catPkg)
	if _, err := os.Stat(srcPkgDir); err != nil {
		return nil, fmt.Errorf("package %s not found in overlay %s", catPkg, sourceOverlay)
	}

	// 3. Find latest non-live ebuild using atom.FindLatest
	srcParsed, err := atom.FindLatest(srcPkgDir)
	if err != nil {
		return nil, fmt.Errorf("find ebuild: %w", err)
	}
	srcEbuildPath := srcParsed.Path

	// 4. Parse source metadata (missing is OK, malformed is an error)
	var remoteIds []*remoteid.RemoteId
	srcMetaPath := filepath.Join(srcPkgDir, "metadata.xml")
	if _, err := os.Stat(srcMetaPath); err == nil {
		meta, err := remoteid.ParseMetadataFile(srcMetaPath)
		if err != nil {
			return nil, fmt.Errorf("parse source metadata %s: %w", srcMetaPath, err)
		}
		remoteIds = meta.GetRemoteIds()
	}

	// 5. Determine target directory from atom.ParseAtom
	targetOverlay := cfg.TargetOverlay
	if targetOverlay == "" {
		targetOverlay, _ = os.Getwd()
	}

	targetPkgDir := filepath.Join(targetOverlay, srcParsed.Category, srcParsed.PN)

	// 6. Version guard — check target dir for equal-or-higher version
	if !cfg.Force {
		targetLatest, err := atom.FindLatest(targetPkgDir)
		if err == nil {
			// Target has an ebuild — compare PVRs
			if version.Compare(srcParsed.PVR, targetLatest.PVR) <= 0 {
				return nil, fmt.Errorf("target already has %s (>= %s); use --force to overwrite", targetLatest.PVR, srcParsed.PVR)
			}
		}
		// atom.ErrNoEbuilds means target dir is empty — proceed
	}

	// 7. Build result paths
	resultEbuildPath := filepath.Join(targetPkgDir, srcParsed.PF+".ebuild")
	resultMetaPath := filepath.Join(targetPkgDir, "metadata.xml")

	// 8. Dry-run: no filesystem writes at all
	if cfg.DryRun {
		log.Msg("Importing %s from ::%s", catPkg, repoName)
		log.Msg("Source:   %s", srcPkgDir)
		return &ImportResult{
			SourcePath: srcEbuildPath,
			EbuildPath: resultEbuildPath,
			MetaPath:   resultMetaPath,
			RepoName:   repoName,
		}, nil
	}

	// 9. Provenance lines
	log.Msg("Importing %s from ::%s", catPkg, repoName)
	log.Msg("Source:   %s", srcPkgDir)

	// 10. Create target directory
	if err := os.MkdirAll(targetPkgDir, 0755); err != nil {
		return nil, fmt.Errorf("create target dir: %w", err)
	}

	// 11. Copy ebuild
	if err := copyEbuild(srcEbuildPath, targetPkgDir); err != nil {
		return nil, fmt.Errorf("copy ebuild: %w", err)
	}
	log.Msg("Copied:   %s", filepath.Base(srcEbuildPath))

	// 12. Copy files/ directory
	if err := copyFilesDir(srcPkgDir, targetPkgDir); err != nil {
		return nil, fmt.Errorf("copy files/: %w", err)
	}
	if _, err := os.Stat(filepath.Join(srcPkgDir, "files")); err == nil {
		log.Msg("Copied:   files/")
	}

	// 13. Create or merge metadata.xml
	if _, err := os.Stat(resultMetaPath); err == nil {
		// Target metadata exists — merge
		if err := mergeMetadata(resultMetaPath, cfg.MaintainerEmail, cfg.MaintainerName, remoteIds); err != nil {
			return nil, fmt.Errorf("merge metadata: %w", err)
		}
	} else {
		// Target metadata doesn't exist — create
		if err := createMetadata(resultMetaPath, cfg.MaintainerEmail, cfg.MaintainerName, remoteIds); err != nil {
			return nil, fmt.Errorf("create metadata: %w", err)
		}
	}
	log.Msg("Created:  metadata.xml")

	// 14. Run prepare (if configured and not dry-run)
	if cfg.Prepare != nil {
		if err := cfg.Prepare(resultEbuildPath); err != nil {
			return &ImportResult{
				EbuildPath: resultEbuildPath,
				MetaPath:   resultMetaPath,
				SourcePath: srcEbuildPath,
				RepoName:   repoName,
			}, fmt.Errorf("prepare imported ebuild: %w", err)
		}
	}

	return &ImportResult{
		EbuildPath: resultEbuildPath,
		MetaPath:   resultMetaPath,
		SourcePath: srcEbuildPath,
		RepoName:   repoName,
		Prepared:   cfg.Prepare != nil,
	}, nil
}
