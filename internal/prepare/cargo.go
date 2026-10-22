package prepare

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"bennypowers.dev/larry/internal/atom"
	"bennypowers.dev/larry/internal/ebuild"
	"bennypowers.dev/larry/internal/github"
	"bennypowers.dev/larry/internal/httpclient"
	"bennypowers.dev/larry/internal/logger"
)

// lookPath is exec.LookPath, stubbable so tests can exercise the native parser
// even on a workstation that has pycargoebuild installed.
var lookPath = exec.LookPath

// cratesDownloadBase is the static.crates.io download base URL. Tests
// replace it to point at an httptest server.
var cratesDownloadBase = "https://static.crates.io/crates/"

// forgeKind identifies the upstream forge for a cargo package.
type forgeKind int

const (
	forgeGitHub forgeKind = iota
	forgeSourceHut
)

// CargoCrate represents a parsed entry from Cargo.lock.
type CargoCrate struct {
	Name    string
	Version string
	Source  string
}

// PrepareCargo is the cargo type-specific pipeline for `larry prepare`.
// It finds the upstream repository, downloads Cargo.lock, generates CRATES,
// and updates the ebuild.
func PrepareCargo(ei *ebuild.EbuildInfo, cfg *PrepareConfig) error {
	// Early exit for dry-run
	if cfg.DryRun {
		return nil
	}

	// 1. Find the upstream forge, owner, repo
	raw, err := os.ReadFile(ei.Path)
	if err != nil {
		return fmt.Errorf("cargo: read ebuild: %w", err)
	}
	pe, err := atom.ParseEbuildPath(ei.Path)
	if err != nil {
		pe = nil
	}
	vars := ebuild.BuildVariableMap(string(raw), pe)
	forge, owner, repo, err := findCargoRepo(string(raw), vars)
	if err != nil {
		return fmt.Errorf("cargo: find repo: %w", err)
	}

	// 2. Derive the version tag
	expandedText := ebuild.ExpandVariables(string(raw), vars)
	tag := cargoTag(forge, ei.PN(), ei.PV(), expandedText)

	// 3. Download Cargo.lock
	lockPath, err := fetchCargoLock(nil, forge, owner, repo, tag)
	if err != nil {
		return fmt.Errorf("cargo: failed to download Cargo.lock for %s/%s at tag %s: %w", owner, repo, tag, err)
	}
	defer func() { _ = os.Remove(lockPath) }() // best-effort cleanup

	// 4. Parse Cargo.lock
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("cargo: read Cargo.lock: %w", err)
	}
	crates := parseCargoLock(string(lockContent))
	if len(crates) == 0 {
		return fmt.Errorf("cargo: no crates found in Cargo.lock")
	}

	// 5. Generate CRATES variable and update ebuild
	count, err := updateCargoEbuild(ei, lockPath, crates, cfg, nil)
	if err != nil {
		return fmt.Errorf("cargo: update ebuild: %w", err)
	}
	logger.Debug("updated CRATES with " + fmt.Sprintf("%d", count) + " crate entries")

	// 6. Generate Manifest
	if err := runManifest(manifestRequest{
		Overlay:    cfg.Overlay,
		CatPkg:     ei.Category + "/" + ei.PN(),
		Distdir:    cfg.Distdir,
		EbuildPath: ei.Path,
		Prefetch:   cargoPrefetchPackage,
	}); err != nil {
		return fmt.Errorf("cargo: manifest: %w", err)
	}

	return nil
}

// fetcher is the minimal HTTP surface fetchCargoLock needs.
// *httpclient.Client already satisfies it.
type fetcher interface {
	Get(url string) (*http.Response, error)
}

