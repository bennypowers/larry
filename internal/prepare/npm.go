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
	"bennypowers.dev/larry/internal/logger"
)

// npmPrepare implements the npm type-specific prepare pipeline.
// Steps:
// 1. Resolve SRC_URI → npm registry URL
// 2. Download distfile to DISTDIR
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

	// Backup before mutations
	if err := Backup(ei.Path); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer os.Remove(ei.Path + ".bak")

	// 1. Resolve SRC_URI
	resolvedURL := npmFirstRegistryURL(ei)
	if resolvedURL == "" {
		Restore(ei.Path)
		return fmt.Errorf("npm: could not resolve SRC_URI to npm registry URL")
	}

	// 2. Download distfile to distdir
	client := httpclient.NewClient()
	downloaded, err := npmDownloadDistfile(resolvedURL, distDir, client)
	if err != nil {
		Restore(ei.Path)
		return fmt.Errorf("download distfile: %w", err)
	}

	// Skip copy if download already saved directly to distdir
	downloadedBase := filepath.Base(downloaded)
	if distDir != "" && downloadedBase != "" {
		distPath := filepath.Join(distDir, downloadedBase)
		if distPath != downloaded {
			if err := npmCopyToDistdir(downloaded, distDir); err != nil {
				Restore(ei.Path)
				return fmt.Errorf("copy to distdir: %w", err)
			}
		}
	}

	// 2b. Upload vendored deps to GitHub release if SRC_URI references one
	releasesURL := extractReleaseURL(ei.SRC_URI)
	if releasesURL != "" {
		logger.Debug("found release URL: " + releasesURL)
		rawTag, rawAssetName := parseReleaseURL(releasesURL)
		vars := npmVars(ei)
		tag := ebuild.ExpandVariables(rawTag, vars)
		assetName := ebuild.ExpandVariables(rawAssetName, vars)
		logger.Debug("release tag: " + tag + " asset: " + assetName)
		if tag != "" && assetName != "" {
			assetPath := filepath.Join(distDir, assetName)
			gh := github.NewClient()
			if _, err := os.Stat(assetPath); err == nil {
				// Check if the existing tarball has the correct npm-cache/ format.
				// Old tarballs created by larry had node_modules/ which is incompatible
				// with `npm --offline --cache` in the ebuild. Regenerate if needed.
				if !npmDepsTarballHasCache(assetPath) {
					logger.Debug("asset has wrong format (node_modules instead of npm-cache), regenerating...")
					if err := npmCreateDepsTarball(downloaded, distDir, assetName); err != nil {
						Restore(ei.Path)
						return fmt.Errorf("npm: create deps tarball: %w", err)
					}
					logger.Debug("asset regenerated, uploading...")
				} else {
					logger.Debug("uploading asset: " + assetPath)
				}
				exists, err := ghReleaseViewExists(tag, cfg.GitHubRepo, gh.Runner())
				if err != nil {
					Restore(ei.Path)
					return fmt.Errorf("npm: check release %s: %w", tag, err)
				}
				if !exists {
					if err := ghReleaseCreate(tag, cfg.GitHubRepo, gh.Runner()); err != nil {
						Restore(ei.Path)
						return fmt.Errorf("npm: create release %s: %w", tag, err)
					}
				}
				if err := ghUploadAsset(tag, assetPath, cfg.GitHubRepo, gh.Runner()); err != nil {
					Restore(ei.Path)
					return fmt.Errorf("npm: upload asset %s: %w", assetName, err)
				}
			} else {
				// Asset not in distdir — create it from npm package
				logger.Debug("asset not in distdir, creating...")
				if err := npmCreateDepsTarball(downloaded, distDir, assetName); err != nil {
					Restore(ei.Path)
					return fmt.Errorf("npm: create deps tarball: %w", err)
				}
				logger.Debug("asset created, uploading...")
				if err := npmUploadAsset(tag, assetName, distDir, cfg.GitHubRepo, gh.Runner()); err != nil {
					Restore(ei.Path)
					return fmt.Errorf("npm: upload asset: %w", err)
				}
				// Remove stale Manifest (first run failed before fetching)
				manifestPath := filepath.Join(filepath.Dir(ei.Path), "Manifest")
				_ = os.Remove(manifestPath)
			}
		}
	} else {
		logger.Debug("no GitHub release URL found in SRC_URI")
	}

	// 3. Run pkgdev manifest
	if err := runManifest(manifestRequest{
		Overlay:    cfg.Overlay,
		CatPkg:     ei.Category + "/" + ei.PN(),
		Distdir:    distDir,
		EbuildPath: ei.Path,
		Prefetch:   nil,
	}); err != nil {
		Restore(ei.Path)
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

// npmVars builds a VariableMap from the ebuild info using the shared expander.
func npmVars(ei *ebuild.EbuildInfo) ebuild.VariableMap {
	pe, err := atom.ParseEbuildPath(ei.Path)
	if err != nil {
		pe = nil
	}
	vars := ebuild.BuildVariableMap(ei.Raw, pe)
	// Fallback: when pe is nil (e.g. test fixtures without a real path),
	// set core vars from EbuildInfo fields so expansion still works.
	if pe == nil {
		if _, ok := vars["PN"]; !ok {
			vars["PN"] = ei.PN()
		}
		if _, ok := vars["PV"]; !ok {
			vars["PV"] = ei.PV()
		}
		if _, ok := vars["P"]; !ok {
			vars["P"] = ei.P()
		}
	}
	return vars
}

// npmFirstRegistryURL reproduces the observable behaviour of the old
// npmResolveSRCURI on top of the shared expander. Plan 22a replaces
// this with ParseSrcUri.
func npmFirstRegistryURL(ei *ebuild.EbuildInfo) string {
	vars := npmVars(ei)
	expanded := strings.TrimSpace(ebuild.ExpandVariables(ei.SRC_URI, vars))
	if idx := strings.Index(expanded, "\n"); idx >= 0 {
		expanded = strings.TrimSpace(expanded[:idx])
	}
	const prefix = "mirror://npm/"
	if !strings.HasPrefix(expanded, prefix) {
		return ""
	}
	path := strings.TrimPrefix(expanded, prefix)
	if idx := strings.Index(path, " -> "); idx >= 0 {
		path = path[:idx]
	}
	return "https://registry.npmjs.org/" + path
}

// npmExtractDistfileName extracts the distfile name from an npm registry URL.
func npmExtractDistfileName(url string) string {
	// Handle npm scoped packages:
	// https://registry.npmjs.org/@scope/pkg/-/@scope/pkg-1.0.0.tgz
	if idx := strings.LastIndex(url, "/-"); idx >= 0 {
		return strings.TrimPrefix(url[idx+2:], "/")
	}
	// Fallback: take last path segment
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return strings.TrimPrefix(parts[len(parts)-1], "/")
	}
	return ""
}

