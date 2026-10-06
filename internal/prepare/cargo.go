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
	"bennypowers.dev/larry/internal/logger"
	"sort"
)

// lookPath is exec.LookPath, stubbable so tests can exercise the native parser
// even on a workstation that has pycargoebuild installed.
var lookPath = exec.LookPath

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

	// 1. Find the upstream repository (owner/repo)
	owner, repo := findCargoRepo(ei)

	// 2. Find and download Cargo.lock
	clientGH := github.NewClient()
	lockPath, err := findCargoLockAndDownload(clientGH, owner, repo, ei.PV())
	if err != nil {
		return fmt.Errorf("cargo: find and download Cargo.lock: %w", err)
	}

	// 3. Parse Cargo.lock
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("cargo: read Cargo.lock: %w", err)
	}
	crates := parseCargoLock(string(lockContent))
	if len(crates) == 0 {
		return fmt.Errorf("cargo: no crates found in Cargo.lock")
	}

	// 4. Generate CRATES variable and update ebuild
	cratesStr := formatCRATES(crates)
	count, err := updateCargoEbuild(ei, cratesStr, crates, cfg)
	if err != nil {
		return fmt.Errorf("cargo: update ebuild: %w", err)
	}
	logger.Debug("updated CRATES with " + fmt.Sprintf("%d", count) + " crate entries")

	// 5. Generate Manifest
	if err := runManifest(manifestRequest{
		Overlay:    cfg.Overlay,
		CatPkg:     ei.Category + "/" + ei.PN(),
		Distdir:    cfg.Distdir,
		EbuildPath: ei.Path,
		Prefetch:   nil,
	}); err != nil {
		return fmt.Errorf("cargo: manifest: %w", err)
	}

	return nil
}

// findCargoRepo extracts the GitHub owner/repo from SRC_URI or HOMEPAGE.
func findCargoRepo(ei *ebuild.EbuildInfo) (owner, repo string) {
	// Try HOMEPAGE first: https://github.com/owner/repo
	if ei.HOMEPAGE != "" {
		re := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)`)
		matches := re.FindStringSubmatch(ei.HOMEPAGE)
		if len(matches) == 3 {
			return strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2])
		}
	}

	// Try SRC_URI: mirror://github/${PN}/${P}.tar.gz
	// or https://github.com/owner/repo/...
	if ei.SRC_URI != "" {
		re := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)`)
		matches := re.FindStringSubmatch(ei.SRC_URI)
		if len(matches) == 3 {
			return strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2])
		}
	}

	return "", ""
}

// findCargoLockAndDownload finds and downloads Cargo.lock from upstream.
// Returns the local path to the downloaded Cargo.lock.
func findCargoLockAndDownload(client *github.Client, owner, repo, tag string) (lockPath string, err error) {
	// Try the GitHub tree API for Cargo.lock
	if owner != "" && repo != "" {
		return findCargoLockFromTreeAPI(client, owner, repo, tag)
	}
	return "", fmt.Errorf("cargo: could not find Cargo.lock: no owner/repo")
}

