package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"bennypowers.dev/larry/internal/github"
	"bennypowers.dev/larry/internal/httpclient"
	"bennypowers.dev/larry/internal/larryconfig"
	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/remoteid"
)

// ---- GitHub BatchResolve ----

// graphqlRunner captures every `gh api graphql` query and answers with a fixed
// payload, so tests can assert on both the request shape and call count.
type graphqlRunner struct {
	mu       sync.Mutex
	queries  []string
	response []byte
	err      error
}

func (r *graphqlRunner) Run(cmd *exec.Cmd) error { return nil }

func (r *graphqlRunner) Output(cmd *exec.Cmd) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range cmd.Args {
		if strings.HasPrefix(a, "query=") {
			r.queries = append(r.queries, strings.TrimPrefix(a, "query="))
		}
	}
	if r.err != nil {
		return nil, r.err
	}
	return r.response, nil
}

func (r *graphqlRunner) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.queries...)
}

func githubBatchRequests(repos ...string) []provider.BatchRequest {
	reqs := make([]provider.BatchRequest, 0, len(repos))
	for _, spec := range repos {
		owner, repo, _ := strings.Cut(spec, "/")
		reqs = append(reqs, provider.BatchRequest{
			ID: remoteid.RemoteId{Type: remoteid.TypeGitHub, Owner: owner, Repo: repo},
		})
	}
	return reqs
}

func batchGraphQLPayload(t *testing.T, repos map[string][]batchReleaseNode) []byte {
	t.Helper()
	data := make(map[string]batchRepoResult, len(repos))
	for alias, nodes := range repos {
		var r batchRepoResult
		r.Releases.Nodes = nodes
		data[alias] = r
	}
	body, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestGitHubBatchResolve_OneQueryForManyRepos(t *testing.T) {
	runner := &graphqlRunner{response: batchGraphQLPayload(t, map[string][]batchReleaseNode{
		"r0": {{TagName: "v1.2.3"}, {TagName: "v1.3.0"}},
		"r1": {{TagName: "v2.0.0"}},
	})}
	p := &GitHubProvider{Client: github.NewClientWithRunner(runner)}

	reqs := githubBatchRequests("acme/alpha", "acme/beta")
	out, err := p.BatchResolve(context.Background(), reqs, false)
	if err != nil {
		t.Fatalf("BatchResolve: %v", err)
	}
	if got := len(runner.calls()); got != 1 {
		t.Fatalf("gh calls = %d, want 1", got)
	}
	if !strings.Contains(runner.calls()[0], `repository(owner: "acme", name: "alpha")`) {
		t.Errorf("query missing alpha alias:\n%s", runner.calls()[0])
	}
	if !strings.Contains(runner.calls()[0], `repository(owner: "acme", name: "beta")`) {
		t.Errorf("query missing beta alias:\n%s", runner.calls()[0])
	}

	if got := out[provider.BatchKey(reqs[0].ID)]; got == nil || got.Version != "1.3.0" {
		t.Errorf("alpha resolution = %+v, want 1.3.0", got)
	}
	if got := out[provider.BatchKey(reqs[1].ID)]; got == nil || got.Version != "2.0.0" {
		t.Errorf("beta resolution = %+v, want 2.0.0", got)
	}
}

func TestGitHubBatchResolve_SkipsDraftsAndPrereleases(t *testing.T) {
	runner := &graphqlRunner{response: batchGraphQLPayload(t, map[string][]batchReleaseNode{
		"r0": {{TagName: "v9.0.0", IsDraft: true}},
		"r1": {{TagName: "v9.0.0", IsPrerelease: true}, {TagName: "v3.1.0"}},
	})}
	p := &GitHubProvider{Client: github.NewClientWithRunner(runner)}

	reqs := githubBatchRequests("acme/alpha", "acme/beta")
	out, err := p.BatchResolve(context.Background(), reqs, false)
	if err != nil {
		t.Fatalf("BatchResolve: %v", err)
	}
	if _, ok := out[provider.BatchKey(reqs[0].ID)]; ok {
		t.Errorf("draft-only repo should produce no resolution")
	}
	if got := out[provider.BatchKey(reqs[1].ID)]; got == nil || got.Version != "3.1.0" {
		t.Errorf("beta resolution = %+v, want 3.1.0", got)
	}
}

func TestGitHubBatchResolve_PrereleasesWhenAllowed(t *testing.T) {
	runner := &graphqlRunner{response: batchGraphQLPayload(t, map[string][]batchReleaseNode{
		"r0": {{TagName: "v4.0.0-beta.1", IsPrerelease: true}},
	})}
	p := &GitHubProvider{Client: github.NewClientWithRunner(runner)}

	reqs := githubBatchRequests("acme/alpha")
	out, err := p.BatchResolve(context.Background(), reqs, true)
	if err != nil {
		t.Fatalf("BatchResolve: %v", err)
	}
	if got := out[provider.BatchKey(reqs[0].ID)]; got == nil || got.Version != "4.0.0_beta1" {
		t.Errorf("resolution = %+v, want 4.0.0_beta1", got)
	}
}

func TestGitHubBatchResolve_SkipsTagsStrategy(t *testing.T) {
	runner := &graphqlRunner{response: batchGraphQLPayload(t, nil)}
	p := &GitHubProvider{Client: github.NewClientWithRunner(runner)}

	reqs := githubBatchRequests("acme/alpha", "acme/beta")
	reqs[1].Options = &larryconfig.ResolveOptions{Strategy: "tags"}
	out, err := p.BatchResolve(context.Background(), reqs, false)
	if err != nil {
		t.Fatalf("BatchResolve: %v", err)
	}
	if got := len(runner.calls()); got != 1 {
		t.Fatalf("gh calls = %d, want 1", got)
	}
	if strings.Contains(runner.calls()[0], "beta") {
		t.Errorf("tags-strategy repo must not appear in the batch query:\n%s", runner.calls()[0])
	}
	if _, ok := out[provider.BatchKey(reqs[1].ID)]; ok {
		t.Error("tags-strategy repo must not be resolved by the batch")
	}
}

func TestGitHubBatchResolve_ChunksLargeRequest(t *testing.T) {
	runner := &graphqlRunner{response: batchGraphQLPayload(t, nil)}
	p := &GitHubProvider{Client: github.NewClientWithRunner(runner)}

	repos := make([]string, batchChunkSize+1)
	for i := range repos {
		repos[i] = fmt.Sprintf("acme/repo%d", i)
	}
	if _, err := p.BatchResolve(context.Background(), githubBatchRequests(repos...), false); err != nil {
		t.Fatalf("BatchResolve: %v", err)
	}
	if got := len(runner.calls()); got != 2 {
		t.Errorf("gh calls = %d, want 2 (%d repos)", got, len(repos))
	}
}

func TestGitHubBatchResolve_GraphQLErrorIsReported(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"errors": []map[string]string{{"message": "rate limited"}},
	})
	runner := &graphqlRunner{response: body}
	p := &GitHubProvider{Client: github.NewClientWithRunner(runner)}

	out, err := p.BatchResolve(context.Background(), githubBatchRequests("acme/alpha"), false)
	if err == nil {
		t.Fatal("want error for GraphQL errors payload")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("err = %v, want it to mention rate limited", err)
	}
	if len(out) != 0 {
		t.Errorf("out = %v, want empty on error", out)
	}
}