// findCargoRepo resolves the upstream forge from the variable-expanded ebuild text.
// owner includes the leading "~" for SourceHut. Returns an error when nothing matches.
func findCargoRepo(raw string, vars ebuild.VariableMap) (forge forgeKind, owner, repo string, err error) {
	text := ebuild.ExpandVariables(raw, vars)

	// Pattern 1: github.com
	if m := regexp.MustCompile(`https://github\.com/([^/\s"]+)/([^/\s"]+)`).FindStringSubmatch(text); m != nil {
		repo := strings.TrimRight(m[2], "/")
		repo = strings.TrimSuffix(repo, ".git")
		return forgeGitHub, m[1], repo, nil
	}

	// Pattern 2: git.sr.ht (the URL that actually resolves)
	if m := regexp.MustCompile(`https://git\.sr\.ht/(~[^/\s"]+)/([^/\s"]+)`).FindStringSubmatch(text); m != nil {
		repo := strings.TrimRight(m[2], "/")
		repo = strings.TrimSuffix(repo, ".git")
		return forgeSourceHut, m[1], repo, nil
	}

	// Pattern 3: sr.ht (HOMEPAGE form — rewrite implicit in URL template)
	if m := regexp.MustCompile(`https://sr\.ht/(~[^/\s"]+)/([^/\s"]+)`).FindStringSubmatch(text); m != nil {
		repo := strings.TrimRight(m[2], "/")
		repo = strings.TrimSuffix(repo, ".git")
		return forgeSourceHut, m[1], repo, nil
	}

	return 0, "", "", fmt.Errorf("cargo: no github.com or sr.ht URL in ebuild")
}

// cargoTag derives the version tag for a cargo package on the given forge.
func cargoTag(forge forgeKind, pn, pv, expandedText string) string {
	switch forge {
	case forgeGitHub:
		return "v" + atom.StripRevision(pv)
	case forgeSourceHut:
		// Try archive/ tag from expanded SRC_URI
		if m := regexp.MustCompile(`archive/([^/\s"]+)\.tar(\.[A-Za-z0-9]+)?`).FindStringSubmatch(expandedText); m != nil {
			return m[1]
		}
		// Fall back to PN-PV
		return pn + "-" + atom.StripRevision(pv)
	}
	return ""
}

// buildURL builds the raw URL for fetching Cargo.lock.
func buildURL(forge forgeKind, owner, repo, tag, subdir string) string {
	switch forge {
	case forgeGitHub:
		return "https://raw.githubusercontent.com/" + owner + "/" + repo + "/" + tag + "/" + subdir + "Cargo.lock"
	case forgeSourceHut:
		return "https://git.sr.ht/" + owner + "/" + repo + "/blob/" + tag + "/" + subdir + "Cargo.lock"
	}
	return ""
}

// fetchCargoLock downloads Cargo.lock for the given forge/owner/repo/tag into a fresh
// temp dir and returns the file path. fetch may be nil, in which case
// httpclient.NewClient() is used.
func fetchCargoLock(fetch fetcher, forge forgeKind, owner, repo, tag string) (string, error) {
	if fetch == nil {
		fetch = httpclient.NewClient()
	}

	for _, subdir := range []string{"", "bindings/python/"} {
		url := buildURL(forge, owner, repo, tag, subdir)
		resp, err := fetch.Get(url)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			continue
		}

		tmpDir, err := os.MkdirTemp("", "larry-cargo-*")
		if err != nil {
			_ = resp.Body.Close()
			continue
		}

		destPath := filepath.Join(tmpDir, "Cargo.lock")
		if err := func() error {
			defer func() { _ = resp.Body.Close() }() // best-effort
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("read Cargo.lock: %w", err)
			}
			if err := os.WriteFile(destPath, data, 0644); err != nil {
				return fmt.Errorf("write Cargo.lock: %w", err)
			}
			return nil
		}(); err != nil {
			_ = os.RemoveAll(tmpDir) // best-effort cleanup on failure
			continue
		}

		return destPath, nil
	}

	return "", fmt.Errorf("cargo: failed to download Cargo.lock for %s/%s at tag %s", owner, repo, tag)
}

