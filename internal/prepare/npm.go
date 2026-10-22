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
	"bennypowers.dev/larry/internal/output"
)

// npmRunner executes the npm subprocess. Tests replace it.
var npmRunner = func(cmd *exec.Cmd) error {
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprint(os.Stderr, string(out))
		return err
	}
	return nil
}

// npmRegistryBase is the npm registry base URL used to resolve npm
// distfiles (both mirror://npm/ and direct registry.npmjs.org forms are
// rewritten onto it). Tests replace it to point at an httptest server.
var npmRegistryBase = "https://registry.npmjs.org/"

// npmDownloadURL rewrites any npm registry URL — the legacy mirror://npm/
// scheme or a direct registry.npmjs.org URL — onto npmRegistryBase with the
// registry-relative path, so tests can redirect downloads.
func npmDownloadURL(url string) string {
	for _, prefix := range []string{"mirror://npm/", "https://registry.npmjs.org/", "http://registry.npmjs.org/"} {
		if rest, ok := strings.CutPrefix(url, prefix); ok {
			return npmRegistryBase + rest
		}
	}
	return url
}

// archiveRunner executes the tar subprocess. Tests replace it.
var archiveRunner = func(cmd *exec.Cmd) error { return cmd.Run() }

// npmPrepare implements the npm type-specific prepare pipeline.
// Steps:
// 1. Parse SRC_URI entries, apply the ${P} tag guard, download all npm entries
// 2. Upload vendored deps to GitHub release if SRC_URI references one
// 3. Run pkgdev manifest
// On failure: restore from backup.
func npmPrepare(ei *ebuild.EbuildInfo, cfg *PrepareConfig) (retErr error) {
	if cfg.DryRun {
		return nil
	}

	distDir := cfg.Distdir
	if distDir == "" {
		distDir = getEnv("PORTAGE_DISTDIR")
	}

	// Guard (D4): reject ${P} in release tag before any work.
	if strings.Contains(ei.Raw, "releases/download/${P}/") {
		return fmt.Errorf("npm: SRC_URI uses ${P} as release tag but deps upload to the ${PN} tag; change releases/download/${P}/ to releases/download/${PN}/")
	}

	// Backup before mutations
	if err := Backup(ei.Path); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer func() { _ = os.Remove(ei.Path + ".bak") }() // best-effort cleanup

	// 1. Parse SRC_URI entries using the shared expander.
	pe, err := atom.ParseEbuildPath(ei.Path)
	if err != nil {
		pe = nil
	}
	vars := ebuild.BuildVariableMap(ei.Raw, pe)
	entries, err := ebuild.ParseSrcUri(ei.SRC_URI, vars)
	if err != nil {
		return fmt.Errorf("npm: parse SRC_URI: %w", err)
	}

	// Filter to npm entries (mirror://npm/ or direct registry URLs).
	var npmEntries []ebuild.SrcUriEntry
	for _, e := range entries {
		if ebuild.IsNpmRegistryURL(e.URL) {
			npmEntries = append(npmEntries, e)
		}
	}
	if len(npmEntries) == 0 {
		_ = Restore(ei.Path) // best-effort recovery after failure
		return fmt.Errorf("npm: no mirror://npm/ or registry.npmjs.org/ entries found in SRC_URI of %s", ei.Path)
	}

	// Download every npm entry to distdir/<Filename>.
	var client *httpclient.Client
	if cfg.HTTPClient != nil {
		client = httpclient.NewClientWithTransport(cfg.HTTPClient.Transport)
	} else {
		client = httpclient.NewClient()
	}
	for _, e := range npmEntries {
		destPath := filepath.Join(distDir, e.Filename)
		// Skip if file already exists and is non-empty. Safe: downloads are
		// atomic (npmDownloadDistfile renames into place only after the full
		// body matches Content-Length), so a non-empty file here is always a
		// complete download — a short one never leaves a file behind.
		if info, err := os.Stat(destPath); err == nil && info.Size() > 0 {
			continue
		}
		rewritten := npmDownloadURL(e.URL)
		if _, err := npmDownloadDistfile(rewritten, distDir, e.Filename, client); err != nil {
			_ = Restore(ei.Path) // best-effort recovery after failure
			return fmt.Errorf("npm: download distfile %s: %w", e.Filename, err)
		}
	}

	// Collect all distfile names for manifest verification.
	var distfileNames []string
	for _, e := range entries {
		distfileNames = append(distfileNames, e.Filename)
	}

	// The "main" distfile for the rest of the pipeline is entries[0].Filename.
	mainDistfile := npmEntries[0].Filename

	// 2b. Upload vendored deps to GitHub release if SRC_URI references one
	releasesURL := extractReleaseURL(ei.SRC_URI)
	if releasesURL != "" {
		output.Debug("found release URL: " + releasesURL)
		rawTag, rawAssetName := parseReleaseURL(releasesURL)
		tag := ebuild.ExpandVariables(rawTag, vars)
		assetName := ebuild.ExpandVariables(rawAssetName, vars)
		output.Debug("release tag: " + tag + " asset: " + assetName)
		if tag != "" && assetName != "" {
			// The asset name reaches several filepath.Join sites (DISTDIR,
			// deps-tarball temp dir) — the regex group admits slashes, so a
			// crafted releases/download URL could traverse out of them.
			// Reject anything that is not a plain basename.
			if assetName == "." || assetName == ".." || filepath.Base(assetName) != assetName {
				_ = Restore(ei.Path) // best-effort recovery after failure
				return fmt.Errorf("npm: invalid release asset name %q in %s (must be a plain filename)", assetName, releasesURL)
			}
			assetPath := filepath.Join(distDir, assetName)
			gh := github.NewClient()
			if cfg.Runner != nil {
				gh = github.NewClientWithRunner(cfg.Runner)
			}
			if _, err := os.Stat(assetPath); err == nil {
				// Check if the existing tarball has the correct npm-cache/ format.
				// Old tarballs created by larry had node_modules/ which is incompatible
				// with `npm --offline --cache` in the ebuild. Regenerate if needed.
				if !npmDepsTarballHasCache(assetPath) {
					output.Debug("asset has wrong format (node_modules instead of npm-cache), regenerating...")
				if err := npmCreateDepsTarball(mainDistfile, distDir, assetName); err != nil {
					_ = Restore(ei.Path) // best-effort recovery after failure
					return fmt.Errorf("npm: create deps tarball: %w", err)
				}
				output.Debug("asset regenerated, uploading...")
			} else {
				output.Debug("uploading asset: " + assetPath)
			}
			exists, err := ghReleaseViewExists(tag, cfg.GitHubRepo, gh.Runner())
			if err != nil {
				_ = Restore(ei.Path) // best-effort recovery after failure
				return fmt.Errorf("npm: check release %s: %w", tag, err)
			}
			if !exists {
				if err := ghReleaseCreate(tag, cfg.GitHubRepo, ei.PN(), gh.Runner()); err != nil {
					_ = Restore(ei.Path) // best-effort recovery after failure
					return fmt.Errorf("npm: create release %s: %w", tag, err)
				}
			}
			if err := ghUploadAsset(tag, assetPath, cfg.GitHubRepo, gh.Runner()); err != nil {
				_ = Restore(ei.Path) // best-effort recovery after failure
				return fmt.Errorf("npm: upload asset %s: %w", assetName, err)
			}
		} else {
			// Asset not in distdir — create it from npm package
			output.Debug("asset not in distdir, creating...")
			if err := npmCreateDepsTarball(mainDistfile, distDir, assetName); err != nil {
				_ = Restore(ei.Path) // best-effort recovery after failure
				return fmt.Errorf("npm: create deps tarball: %w", err)
			}
			output.Debug("asset created, uploading...")
			if err := npmUploadAsset(tag, assetName, distDir, cfg.GitHubRepo, ei.PN(), gh.Runner()); err != nil {
				_ = Restore(ei.Path) // best-effort recovery after failure
				return fmt.Errorf("npm: upload asset %s: %w", assetName, err)
			}
		}
		}
	} else {
		output.Debug("no GitHub release URL found in SRC_URI")
	}

	// 3. Run pkgdev manifest
	if err := runManifest(manifestRequest{
		Overlay:    cfg.Overlay,
		CatPkg:     ei.Category + "/" + ei.PN(),
		Distdir:    distDir,
		EbuildPath: ei.Path,
		Distfiles:  distfileNames,
		Prefetch:   npmPrefetchPackage,
	}); err != nil {
		_ = Restore(ei.Path) // best-effort recovery after failure
		return fmt.Errorf("npm: manifest: %w", err)
	}

	return nil
}