func TestGitHubBatchResolve_ContextCanceled(t *testing.T) {
	runner := &graphqlRunner{response: batchGraphQLPayload(t, nil)}
	p := &GitHubProvider{Client: github.NewClientWithRunner(runner)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.BatchResolve(ctx, githubBatchRequests("acme/alpha", "acme/beta"), false)
	if err == nil {
		t.Fatal("want context.Canceled")
	}
	if len(runner.calls()) != 0 {
		t.Errorf("gh calls = %d, want 0 after cancellation", len(runner.calls()))
	}
}

// ---- SourceHut BatchResolve ----

// staticTransport answers every request with the same GraphQL payload.
type staticTransport struct {
	mu       sync.Mutex
	requests []string
	body     string
	status   int
}

func (s *staticTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, _ := io.ReadAll(req.Body)
	s.requests = append(s.requests, string(body))
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     make(http.Header),
	}, nil
}

func (s *staticTransport) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func sourceHutBatchRequests(specs ...string) []provider.BatchRequest {
	reqs := make([]provider.BatchRequest, 0, len(specs))
	for _, spec := range specs {
		reqs = append(reqs, provider.BatchRequest{
			ID: remoteid.RemoteId{Type: remoteid.TypeSourceHut, Host: "git.sr.ht", Path: spec},
		})
	}
	return reqs
}