// npmDownloadDistfile downloads a distfile from the resolved URL to destDir.
func npmDownloadDistfile(url, destDir string, client *httpclient.Client) (string, error) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("npm: unsupported URL scheme: %s", url)
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("npm: GET request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm: HTTP %d for %s", resp.StatusCode, url)
	}

	// Determine destination filename
	distName := npmExtractDistfileName(url)
	if distName == "" {
		distName = "downloaded.tgz"
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

	destPath := filepath.Join(destDir, distName)

	out, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("npm: create dest file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", fmt.Errorf("npm: write distfile: %w", err)
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
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("npm: create dest: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("npm: copy distfile: %w", err)
	}

	return nil
}

// npmUploadAsset uploads a deps tarball to the GitHub release.
func npmUploadAsset(tag, assetName, distDir, repo string, runner github.CmdRunner) error {
	// Check if release exists
	exists, err := ghReleaseViewExists(tag, repo, runner)
	if err != nil {
		return fmt.Errorf("check release %s: %w", tag, err)
	}
	if !exists {
		if err := ghReleaseCreate(tag, repo, runner); err != nil {
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
	defer f.Close()

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
		defer gr.Close()
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
	defer os.RemoveAll(tmpDir)

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
	var cmd *exec.Cmd
	cmd = exec.Command("npm", "install", "--ignore-scripts", "--cache", npmCacheDir, npmTarball)
	cmd.Dir = tmpDir
	logger.Debug("running: " + strings.Join(cmd.Args, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprint(os.Stderr, string(output))
		return fmt.Errorf("npm install: %w", err)
	}

	// Create deps tarball from the npm cache directory
	outputTarball := filepath.Join(tmpDir, assetName)
	cmd = exec.Command("tar", "cJf", outputTarball, "-C", tmpDir, "npm-cache")
	if err := cmd.Run(); err != nil {
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

// ---------------------------------------------------------------------------
// Vendored helpers (use existing functions from vendored.go)
// These are called from npm pipeline when SRC_URI includes GitHub releases URLs.
// ---------------------------------------------------------------------------