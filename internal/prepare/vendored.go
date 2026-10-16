package prepare

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/github"
	"bennypowers.dev/larry/internal/larryconfig"
)

// DepType identifies the dependency ecosystem.
type DepType string

const (
	DepNPM    DepType = "npm"
	DepCargo  DepType = "cargo"
	DepGo     DepType = "go"
)

// DepSpec describes a single vendored dependency directory.
type DepSpec struct {
	Type           DepType // npm | cargo | go
	SourcePath     string  // relative to S, e.g. "src/web-app"
	DistName       string  // distfile name, e.g. "pkg-1.0.0-npm-deps.tar.xz"
	TarballContent string  // "npm-cache", "vendor"
}

// ---------------------------------------------------------------------------
// parseDepSpec — infer DepSpec from distfile name
// ---------------------------------------------------------------------------

// parseDepSpec infers a DepSpec from a distfile name.
func parseDepSpec(distName string) DepSpec {
	var depType DepType
	var tarballContent string

	switch {
	case strings.HasSuffix(distName, "-npm-deps.tar.xz") ||
		strings.HasSuffix(distName, "-npm-deps.tar.gz") ||
		strings.HasSuffix(distName, "-npm-deps.tar.zst"):
		depType = DepNPM
		tarballContent = "npm-cache"
	case strings.HasSuffix(distName, "-cargo-deps.tar.xz") ||
		strings.HasSuffix(distName, "-cargo-deps.tar.gz") ||
		strings.HasSuffix(distName, "-cargo-deps.tar.zst"):
		depType = DepCargo
		tarballContent = "vendor"
	case strings.HasSuffix(distName, "-go-deps.tar.gz") ||
		strings.HasSuffix(distName, "-go-deps.tar.xz"):
		depType = DepGo
		tarballContent = "vendor"
	default:
		return DepSpec{}
	}

	return DepSpec{
		Type:           depType,
		DistName:       distName,
		TarballContent: tarballContent,
	}
}

// parseDepSpecsFromURI extracts DepSpecs from a SRC_URI containing
// "releases/download/${PN}/" entries. PN comes from *atom.ParsedEbuild.
func parseDepSpecsFromURI(srcURI string, pe *atom.ParsedEbuild) []DepSpec {
	distNames := extractSRCURIForPN(srcURI, pe.PN)
	deps := make([]DepSpec, 0, len(distNames))
	for _, dn := range distNames {
		ds := parseDepSpec(dn)
		if ds.Type != "" {
			deps = append(deps, ds)
		}
	}
	return deps
}

// extractSRCURIForPN extracts distfile names from SRC_URI for a specific package.
func extractSRCURIForPN(srcURI, pkg string) []string {
	var dists []string
	pattern := fmt.Sprintf("releases/download/%s/", pkg)

	for _, part := range strings.Fields(srcURI) {
		if strings.Contains(part, pattern) {
			// Extract just the filename
			idx := strings.LastIndex(part, "/")
			if idx >= 0 {
				dists = append(dists, part[idx+1:])
			}
		}
	}
	return dists
}

// ---------------------------------------------------------------------------
// downloadFileFromURL — download a file from a URL
// ---------------------------------------------------------------------------

// downloadFileFromURL downloads a file from url to dest.
func downloadFileFromURL(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }() // best-effort cleanup

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer func() { _ = out.Close() }() // best-effort close

	_, err = io.Copy(out, resp.Body)
	return err
}

// ---------------------------------------------------------------------------
// extractTarball — extract a tar.gz/xz/zst archive
// ---------------------------------------------------------------------------

// extractTarball extracts a tarball to destDir.
func extractTarball(tarballPath, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	// Determine compression from extension
	var cmd *exec.Cmd
	switch {
	case strings.HasSuffix(tarballPath, ".tar.gz") || strings.HasSuffix(tarballPath, ".tgz"):
		cmd = exec.Command("tar", "xzf", tarballPath)
	case strings.HasSuffix(tarballPath, ".tar.xz"):
		cmd = exec.Command("tar", "xJf", tarballPath)
	case strings.HasSuffix(tarballPath, ".tar.zst"):
		cmd = exec.Command("tar", "xIf", tarballPath)
	case strings.HasSuffix(tarballPath, ".tar"):
		cmd = exec.Command("tar", "xf", tarballPath)
	default:
		return fmt.Errorf("unsupported compression for %s", tarballPath)
	}

	cmd.Dir = destDir
	return cmd.Run()
}

// ---------------------------------------------------------------------------
// createVendorTarball — package a vendor dir into a tarball
// ---------------------------------------------------------------------------