func sourceHutPayload(t *testing.T, user string, repos map[string][]string) string {
	t.Helper()
	type target struct {
		Oid string `json:"oid"`
	}
	type node struct {
		Name   string `json:"name"`
		Target target `json:"target"`
	}
	type edge struct {
		Node node `json:"node"`
	}
	type refs struct {
		Edges []edge `json:"edges"`
	}
	type repoNode struct {
		Name string `json:"name"`
		Refs refs   `json:"refs"`
	}
	type repoEdge struct {
		Node repoNode `json:"node"`
	}
	type repositories struct {
		Edges []repoEdge `json:"edges"`
	}
	type userNode struct {
		Repositories repositories `json:"repositories"`
	}
	payload := struct {
		Data struct {
			User userNode `json:"user"`
		} `json:"data"`
	}{}
	for name, tags := range repos {
		var rn repoNode
		rn.Name = name
		for _, tag := range tags {
			rn.Refs.Edges = append(rn.Refs.Edges, edge{Node: node{Name: "refs/tags/" + tag}})
		}
		payload.Data.User.Repositories.Edges = append(payload.Data.User.Repositories.Edges, repoEdge{Node: rn})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if user == "" {
		t.Fatal("user is required")
	}
	return string(body)
}

func TestSourceHutBatchResolve_OneRequestPerUser(t *testing.T) {
	transport := &staticTransport{body: sourceHutPayload(t, "alice", map[string][]string{
		"alpha": {"alpha-1.0.0", "alpha-1.2.0"},
		"beta":  {"beta-2.0.0"},
	})}
	p := &SourceHutProvider{Client: httpclient.NewClientWithTransport(transport)}

	reqs := sourceHutBatchRequests("~alice/alpha", "~alice/beta")
	out, err := p.BatchResolve(context.Background(), reqs, false)
	if err != nil {
		t.Fatalf("BatchResolve: %v", err)
	}
	if got := transport.count(); got != 1 {
		t.Fatalf("HTTP requests = %d, want 1", got)
	}
	if got := out[provider.BatchKey(reqs[0].ID)]; got == nil || got.Version != "1.2.0" {
		t.Errorf("alpha resolution = %+v, want 1.2.0", got)
	}
	if got := out[provider.BatchKey(reqs[1].ID)]; got == nil || got.Version != "2.0.0" {
		t.Errorf("beta resolution = %+v, want 2.0.0", got)
	}
}

func TestSourceHutBatchResolve_PicksHighestTag(t *testing.T) {
	transport := &staticTransport{body: sourceHutPayload(t, "alice", map[string][]string{
		"alpha": {"alpha-1.10.0", "alpha-1.9.0"},
	})}
	p := &SourceHutProvider{Client: httpclient.NewClientWithTransport(transport)}

	reqs := sourceHutBatchRequests("~alice/alpha")
	out, err := p.BatchResolve(context.Background(), reqs, false)
	if err != nil {
		t.Fatalf("BatchResolve: %v", err)
	}
	if got := out[provider.BatchKey(reqs[0].ID)]; got == nil || got.Version != "1.10.0" {
		t.Errorf("resolution = %+v, want 1.10.0", got)
	}
}

func TestSourceHutBatchResolve_FiltersPrereleases(t *testing.T) {
	transport := &staticTransport{body: sourceHutPayload(t, "alice", map[string][]string{
		"alpha": {"alpha-2.0.0", "alpha-2.1.0-rc.1"},
	})}
	p := &SourceHutProvider{Client: httpclient.NewClientWithTransport(transport)}

	reqs := sourceHutBatchRequests("~alice/alpha")
	out, err := p.BatchResolve(context.Background(), reqs, false)
	if err != nil {
		t.Fatalf("BatchResolve: %v", err)
	}
	if got := out[provider.BatchKey(reqs[0].ID)]; got == nil || got.Version != "2.0.0" {
		t.Errorf("resolution = %+v, want 2.0.0", got)
	}
}

func TestSourceHutFetchLatestVersion_StripsProjectPrefix(t *testing.T) {
	transport := &staticTransport{body: sourceHutPayload(t, "alice", map[string][]string{
		"alpha": {"alpha-1.9.0", "alpha-1.10.0"},
	})}
	p := &SourceHutProvider{Client: httpclient.NewClientWithTransport(transport)}

	got, err := p.FetchLatestVersion(remoteid.RemoteId{
		Type: remoteid.TypeSourceHut,
		Host: "git.sr.ht",
		Path: "~alice/alpha",
	}, false)
	if err != nil {
		t.Fatalf("FetchLatestVersion: %v", err)
	}
	if got == nil || got.Format() != "1.10.0" {
		t.Errorf("version = %v, want 1.10.0", got)
	}
	if transport.count() != 1 {
		t.Errorf("HTTP requests = %d, want 1 (GraphQL path)", transport.count())
	}
}

func TestSourceHutBatchResolve_MissingProjectFallsBack(t *testing.T) {
	transport := &staticTransport{body: sourceHutPayload(t, "alice", map[string][]string{
		"alpha": {"alpha-1.0.0"},
	})}
	p := &SourceHutProvider{Client: httpclient.NewClientWithTransport(transport)}

	reqs := sourceHutBatchRequests("~alice/alpha", "~alice/missing")
	out, err := p.BatchResolve(context.Background(), reqs, false)
	if err != nil {
		t.Fatalf("BatchResolve: %v", err)
	}
	if _, ok := out[provider.BatchKey(reqs[1].ID)]; ok {
		t.Error("missing project must not produce a resolution")
	}
}

func TestSourceHutBatchResolve_ErrorIsReturned(t *testing.T) {
	transport := &staticTransport{status: http.StatusInternalServerError, body: "boom"}
	p := &SourceHutProvider{Client: httpclient.NewClientWithTransport(transport)}

	_, err := p.BatchResolve(context.Background(), sourceHutBatchRequests("~alice/alpha"), false)
	if err == nil {
		t.Fatal("want error for 500 response")
	}
}
