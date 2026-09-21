package prepare

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

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
	resolvedURL := npmResolveSRCURI(ei.SRC_URI, ei)
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
		tag := expandEbuildVars(rawTag, ei)
		assetName := expandEbuildVars(rawAssetName, ei)
		logger.Debug("release tag: " + tag + " asset: " + assetName)
		if tag != "" && assetName != "" {
			assetPath := filepath.Join(distDir, assetName)
			if _, err := os.Stat(assetPath); err == nil {
				logger.Debug("uploading asset: " + assetPath)
				gh := github.NewClient()
				exists, err := ghReleaseViewExists(tag, gh.Runner())
				if err != nil {
					Restore(ei.Path)
					return fmt.Errorf("npm: check release %s: %w", tag, err)
				}
				if !exists {
					if err := ghReleaseCreate(tag, gh.Runner()); err != nil {
						Restore(ei.Path)
						return fmt.Errorf("npm: create release %s: %w", tag, err)
					}
				}
				if err := ghUploadAsset(tag, assetPath, gh.Runner()); err != nil {
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
				if err := npmUploadAsset(tag, assetName, distDir); err != nil {
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
	if err := runManifest(cfg.Overlay, ei.Path, distDir); err != nil {
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

// npmResolveSRCURI converts a mirror://npm/ SRC_URI to an npm registry URL.
// Handles ${MY_NPM_SCOPE} for scoped packages like @scope/pkg.
// Strips Gentoo's -> rename syntax (e.g., "pkg.tgz -> renamename.tgz" -> "pkg.tgz").
func npmResolveSRCURI(srcURI string, ei *ebuild.EbuildInfo) string {
	vars := map[string]string{
		"${PN}":           ei.PN(),
		"${PV}":           ei.PV(),
		"${P}":            ei.P(),
		"${MY_NPM_SCOPE}": extractVar(ei.Raw, "MY_NPM_SCOPE"),
		"${MY_NPM_NAME}":  extractVar(ei.Raw, "MY_NPM_NAME"),
	}

	resolved := srcURI
	for k, v := range vars {
		if v != "" {
			resolved = strings.ReplaceAll(resolved, k, v)
		}
	}

	// Strip leading/trailing whitespace and newlines from multi-line SRC_URI
	resolved = strings.TrimSpace(resolved)

	// Take only the first URL line (in case of multi-line SRC_URI)
	if idx := strings.Index(resolved, "\n"); idx >= 0 {
		resolved = strings.TrimSpace(resolved[:idx])
	}

	// Check for mirror://npm/ pattern
	const prefix = "mirror://npm/"
	if !strings.HasPrefix(resolved, prefix) {
		return ""
	}

	path := strings.TrimPrefix(resolved, prefix)

	// Strip Gentoo's -> rename syntax
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

	destPath := filepath.Join(destDir, distName)

	// Create destDir if it doesn't exist
	if destDir != "" {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return "", fmt.Errorf("npm: create distdir: %w", err)
		}
	}

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
func npmUploadAsset(tag, assetName, distDir string) error {
	gh := github.NewClient()

	// Check if release exists
	exists, err := ghReleaseViewExists(tag, gh.Runner())
	if err != nil {
		return fmt.Errorf("check release %s: %w", tag, err)
	}
	if !exists {
		if err := ghReleaseCreate(tag, gh.Runner()); err != nil {
			return fmt.Errorf("create release %s: %w", tag, err)
		}
	}

	assetPath := filepath.Join(distDir, assetName)
	if err := ghUploadAsset(tag, assetPath, gh.Runner()); err != nil {
		return fmt.Errorf("upload asset %s: %w", assetName, err)
	}
	fmt.Fprintln(os.Stderr, "[larry] npm: asset uploaded to GitHub release")
	return nil
}

// npmCreateDepsTarball extracts an npm package, runs npm ci, and packages
// node_modules into a tarball for the DISTDIR.
func npmCreateDepsTarball(npmTarball, distDir, assetName string) error {
	tmpDir, err := os.MkdirTemp("", "larry-npm-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Extract npm package
	extractDir := filepath.Join(tmpDir, "pkg")
	if err := extractTarball(npmTarball, extractDir); err != nil {
		return fmt.Errorf("extract npm package: %w", err)
	}

	// Find package.json — it might be at root or in a subdirectory
	var pkgDir string
	if _, err := os.Stat(filepath.Join(extractDir, "package.json")); err == nil {
		pkgDir = extractDir
	} else {
		// Look for package.json in first-level subdirectories
		entries, err := os.ReadDir(extractDir)
		if err != nil {
			return fmt.Errorf("list extracted contents: %w", err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				if _, err := os.Stat(filepath.Join(extractDir, entry.Name(), "package.json")); err == nil {
					pkgDir = filepath.Join(extractDir, entry.Name())
					break
				}
			}
		}
	}

	if pkgDir == "" {
		return fmt.Errorf("package.json not found in extracted npm package")
	}

	// Create .npmrc to suppress audit/fund prompts
	npmrc := filepath.Join(pkgDir, ".npmrc")
	if err := os.WriteFile(npmrc, []byte("audit=false\nfund=false\n"), 0644); err != nil {
		return fmt.Errorf("write .npmrc: %w", err)
	}

	// Check for lockfile — prefer npm-shrinkwrap.json, then package-lock.json
	lockfile := ""
	for _, name := range []string{"npm-shrinkwrap.json", "package-lock.json"} {
		if _, err := os.Stat(filepath.Join(pkgDir, name)); err == nil {
			lockfile = name
			break
		}
	}

	// Run npm ci (with lockfile) or npm install (without)
	var cmd *exec.Cmd
	if lockfile != "" {
		cmd = exec.Command("npm", "ci", "--ignore-scripts")
	} else {
		cmd = exec.Command("npm", "install", "--ignore-scripts")
	}
	cmd.Dir = pkgDir
	logger.Debug("running: " + strings.Join(cmd.Args, " "))
	if output, err := cmd.CombinedOutput(); err != nil {
		// If npm ci failed (lockfile out of sync), fall back to npm install
		if cmd.Args[1] == "ci" {
			logger.Debug("npm ci failed, falling back to npm install...")
			cmd = exec.Command("npm", "install", "--ignore-scripts")
			cmd.Dir = pkgDir
			output, err = cmd.CombinedOutput()
		}
		if err != nil {
			fmt.Fprint(os.Stderr, string(output))
			return fmt.Errorf("npm %s: %w", cmd.Args[1], err)
		}
	}

	// Create deps tarball
	outputTarball := filepath.Join(tmpDir, assetName)
	if err := createVendorTarball(pkgDir, "node_modules", outputTarball); err != nil {
		return fmt.Errorf("create deps tarball: %w", err)
	}

	// Copy to distdir
	if distDir != "" {
		if err := os.MkdirAll(distDir, 0755); err != nil {
			return fmt.Errorf("create distdir: %w", err)
		}
		destPath := filepath.Join(distDir, assetName)
		data, err := os.ReadFile(outputTarball)
		if err != nil {
			return fmt.Errorf("read tarball: %w", err)
		}
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			return fmt.Errorf("write distfile: %w", err)
		}
	}

	return nil
}

// extractVar extracts a variable value from ebuild content.
// Handles both single-line and multi-line quoted values.
func extractVar(content, name string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, name+"=") {
			val := strings.TrimPrefix(line, name+"=")
			val = strings.Trim(val, `"' `)
			return val
		}
	}
	return ""
}

// expandEbuildVars expands common ebuild variables in a string.
// Handles ${P}, ${PV}, ${PN}, ${CATEGORY}.
func expandEbuildVars(s string, ei *ebuild.EbuildInfo) string {
	if strings.Contains(s, "${") {
		s = strings.NewReplacer(
			"${P}",      ei.P(),
			"${PV}",     ei.PV(),
			"${PN}",     ei.PN(),
			"${CATEGORY}", ei.Category,
		).Replace(s)
	}
	return s
}

// ---------------------------------------------------------------------------
// Vendored helpers (use existing functions from vendored.go)
// These are called from npm pipeline when SRC_URI includes GitHub releases URLs.
// ---------------------------------------------------------------------------