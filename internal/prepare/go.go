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

	"github.com/ulikunitz/xz"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/github"
	"bennypowers.dev/larry/internal/httpclient"
)

var githubRe = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)`)

// PrepareGo is the go-specific prepare pipeline for `larry prepare`.
// It vendors Go dependencies, creates a vendor tarball, and runs pkgdev manifest.
func PrepareGo(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error {
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

	// 7. Create vendor tarball (xz compressed) in package directory
	outPath := filepath.Join(filepath.Dir(ei.Path), ei.P()+"-deps.tar.xz")
	if err := createGoVendorTarball(srcDir, outPath); err != nil {
		return fmt.Errorf("go: create vendor tarball: %w", err)
	}

	// 8. Copy to distdir (script writes pkg-dir first, then cp to DISTDIR)
	if err := copyToDistdir(outPath, distDir); err != nil {
		return fmt.Errorf("go: copy to distdir: %w", err)
	}

	// 9. Upload to GitHub release (if repo configured)
	if cfg.GitHubRepo != "" {
		gh := github.NewClient()
		tag := "v" + atom.StripRevision(ei.PVVal)
		if err := npmUploadAsset(tag, filepath.Base(outPath), distDir, cfg.GitHubRepo, ei.PN(), gh.Runner()); err != nil {
			fmt.Fprintf(os.Stderr, "[larry] go: upload deps asset: %v (manifest may fail if distfiles are unavailable)\n", err)
		}
	}

	// 10. Run pkgdev manifest with catPkg (plan 21)
	pe, err := atom.ParseEbuildPath(ei.Path)
	if err != nil {
		return fmt.Errorf("go: parse ebuild path: %w", err)
	}
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

// extractGoSource extracts a gzip tarball from the given reader into destDir.
// Uses archive/tar + compress/gzip (no subprocess).
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

		target := filepath.Join(destDir, header.Name)

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
	return runner.Run(cmd)
}

// createGoVendorTarball creates an xz-compressed tarball of the vendor/
// directory at outPath.  The tarball root is vendor/ (matching what the
// go-module eclass expects).
func createGoVendorTarball(srcDir, outPath string) error {
	vendorDir := filepath.Join(srcDir, "vendor")
	if _, err := os.Stat(vendorDir); err != nil {
		return fmt.Errorf("vendor directory not found: %s", vendorDir)
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

	xzw, err := xz.WriterConfig{DictCap: 64 * 1024 * 1024}.NewWriter(outFile)
	if err != nil {
		return fmt.Errorf("create xz writer: %w", err)
	}
	defer func() { _ = xzw.Close() }() // best-effort close

	tr := tar.NewWriter(xzw)
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