// releaseURLPattern matches GitHub releases download URLs.
// Example: https://github.com/owner/repo/releases/download/tag/file.tar.xz
var releaseURLPattern = regexp.MustCompile(
	`https://github\.com/([^/]+)/([^/]+)/releases/download/([^/]+)/([^?]+)`,
)

// extractReleaseURL finds a GitHub releases/download URL in the SRC_URI.
func extractReleaseURL(srcURI string) string {
	for _, part := range strings.Fields(srcURI) {
		if releaseURLPattern.MatchString(part) {
			return part
		}
	}
	return ""
}

// parseReleaseURL extracts tag and asset name from a GitHub release URL.
func parseReleaseURL(url string) (tag, assetName string) {
	matches := releaseURLPattern.FindStringSubmatch(url)
	if len(matches) < 5 {
		return "", ""
	}
	return matches[3], matches[4]
}

// atomicWriteBody streams body to destPath+".part" and renames it into place
// only after the full body copies successfully and matches contentLength
// (when the server sent one, i.e. contentLength >= 0). On any error the
// .part file is removed and nothing is left at destPath, so a later run
// never mistakes a short download for a complete file.
func atomicWriteBody(body io.Reader, destPath string, contentLength int64) error {
	partPath := destPath + ".part"

	out, err := os.Create(partPath)
	if err != nil {
		return fmt.Errorf("create dest file: %w", err)
	}

	written, copyErr := io.Copy(out, body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(partPath)
		return fmt.Errorf("write distfile: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(partPath)
		return fmt.Errorf("close distfile: %w", closeErr)
	}
	if contentLength >= 0 && written != contentLength {
		_ = os.Remove(partPath)
		return fmt.Errorf("truncated: wrote %d bytes, Content-Length %d", written, contentLength)
	}
	if err := os.Rename(partPath, destPath); err != nil {
		_ = os.Remove(partPath)
		return fmt.Errorf("finalize distfile: %w", err)
	}
	return nil
}

// npmDownloadDistfile downloads a distfile from the resolved URL to destDir.
// The filename is the DISTDIR basename to use (not derived from the URL).
// The download is atomic: the body is written to <dest>.part and renamed into
// place only after the full body copies successfully and matches Content-Length
// (when the server sends one). A failed or short download therefore never
// leaves a file that a later run treats as complete.
func npmDownloadDistfile(url, destDir, filename string, client *httpclient.Client) (string, error) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("npm: unsupported URL scheme: %s", url)
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("npm: GET request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // best-effort cleanup

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm: HTTP %d for %s", resp.StatusCode, url)
	}

	// Create destDir if it doesn't exist
	if destDir != "" {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return "", fmt.Errorf("npm: create distdir: %w", err)
		}
	} else {
		// Use absolute path so tar works when cmd.Dir changes
		cwd, _ := os.Getwd()
		destDir = cwd
	}

	// Defensive basename check: the rename target from SRC_URI (or a release
	// asset name) must never escape DISTDIR via "/", ".." or an absolute path.
	if filename == "" || filename == "." || filename == ".." || filepath.Base(filename) != filename {
		return "", fmt.Errorf("npm: invalid distfile name %q", filename)
	}

	destPath := filepath.Join(destDir, filename)
	if err := atomicWriteBody(resp.Body, destPath, resp.ContentLength); err != nil {
		return "", fmt.Errorf("npm: %w", err)
	}

	return destPath, nil
}