// createVendorTarball creates a tarball of contentDir under baseDir.
func createVendorTarball(baseDir, contentDir, outputPath string) error {
	contentPath := filepath.Join(baseDir, contentDir)
	if _, err := os.Stat(contentPath); err != nil {
		return fmt.Errorf("content dir %s not found: %w", contentPath, err)
	}

	// Use tar cf with -C to set working dir
	cmd := exec.Command("tar", "cf", outputPath, "-C", baseDir, contentDir)
	if strings.HasSuffix(outputPath, ".gz") || strings.HasSuffix(outputPath, ".tgz") {
		cmd = exec.Command("tar", "czf", outputPath, "-C", baseDir, contentDir)
	} else if strings.HasSuffix(outputPath, ".xz") {
		cmd = exec.Command("tar", "cJf", outputPath, "-C", baseDir, contentDir)
	} else if strings.HasSuffix(outputPath, ".zst") {
		cmd = exec.Command("tar", "cIf", outputPath, "-C", baseDir, contentDir)
	}

	return cmd.Run()
}

// ---------------------------------------------------------------------------
// copyTarballToDistdir — copy a tarball to DISTDIR
// ---------------------------------------------------------------------------

// copyTarballToDistdir copies srcTarball into distdir, creating distdir if needed.
func copyTarballToDistdir(srcTarball, distdir string) error {
	if err := os.MkdirAll(distdir, 0755); err != nil {
		return fmt.Errorf("create distdir %s: %w", distdir, err)
	}

	base := filepath.Base(srcTarball)
	dest := filepath.Join(distdir, base)

	data, err := os.ReadFile(srcTarball)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcTarball, err)
	}

	return os.WriteFile(dest, data, 0644)
}

// ---------------------------------------------------------------------------
// GitHub helpers — release view, create, upload
// ---------------------------------------------------------------------------

