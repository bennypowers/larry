package github

import (
	"os/exec"
)

// Client wraps the gh CLI for GitHub operations.
type Client struct{}

// NewClient creates a new gh CLI client.
func NewClient() *Client {
	return &Client{}
}

// LatestRelease gets the latest release name from GitHub.
func (c *Client) LatestRelease(owner, repo string) (string, error) {
	cmd := exec.Command("gh", "release", "list", "--limit", "1", "--json", "tagName", "-q", ".[0].tagName")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// UploadAttachment uploads a file to a GitHub release.
func (c *Client) UploadAttachment(owner, repo, release, file string) error {
	cmd := exec.Command("gh", "release", "upload", release, file)
	return cmd.Run()
}
