package providers

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"bennypowers.dev/larry/internal/github"
	"bennypowers.dev/larry/internal/git"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// GitHubProvider implements upstream version discovery for GitHub.
// Uses the gh CLI for all GitHub API interaction.
type GitHubProvider struct {
	RateLimiter *ratelimit.RateLimiter
	Client      *github.Client
}

func (p *GitHubProvider) Type() remoteid.RemoteIdType {
	return remoteid.TypeGitHub
}

func (p *GitHubProvider) Metadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		Name:        "GitHub",
		Description: "GitHub releases and tags",
		Homepage:    "https://github.com",
	}
}

func (p *GitHubProvider) Name() string {
	return "GitHub"
}

// FetchLatestVersion fetches the latest release version from GitHub.
// It uses the gh CLI to query the latest release.
// If allowPrereleases is true, prereleases are included in the results.
// Falls back to git ls-remote for non-standard tags (e.g., pios/1%20260519-1_...).
func (p *GitHubProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if rid.Owner == "" || rid.Repo == "" {
		return nil, fmt.Errorf("github: missing owner/repo in RemoteId")
	}

	if p.Client == nil {
		p.Client = github.NewClient()
	}

	// Try to get both release and tag versions, then return the highest one.
	// This handles repos where the "latest release" (by publish date) is not
	// the highest version (e.g., comfy-aimdo has a v0.0.0 release but v0.5.5 tag).
	var bestVersion *version.Version
	var bestTag string

	// 1. Try gh release list (latest release by publish date)
	relTag, _, err := p.Client.LatestRelease(rid.Owner, rid.Repo)
	if err == nil && relTag != "" {
		if v, err := tryParseVersion(relTag); err == nil {
			if !allowPrereleases && v.IsPrerelease() {
				// Release is a prerelease — don't use it, but continue checking tags
			} else if bestVersion == nil || v.Compare(bestVersion) > 0 {
				bestVersion = v
				bestTag = relTag
			}
		}
	}

	// 2. Try gh api tags (latest tag by commit date)
	tagTag, err := p.Client.LatestTag(rid.Owner, rid.Repo)
	if err == nil && tagTag != "" {
		if v, err := tryParseVersion(tagTag); err == nil {
			if !allowPrereleases && v.IsPrerelease() {
				// Tag is a prerelease — don't use it, but continue
			} else if bestVersion == nil || v.Compare(bestVersion) > 0 {
				bestVersion = v
				bestTag = tagTag
			}
		}
	}

	if bestVersion != nil {
		return bestVersion, nil
	}

	// Both release and tag returned non-version tags (e.g., pios/1%20...).
	// Fall back to git ls-remote to find version-like tags.
	tag, err := fetchLatestTagByRemote(rid.Owner, rid.Repo)
	if err != nil {
		// 3. Fall back to GraphQL date-based resolution
		tag, err = fetchLatestTagByDate(rid.Owner, rid.Repo)
		if err != nil {
			return nil, fmt.Errorf("github: failed to resolve latest version for %s/%s: %w", rid.Owner, rid.Repo, err)
		}
	}

	v, err := tryParseVersion(tag)
	if err != nil {
		// 4. Fall back to date-based comparison for non-standard tags
		v, err = extractVersionFromDate(tag)
		if err != nil {
			return nil, fmt.Errorf("github: failed to resolve version from %q: %w", tag, err)
		}
	}

	if !allowPrereleases && v.IsPrerelease() {
		return nil, fmt.Errorf("github: latest release %q is a prerelease", bestTag)
	}

	return v, nil
}

// tryParseVersion tries to parse a tag as a version, stripping common prefixes.
func tryParseVersion(tag string) (*version.Version, error) {
	stripped := stripTagPrefix(tag)
	if stripped == "" || !versionStartsWithDigit(stripped) {
		return nil, fmt.Errorf("tag %q does not look like a version", tag)
	}
	norm := version.Normalize(stripped)
	return version.Parse(norm)
}

// versionStartsWithDigit checks if the string starts with a digit.
func versionStartsWithDigit(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= '0' && s[0] <= '9'
}

// stripTagPrefix strips common tag prefixes like v, v., release-, bun-, rocm-, pios/.
// Also handles series-number formats like "1 20260519-1_bpo" -> "20260519-1_bpo"
func stripTagPrefix(tag string) string {
	// URL-decode %20 -> space and other common encodings
	tag = strings.ReplaceAll(tag, "%20", " ")
	// Strip leading prefixes
	tag = strings.TrimPrefix(tag, "release-")
	tag = strings.TrimPrefix(tag, "bun-")
	tag = strings.TrimPrefix(tag, "rocm-")
	tag = strings.TrimPrefix(tag, "pios/")
	// Strip v or v. prefix
	if len(tag) >= 2 && (tag[0] == 'v' || tag[0] == 'V') {
		if tag[1] == '.' {
			// v. prefix: strip both v and .
			if len(tag) >= 3 && versionStartsWithDigit(tag[2:]) {
				tag = tag[2:]
			}
		} else if versionStartsWithDigit(tag[1:]) {
			// v prefix: strip just v
			tag = tag[1:]
		}
	}
	// Strip ^{...} annotation (from annotated tags) — via Normalize (D6)
	tag = version.Normalize(tag)
	// Handle series-number formats: "1 20260519-1_bpo" -> "20260519-1_bpo"
	// Common in Raspberry Pi OS packages where first number is series
	if i := strings.Index(tag, " "); i > 0 {
		tag = tag[i+1:]
	}
	return strings.TrimSpace(tag)
}