// ghReleaseViewExists checks if a GitHub release tag exists.
func ghReleaseViewExists(tag, repo string, runner github.CmdRunner) (bool, error) {
	cmd := exec.Command("gh", "release", "view", tag, "--json", "tagName,url")
	if repo != "" {
		cmd.Args = append(cmd.Args, "--repo", repo)
	}
	// Don't override cmd.Env — let it inherit from os.Environ() so auth tokens work

	out, err := runner.Output(cmd)
	if err != nil {
		return false, nil // not found is not an error for our purposes
	}

	if len(out) == 0 {
		return false, nil
	}

	// Verify it's actually our tag
	var result struct {
		TagName string `json:"tagName"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return false, err
	}

	return result.TagName == tag, nil
}

// ghReleaseCreate creates a GitHub release for the given tag.
func ghReleaseCreate(tag, repo, pn string, runner github.CmdRunner) error {
	cmd := exec.Command("gh", "release", "create", tag,
		"--title", pn+" deps",
		"--notes", "npm dependency cache for "+pn)
	if repo != "" {
		cmd.Args = append(cmd.Args, "--repo", repo)
	}
	return runner.Run(cmd)
}

// ghUploadAsset uploads a tarball asset to a GitHub release.
func ghUploadAsset(tag, tarballPath, repo string, runner github.CmdRunner) error {
	if _, err := os.Stat(tarballPath); err != nil {
		return fmt.Errorf("asset file not found: %w", err)
	}

	cmd := exec.Command("gh", "release", "upload", tag, tarballPath, "--clobber")
	if repo != "" {
		cmd.Args = append(cmd.Args, "--repo", repo)
	}
	return runner.Run(cmd)
}

// ---------------------------------------------------------------------------
// resolveBuildDir returns the working directory for a vendored dep spec.
// When vendor_dirs is configured for this package, it uses the first entry;
// otherwise it falls back to the extract root.
func resolveBuildDir(extractDir string, pe *atom.ParsedEbuild, pkgCfg *larryconfig.Config, ds DepSpec) string {
	if pkgCfg != nil {
		if dirs, ok := pkgCfg.VendorDirs[pe.PN]; ok && len(dirs) > 0 {
			return filepath.Join(extractDir, dirs[0])
		}
	}
	if ds.SourcePath != "" {
		return filepath.Join(extractDir, ds.SourcePath)
	}
	return extractDir
}

// ---------------------------------------------------------------------------
// PrepareVendored — main vendored pipeline entry point
// ---------------------------------------------------------------------------

// PrepareVendored runs the vendored-dependency pipeline.
func PrepareVendored(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error {
	if cfg.DryRun {
		return nil
	}

	distdir := cfg.Distdir

	// 1. Parse ebuild path to get PN from directory name
	pe, err := atom.ParseEbuildPath(ei.Path)
	if err != nil {
		return fmt.Errorf("parse ebuild path: %w", err)
	}

	// 2. Load per-package config for vendor_dirs
	pkgCfg, err := larryconfig.LoadConfig(filepath.Dir(ei.Path))
	if err != nil {
		return fmt.Errorf("vendored: read .larry/config.yaml: %w", err)
	}

	// 3. Extract dep specs from SRC_URI
	depSpecs := parseDepSpecsFromURI(ei.SRC_URI, pe)
	if len(depSpecs) == 0 {
		return fmt.Errorf("no vendored deps found in SRC_URI")
	}

	// 2. Download source tarball
	sourceURL := downloadSourceURL(ei)
	if sourceURL == "" {
		return fmt.Errorf("no source tarball URL in SRC_URI")
	}

	tmpDir, err := os.MkdirTemp("", "larry-vendored-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }() // best-effort cleanup

	sourceTarball := filepath.Join(tmpDir, "source.tar.gz")
	if err := downloadFileFromURL(sourceURL, sourceTarball); err != nil {
		return fmt.Errorf("download source: %w", err)
	}

	// 3. Extract source
	extractDir := filepath.Join(tmpDir, "source")
	if err := extractTarball(sourceTarball, extractDir); err != nil {
		return fmt.Errorf("extract source: %w", err)
	}

	// 4. For each dep, resolve build dir, run vendor command, create tarball, upload
	gh := github.NewClient()
	for _, ds := range depSpecs {
		// Resolve working directory (uses vendor_dirs from .larry/config.yaml when present)
		buildDir := resolveBuildDir(extractDir, pe, pkgCfg, ds)
		if err := runVendorCommand(ds.Type, buildDir); err != nil {
			return fmt.Errorf("vendor %s: %w", ds.Type, err)
		}

		// Create tarball
		outputTarball := filepath.Join(tmpDir, ds.DistName)
		if err := createVendorTarball(extractDir, ds.TarballContent, outputTarball); err != nil {
			return fmt.Errorf("create tarball %s: %w", ds.DistName, err)
		}

		// Copy to distdir
		if err := copyTarballToDistdir(outputTarball, distdir); err != nil {
			return fmt.Errorf("copy to distdir: %w", err)
		}

		// Upload to GitHub release
		exists, err := ghReleaseViewExists(ei.Pkg, cfg.GitHubRepo, gh.Runner())
		if err != nil {
			return fmt.Errorf("check release %s: %w", ei.Pkg, err)
		}
		if !exists {
			if err := ghReleaseCreate(ei.Pkg, cfg.GitHubRepo, ei.Pkg, gh.Runner()); err != nil {
				return fmt.Errorf("create release %s: %w", ei.Pkg, err)
			}
		}
		if err := ghUploadAsset(ei.Pkg, outputTarball, cfg.GitHubRepo, gh.Runner()); err != nil {
			return fmt.Errorf("upload asset %s: %w", ds.DistName, err)
		}
	}

	// 5. Run manifest
	return runManifest(manifestRequest{
		Overlay:    cfg.Overlay,
		CatPkg:     ei.Category + "/" + ei.PN(),
		Distdir:    distdir,
		EbuildPath: ei.Path,
		Prefetch:   nil,
	})
}

// ---------------------------------------------------------------------------
// Vendor command helpers
// ---------------------------------------------------------------------------

// runVendorCommand runs the appropriate vendor command for the dep type.
func runVendorCommand(depType DepType, workDir string) error {
	switch depType {
	case DepNPM:
		// Create .npmrc to suppress audit/fund prompts
		npmrc := filepath.Join(workDir, ".npmrc")
		if err := os.WriteFile(npmrc, []byte("audit=false\nfund=false\n"), 0644); err != nil {
			return fmt.Errorf("write .npmrc: %w", err)
		}
		cmd := exec.Command("npm", "ci", "--ignore-scripts")
		cmd.Dir = workDir
		return cmd.Run()
	case DepCargo:
		cmd := exec.Command("cargo", "vendor", "vendor/")
		cmd.Dir = workDir
		return cmd.Run()
	case DepGo:
		cmd := exec.Command("go", "mod", "vendor")
		cmd.Dir = workDir
		return cmd.Run()
	default:
		return fmt.Errorf("unknown dep type: %s", depType)
	}
}

// ---------------------------------------------------------------------------
// downloadSourceURL — extract source tarball URL from SRC_URI
// ---------------------------------------------------------------------------

// downloadSourceURL returns the source tarball URL from ebuild SRC_URI.
// It excludes releases/download/ URLs (those are dep tarballs, not source).
func downloadSourceURL(ei *ebuild.EbuildInfo) string {
	srcURI := ei.SRC_URI
	if srcURI == "" {
		return ""
	}

	// Find the first URL that is NOT a releases/download/ entry
	for _, part := range strings.Fields(srcURI) {
		if !strings.Contains(part, "releases/download/") &&
			(strings.HasPrefix(part, "http://") || strings.HasPrefix(part, "https://") ||
				strings.HasPrefix(part, "mirror://")) {
			return part
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// HTTP helper (standalone to avoid import cycle with httpclient)
// ---------------------------------------------------------------------------