// npmCopyToDistdir copies a file to the DISTDIR.
func npmCopyToDistdir(srcPath, distDir string) error {
	if distDir == "" {
		return nil
	}

	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("npm: stat source: %w", err)
	}

	if srcInfo.IsDir() {
		return nil
	}

	// Create distdir if needed
	if err := os.MkdirAll(distDir, 0755); err != nil {
		return fmt.Errorf("npm: create distdir: %w", err)
	}

	destPath := filepath.Join(distDir, filepath.Base(srcPath))

	// Copy file
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("npm: open source: %w", err)
	}
	defer func() { _ = src.Close() }() // best-effort close

	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("npm: create dest: %w", err)
	}
	defer func() { _ = dst.Close() }() // best-effort close

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("npm: copy distfile: %w", err)
	}

	return nil
}

// npmUploadAsset uploads a deps tarball to the GitHub release.
func npmUploadAsset(tag, assetName, distDir, repo, pn string, runner github.CmdRunner) error {
	// Check if release exists
	exists, err := ghReleaseViewExists(tag, repo, runner)
	if err != nil {
		return fmt.Errorf("check release %s: %w", tag, err)
	}
	if !exists {
		if err := ghReleaseCreate(tag, repo, pn, runner); err != nil {
			return fmt.Errorf("create release %s: %w", tag, err)
		}
	}

	assetPath := filepath.Join(distDir, assetName)
	if err := ghUploadAsset(tag, assetPath, repo, runner); err != nil {
		return fmt.Errorf("upload asset %s: %w", assetName, err)
	}
	fmt.Fprintln(os.Stderr, "[larry] npm: asset uploaded to GitHub release")
	return nil
}

