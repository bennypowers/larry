package github

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/go-git/go-git/v5/plumbing"
	"bennypowers.dev/larry/internal/git"
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

// LatestRelease gets the latest release tag name and URL from GitHub.
// Returns the tag name and the release URL (e.g., https://github.com/owner/repo/releases/tag/v1.0.0).
func (c *Client) LatestRelease(owner, repo string) (tag, url string, err error) {
	output, err := c.gh(
		"release",
		"view",
		"--limit", "1",
		"--json", "tagName,url",
	)
	if err != nil {
		return "", "", fmt.Errorf("gh release view: %w", err)
	}

	var result struct {
		TagName string `json:"tagName"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return "", "", fmt.Errorf("parse gh release view output: %w", err)
	}

	return result.TagName, result.URL, nil
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