// fetchLatestTagByRemote uses git ls-remote to find the latest version-like tag.
func fetchLatestTagByRemote(owner, repo string) (string, error) {
	cmd := exec.Command(
		"git", "ls-remote", "--tags",
		fmt.Sprintf("https://github.com/%s/%s.git", owner, repo),
	)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote: %w", err)
	}

	// Parse output: <hash>\trefs/tags/<tag>
	var tags []string
	for _, line := range strings.Split(string(output), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		tag := strings.TrimPrefix(parts[1], "refs/tags/")
		stripped := stripTagPrefix(tag)
		if stripped != "" && versionStartsWithDigit(stripped) {
			tags = append(tags, stripped)
		}
	}

	if len(tags) == 0 {
		return "", fmt.Errorf("no version-like tags found")
	}

	// Sort versions and return the latest
	return sortAndLatestVersion(tags)
}

// sortAndLatestVersion sorts versions and returns the latest.
func sortAndLatestVersion(tags []string) (string, error) {
	if len(tags) == 0 {
		return "", fmt.Errorf("no tags provided")
	}
	var latest *version.Version
	var latestTag string
	for _, tag := range tags {
		norm := version.Normalize(tag)
		v, err := version.Parse(norm)
		if err != nil {
			continue
		}
		if latest == nil || v.Compare(latest) > 0 {
			latest = v
			latestTag = tag
		}
	}
	if latest == nil {
		return "", fmt.Errorf("no valid versions found in %d tags", len(tags))
	}
	return latestTag, nil
}

// extractVersionFromDate extracts a YYYYMMDD date from non-standard tag names.
// Uses version.ExtractDate — exactly 8 digits (20\d{6}), no 6-digit fallback (D8).
func extractVersionFromDate(tag string) (*version.Version, error) {
	rawDate, ok := version.ExtractDate(tag)
	if !ok {
		return nil, fmt.Errorf("no date found in tag %q", tag)
	}

	norm := version.Normalize(rawDate)
	return version.Parse(norm)
}

// fetchLatestTagByDate uses the GitHub GraphQL API to find the latest tagged commit.
func fetchLatestTagByDate(owner, repo string) (string, error) {
	query := `
	query($owner: String!, $name: String!) {
		repository(owner: $owner, name: $name) {
			refs(refPrefix: "refs/tags/", orderBy: {field: TAG_COMMIT_DATE, direction: DESC}, first: 10) {
				nodes {
					name
					target {
						... on Commit { committedDate }
						... on Tag { target { ... on Commit { committedDate } } tagger { date } }
					}
				}
			}
		}
	}`

	cmd := exec.Command(
		"gh", "api", "graphql",
		"-f", "query="+query,
		"-f", "owner="+owner,
		"-f", "name="+repo,
	)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh api graphql: %w", err)
	}

	// Parse JSON to extract tag names and dates
	var result struct {
		Data struct {
			Repository struct {
				Refs struct {
					Nodes []struct {
						Name string `json:"name"`
						Target struct {
							CommittedDate string `json:"committedDate"`
							TaggerDate    string `json:"date"`
						}
					} `json:"nodes"`
				} `json:"refs"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return "", fmt.Errorf("parse graphql response: %w", err)
	}

	// Find the first tag that looks like a version
	for _, node := range result.Data.Repository.Refs.Nodes {
		tag := strings.TrimSpace(node.Name)
		stripped := stripTagPrefix(tag)
		if stripped != "" && versionStartsWithDigit(stripped) {
			return stripped, nil
		}
	}

	return "", fmt.Errorf("no version-like tags found via date-based resolution")
}

// FetchLatestVersionWithRepo fetches the latest version from a local repo's origin.
// This is used by the upstream command when a remote-id is available but we need
// to resolve tags from the local git repo.
func (p *GitHubProvider) FetchLatestVersionWithRepo(repo *git.Repo) (*version.Version, error) {
	if repo == nil {
		return nil, fmt.Errorf("github: nil repo")
	}

	tag, err := github.GetLatestTag(repo)
	if err != nil {
		return nil, fmt.Errorf("github: failed to get latest tag: %w", err)
	}

	normTag := version.Normalize(tag)
	v, err := version.Parse(normTag)
	if err != nil {
		return nil, fmt.Errorf("github: failed to parse version %q: %w", normTag, err)
	}

	return v, nil
}

// IsPrerelease reports whether v is a pre-release version.
// Delegates to version.Version.IsPrerelease().
func (p *GitHubProvider) IsPrerelease(v *version.Version) bool {
	return v.IsPrerelease()
}

// NewGitHubProvider creates a new GitHubProvider with the given rate limiter.
func NewGitHubProvider(rl *ratelimit.RateLimiter) provider.UpstreamProvider {
	return &GitHubProvider{
		RateLimiter: rl,
		Client:      github.NewClient(),
	}
}

// NewGitHubProviderWithClient creates a new GitHubProvider with a specific gh CLI client.
// Useful for testing with a stub client.
func NewGitHubProviderWithClient(rl *ratelimit.RateLimiter, client *github.Client) provider.UpstreamProvider {
	return &GitHubProvider{
		RateLimiter: rl,
		Client:      client,
	}
}