// findCargoLockFromTreeAPI tries to find Cargo.lock in the GitHub tree API.
// The tag parameter is used as the ref when querying the GitHub API to ensure
// the correct version's Cargo.lock is fetched (critical for version bumps and revisions).
// It handles common tag naming conventions: with or without the "v" prefix (e.g., "0.3.3" → "v0.3.3").
func findCargoLockFromTreeAPI(client *github.Client, owner, repo, tag string) (string, error) {
	// Common paths to try
	fallbackPaths := []string{
		"Cargo.lock",
		"src/Cargo.lock",
		"rust/Cargo.lock",
		"lib/Cargo.lock",
		"crates/Cargo.lock",
	}

	// Determine the ref to try, with common tag naming conventions.
	var refs []string
	if tag != "" {
		refs = append(refs, tag)
		// Try with "v" prefix if the tag doesn't already have it
		if !strings.HasPrefix(tag, "v") && !strings.HasPrefix(tag, "V") {
			refs = append(refs, "v"+tag)
		}
		// Also try without "v" prefix (in case tag has it)
		if strings.HasPrefix(tag, "v") || strings.HasPrefix(tag, "V") {
			refs = append(refs, tag[1:])
		}
	}
	refs = append(refs, "") // empty string = default branch (no ?ref= param)

	for _, ref := range refs {
		for _, path := range fallbackPaths {
			// Build the API call
			refParam := ""
			if ref != "" {
				refParam = "?ref=" + ref
			}
			apiCall := "repos/" + owner + "/" + repo + "/contents/" + path + refParam

			// Use "repos/" prefix required by gh api for the contents endpoint.
			output, err := client.Gh("api", apiCall)
			if err != nil {
				continue
			}

			// Parse JSON to get download URL
			var result struct {
				Name        string `json:"name"`
				DownloadURL string `json:"download_url"`
			}
			if err := json.Unmarshal(output, &result); err != nil {
				continue
			}

			// If download_url is empty, the response may be a base64-encoded blob.
			// In that case, decode the "content" field as fallback.
			if result.DownloadURL == "" {
				var blobResult struct {
					Content  string `json:"content"`
					Encoding string `json:"encoding"`
				}
				if json.Unmarshal(output, &blobResult) == nil && blobResult.Content != "" {
					tmpDir, err := os.MkdirTemp("", "larry-cargo-*")
					if err != nil {
						continue
					}
					destPath := filepath.Join(tmpDir, "Cargo.lock")
					if err := os.WriteFile(destPath, []byte(blobResult.Content), 0644); err != nil {
						continue
					}
					return destPath, nil
				}
				continue
			}

			// Download the Cargo.lock
			tmpDir, err := os.MkdirTemp("", "larry-cargo-*")
			if err != nil {
				continue
			}
			destPath := filepath.Join(tmpDir, "Cargo.lock")
			clientHTTP := httpclient.NewClient()
			if err := downloadCargoLock(result.DownloadURL, destPath, clientHTTP); err != nil {
				continue
			}
			return destPath, nil
		}
	}

	return "", fmt.Errorf("cargo: Cargo.lock not found in %s/%s", owner, repo)
}

// findCargoLockFromTree is the exported version for testing (same as tree API above).
func findCargoLockFromTree(client *github.Client, owner, repo, tag string) (string, error) {
	return findCargoLockFromTreeAPI(client, owner, repo, tag)
}

// downloadCargoLock downloads a Cargo.lock from a URL.
func downloadCargoLock(url, dest string, client *httpclient.Client) error {
	// Create directory if needed
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
	defer resp.Body.Close()

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
// Returns the number of crate entries written.
func updateCargoEbuild(ei *ebuild.EbuildInfo, cratesStr string, crates []CargoCrate, cfg *PrepareConfig) (int, error) {
	ebuildPath := ei.Path

	if cfg.DryRun {
		return 0, nil
	}

	// Check if pycargoebuild is available for better update
	_, pycargoErr := lookPath("pycargoebuild")
	if pycargoErr == nil {
		// Try pycargoebuild --inplace update
		tmpDir := filepath.Dir(ebuildPath)
		// Create a minimal Cargo.toml for pycargoebuild
		cargoToml := fmt.Sprintf(`[package]
name = "%s"
version = "%s"
edition = "2021"
`, ei.Pkg, ei.PV())
		cargoTomlPath := filepath.Join(tmpDir, "Cargo.toml")
		if err := os.WriteFile(cargoTomlPath, []byte(cargoToml), 0644); err != nil {
			// Fall through to native parsing
		} else {
			defer os.Remove(cargoTomlPath)

			// pycargoebuild requires Cargo.lock in the working directory.
			// If it's not present, fall through to native parsing.
			cargoLockPath := filepath.Join(tmpDir, "Cargo.lock")
			if _, err := os.Stat(cargoLockPath); err != nil {
				// No Cargo.lock — native parsing will handle it
			} else {
				runner := github.NewClient().Runner()
				if err := runPycargoebuild(tmpDir, ebuildPath, runner); err != nil {
					// pycargoebuild failed, fall through to native
					count, err := updateCargoEbuildNative(ebuildPath, crates)
					return count, err
				}
				return 0, nil
			}
		}
	}

	// Native parsing fallback
	return updateCargoEbuildNative(ebuildPath, crates)
}

// runPycargoebuild runs pycargoebuild --inplace on the ebuild.
func runPycargoebuild(dir, ebuildPath string, runner github.CmdRunner) error {
	cmd := exec.Command("pycargoebuild", dir, "--no-write-crate-tarball", "--no-manifest", "--inplace", ebuildPath)
	cmd.Dir = dir
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


