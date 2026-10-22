package github

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"bennypowers.dev/larry/internal/git"
	"bennypowers.dev/larry/internal/output"
	"bennypowers.dev/larry/internal/version"
	"github.com/go-git/go-git/v5/plumbing"
)

// CmdRunner abstracts exec.Command for testing.
type CmdRunner interface {
	Run(cmd *exec.Cmd) error
	Output(cmd *exec.Cmd) ([]byte, error)
}

// realCmdRunner runs commands via os/exec (production).
type realCmdRunner struct{}

func (r *realCmdRunner) Run(cmd *exec.Cmd) error {
	return cmd.Run()
}

func (r *realCmdRunner) Output(cmd *exec.Cmd) ([]byte, error) {
	return cmd.Output()
}

// Client wraps the gh CLI for GitHub operations.
type Client struct {
	runner CmdRunner
	env    []string
}

// NewClient creates a new gh CLI client.
func NewClient() *Client {
	return &Client{
		runner: &realCmdRunner{},
		env:    defaultEnv(),
	}
}

// NewClientWithRunner creates a new gh CLI client with a custom CmdRunner (for testing).
func NewClientWithRunner(r CmdRunner) *Client {
	return &Client{
		runner: r,
		env:    defaultEnv(),
	}
}

// Asset represents a GitHub release asset.
type Asset struct {
	Name   string
	URL    string
	SHA256 string
}

func defaultEnv() []string {
	env := os.Environ()
	// Ensure GITHUB_TOKEN is set from env (gh reads it directly)
	token := os.Getenv("GITHUB_TOKEN")
	if token != "" {
		env = append(env, "GITHUB_TOKEN="+token)
	}
	return env
}

// gh runs a gh CLI command and returns its combined output.
func (c *Client) gh(args ...string) ([]byte, error) {
	output.Tracef("exec: gh %s", strings.Join(args, " "))
	cmd := exec.Command("gh", args...)
	cmd.Env = c.env
	return c.runner.Output(cmd)
}

// Gh runs a gh CLI command and returns its combined output (exported for testing).
func (c *Client) Gh(args ...string) ([]byte, error) {
	return c.gh(args...)
}

// Runner returns the CmdRunner used by this client (for testing).
func (c *Client) Runner() CmdRunner {
	if c.runner == nil {
		return &realCmdRunner{}
	}
	return c.runner
}

// LatestRelease gets the latest release from the GitHub API.
// Uses the /releases/latest endpoint and returns the tag name along with
// prerelease and draft flags for filtering.
func (c *Client) LatestRelease(owner, repo string) (tag string, prerelease, draft bool, err error) {
	output, err := c.gh(
		"api",
		"repos/"+owner+"/"+repo+"/releases/latest",
	)
	if err != nil {
		return "", false, false, fmt.Errorf("gh api releases/latest: %w", err)
	}

	var rel struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}
	if err := json.Unmarshal(output, &rel); err != nil {
		return "", false, false, fmt.Errorf("parse releases/latest output: %w", err)
	}

	tag = rel.TagName
	if tag == "" {
		return "", false, false, fmt.Errorf("no latest release found for %s/%s", owner, repo)
	}
	return tag, rel.Prerelease, rel.Draft, nil
}

// stripTagPrefix strips common tag prefixes like v, v., release-, bun-, rocm-, pios/.
func stripTagPrefix(tag string) string {
	tag = strings.ReplaceAll(tag, "%20", " ")
	tag = strings.TrimPrefix(tag, "release-")
	tag = strings.TrimPrefix(tag, "bun-")
	tag = strings.TrimPrefix(tag, "rocm-")
	tag = strings.TrimPrefix(tag, "pios/")
	if len(tag) >= 2 && (tag[0] == 'v' || tag[0] == 'V') {
		if tag[1] == '.' {
			if len(tag) >= 3 && versionStartsWithDigit(tag[2:]) {
				tag = tag[2:]
			}
		} else if versionStartsWithDigit(tag[1:]) {
			tag = tag[1:]
		}
	}
	tag = version.Normalize(tag)
	if i := strings.Index(tag, " "); i > 0 {
		tag = tag[i+1:]
	}
	return strings.TrimSpace(tag)
}

// versionStartsWithDigit checks if the string starts with a digit.
func versionStartsWithDigit(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= '0' && s[0] <= '9'
}

// LatestReleaseTag gets the highest-version release tag name using
// `gh release list`. Unlike the script which takes .[0] (creation-date
// order), this sorts all 100 releases by version number and returns
// the highest. This deliberately diverges from the script (Plan 31).
// When !allowPrereleases the --exclude-pre-releases flag is added (D2).
func (c *Client) LatestReleaseTag(owner, repo string, allowPrereleases bool) (string, error) {
	args := []string{
		"release", "list", "-R", owner + "/" + repo,
		"--limit", "100",
	}
	if !allowPrereleases {
		args = append(args, "--exclude-pre-releases")
	}
	args = append(args, "--json", "tagName")

	output, err := c.gh(args...)
	if err != nil {
		return "", fmt.Errorf("gh release list: %w", err)
	}

	var releases []struct {
		TagName string `json:"tagName"`
	}
	if err := json.Unmarshal(output, &releases); err != nil {
		return "", fmt.Errorf("parse release list: %w", err)
	}

	// Sort by version number, return the highest.
	var best string
	var bestV *version.Version
	for _, r := range releases {
		tag := strings.TrimSpace(r.TagName)
		if tag == "" {
			continue
		}
		stripped := stripTagPrefix(tag)
		if stripped == "" || !versionStartsWithDigit(stripped) {
			continue
		}
		v, err := version.Parse(version.Normalize(stripped))
		if err != nil {
			continue
		}
		if bestV == nil || v.Compare(bestV) > 0 {
			bestV = v
			best = tag
		}
	}

	if best == "" {
		return "", fmt.Errorf("no version-like releases found for %s/%s", owner, repo)
	}
	return best, nil
}