// npmCreateDepsTarball extracts an npm package, runs npm ci, and packages
// node_modules into a tarball for the DISTDIR.
// npmDepsTarballHasCache checks if a deps tarball has the correct npm-cache/
// format (required for `npm --offline --cache` in ebuilds). Returns false if
// the tarball has the old node_modules/ format or can't be read.
func npmDepsTarballHasCache(tarballPath string) bool {
	f, err := os.Open(tarballPath)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }() // best-effort close

	var reader io.Reader
	switch {
	case strings.HasSuffix(tarballPath, ".xz"):
		xr, err2 := xz.NewReader(f)
		if err2 != nil {
			return false
		}
		reader = xr
	case strings.HasSuffix(tarballPath, ".gz") || strings.HasSuffix(tarballPath, ".tgz"):
		gr, err2 := gzip.NewReader(f)
		if err2 != nil {
			return false
		}
		defer func() { _ = gr.Close() }() // best-effort close
		reader = gr
	default:
		return false
	}

	tr := tar.NewReader(reader)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return false
		}
		if err != nil {
			return false
		}
		if hdr.Name == "npm-cache/" || strings.HasPrefix(hdr.Name, "npm-cache/") {
			return true
		}
	}
}

func npmCreateDepsTarball(npmTarball, distDir, assetName string) error {
	tmpDir, err := os.MkdirTemp("", "larry-npm-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }() // best-effort cleanup

	// Create a dedicated npm cache directory (matching fish script: ./npm-cache)
	npmCacheDir := filepath.Join(tmpDir, "npm-cache")
	if err := os.MkdirAll(npmCacheDir, 0755); err != nil {
		return fmt.Errorf("create npm cache dir: %w", err)
	}

	// Create .npmrc to suppress audit/fund prompts in temp dir
	npmrc := filepath.Join(tmpDir, ".npmrc")
	if err := os.WriteFile(npmrc, []byte("audit=false\nfund=false\n"), 0644); err != nil {
		return fmt.Errorf("write .npmrc: %w", err)
	}

	// Run npm install with --cache, passing the .tgz directly.
	// This matches the fish script: npm --cache ./npm-cache install "$DISTDIR/$distfile_name"
	// Passing the .tgz directly ensures npm reads the package.json/npm-shrinkwrap.json
	// from inside the tarball and resolves all transitive dependencies through the cache.
	// Unlike extracting first (which triggers shrinkwrap's resolved URLs to bypass cache),
	// passing the .tgz directly causes npm to resolve from the registry cache.
	// --ignore-scripts mirrors the vendored pipeline's npm ci: package
	// lifecycle scripts must never run on the workstation, and they only
	// affect node_modules — never the _cacache cache we tar.
	// npmTarball is a filename relative to distDir — resolve to the full path.
	if !filepath.IsAbs(npmTarball) {
		npmTarball = filepath.Join(distDir, npmTarball)
	}
	cmd := exec.Command("npm", "--cache", npmCacheDir, "install", npmTarball, "--ignore-scripts")
	cmd.Dir = tmpDir
	output.Debug("running: " + strings.Join(cmd.Args, " "))
	if err := npmRunner(cmd); err != nil {
		return fmt.Errorf("npm install: %w", err)
	}

	// Create deps tarball from the npm cache directory
	outputTarball := filepath.Join(tmpDir, assetName)
	cmd = exec.Command("tar", "-caf", outputTarball, "-C", tmpDir, "npm-cache")
	if err := archiveRunner(cmd); err != nil {
		return fmt.Errorf("create deps tarball: %w", err)
	}

	// Copy to distdir (or CWD if distdir is empty)
	destDir := distDir
	if destDir == "" {
		destDir = "."
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}
	destPath := filepath.Join(destDir, assetName)
	data, err := os.ReadFile(outputTarball)
	if err != nil {
		return fmt.Errorf("read tarball: %w", err)
	}
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return fmt.Errorf("write distfile: %w", err)
	}

	return nil
}