// downloadCargoLock downloads a Cargo.lock from a URL using the given client.
// Used by standalone HTTP download tests.
func downloadCargoLock(url, dest string, client *httpclient.Client) error {
	dir := filepath.Dir(dest)
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create dest dir: %w", err)
		}
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download Cargo.lock: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // best-effort cleanup

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download Cargo.lock: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read Cargo.lock: %w", err)
	}

	if err := os.WriteFile(dest, data, 0644); err != nil {
		return fmt.Errorf("write Cargo.lock: %w", err)
	}

	return nil
}

// parseCargoLock parses a Cargo.lock TOML file and extracts crate names/versions.
func parseCargoLock(content string) []CargoCrate {
	var crates []CargoCrate

	// Split by [[package]] markers
	packageMarker := "[[package]]"
	// Use strings.Split to get blocks between markers
	parts := strings.Split(content, packageMarker)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		lines := strings.Split(part, "\n")
		var name, version, source string

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, `name = "`) {
				name = strings.TrimPrefix(line, `name = "`)
				name = strings.TrimSuffix(name, `"`)
			} else if strings.HasPrefix(line, `version = "`) {
				version = strings.TrimPrefix(line, `version = "`)
				version = strings.TrimSuffix(version, `"`)
			} else if strings.HasPrefix(line, `source = "`) {
				source = strings.TrimPrefix(line, `source = "`)
				source = strings.TrimSuffix(source, `"`)
			}
		}

		// Skip if no source (workspace root packages have no source)
		if source == "" || name == "" || version == "" {
			continue
		}

		// D1: Only accept registry+ sources. Git-sourced deps land in CRATES=
		// and the cargo eclass cannot resolve them.
		if !strings.HasPrefix(source, "registry+") {
			continue
		}

		crates = append(crates, CargoCrate{
			Name:    name,
			Version: version,
			Source:  source,
		})
	}

	return crates
}