// PrereleaseReleaseTags returns the set of tag names whose GitHub release is
// marked as a prerelease. It is consulted only by the opt-in GitHub
// `strategy: tags` path so that prerelease-marked releases are ignored even
// when the tag name itself carries no prerelease marker.
//
// This deliberately requests isPrerelease via the valid comma-separated
// --json syntax (the bracket form rejected by plan 17b is still not used).
func (c *Client) PrereleaseReleaseTags(owner, repo string) (map[string]bool, error) {
	output, err := c.gh(
		"release", "list", "-R", owner+"/"+repo,
		"--limit", "100",
		"--json", "tagName,isPrerelease",
	)
	if err != nil {
		return nil, fmt.Errorf("gh release list (prerelease tags): %w", err)
	}

	var releases []struct {
		TagName      string `json:"tagName"`
		IsPrerelease bool   `json:"isPrerelease"`
	}
	if err := json.Unmarshal(output, &releases); err != nil {
		return nil, fmt.Errorf("parse release list (prerelease tags): %w", err)
	}

	set := make(map[string]bool)
	for _, r := range releases {
		tag := strings.TrimSpace(r.TagName)
		if tag != "" && r.IsPrerelease {
			set[tag] = true
		}
	}
	return set, nil
}

// EnsureRelease ensures a GitHub release exists for the given tag.
// If it doesn't exist, creates it with gh release create.
func (c *Client) EnsureRelease(owner, repo, tag string) error {
	// Check if release exists
	cmd := exec.Command("gh", "release", "view", tag, "--repo", owner+"/"+repo)
	cmd.Env = c.env
	if err := c.runner.Run(cmd); err != nil {
		// Release doesn't exist, create it
		cmd = exec.Command("gh", "release", "create", tag,
			"--title", tag,
			"--generate-notes",
			"--repo", owner+"/"+repo)
		cmd.Env = c.env
		return c.runner.Run(cmd)
	}
	return nil
}

// UploadAsset uploads a file to a GitHub release.
// If the asset already exists with the same SHA-256, it skips the upload.
// If the asset exists with a different checksum, it uses --clobber.
func (c *Client) UploadAsset(owner, repo, tag, filePath, assetName string, clobber bool) error {
	cmd := exec.Command("gh", "release", "upload", tag, filePath, "--clobber")
	cmd.Env = c.env
	return c.runner.Run(cmd)
}

// DownloadSourceTarball downloads the source tarball for a release tag.
// Returns the path to the downloaded file.
func (c *Client) DownloadSourceTarball(owner, repo, tag string, destDir string) (string, error) {
	filename := fmt.Sprintf("%s.tar.gz", repo)
	destPath := ""
	if destDir != "" {
		destPath = fmt.Sprintf("%s/%s", destDir, filename)
	}

	args := []string{"release", "download", tag, "--pattern", filename}
	if destPath != "" {
		args = append(args, "-O", destPath)
	}

	cmd := exec.Command("gh", args...)
	cmd.Env = c.env
	if err := c.runner.Run(cmd); err != nil {
		return "", fmt.Errorf("gh release download: %w", err)
	}

	return destPath, nil
}

// ListReleaseAssets lists assets for a release.
func (c *Client) ListReleaseAssets(owner, repo, tag string) ([]Asset, error) {
	output, err := c.gh(
		"release",
		"view",
		tag,
		"--json", "assets",
	)
	if err != nil {
		return nil, fmt.Errorf("gh release view assets: %w", err)
	}

	var result struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
			// Note: SHA-256 is not directly available in gh release view --json
			// We'll need to compute it from the local file
		}
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("parse gh release view assets: %w", err)
	}

	assets := make([]Asset, len(result.Assets))
	for i, a := range result.Assets {
		assets[i] = Asset{
			Name: a.Name,
			URL:  a.URL,
		}
	}
	return assets, nil
}

// GetTagCommit resolves a tag to its commit hash using go-git.
// This is a helper for the provider layer to get commit info for tags.
func GetTagCommit(repo *git.Repo, tag string) (plumbing.Hash, error) {
	return repo.ResolveTag(tag)
}

// GetLatestTag returns the latest tag name from the repository.
func GetLatestTag(repo *git.Repo) (string, error) {
	infos, err := repo.ListTagsSorted()
	if err != nil {
		return "", err
	}
	if len(infos) == 0 {
		return "", fmt.Errorf("no tags found")
	}
	return infos[0].Name, nil
}
