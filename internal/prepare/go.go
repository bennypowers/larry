package prepare

import (
	"encoding/json"
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
)

// PrepareGo is the go-specific prepare pipeline for `larry prepare`.
// It vendors Go dependencies, creates a tarball, and uploads to GitHub releases.
func PrepareGo(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error {
	// Early exit for dry-run
	if cfg.DryRun {
		return nil
	}

	// 1. Find the upstream repository
	owner, repo := findGoRepo(ei)
	if owner == "" || repo == "" {
		return fmt.Errorf("go: could not resolve GitHub owner/repo from SRC_URI or HOMEPAGE")
	}

	// 2. Create temp work directory
	workDir, err := os.MkdirTemp("", "larry-go-*")
	if err != nil {
		return fmt.Errorf("go: create temp dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	// 3. Download source tarball (v${PV}.tar.gz)
	clientGH := github.NewClient()
	sourceTarball := filepath.Join(workDir, fmt.Sprintf("v%s.tar.gz", ei.PV()))
	err = downloadGoSource(clientGH, owner, repo, ei.PV(), sourceTarball)
	if err != nil {
		return fmt.Errorf("go: download source: %w", err)
	}

	// 4. Extract source
	extractDir := filepath.Join(workDir, "extracted")
	os.MkdirAll(extractDir, 0755)
	err = extractGoSource(sourceTarball, extractDir)
	if err != nil {
		return fmt.Errorf("go: extract source: %w", err)
	}

	// 5. Run go mod vendor
	// Find the Go module root (may be in subdirectory)
	moduleRoot := findGoModuleRoot(extractDir)
	if moduleRoot == "" {
		return fmt.Errorf("go: no go.mod found in extracted source")
	}

	runner := github.NewClient().Runner()
	err = runGoModVendor(moduleRoot, runner)
	if err != nil {
		return fmt.Errorf("go: go mod vendor: %w", err)
	}

	// 6. Create vendor tarball
	distDir := cfg.Distdir
	if distDir == "" {
		distDir = "/usr/portage/distfiles"
	}

	err = createGoVendorTarball(moduleRoot, filepath.Join(moduleRoot, ei.Pkg+"-"+ei.PV()+".ebuild"), distDir)
	if err != nil {
		return fmt.Errorf("go: create vendor tarball: %w", err)
	}

	// 7. Check if release exists, create if not
	releaseExists, err := checkGoReleaseExists(clientGH, owner, repo, ei.PV())
	if err != nil {
		return fmt.Errorf("go: check release: %w", err)
	}

	if !releaseExists {
		err = createGoRelease(clientGH, owner, repo, ei.PV())
		if err != nil {
			return fmt.Errorf("go: create release: %w", err)
		}
	}

	// 8. Upload vendor tarball
	pkgName := ei.Pkg
	distfile := filepath.Join(distDir, pkgName+"-"+ei.PV()+"-deps.tar.gz")
	err = uploadGoRelease(clientGH, owner, repo, ei.PV(), filepath.Base(distfile))
	if err != nil {
		return fmt.Errorf("go: upload release: %w", err)
	}

	// 9. Run pkgdev manifest
	if err := runManifest(cfg.Overlay, ei.Path, distDir); err != nil {
		return fmt.Errorf("go: pkgdev manifest: %w", err)
	}

	return nil
}

// findGoRepo extracts the GitHub owner/repo from SRC_URI or HOMEPAGE.
func findGoRepo(ei *ebuild.EbuildInfo) (owner, repo string) {
	// Try HOMEPAGE first: https://github.com/owner/repo
	if ei.HOMEPAGE != "" {
		re := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)`)
		matches := re.FindStringSubmatch(ei.HOMEPAGE)
		if len(matches) == 3 {
			return matches[1], matches[2]
		}
	}

	// Try SRC_URI: mirror://github/${PN}/${P}.tar.gz
	if ei.SRC_URI != "" {
		re := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)`)
		matches := re.FindStringSubmatch(ei.SRC_URI)
		if len(matches) == 3 {
			return matches[1], matches[2]
		}
	}

	return "", ""
}

// downloadGoSource downloads the source tarball from GitHub.
func downloadGoSource(client *github.Client, owner, repo, version, dest string) error {
	// Download the source tarball using gh CLI
	output, err := client.Gh("api", owner+"/"+repo+"/releases/tags/v"+version)
	if err != nil {
		return fmt.Errorf("gh release API: %w", err)
	}

	// Parse JSON to get download URL
	var result struct {
		Assets []struct {
			BrowserDownloadURL string `json:"browser_download_url"`
			Name               string `json:"name"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return fmt.Errorf("parse release JSON: %w", err)
	}

	// Find the source tarball (v${PV}.tar.gz)
	var downloadURL string
	for _, asset := range result.Assets {
		if strings.Contains(asset.Name, "v"+version) && strings.HasSuffix(asset.Name, ".tar.gz") {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		// Fallback: construct direct download URL
		downloadURL = fmt.Sprintf("https://github.com/%s/%s/archive/refs/tags/v%s.tar.gz", owner, repo, version)
	}

	// Download the file
	clientHTTP := httpclient.NewClient()
	return downloadGoSourceURL(downloadURL, dest, clientHTTP)
}

// downloadGoSourceURL downloads a Go source tarball from a URL.
func downloadGoSourceURL(url, dest string, client *httpclient.Client) error {
	// Create directory if needed
	dir := filepath.Dir(dest)
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create dest dir: %w", err)
		}
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download Go source: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download Go source: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read Go source: %w", err)
	}

	if err := os.WriteFile(dest, data, 0644); err != nil {
		return fmt.Errorf("write Go source: %w", err)
	}

	return nil
}

// extractGoSource extracts a Go source tarball to a directory.
func extractGoSource(tarballPath, destDir string) error {
	cmd := exec.Command("tar", "xzf", tarballPath, "-C", destDir)
	return cmd.Run()
}

// findGoModuleRoot finds the directory containing go.mod in the extracted source.
func findGoModuleRoot(root string) string {
	// First check the root
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return root
	}

	// Search subdirectories
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subPath := filepath.Join(root, entry.Name())
			if found := findGoModuleRoot(subPath); found != "" {
				return found
			}
		}
	}

	return ""
}

// runGoModVendor runs go mod vendor in the given directory.
func runGoModVendor(dir string, runner github.CmdRunner) error {
	cmd := exec.Command("go", "mod", "vendor")
	cmd.Dir = dir
	return runner.Run(cmd)
}

// createGoVendorTarball creates a tarball of the vendor/ directory.
func createGoVendorTarball(moduleRoot, ebuildPath, distDir string) error {
	vendorDir := filepath.Join(moduleRoot, "vendor")
	if _, err := os.Stat(vendorDir); err != nil {
		return fmt.Errorf("vendor directory not found: %s", vendorDir)
	}

	// Ensure distdir exists
	if err := os.MkdirAll(distDir, 0755); err != nil {
		return fmt.Errorf("create distdir: %w", err)
	}

	// Parse ebuild to get P (package name-version)
	ebuildContent, err := os.ReadFile(ebuildPath)
	if err != nil {
		return fmt.Errorf("read ebuild: %w", err)
	}

	// Extract P from P= variable (multiline: P= can appear anywhere)
	var pVal string
	re := regexp.MustCompile(`(?m)^P\s*=\s*"([^"]+)"`)
	matches := re.FindSubmatch(ebuildContent)
	if len(matches) == 2 {
		pVal = string(matches[1])
	} else {
		// Fallback: use directory name
		pVal = filepath.Base(filepath.Dir(ebuildPath))
	}

	// Create tarball: ${P}-deps.tar.gz
	destFile := filepath.Join(distDir, pVal+"-deps.tar.gz")
	cmd := exec.Command("tar", "czf", destFile, "-C", vendorDir, ".")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("create vendor tarball: %w", err)
	}

	return nil
}