// formatCRATES formats a list of CargoCrate into CRATES variable content.
// De-duplicates and byte-sorts entries (matching script's `sort -u`).
func formatCRATES(crates []CargoCrate) string {
	if len(crates) == 0 {
		return "\n"
	}
	// Build "name@version" keys, deduplicate, sort
	seen := make(map[string]struct{}, len(crates))
	keys := make([]string, 0, len(crates))
	for _, c := range crates {
		key := c.Name + "@" + c.Version
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("\n")
	for _, k := range keys {
		sb.WriteString("\t" + k + "\n")
	}
	return sb.String()
}

// updateCargoEbuild updates the CRATES variable in an ebuild file.
// If pycargoebuild is available, it runs in an isolated temp dir with the Cargo.lock
// and a synthetic Cargo.toml. On failure it falls back to the native parser.
// runner may be nil (uses github.NewClient().Runner()).
func updateCargoEbuild(ei *ebuild.EbuildInfo, lockPath string, crates []CargoCrate,
	cfg *PrepareConfig, runner github.CmdRunner) (int, error) {

	ebuildPath := ei.Path

	if cfg.DryRun {
		return 0, nil
	}

	// Check if pycargoebuild is available
	_, pycargoErr := lookPath("pycargoebuild")
	if pycargoErr == nil {
		// Run pycargoebuild in an isolated temp dir — never in the overlay.
		work, err := os.MkdirTemp("", "larry-cargo-*")
		if err != nil {
			// Fall through to native parsing
			return updateCargoEbuildNative(ebuildPath, crates)
		}
		defer func() { _ = os.RemoveAll(work) }() // best-effort cleanup

		// Copy the downloaded Cargo.lock into the work dir.
		lockData, err := os.ReadFile(lockPath)
		if err != nil {
			return updateCargoEbuildNative(ebuildPath, crates)
		}
		if err := os.WriteFile(filepath.Join(work, "Cargo.lock"), lockData, 0644); err != nil {
			return updateCargoEbuildNative(ebuildPath, crates)
		}

		// Write a synthetic Cargo.toml for pycargoebuild.
		cargoToml := fmt.Sprintf(`[package]
name = "%s"
version = "%s"
edition = "2021"
`, ei.Pkg, ei.PV())
		if err := os.WriteFile(filepath.Join(work, "Cargo.toml"), []byte(cargoToml), 0644); err != nil {
			return updateCargoEbuildNative(ebuildPath, crates)
		}

		if runner == nil {
			runner = github.NewClient().Runner()
		}
		if err := runPycargoebuild(work, ebuildPath, runner); err != nil {
			// pycargoebuild failed, fall through to native
			return updateCargoEbuildNative(ebuildPath, crates)
		}
		// pycargoebuild ran successfully — the ebuild was updated in place.
		return 0, nil
	}

	// pycargoebuild not available — native fallback
	return updateCargoEbuildNative(ebuildPath, crates)
}

// runPycargoebuild runs pycargoebuild --inplace on the ebuild.
// The command's working directory is set to dir (typically a temp dir holding
// Cargo.lock and a synthetic Cargo.toml).
func runPycargoebuild(dir, ebuildPath string, runner github.CmdRunner) error {
	cmd := exec.Command("pycargoebuild", dir, "--no-write-crate-tarball", "--no-manifest", "--inplace", ebuildPath)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return runner.Run(cmd)
}

// updateCargoEbuildNative replaces an existing CRATES="…" block, or inserts one when
// absent. Returns the number of crate entries written.
//
// Algorithm:
//
//	1. If a CRATES="…" block exists (anchored at line start), replace it.
//	2. Otherwise insert before the first "inherit" line.
//	3. If no "inherit" line, insert after the "EAPI=" line.
//	4. If no "EAPI=" line, prepend to the file.
func updateCargoEbuildNative(ebuildPath string, crates []CargoCrate) (int, error) {
	content, err := os.ReadFile(ebuildPath)
	if err != nil {
		return 0, fmt.Errorf("read ebuild: %w", err)
	}

	cratesStr := formatCRATES(crates)

	// Count the number of deduplicated entries for the return value.
	seen := make(map[string]struct{})
	for _, c := range crates {
		key := c.Name + "@" + c.Version
		seen[key] = struct{}{}
	}
	entryCount := len(seen)

	newBlock := "CRATES=\"" + cratesStr + "\""

	// 1. Try to replace an existing CRATES="…" block (anchored at line start).
	cratesRe := regexp.MustCompile(`(?sm)^CRATES\s*=\s*"`)
	endRe := regexp.MustCompile(`(?sm)^"\s*$`)
	startMatch := cratesRe.FindIndex(content)
	if startMatch != nil {
		// Find the closing quote line after the start.
		rest := content[startMatch[1]:]
		endMatch := endRe.FindIndex(rest)
		if endMatch != nil {
			// Replace: pre + newBlock + post
			pre := content[:startMatch[0]]
			post := content[startMatch[1]+endMatch[1]:]
			newContent := append(pre, append([]byte(newBlock), post...)...)
			if err := os.WriteFile(ebuildPath, newContent, 0644); err != nil {
				return 0, fmt.Errorf("write ebuild: %w", err)
			}
			return entryCount, nil
		}
	}

	// 2. No existing CRATES= — insert before first "inherit" line.
	inheritRe := regexp.MustCompile(`(?m)^inherit\b`)
	inheritMatch := inheritRe.FindIndex(content)
	if inheritMatch != nil {
		var sb strings.Builder
		sb.Write(content[:inheritMatch[0]])
		sb.WriteString("\n" + newBlock + "\n\n")
		sb.Write(content[inheritMatch[0]:])
		newContent := []byte(sb.String())
		if err := os.WriteFile(ebuildPath, newContent, 0644); err != nil {
			return 0, fmt.Errorf("write ebuild: %w", err)
		}
		return entryCount, nil
	}

	// 3. No "inherit" — insert after "EAPI=" line.
	eapiRe := regexp.MustCompile(`(?m)^EAPI=\d+\s*$`)
	eapiMatch := eapiRe.FindIndex(content)
	if eapiMatch != nil {
		afterEapi := content[eapiMatch[1]:]
		var sb strings.Builder
		sb.Write(content[:eapiMatch[0]])
		sb.WriteByte('\n')
		sb.WriteString(newBlock)
		sb.WriteString("\n\n")
		sb.Write(afterEapi)
		newContent := []byte(sb.String())
		if err := os.WriteFile(ebuildPath, newContent, 0644); err != nil {
			return 0, fmt.Errorf("write ebuild: %w", err)
		}
		return entryCount, nil
	}

	// 4. Nothing to anchor to — prepend.
	var sb strings.Builder
	sb.WriteString(newBlock + "\n\n")
	sb.Write(content)
	newContent := []byte(sb.String())
	if err := os.WriteFile(ebuildPath, newContent, 0644); err != nil {
		return 0, fmt.Errorf("write ebuild: %w", err)
	}
	return entryCount, nil
}

// cargoPrefetchPackage downloads, into distdir, every cargo crate declared by
// every *.ebuild in pkgDir. Files already present and non-empty are skipped.
// Errors on individual crates are logged and skipped.
func cargoPrefetchPackage(pkgDir, distdir string) error {
	// Create distdir if it doesn't exist
	if err := os.MkdirAll(distdir, 0o755); err != nil {
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

	client := httpclient.NewClient()

	for _, ebuildPath := range files {
		// Skip .bak files
		if strings.HasSuffix(ebuildPath, ".ebuild.bak") {
			continue
		}

		data, err := os.ReadFile(ebuildPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[larry] cargo prefetch: skip %s: %v\n", ebuildPath, err)
			continue
		}

		// Extract CRATES variable from ebuild
		crates := extractCrates(string(data))
		if len(crates) == 0 {
			continue
		}

		for _, c := range crates {
			crateURL := cratesDownloadBase + c.Name + "/" + c.Name + "-" + c.Version + ".crate"
			filename := c.Name + "-" + c.Version + ".crate"
			destPath := filepath.Join(distdir, filename)

			// Skip if already exists and is non-empty. Safe: downloads are
			// atomic (rename into place only after the full body matches
			// Content-Length), so a non-empty file is always complete.
			if info, err := os.Stat(destPath); err == nil && info.Size() > 0 {
				continue
			}

			// Download
			resp, err := client.Get(crateURL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[larry] cargo prefetch: %s: %v\n", filename, err)
				continue
			}
			// Close body immediately — we only care about the status code
			_ = resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				fmt.Fprintf(os.Stderr, "[larry] cargo prefetch: %s: HTTP %d\n", filename, resp.StatusCode)
				continue
			}

			// Download the actual file
			resp, err = client.Get(crateURL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[larry] cargo prefetch: %s: %v\n", filename, err)
				continue
			}
			if resp.StatusCode != http.StatusOK {
				_ = resp.Body.Close()
				fmt.Fprintf(os.Stderr, "[larry] cargo prefetch: %s: HTTP %d\n", filename, resp.StatusCode)
				continue
			}

			// Atomic write: a short or failed download must never leave a
			// file that a later run treats as complete.
			if err := atomicWriteBody(resp.Body, destPath, resp.ContentLength); err != nil {
				fmt.Fprintf(os.Stderr, "[larry] cargo prefetch: %s: %v\n", filename, err)
			}
			_ = resp.Body.Close()
		}
	}

	return nil
}

// crateEntry represents a parsed crate from the CRATES variable.
type crateEntry struct {
	Name    string
	Version string
}

var cratesRe = regexp.MustCompile(`(?m)^[ \t]+([^@\s]+)@([^\s]+)\n`)

// extractCrates parses the CRATES variable from ebuild content.

func extractCrates(content string) []crateEntry {
	matches := cratesRe.FindAllStringSubmatch(content, -1)
	var crates []crateEntry
	for _, m := range matches {
		crates = append(crates, crateEntry{Name: m[1], Version: m[2]})
	}
	return crates
}


