package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"bennypowers.dev/larry/internal/httpclient"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/ratelimit"
	"bennypowers.dev/larry/internal/version"
)

// SourceHutProvider implements upstream version discovery for SourceHut.
//
// API Strategy: SourceHut does not have a conventional "latest release" REST endpoint.
// We use the GraphQL API at https://api.sr.ht to query refs/tags sorted by date,
// or fall back to parsing the git-refs page at https://git.sr.ht/~user/project/refs.
//
// The GraphQL approach requires authentication via GITHUB_TOKEN or a SourceHut API token.
// The HTML fallback approach works without auth but is slower.
type SourceHutProvider struct {
	RateLimiter *ratelimit.RateLimiter
	Client      *httpclient.Client
}

func (p *SourceHutProvider) Type() remoteid.RemoteIdType {
	return remoteid.TypeSourceHut
}

func (p *SourceHutProvider) Metadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		Name:        "SourceHut",
		Description: "SourceHut git hosting",
		Homepage:    "https://sr.ht",
	}
}

func (p *SourceHutProvider) Name() string {
	return "SourceHut"
}

// sourceHutGraphQLResponse represents the response from SourceHut's GraphQL API
type sourceHutGraphQLResponse struct {
	User struct {
		Repositories struct {
			Edges []struct {
				Node struct {
					Name  string `json:"name"`
					Refs  struct {
						Edges []struct {
							Node struct {
								Name string `json:"name"`
								Target struct {
									Oid string `json:"oid"`
								} `json:"target"`
							} `json:"node"`
						} `json:"edges"`
					} `json:"refs"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"repositories"`
	} `json:"user"`
}

// sourceHutGraphQLRequest is the request body for the GraphQL query
type sourceHutGraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

func (p *SourceHutProvider) FetchLatestVersion(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	if rid.Host == "" || rid.Path == "" {
		return nil, fmt.Errorf("sourcehut: missing host/path in RemoteId")
	}

	if p.Client == nil {
		p.Client = httpclient.NewClient()
	}

	// Try GraphQL API first
	v, err := p.fetchViaGraphQL(rid, allowPrereleases)
	if err == nil {
		return v, nil
	}
	// GraphQL may fail without auth, fall back to HTML parsing

	// Fallback: parse the refs page
	return p.fetchViaHTML(rid, allowPrereleases)
}

// fetchViaGraphQL queries SourceHut's GraphQL API for the latest tag.
func (p *SourceHutProvider) fetchViaGraphQL(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	// Extract user and project from path (format: ~user/project)
	path := rid.Path
	if !strings.HasPrefix(path, "~") {
		return nil, fmt.Errorf("sourcehut: invalid path format %q, expected ~user/project", path)
	}

	parts := strings.SplitN(strings.TrimPrefix(path, "~"), "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("sourcehut: invalid path format %q", path)
	}

	user := parts[0]
	project := parts[1]

	// Build GraphQL query
	query := `
		query GetUserRepos($user: String!) {
			user(login: $user) {
				repositories(first: 10) {
					edges {
						node {
							name
							refs(refPrefix: "refs/tags/", first: 10, orderBy: {field: CREATED_AT, direction: DESC}) {
								edges {
									node {
										name
										target {
											... on Commit {
												oid
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	`

	reqBody := sourceHutGraphQLRequest{
		Query: query,
		Variables: map[string]interface{}{
			"user": user,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("sourcehut: failed to marshal request: %w", err)
	}

	apiURL := "https://api.sr.ht/gitea"
	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("sourcehut: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Use DoWithRetry for rate limiting
	resp, err := p.Client.DoWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("sourcehut: failed to query GraphQL API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sourcehut: failed to read response: %w", err)
	}

	// Check for auth error
	var gqlResp struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Data *sourceHutGraphQLResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return nil, fmt.Errorf("sourcehut: failed to parse response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("sourcehut: GraphQL error: %s", gqlResp.Errors[0].Message)
	}

	if gqlResp.Data == nil {
		return nil, fmt.Errorf("sourcehut: no data in response")
	}

	// Find the matching repository
	for _, edge := range gqlResp.Data.User.Repositories.Edges {
		if edge.Node.Name == project {
			// Get the first tag (most recent by created_at)
			for _, refEdge := range edge.Node.Refs.Edges {
				tagName := refEdge.Node.Name
				// Strip refs/tags/ prefix
				tagName = strings.TrimPrefix(tagName, "refs/tags/")

				normVer := version.Normalize(tagName)
				v, err := version.Parse(normVer)
				if err != nil {
					continue
				}

				if !allowPrereleases && v.IsPrerelease() {
					continue
				}

				return v, nil
			}
		}
	}

	return nil, fmt.Errorf("sourcehut: no tags found for %s", rid.Path)
}

// fetchViaHTML parses the refs page HTML to find the latest tag.
// This is a fallback when GraphQL is unavailable (no auth).
func (p *SourceHutProvider) fetchViaHTML(rid remoteid.RemoteId, allowPrereleases bool) (*version.Version, error) {
	// Build the refs URL: https://git.sr.ht/~user/project/refs
	refsURL := fmt.Sprintf("https://%s/%s/refs", rid.Host, rid.Path)

	resp, err := p.Client.GetWithRetry(refsURL)
	if err != nil {
		return nil, fmt.Errorf("sourcehut: failed to fetch refs page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sourcehut: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sourcehut: failed to read response: %w", err)
	}

	// Parse HTML for tag links
	// SourceHut uses a format like: /~user/project/commit/<hash>/<tag-name>
	// We need to extract tag names from the refs page
	// Look for <a> tags with href containing refs/tags/
	tagRe := `<a[^>]*href=["'].*?refs/tags/([^"'>]+)["']`
	tags := extractTagsFromHTML(string(body), tagRe)

	if len(tags) == 0 {
		return nil, fmt.Errorf("sourcehut: no tags found")
	}

	// Take the first tag (should be sorted by date on the page)
	normVer := version.Normalize(tags[0])
	v, err := version.Parse(normVer)
	if err != nil {
		return nil, fmt.Errorf("sourcehut: failed to parse version %q: %w", tags[0], err)
	}

	if !allowPrereleases && v.IsPrerelease() {
		return nil, fmt.Errorf("sourcehut: latest tag %q is a prerelease", tags[0])
	}

	return v, nil
}

// extractTagsFromHTML extracts tag names from HTML.
func extractTagsFromHTML(html, pattern string) []string {
	_ = pattern // unused in simple implementation

	// Simple extraction: look for refs/tags/ in anchor hrefs
	var tags []string
	start := 0
	for {
		idx := strings.Index(html[start:], "refs/tags/")
		if idx == -1 {
			break
		}
		idx += start + len("refs/tags/")
		// Extract until end of path
		end := idx
		for end < len(html) {
			ch := html[end]
			if ch == '"' || ch == '\'' || ch == '>' || ch == ' ' {
				break
			}
			end++
		}
		if end > idx {
			tag := html[idx:end]
			tags = append(tags, tag)
		}
		start = end
	}
	return tags
}

// IsPrerelease reports whether v is a pre-release version.
func (p *SourceHutProvider) IsPrerelease(v *version.Version) bool {
	return v.IsPrerelease()
}

// NewSourceHutProvider creates a new SourceHutProvider with the given rate limiter.
func NewSourceHutProvider(rl *ratelimit.RateLimiter) provider.UpstreamProvider {
	return &SourceHutProvider{
		RateLimiter: rl,
		Client:      httpclient.NewClient(),
	}
}

// NewSourceHutProviderWithClient creates a new SourceHutProvider with a specific HTTP client.
// Useful for testing with httptest.Server.
func NewSourceHutProviderWithClient(rl *ratelimit.RateLimiter, client *httpclient.Client) provider.UpstreamProvider {
	return &SourceHutProvider{
		RateLimiter: rl,
		Client:      client,
	}
}
