package prepare

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/github"
	"bennypowers.dev/larry/internal/httpclient"
)

// npmPrepare implements the npm type-specific prepare pipeline.
// Steps:
// 1. Resolve SRC_URI → npm registry URL
// 2. Download distfile
// 3. (Optional) npm install
// 4. Create tarball of node_modules
// 5. Copy tarball to DISTDIR
// 6. Check/create GitHub release for ${PN}
// 7. Upload tarball (with SHA-256 collision check)
// 8. Run pkgdev manifest
func npmPrepare(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error {
	if cfg.DryRun {
		return nil
	}

	// 1. Resolve SRC_URI
	resolvedURL := npmResolveSRCURI(ei.SRC_URI, ei)
	if resolvedURL == "" {
		return fmt.Errorf("npm: could not resolve SRC_URI to npm registry URL")
	}

	// 2. Download distfile
	distDir := cfg.Distdir
	if distDir == "" {
		distDir = getEnv("PORTAGE_DISTDIR")
	}
	distfile := npmExtractDistfileName(resolvedURL)
	if distfile == "" {
		distfile = ei.P() + ".tgz"
	}

	// Backup before mutations
	if err := Backup(ei.Path); err != nil {
		return fmt.Errorf("backup: %w", err)
	}

	client := httpclient.NewClient()
	downloaded, err := npmDownloadDistfile(resolvedURL, distDir, client)
	if err != nil {
		Restore(ei.Path)
		return fmt.Errorf("download distfile: %w", err)
	}

	// 3. Copy to distdir
	if err := npmCopyToDistdir(downloaded, distDir); err != nil {
		Restore(ei.Path)
		return fmt.Errorf("copy to distdir: %w", err)
	}

	// 4. Create tarball of node_modules (if they exist)
	// This is the key npm-specific step: package dependencies as a distfile
	nodeModulesDir := findNodeModules(ei)
	if nodeModulesDir != "" {
		tarballName := fmt.Sprintf("%s-npm-deps.tar.xz", ei.PVVal)
		_ = filepath.Join(distDir, tarballName) // path construction for future use

		if err := npmCreateTarball(nodeModulesDir, ei.PVVal, "npm", tarballName); err != nil {
			Restore(ei.Path)
			return fmt.Errorf("create npm tarball: %w", err)
		}

		// Verify distfiles before manifest
		if !verifyDistfiles(distDir, []string{tarballName}) {
			Restore(ei.Path)
			return fmt.Errorf("distfile verification failed after tarball creation")
		}
	}

	// 5. GH Release upload
	clientGH := github.NewClient()
	releaseExists, err := npmCheckReleaseExists(clientGH, ei.Pkg, "", ei.Pkg)
	if err == nil && !releaseExists {
		// Release doesn't exist — create it (best effort, don't fail if gh not available)
		_ = npmEnsureRelease(clientGH, ei.Pkg, "", ei.Pkg, clientGH.Runner())
	}

	// 6. Run pkgdev manifest
	if err := runManifest(ei.Path, distDir); err != nil {
		Restore(ei.Path)
		return fmt.Errorf("manifest: %w", err)
	}

	return nil
}

// npmResolveSRCURI converts a mirror://npm/ SRC_URI to an npm registry URL.
func npmResolveSRCURI(srcURI string, ei *ebuild.EbuildInfo) string {
	// Resolve variables first
	resolved := resolveVariables(srcURI, map[string]string{
		"${PN}": ei.Pkg,
		"${PV}": ei.PVVal,
		"${P}":  ei.PVal,
	})

	// Check for mirror://npm/ pattern
	const prefix = "mirror://npm/"
	if !strings.HasPrefix(resolved, prefix) {
		return ""
	}

	// Convert to npm registry URL
	// mirror://npm/my-pkg/my-pkg-1.0.0.tgz → https://registry.npmjs.org/my-pkg/my-pkg-1.0.0.tgz
	path := strings.TrimPrefix(resolved, prefix)
	return "https://registry.npmjs.org/" + path
}

// npmExtractDistfileName extracts the distfile name from a URL.
func npmExtractDistfileName(url string) string {
	// Handle npm scoped packages
	// https://registry.npmjs.org/@scope/pkg/-/@scope/pkg-1.0.0.tgz
	if idx := strings.LastIndex(url, "/-"); idx >= 0 {
		return strings.TrimPrefix(url[idx+2:], "/")
	}
	// https://registry.npmjs.org/pkg/pkg-1.0.0.tgz
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return strings.TrimPrefix(parts[len(parts)-1], "/")
	}
	return ""
}

