package providers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"bennypowers.dev/larry/internal/github"
	"bennypowers.dev/larry/internal/git"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// graphqlTagsQuery is the GraphQL query used by fetchLatestTagByDate.
// It is exported for tests via the testutil package so they can build the
// exact stub key used in production.
const graphqlTagsQuery = `
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

// GitHubProvider implements upstream version discovery for GitHub.
// Uses the gh CLI for all GitHub API interaction.
type GitHubProvider struct {
	RateLimiter *ratelimit.RateLimiter
	Client      *github.Client
	// TagLister lists remote tag names for a clone URL. Defaults to
	// git.ListRemoteTags; tests substitute a closure.
	TagLister func(url string) ([]string, error)
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

// var _ provider.ResolverWithTags = (*GitHubProvider)(nil)

// FetchResolution implements the ResolverWithTags interface. It runs the
// 4-tier cascade (D1-D3) and returns a Resolution. Tiers 1→2 are reconciled;
// tier 3 (go-git tag listing) and tier 4 (date-based GraphQL) are fallbacks.
func (p *GitHubProvider) FetchResolution(rid remoteid.RemoteId, allowPrereleases bool) (*provider.Resolution, error) {
	if rid.Owner == "" || rid.Repo == "" {
		return nil, fmt.Errorf("github: missing owner/repo in RemoteId")
	}

	if p.Client == nil {
		p.Client = github.NewClient()
	}

	// Tier 1 — official latest release via /releases/latest (D1).
	relTag, prerelease, draft, _ := p.Client.LatestRelease(rid.Owner, rid.Repo)
	var relV *version.Version
	if relTag != "" && !draft && (!prerelease || allowPrereleases) {
		if v, err := tryParseVersion(relTag); err == nil {
			relV = v
		}
	}

	// Tier 2 — newest release from the release list (D2).
	tagTag, _ := p.Client.LatestReleaseTag(rid.Owner, rid.Repo, allowPrereleases)
	var tagV *version.Version
	if tagTag != "" {
		if v, err := tryParseVersion(tagTag); err == nil {
			tagV = v
		}
	}

	// Reconcile tiers 1 and 2: if both are version-like, return the newer;
	// else whichever is version-like; else fall through to tier 3.
	if relV != nil && tagV != nil {
		if relV.Compare(tagV) > 0 {
			return &provider.Resolution{Version: relV.Format()}, nil
		}
		return &provider.Resolution{Version: tagV.Format()}, nil
	}
	if relV != nil {
		return &provider.Resolution{Version: relV.Format()}, nil
	}
	if tagV != nil {
		return &provider.Resolution{Version: tagV.Format()}, nil
	}

	// Tier 3 — remote tags via go-git.
	tag, err := p.fetchLatestTagByRemote(rid.Owner, rid.Repo)
	if err == nil && tag != "" {
		if v, err := tryParseVersion(tag); err == nil {
			return &provider.Resolution{Version: v.Format()}, nil
		}
	}

	// Tier 4 — tag by commit date (D3, D6).
	tag, date, err := p.fetchLatestTagByDate(rid.Owner, rid.Repo)
	if err == nil && tag != "" {
		return &provider.Resolution{Tag: tag, TagDate: date, ByDate: true}, nil
	}

	return nil, fmt.Errorf("github: failed to resolve latest version for %s/%s", rid.Owner, rid.Repo)
}

// FetchLatestVersion fetches the latest release version from GitHub.
// It is a thin wrapper over FetchResolution that returns an error when
// the resolution came from the date-based path (ByDate=true).
// If allowPrereleases is true, prereleases are included in the results.
// Falls back to git ls-remote for non-standard tags (e.g., pios/1%20260519-1_...).
func (p *GitHubProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	res, err := p.FetchResolution(rid, allowPrereleases)
	if err != nil {
		return nil, err
	}
	if res.ByDate {
		// Date-based resolution — cannot compare with ebuild PV via version.Compare.
		// For backward compatibility, return the tag extracted as a version.
		tag := res.Tag
		if tag == "" {
			return nil, fmt.Errorf("github: date-based resolution produced no tag")
		}
		// Try to parse tag as a version first
		if v, err := tryParseVersion(tag); err == nil {
			if !allowPrereleases && v.IsPrerelease() {
				return nil, fmt.Errorf("github: latest tag %q is a prerelease", tag)
			}
			return v, nil
		}
		// Fall back to date-based version extraction
		v, err := extractVersionFromDate(tag)
		if err != nil {
			return nil, fmt.Errorf("github: failed to resolve version from %q: %w", tag, err)
		}
		return v, nil
	}
	if res.Version != "" {
		v, err := version.Parse(res.Version)
		if err != nil {
			return nil, fmt.Errorf("github: failed to parse version %q: %w", res.Version, err)
		}
		if !allowPrereleases && v.IsPrerelease() {
			return nil, fmt.Errorf("github: latest tag %q is a prerelease", res.Version)
		}
		return v, nil
	}
	return nil, fmt.Errorf("github: no version resolved for %s/%s", rid.Owner, rid.Repo)
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

// fetchLatestTagByRemote lists remote tags via go-git and returns the latest
// version-like tag.
func (p *GitHubProvider) fetchLatestTagByRemote(owner, repo string) (string, error) {
	if p.TagLister == nil {
		p.TagLister = git.ListRemoteTags
	}

	url := "https://github.com/" + owner + "/" + repo + ".git"
	rawTags, err := p.TagLister(url)
	if err != nil {
		return "", fmt.Errorf("list remote tags for %s/%s: %w", owner, repo, err)
	}

	// Filter to version-like tags and sort
	var tags []string
	for _, tag := range rawTags {
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

// fetchLatestTagByDate uses the GitHub GraphQL API to find the latest
// tagged commit. Returns (tag, date, nil) or an error.
// Skips tags matching the pre-release name filter (D6).
// Prefers a date extracted from the tag name itself (D3).
func (p *GitHubProvider) fetchLatestTagByDate(owner, repo string) (tag, date string, err error) {
	prereleaseTagRe := regexp.MustCompile(`(?i)alpha|beta|rc|dev|nightly|canary`)

	output, err := p.Client.Gh("api", "graphql", "-f", "query="+graphqlTagsQuery, "-f", "owner="+owner, "-f", "name="+repo)
	if err != nil {
		return "", "", fmt.Errorf("gh api graphql: %w", err)
	}

	// The GraphQL query returns target as either a Commit or a Tag.
	// Commit: {"committedDate": "..."}
	// Tag:    {"target": {"committedDate": "..."}, "tagger": {"date": "..."}}
	var result struct {
		Data struct {
			Repository struct {
				Refs struct {
					Nodes []struct {
						Name   string `json:"name"`
						Target targetUnion
					} `json:"nodes"`
				} `json:"refs"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return "", "", fmt.Errorf("parse graphql response: %w", err)
	}

	// Find the first node that produces a non-empty date.
	for _, node := range result.Data.Repository.Refs.Nodes {
		rawName := strings.TrimSpace(node.Name)
		// D6: Skip pre-release tag names
		if prereleaseTagRe.MatchString(rawName) {
			continue
		}

		// D3: Try to extract a date from the tag name itself
		if extractedDate, ok := version.ExtractDate(rawName); ok {
			return rawName, extractedDate, nil
		}

		// Otherwise, take the first non-empty date from the commit data
		raw := node.Target.dateString()
		if len(raw) >= 10 {
			return rawName, strings.ReplaceAll(raw[:10], "-", ""), nil
		}
	}

	return "", "", fmt.Errorf("no version-like tags found via date-based resolution")
}

// targetUnion captures the GraphQL "target" field which can be a Commit
// or a Tag, each with different nested structures.
type targetUnion struct {
	CommittedDate string `json:"committedDate"`
	Target        *struct {
		CommittedDate string `json:"committedDate"`
	} `json:"target"`
	Tagger *struct {
		Date string `json:"date"`
	} `json:"tagger"`
}

// dateString returns the first non-empty date string from the union.
func (t targetUnion) dateString() string {
	if t.CommittedDate != "" {
		return t.CommittedDate
	}
	if t.Target != nil && t.Target.CommittedDate != "" {
		return t.Target.CommittedDate
	}
	if t.Tagger != nil && t.Tagger.Date != "" {
		return t.Tagger.Date
	}
	return ""
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