// checkGoReleaseExists checks if a GitHub release exists.
func checkGoReleaseExists(client *github.Client, owner, repo, tag string) (bool, error) {
	_, err := client.Gh("release", "view", tag)
	if err != nil {
		// Release doesn't exist (or error)
		return false, nil
	}
	return true, nil
}

// createGoRelease creates a GitHub release.
func createGoRelease(client *github.Client, owner, repo, tag string) error {
	_, err := client.Gh("release", "create", tag, "-t", "v"+tag, "-n", "Release v"+tag)
	if err != nil {
		return fmt.Errorf("create release: %w", err)
	}
	return nil
}

// uploadGoRelease uploads a file to a GitHub release.
func uploadGoRelease(client *github.Client, owner, repo, tag, filename string) error {
	_, err := client.Gh("release", "upload", tag, filepath.Join("/tmp", filename), "-F", filename)
	if err != nil {
		return fmt.Errorf("upload release asset: %w", err)
	}
	return nil
}

// copyToDistdir copies a tarball to the distdir.
func copyToDistdir(src, distDir string) error {
	// Ensure distdir exists
	if err := os.MkdirAll(distDir, 0755); err != nil {
		return fmt.Errorf("create distdir: %w", err)
	}

	// Read source
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}

	// Write to distdir
	dst := filepath.Join(distDir, filepath.Base(src))
	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("write to distdir: %w", err)
	}

	return nil
}