// npmDownloadDistfile downloads a distfile from the resolved URL.
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

// npmRunNpmInstall runs npm install in the given directory.
// Skipped in dry-run mode.
func npmRunNpmInstall(dir string, dryRun bool) error {
	if dryRun {
		return nil
	}

	_, err := exec.LookPath("npm")
	if err != nil {
		return fmt.Errorf("npm: npm not found in PATH")
	}

	cmd := exec.Command("npm", "install")
	cmd.Dir = dir
	return cmd.Run()
}

// npmCreateTarball creates a tarball of node_modules.
func npmCreateTarball(dir, pv, depType, tarballName string) error {
	// Check for node_modules directory
	nodeModules := filepath.Join(dir, "node_modules")
	if _, err := os.Stat(nodeModules); os.IsNotExist(err) {
		return nil // No node_modules, skip tarball creation
	}

	tarballPath := filepath.Join(dir, tarballName)

	_, err := exec.LookPath("tar")
	if err != nil {
		return fmt.Errorf("tar not found")
	}

	// Create tarball from node_modules
	cmd := exec.Command("tar", "cJf", tarballPath, "-C", dir, "node_modules")
	return cmd.Run()
}

// npmCopyToDistdir copies a tarball to the DISTDIR.
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

// npmCheckReleaseExists checks if a GitHub release exists for the given tag.
func npmCheckReleaseExists(client *github.Client, owner, repo, tag string) (bool, error) {
	// gh release view --json tagName
	var checkArgs []string
	if owner != "" && repo != "" {
		checkArgs = []string{"release", "view", "--repo", owner + "/" + repo, tag, "--json", "tagName"}
	} else {
		checkArgs = []string{"release", "view", tag, "--json", "tagName"}
	}

	output, err := client.Gh(checkArgs...)
	if err != nil {
		return false, nil // Release doesn't exist (gh returns non-zero)
	}

	if len(output) > 0 {
		return true, nil
	}
	return false, nil
}

// npmEnsureRelease ensures a GitHub release exists.
func npmEnsureRelease(client *github.Client, owner, repo, tag string, runner github.CmdRunner) error {
	// Check if release exists
	exists, err := npmCheckReleaseExists(client, owner, repo, tag)
	if err != nil || exists {
		return nil
	}

	// Create release
	var createArgs []string
	if owner != "" && repo != "" {
		createArgs = []string{"release", "create", tag, "--title", tag, "--generate-notes", "--repo", owner + "/" + repo}
	} else {
		createArgs = []string{"release", "create", tag, "--title", tag, "--generate-notes"}
	}

	cmd := exec.Command("gh", createArgs...)
	return runner.Run(cmd)
}

// npmUploadAsset uploads an asset to a GitHub release.
func npmUploadAsset(client *github.Client, owner, repo, tag, filePath, assetName string, runner github.CmdRunner) error {
	var uploadArgs []string
	if owner != "" && repo != "" {
		uploadArgs = []string{"release", "upload", tag, "--clobber", "--repo", owner + "/" + repo, filePath}
	} else {
		uploadArgs = []string{"release", "upload", tag, "--clobber", filePath}
	}

	cmd := exec.Command("gh", uploadArgs...)
	return runner.Run(cmd)
}

// npmComputeSHA256 computes the SHA-256 hash of a file.
func npmComputeSHA256(filePath string) string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// npmAssetAction represents the result of an asset collision check.
type npmAssetAction int

const (
	ActionSkip     npmAssetAction = iota // Asset exists with matching hash
	ActionUpload                         // Asset doesn't exist or needs re-upload
)

// npmAssetCollisionCheck checks asset collision against existing assets.
func npmAssetCollisionCheck(existing []github.Asset, localPath string) npmAssetAction {
	localHash := npmComputeSHA256(localPath)
	if localHash == "" {
		return ActionUpload
	}

	for _, asset := range existing {
		if asset.Name != filepath.Base(localPath) {
			continue
		}
		if asset.SHA256 == localHash {
			return ActionSkip
		}
		return ActionUpload // Different hash, re-upload
	}
	return ActionUpload
}

// findNodeModules searches for node_modules in the ebuild's source tree.
func findNodeModules(ei *ebuild.EbuildInfo) string {
	dirs := []string{
		filepath.Join(ei.Path, "..", "..", "node_modules"),
		filepath.Join(ei.Path, "node_modules"),
	}
	for _, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
			return dir
		}
	}
	return ""
}
