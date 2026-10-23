package prepare

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ulikunitz/xz"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/github"
	"bennypowers.dev/larry/internal/httpclient"
)

var githubRe = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)`)

// PrepareGo is the go-specific prepare pipeline for `larry prepare`.
// It vendors Go dependencies, creates a vendor tarball, and runs pkgdev manifest.
func PrepareGo(ei *ebuild.EbuildInfo, cfg *PrepareConfig) (retErr error) {
	// Early exit for dry-run
	if cfg.DryRun {
		return nil
	}

	// Defaults for injectable fields
	base := cfg.GitHubBaseURL
	if base == "" {
		base = "https://github.com"
	}
	runner := cfg.Runner
	if runner == nil {
		runner = github.NewClient().Runner()
	}

	// D6: distdir must be set — no default fallback
	distDir := cfg.Distdir
	if distDir == "" {
		return fmt.Errorf("go: distdir is empty")
	}

	// 1. Find the upstream repository
	owner, repo := findGoRepo(ei)
	if owner == "" || repo == "" {
		return fmt.Errorf("go: could not resolve github.com owner/repo from SRC_URI or HOMEPAGE")
	}

	// 2. Tag: v${PV} with revision stripped
	tag := "v" + atom.StripRevision(ei.PVVal)

	// 3. Download source tarball
	srcURL := fmt.Sprintf("%s/%s/%s/archive/refs/tags/%s.tar.gz", base, owner, repo, tag)

	var resp *http.Response
	var getErr error
	if cfg.HTTPClient != nil {
		resp, getErr = cfg.HTTPClient.Get(srcURL)
	} else {
		resp, getErr = httpclient.NewClient().Get(srcURL)
	}
	if getErr != nil {
		return fmt.Errorf("go: download source: %w", getErr)
	}
	defer func() { _ = resp.Body.Close() }() // best-effort cleanup

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("go: download source: HTTP %d", resp.StatusCode)
	}

	// 4. Extract into a temp directory
	workDir, err := os.MkdirTemp("", "larry-go-*")
	if err != nil {
		return fmt.Errorf("go: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }() // best-effort cleanup

	if err := extractGoSource(resp.Body, workDir); err != nil {
		return fmt.Errorf("go: extract source: %w", err)
	}

	// 5. Find the single top-level extracted directory and verify go.mod
	srcDir, err := findGoModuleRoot(workDir)
	if err != nil {
		return fmt.Errorf("go: %w", err)
	}

	// 6. Run go mod vendor
	if err := runGoModVendor(srcDir, runner); err != nil {
		return fmt.Errorf("go: go mod vendor: %w", err)
	}

	// 7. Create vendor tarball in the package directory. The ebuild's SRC_URI
	// is authoritative for both the filename and the compression: overlays in
	// the wild reference `${P}-deps.tar.gz` (committed to the overlay repo) and
	// `${P}-deps.tar.xz` (release asset). Writing a different name leaves
	// pkgdev to fetch the missing one over the network, which 404s for a
	// version that has not been pushed yet.
	pe, err := atom.ParseEbuildPath(ei.Path)
	if err != nil {
		return fmt.Errorf("go: parse ebuild path: %w", err)
	}
	depsName := goDepsDistfile(ei, pe)
	outPath := filepath.Join(filepath.Dir(ei.Path), depsName)
	distPath := filepath.Join(distDir, depsName)
	_, pkgStatErr := os.Stat(outPath)
	_, distStatErr := os.Stat(distPath)
	// A failed run must not orphan artifacts that did not exist before it:
	// `larry bump` deletes the new ebuild, and the tarball would linger
	// untracked beside it.
	defer func() {
		if retErr == nil || !cfg.CleanupOnFailure {
			return
		}
		if pkgStatErr != nil {
			_ = os.Remove(outPath) // best-effort cleanup after failure
		}
		if distStatErr != nil {
			_ = os.Remove(distPath) // best-effort cleanup after failure
		}
	}()
	if err := createGoVendorTarball(srcDir, outPath); err != nil {
		return fmt.Errorf("go: create vendor tarball: %w", err)
	}

	// 8. Copy to distdir (script writes pkg-dir first, then cp to DISTDIR)
	if err := copyToDistdir(outPath, distDir); err != nil {
		return fmt.Errorf("go: copy to distdir: %w", err)
	}

	// 9. Upload to GitHub release when the ebuild's SRC_URI fetches its deps
	// tarball from a release on the configured repo. The tag comes from the
	// ebuild's own URL (like the npm pipeline), so the asset lands exactly
	// where the ebuild will fetch it — never a guessed v${PV} tag.
	if cfg.GitHubRepo != "" {
		if tag, ok := goReleaseTag(ei, pe, cfg.GitHubRepo); ok {
			gh := github.NewClientWithRunner(runner)
			if err := npmUploadAsset(tag, filepath.Base(outPath), distDir, cfg.GitHubRepo, ei.PN(), gh.Runner()); err != nil {
				// The ebuild's SRC_URI fetches this asset — a swallowed
				// failure would ship an ebuild that 404s while larry exits 0.
				return fmt.Errorf("go: upload deps asset: %w", err)
			}
		}
	}

	// 10. Run pkgdev manifest with catPkg (plan 21)
	catPkg := pe.Category + "/" + pe.PN
	if err := runManifest(manifestRequest{
		Overlay:    cfg.Overlay,
		CatPkg:     catPkg,
		Distdir:    distDir,
		EbuildPath: ei.Path,
		Prefetch:   nil,
	}); err != nil {
		return fmt.Errorf("go: pkgdev manifest: %w", err)
	}

	return nil
}

// goDepsDistfile returns the DISTDIR filename the ebuild expects for its
// vendored dependency tarball. The ebuild's SRC_URI is authoritative: Go
// overlays reference both `${P}-deps.tar.gz` and `${P}-deps.tar.xz`. When no
// deps entry is present, the SPEC default (`${P}-deps.tar.gz`) applies.
func goDepsDistfile(ei *ebuild.EbuildInfo, pe *atom.ParsedEbuild) string {
	vars := ebuild.BuildVariableMap(ei.Raw, pe)
	if entries, err := ebuild.ParseSrcUri(ei.SRC_URI, vars); err == nil {
		for _, e := range entries {
			if isDepsDistfile(e.Filename) {
				return e.Filename
			}
		}
	}
	return ei.P() + "-deps.tar.gz"
}

// isDepsDistfile reports whether name is a Go vendor deps tarball. Names that
// belong to the npm/cargo pipelines are excluded: `pkg-1.0.0-npm-deps.tar.xz`
// also ends in `-deps.tar.xz` but is not a Go vendor archive.
func isDepsDistfile(name string) bool {
	if strings.Contains(name, "-npm-deps.") || strings.Contains(name, "-cargo-deps.") {
		return false
	}
	for _, suffix := range []string{"-deps.tar.gz", "-deps.tar.xz", "-deps.tar.zst"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// goReleaseTag returns the release tag the ebuild's SRC_URI fetches its deps
// tarball from, but only when that release lives on the configured repo.
// This mirrors the npm pipeline: the ebuild's own URL is authoritative, so
// uploads land where the ebuild fetches from instead of a guessed tag.
func goReleaseTag(ei *ebuild.EbuildInfo, pe *atom.ParsedEbuild, repo string) (string, bool) {
	releasesURL := extractReleaseURL(ei.SRC_URI)
	if releasesURL == "" {
		return "", false
	}
	m := releaseURLPattern.FindStringSubmatch(releasesURL)
	if len(m) < 5 {
		return "", false
	}
	if !strings.EqualFold(m[1]+"/"+m[2], repo) {
		return "", false
	}
	vars := ebuild.BuildVariableMap(ei.Raw, pe)
	tag := ebuild.ExpandVariables(m[3], vars)
	if tag == "" {
		return "", false
	}
	return tag, true
}

// findGoRepo extracts the GitHub owner/repo from SRC_URI or HOMEPAGE.
func findGoRepo(ei *ebuild.EbuildInfo) (owner, repo string) {
	// Try HOMEPAGE first: https://github.com/owner/repo
	if ei.HOMEPAGE != "" {
		matches := githubRe.FindStringSubmatch(ei.HOMEPAGE)
		if len(matches) == 3 {
			return matches[1], matches[2]
		}
	}

	// Try SRC_URI
	if ei.SRC_URI != "" {
		matches := githubRe.FindStringSubmatch(ei.SRC_URI)
		if len(matches) == 3 {
			return matches[1], matches[2]
		}
	}

	return "", ""
}

// tarConfinedTarget returns the destination path for a tar entry name, or an
// error when the name is absolute or escapes destDir via ".." components.
func tarConfinedTarget(destDir, name string) (string, error) {
	if name == "" || filepath.IsAbs(name) {
		return "", fmt.Errorf("tar entry %q has an absolute or empty path", name)
	}
	root := filepath.Clean(destDir)
	target := filepath.Clean(filepath.Join(root, name))
	if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return "", fmt.Errorf("tar entry %q escapes the extraction directory", name)
	}
	return target, nil
}

// tarLinkEscapes reports whether a symlink/hardlink entry's target would
// resolve outside destDir. Both resolutions are tried — relative to the
// entry's own directory (symlink semantics) and to the extraction root
// (hardlink semantics) — so a link is only rejected when it escapes both.
func tarLinkEscapes(destDir, entryDir, linkname string) bool {
	if linkname == "" {
		return false
	}
	if filepath.IsAbs(linkname) {
		return true
	}
	root := filepath.Clean(destDir)
	confined := func(p string) bool {
		return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
	}
	return !confined(filepath.Clean(filepath.Join(entryDir, linkname))) &&
		!confined(filepath.Clean(filepath.Join(root, linkname)))
}

// extractGoSource extracts a gzip tarball from the given reader into destDir.
// Uses archive/tar + compress/gzip (no subprocess). Every entry is confined
// to destDir: absolute paths and ".." traversal are rejected with an error,
// as are symlink/hardlink entries whose resolved target would escape.
func extractGoSource(r io.Reader, destDir string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("create gzip reader: %w", err)
	}
	defer func() { _ = gzr.Close() }() // best-effort close

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		target, err := tarConfinedTarget(destDir, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create dir: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create parent dir: %w", err)
			}
			outFile, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("create file: %w", err)
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				_ = outFile.Close()
				return fmt.Errorf("write file: %w", err)
			}
			_ = outFile.Close() // best-effort close after successful write
		case tar.TypeSymlink, tar.TypeLink:
			// Link entries are not materialised, but validate their targets
			// so a hostile tarball cannot smuggle an escaping link past the
			// guard.
			if tarLinkEscapes(destDir, filepath.Dir(target), header.Linkname) {
				return fmt.Errorf("tar entry %q links to %q which escapes the extraction directory", header.Name, header.Linkname)
			}
			// Confined link: skipped (not materialised), matching previous behaviour.
		}
	}

	return nil
}

// findGoModuleRoot finds the directory containing go.mod in the extracted source.
// Returns an error if there is not exactly one top-level directory.
func findGoModuleRoot(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("read extracted source: %w", err)
	}

	// Collect top-level directories
	var topDirs []string
	for _, e := range entries {
		if e.IsDir() {
			topDirs = append(topDirs, filepath.Join(root, e.Name()))
		}
	}

	if len(topDirs) != 1 {
		return "", fmt.Errorf("expected exactly one top-level directory in extracted source, found %d", len(topDirs))
	}

	srcDir := topDirs[0]

	// Check for go.mod
	if _, err := os.Stat(filepath.Join(srcDir, "go.mod")); err != nil {
		return "", fmt.Errorf("no go.mod found in extracted source directory")
	}

	return srcDir, nil
}

// runGoModVendor runs go mod vendor in the given directory.
func runGoModVendor(dir string, runner github.CmdRunner) error {
	cmd := exec.Command("go", "mod", "vendor")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return runner.Run(cmd)
}

// createGoVendorTarball creates a gzip- or xz-compressed tarball of the
// vendor/ directory at outPath, choosing the compressor from the extension.
// The tarball root is vendor/ (matching what the go-module eclass expects).
func createGoVendorTarball(srcDir, outPath string) error {
	vendorDir := filepath.Join(srcDir, "vendor")
	if _, err := os.Stat(vendorDir); err != nil {
		return fmt.Errorf("vendor directory not found: %s", vendorDir)
	}

	// Reject unsupported formats before creating (and leaving) an empty file.
	ext := filepath.Ext(outPath)
	if ext != ".gz" && ext != ".xz" {
		return fmt.Errorf("unsupported deps tarball extension %q (want .tar.gz or .tar.xz)", ext)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	outFile, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create tarball: %w", err)
	}
	defer func() { _ = outFile.Close() }() // best-effort close

	var comp io.WriteCloser
	switch ext {
	case ".gz":
		comp, err = gzip.NewWriterLevel(outFile, gzip.BestCompression)
		if err != nil {
			return fmt.Errorf("create gzip writer: %w", err)
		}
	case ".xz":
		comp, err = xz.WriterConfig{DictCap: 64 * 1024 * 1024}.NewWriter(outFile)
		if err != nil {
			return fmt.Errorf("create xz writer: %w", err)
		}
	}
	defer func() { _ = comp.Close() }() // best-effort close

	tr := tar.NewWriter(comp)
	defer func() { _ = tr.Close() }() // best-effort close

	// Walk vendor/ and write entries with "vendor/" prefix
	return filepath.Walk(vendorDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Compute the name relative to srcDir (so vendor/ is at the root)
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("compute rel path: %w", err)
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("create tar header: %w", err)
		}
		hdr.Name = rel

		if err := tr.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write tar header: %w", err)
		}

		// For regular files, copy content
		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("open file for tar: %w", err)
			}
			defer func() { _ = f.Close() }() // best-effort close
			if _, err := io.Copy(tr, f); err != nil {
				return fmt.Errorf("write tar data: %w", err)
			}
		}

		return nil
	})
}

// copyToDistdir copies a tarball to the distdir.
func copyToDistdir(src, distDir string) error {
	// Ensure distdir exists
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return fmt.Errorf("create distdir: %w", err)
	}

	// Read source
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}

	// Write to distdir
	dst := filepath.Join(distDir, filepath.Base(src))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("write to distdir: %w", err)
	}

	return nil
}