// npmPrefetchPackage downloads, into distdir, every npm distfile (mirror://npm/
// or direct registry.npmjs.org URL) declared by every *.ebuild in pkgDir. Files
// already present and non-empty are skipped. Per-ebuild read/parse errors are
// ordinary "not an npm ebuild" cases and are skipped (logged at debug level);
// a download failure for a genuinely missing distfile is fatal — pkgdev cannot
// resolve mirror://npm/ itself, so letting it through would only surface later
// as a confusing fetch error.
func npmPrefetchPackage(pkgDir, distdir string) error {
	// Create distdir if it doesn't exist
	if err := os.MkdirAll(distdir, 0755); err != nil {
		return fmt.Errorf("create distdir: %w", err)
	}

	// Glob for *.ebuild files (not .bak)
	files, err := filepath.Glob(filepath.Join(pkgDir, "*.ebuild"))
	if err != nil {
		return fmt.Errorf("glob ebuilds: %w", err)
	}
	if len(files) == 0 {
		return nil
	}

	// Create HTTP client for downloads
	client := httpclient.NewClient()

	for _, ebuildPath := range files {
		// Skip .bak files
		if strings.HasSuffix(ebuildPath, ".ebuild.bak") {
			continue
		}

		// Read raw content
		data, err := os.ReadFile(ebuildPath)
		if err != nil {
			output.Debugf("npm prefetch: skip %s: %v", ebuildPath, err)
			continue
		}

		// Parse ebuild path for PN/PV/P (Portage core variables)
		pe, pathErr := atom.ParseEbuildPath(ebuildPath)
		if pathErr != nil {
			output.Debugf("npm prefetch: skip %s: %v", ebuildPath, pathErr)
			continue
		}

		// Parse ebuild to extract SRC_URI and variables
		info, err := ebuild.ParseEbuildFromFile(ebuildPath)
		if err != nil {
			output.Debugf("npm prefetch: skip %s: %v", ebuildPath, err)
			continue
		}

		// Build variable map with core Portage variables from ebuild path
		vars := ebuild.BuildVariableMap(string(data), pe)

		// Parse SRC_URI
		entries, err := ebuild.ParseSrcUri(info.SRC_URI, vars)
		if err != nil {
			return fmt.Errorf("npm prefetch: parse SRC_URI of %s: %w", ebuildPath, err)
		}

		// Download every npm entry that's missing
		for _, entry := range entries {
			if !ebuild.IsNpmRegistryURL(entry.URL) {
				continue
			}

			// Skip if already exists and is non-empty. Safe: npmDownloadDistfile
			// writes atomically, so a non-empty file is always a complete
			// download — a short one never leaves a file behind.
			destPath := filepath.Join(distdir, entry.Filename)
			if info, err := os.Stat(destPath); err == nil && info.Size() > 0 {
				continue
			}

			// Download
			rewritten := npmDownloadURL(entry.URL)
			if _, err := npmDownloadDistfile(rewritten, distdir, entry.Filename, client); err != nil {
				return fmt.Errorf("npm prefetch %s: %w", entry.Filename, err)
			}
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Vendored helpers (use existing functions from vendored.go)
// These are called from npm pipeline when SRC_URI includes GitHub releases URLs.
// ---------------------------------------------------------------------------