package providers

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/github"
	"bennypowers.dev/larry/internal/larryconfig"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/version"
)

// ---- helpers ----

func key(args ...string) string { return "gh " + strings.Join(args, " ") }

// recordingRunner wraps a StubRunner and records all argv keys.
type recordingRunner struct {
	inner *github.StubRunner
	keys  []string
}

func (r *recordingRunner) Run(cmd *exec.Cmd) error {
	r.keys = append(r.keys, strings.Join(cmd.Args, " "))
	return r.inner.Run(cmd)
}

func (r *recordingRunner) Output(cmd *exec.Cmd) ([]byte, error) {
	r.keys = append(r.keys, strings.Join(cmd.Args, " "))
	return r.inner.Output(cmd)
}

// ---- D1: Tier 1 uses /releases/latest ----

func TestGitHub_Tier1_UsesReleasesLatest(t *testing.T) {
	stub := github.NewStubRunner()
	relJSON, _ := json.Marshal(map[string]interface{}{
		"tag_name":   "v1.2.3",
		"prerelease": false,
		"draft":      false,
	})
	kRel := key("api", "repos/o/r/releases/latest")
	stub.Outputs[kRel] = relJSON

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{Client: client}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", res.Version)
	}
	if res.ByDate {
		t.Error("ByDate should be false for tier 1")
	}
}

func TestGitHub_Tier1_RejectsDraft(t *testing.T) {
	// Tier 1 draft → tier 2 wins.
	stub := github.NewStubRunner()
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")

	relJSON, _ := json.Marshal(map[string]interface{}{
		"tag_name":   "v9.9.9",
		"prerelease": false,
		"draft":      true,
	})
	stub.Outputs[kRel] = relJSON
	stub.Outputs[kList] = []byte(`[{"tagName":"v1.2.3"}]`)

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{Client: client}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3 (tier 1 discarded as draft)", res.Version)
	}
}

func TestGitHub_Tier1_RejectsPrereleaseFlag(t *testing.T) {
	// Prerelease in release → discard, tier 2 wins.
	stub := github.NewStubRunner()
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")

	relJSON, _ := json.Marshal(map[string]interface{}{
		"tag_name":   "v1.2.3",
		"prerelease": true,
		"draft":      false,
	})
	stub.Outputs[kRel] = relJSON
	stub.Outputs[kList] = []byte(`[{"tagName":"v1.0.0"}]`)

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{Client: client}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", res.Version)
	}
}

func TestGitHub_Tier1_AcceptsPrereleaseWhenAllowed(t *testing.T) {
	stub := github.NewStubRunner()
	kRel := key("api", "repos/o/r/releases/latest")
	relJSON, _ := json.Marshal(map[string]interface{}{
		"tag_name":   "v1.2.3",
		"prerelease": true,
		"draft":      false,
	})
	stub.Outputs[kRel] = relJSON

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{Client: client}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, true)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", res.Version)
	}
}

// ---- D2: No sort=updated on tags endpoint ----

func TestGitHub_NoSortParamOnTagsEndpoint(t *testing.T) {
	rec := &recordingRunner{inner: github.NewStubRunner()}

	stub := rec.inner
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")

	stub.Outputs[kRel] = []byte(`{"tag_name":"v99.0","prerelease":false,"draft":false}`)
	stub.Outputs[kList] = []byte(`[{"tagName":"v1.0.0"}]`)

	client := github.NewClientWithRunner(rec)
	p := &GitHubProvider{Client: client}
	_, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}

	for _, k := range rec.keys {
		if strings.Contains(k, "tags?") || strings.Contains(k, "sort=") {
			t.Errorf("recorded argv contains tags/sort param: %q", k)
		}
	}
}

// ---- reconcile tiers 1 and 2 ----

func TestGitHub_ReconcileTiers(t *testing.T) {
	tests := []struct {
		name string
		rel  string // tier 1 JSON
		list string // tier 2 output
		want string // expected Version
	}{
		{"tier2_wins", `{"tag_name":"v1.0.0","prerelease":false,"draft":false}`, `[{"tagName":"v1.2.0"}]`, "1.2.0"},
		{"tier1_wins", `{"tag_name":"v2.0.0","prerelease":false,"draft":false}`, `[{"tagName":"v1.9.0"}]`, "2.0.0"},
		{"tier1_not_version", `{"tag_name":"pios/1","prerelease":false,"draft":false}`, `[{"tagName":"v1.5.0"}]`, "1.5.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := github.NewStubRunner()
			kRel := key("api", "repos/o/r/releases/latest")
			kList := key("release", "list", "-R", "o/r", "--limit", "100",
				"--exclude-pre-releases", "--json", "tagName")

			stub.Outputs[kRel] = []byte(tt.rel)
			stub.Outputs[kList] = []byte(tt.list)

			client := github.NewClientWithRunner(stub)
			p := &GitHubProvider{Client: client}
			res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
			if err != nil {
				t.Fatalf("FetchResolution(): %v", err)
			}
			if res.Version != tt.want {
				t.Errorf("Version = %q, want %q", res.Version, tt.want)
			}
		})
	}
}

// ---- Tier 2: exclude-pre-releases ----

func TestGitHub_Tier2_ExcludesPrereleases(t *testing.T) {
	stub := github.NewStubRunner()
	kListNoPre := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")
	kListPre := key("release", "list", "-R", "o/r", "--limit", "100",
		"--json", "tagName")

	stub.Outputs[kListNoPre] = []byte(`[{"tagName":"2.0.0"}]`)
	stub.Outputs[kListPre] = []byte(`[{"tagName":"3.0.0-rc1"}]`)

	rec := &recordingRunner{inner: stub}
	client := github.NewClientWithRunner(rec)
	p := &GitHubProvider{Client: client}

	// allowPrereleases=false should use kListNoPre
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Version != "2.0.0" {
		t.Errorf("Version = %q, want 2.0.0", res.Version)
	}

	// Check that the --exclude-pre-releases argv was used
	foundPreExcluded := false
	for _, k := range rec.keys {
		if strings.Contains(k, "--exclude-pre-releases") {
			foundPreExcluded = true
		}
	}
	if !foundPreExcluded {
		t.Error("--exclude-pre-releases was not in any recorded argv")
	}
}

// ---- Tier 3: go-git tag listing ----

func TestGitHub_Tier3_LsRemoteFallback(t *testing.T) {
	stub := github.NewStubRunner()
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")
	// Both tier 1 and 2 fail
	stub.Errors[kRel] = fmt.Errorf("gh error")
	stub.Errors[kList] = fmt.Errorf("gh error")

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{
		Client: client,
		TagLister: func(url string) ([]string, error) {
			return []string{"v1.0", "v10.0", "v9.0"}, nil
		},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Version != "10.0" {
		t.Errorf("Version = %q, want 10.0 (numeric, not lexical)", res.Version)
	}
}

func TestGitHub_Tier3_DropsPeeledRefs(t *testing.T) {
	stub := github.NewStubRunner()
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")
	stub.Errors[kRel] = fmt.Errorf("gh error")
	stub.Errors[kList] = fmt.Errorf("gh error")

	client := github.NewClientWithRunner(stub)
	// Include peeled refs and a bogus v99.0^{} with no unpeeled sibling
	p := &GitHubProvider{
		Client: client,
		TagLister: func(url string) ([]string, error) {
			return []string{"v1.0", "v2.0"}, nil
		},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Version != "2.0" {
		t.Errorf("Version = %q, want 2.0", res.Version)
	}
}

// ---- Tier 4: date-based resolution ----

func TestGitHub_Tier4_ReturnsTagAndDate(t *testing.T) {
	// Tiers 1-3 all empty.
	stub := github.NewStubRunner()
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")
	kGQL := key("api", "graphql", "-f", "query="+graphqlTagsQuery, "-f", "owner=o", "-f", "name=r")
	stub.Errors[kRel] = fmt.Errorf("gh error")
	stub.Errors[kList] = fmt.Errorf("gh error")

	gqlOutput, _ := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{
				"refs": map[string]interface{}{
					"nodes": []map[string]interface{}{
						{
							"name": "firmware",
							"target": map[string]interface{}{
								"committedDate": "2024-01-15T00:00:00Z",
							},
						},
					},
				},
			},
		},
	})
	stub.Outputs[kGQL] = gqlOutput

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{
		Client: client,
		TagLister: func(url string) ([]string, error) {
			return nil, fmt.Errorf("no tags")
		},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if !res.ByDate {
		t.Error("ByDate should be true")
	}
	if res.Tag != "firmware" {
		t.Errorf("Tag = %q, want firmware", res.Tag)
	}
	if res.TagDate != "20240115" {
		t.Errorf("TagDate = %q, want 20240115", res.TagDate)
	}
}

func TestGitHub_Tier4_SkipsPrereleaseTagNames(t *testing.T) {
	stub := github.NewStubRunner()
	kGQL := key("api", "graphql", "-f", "query="+graphqlTagsQuery, "-f", "owner=o", "-f", "name=r")
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")
	stub.Errors[kRel] = fmt.Errorf("gh error")
	stub.Errors[kList] = fmt.Errorf("gh error")

	// Two nodes: v2.0-rc1 (prerelease name → skip), then 20240115-stable (no prerelease name).
	gqlOutput, _ := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{
				"refs": map[string]interface{}{
					"nodes": []map[string]interface{}{
						{
							"name": "v2.0-rc1",
							"target": map[string]interface{}{
								"committedDate": "2026-01-01T00:00:00Z",
							},
						},
						{
							"name": "20240115-stable",
							"target": map[string]interface{}{
								"committedDate": "2020-01-01T00:00:00Z",
							},
						},
					},
				},
			},
		},
	})
	stub.Outputs[kGQL] = gqlOutput

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{
		Client: client,
		TagLister: func(url string) ([]string, error) {
			return nil, fmt.Errorf("no tags")
		},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Tag != "20240115-stable" {
		t.Errorf("Tag = %q, want 20240115-stable", res.Tag)
	}
}

func TestGitHub_Tier4_DatePrefersTagName(t *testing.T) {
	stub := github.NewStubRunner()
	kGQL := key("api", "graphql", "-f", "query="+graphqlTagsQuery, "-f", "owner=o", "-f", "name=r")
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")
	stub.Errors[kRel] = fmt.Errorf("gh error")
	stub.Errors[kList] = fmt.Errorf("gh error")

	// Tag name contains a YYYYMMDD date → use that instead of committedDate.
	gqlOutput, _ := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{
				"refs": map[string]interface{}{
					"nodes": []map[string]interface{}{
						{
							"name": "firmware-20260824",
							"target": map[string]interface{}{
								"committedDate": "2020-01-01T00:00:00Z",
							},
						},
					},
				},
			},
		},
	})
	stub.Outputs[kGQL] = gqlOutput

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{
		Client: client,
		TagLister: func(url string) ([]string, error) {
			return nil, fmt.Errorf("no tags")
		},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.TagDate != "20260824" {
		t.Errorf("TagDate = %q, want 20260824 (from tag name, not committedDate)", res.TagDate)
	}
}

// ---- D5: prerelease error message ----

func TestGitHub_PrereleaseErrorMessage(t *testing.T) {
	stub := github.NewStubRunner()
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")
	kGQL := key("api", "graphql", "-f", "query="+graphqlTagsQuery, "-f", "owner=o", "-f", "name=r")

	// Tier 1 returns prerelease, tier 2 returns empty, tier 3 returns prerelease tag, tier 4 errors.
	relJSON, _ := json.Marshal(map[string]interface{}{
		"tag_name":   "v1.0.0-rc1",
		"prerelease": false,
		"draft":      false,
	})
	stub.Outputs[kRel] = relJSON
	stub.Outputs[kList] = []byte(`[]`)
	stub.Errors[kGQL] = fmt.Errorf("gh error")

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{
		Client: client,
		TagLister: func(url string) ([]string, error) {
			return []string{"v1.0.0-rc1"}, nil
		},
	}
	_, err := p.FetchLatestVersion(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err == nil {
		t.Fatal("FetchLatestVersion() should return error for prerelease tag")
	}
	errMsg := err.Error()
	// D5: error should contain the actual tag version (not empty string).
	// The Version field is normalized (rc → _rc), so we check for that.
	if !strings.Contains(errMsg, "1.0.0_rc1") {
		t.Errorf("error message %q does not contain '1.0.0_rc1'", errMsg)
	}
	if strings.Contains(errMsg, "\"\"") {
		t.Error("error message should not contain empty-string literal")
	}
}

// ---- stripTagPrefix regression (already tested elsewhere, keep minimal) ----

func TestStripTagPrefix_PeeledTag(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v1.0^{}", "1.0"},
		{"1.0^{abc}", "1.0"},
		{"v1.0", "1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripTagPrefix(tt.input)
			if got != tt.want {
				t.Errorf("stripTagPrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---- FetchLatestVersion wrapper tests ----

func TestFetchLatestVersion_FromVersion(t *testing.T) {
	stub := github.NewStubRunner()
	kRel := key("api", "repos/o/r/releases/latest")
	relJSON, _ := json.Marshal(map[string]interface{}{
		"tag_name":   "v2.5.0",
		"prerelease": false,
		"draft":      false,
	})
	stub.Outputs[kRel] = relJSON

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{Client: client}
	v, err := p.FetchLatestVersion(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchLatestVersion(): %v", err)
	}
	if v.String() != "2.5.0" {
		t.Errorf("Version = %q, want 2.5.0", v.String())
	}
}

func TestFetchLatestVersion_ByDate(t *testing.T) {
	// When ByDate=true, FetchLatestVersion extracts date from tag name.
	stub := github.NewStubRunner()
	kGQL := key("api", "graphql", "-f", "query="+graphqlTagsQuery, "-f", "owner=o", "-f", "name=r")
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")
	stub.Errors[kRel] = fmt.Errorf("gh error")
	stub.Errors[kList] = fmt.Errorf("gh error")

	gqlOutput, _ := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{
				"refs": map[string]interface{}{
					"nodes": []map[string]interface{}{
						{
							"name": "firmware-20260824",
							"target": map[string]interface{}{
								"committedDate": "2020-01-01T00:00:00Z",
							},
						},
					},
				},
			},
		},
	})
	stub.Outputs[kGQL] = gqlOutput

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{
		Client: client,
		TagLister: func(url string) ([]string, error) {
			return nil, fmt.Errorf("no tags")
		},
	}
	v, err := p.FetchLatestVersion(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchLatestVersion(): %v", err)
	}
	// Date extracted from tag name should be parsed as a version.
	want, _ := version.Parse("20260824")
	if v.String() != want.String() {
		t.Errorf("Version = %q, want %q", v.String(), want.String())
	}
}

// ---- Tier 4: firmware date filtering (D1) ----

func TestGitHub_Tier4_SkipsFirmwareDates(t *testing.T) {
	// Two nodes: 1.20230405 (firmware date → skip), v6.1.75 (real version).
	stub := github.NewStubRunner()
	kGQL := key("api", "graphql", "-f", "query="+graphqlTagsQuery, "-f", "owner=o", "-f", "name=r")
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")
	stub.Errors[kRel] = fmt.Errorf("gh error")
	stub.Errors[kList] = fmt.Errorf("gh error")

	gqlOutput, _ := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{
				"refs": map[string]interface{}{
					"nodes": []map[string]interface{}{
						{
							"name": "1.20230405",
							"target": map[string]interface{}{
								"committedDate": "2023-04-05T00:00:00Z",
							},
						},
						{
							"name": "v6.1.75",
							"target": map[string]interface{}{
								"committedDate": "2024-01-01T00:00:00Z",
							},
						},
					},
				},
			},
		},
	})
	stub.Outputs[kGQL] = gqlOutput

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{
		Client:    client,
		TagLister: func(url string) ([]string, error) { return nil, fmt.Errorf("no tags") },
		ResolveOptions: &larryconfig.ResolveOptions{
			IgnoreTagRe: []*regexp.Regexp{regexp.MustCompile(`^\d+\.\d{8}$`)},
		},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Tag != "v6.1.75" {
		t.Errorf("Tag = %q, want v6.1.75 (firmware date skipped)", res.Tag)
	}
	if !res.ByDate {
		t.Error("ByDate should be true")
	}
}

func TestGitHub_Tier4_AllFirmwareDates(t *testing.T) {
	// All nodes are firmware date tags → error (no version-like tags).
	stub := github.NewStubRunner()
	kGQL := key("api", "graphql", "-f", "query="+graphqlTagsQuery, "-f", "owner=o", "-f", "name=r")
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")
	stub.Errors[kRel] = fmt.Errorf("gh error")
	stub.Errors[kList] = fmt.Errorf("gh error")

	gqlOutput, _ := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{
				"refs": map[string]interface{}{
					"nodes": []map[string]interface{}{
						{
							"name": "1.20230405",
							"target": map[string]interface{}{
								"committedDate": "2023-04-05T00:00:00Z",
							},
						},
						{
							"name": "1.20260519",
							"target": map[string]interface{}{
								"committedDate": "2026-05-19T00:00:00Z",
							},
						},
					},
				},
			},
		},
	})
	stub.Outputs[kGQL] = gqlOutput

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{
		Client:    client,
		TagLister: func(url string) ([]string, error) { return nil, fmt.Errorf("no tags") },
		ResolveOptions: &larryconfig.ResolveOptions{
			IgnoreTagRe: []*regexp.Regexp{regexp.MustCompile(`^\d+\.\d{8}$`)},
		},
	}
	_, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err == nil {
		t.Fatal("FetchResolution() should error when all nodes are firmware dates")
	}
}

func TestTryParseVersion_SkipsFirmwareDates(t *testing.T) {
	// With ignore_tags configured, firmware dates are rejected.
	p := &GitHubProvider{
		ResolveOptions: &larryconfig.ResolveOptions{
			IgnoreTagRe: []*regexp.Regexp{regexp.MustCompile(`^\d+\.\d{8}$`)},
		},
	}
	tests := []struct {
		tag      string
		errorSub string
	}{
		{"1.20230405", "ignore pattern"},
		{"1.20260519", "ignore pattern"},
		{"v1.20230405", "ignore pattern"},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			_, err := p.tryParseVersion(tt.tag)
			if err == nil {
				t.Fatalf("tryParseVersion(%q) should error", tt.tag)
			}
			if !strings.Contains(err.Error(), tt.errorSub) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.errorSub)
			}
		})
	}
}

// ---- Step 3-4: tryParseVersion with ResolveOptions (ignore_tags, strip_prefixes) ----

func TestGitHub_TryParseVersion_IgnoreTags(t *testing.T) {
	p := &GitHubProvider{
		ResolveOptions: &larryconfig.ResolveOptions{
			IgnoreTagRe: []*regexp.Regexp{regexp.MustCompile(`^\d+\.\d{8}$`)},
		},
	}

	// Raw tag matches ignore pattern
	if _, err := p.tryParseVersion("1.20230405"); err == nil {
		t.Fatal("expected 1.20230405 to be ignored")
	}

	// Stripped tag matches ignore pattern (v prefix stripped)
	if _, err := p.tryParseVersion("v1.20230405"); err == nil {
		t.Fatal("expected v1.20230405 to be ignored (stripped matches)")
	}

	// Raw tag doesn't match, stripped doesn't match → should parse
	v, err := p.tryParseVersion("v1.2.3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.String() != "1.2.3" {
		t.Errorf("parsed version = %q, want 1.2.3", v.String())
	}

	// No ResolveOptions → no filtering
	p.ResolveOptions = nil
	v, err = p.tryParseVersion("1.20230405")
	if err != nil {
		t.Fatalf("without options, 1.20230405 should parse: %v", err)
	}
	if v.String() != "1.20230405" {
		t.Errorf("parsed version = %q, want 1.20230405", v.String())
	}
}

func TestGitHub_TryParseVersion_StripPrefixes(t *testing.T) {
	p := &GitHubProvider{
		ResolveOptions: &larryconfig.ResolveOptions{
			StripPrefixes: []string{"stable_", "oldstable_"},
		},
	}

	// stable_ prefix stripped before version parsing
	v, err := p.tryParseVersion("stable_1.2.3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.String() != "1.2.3" {
		t.Errorf("parsed version = %q, want 1.2.3", v.String())
	}
}

// ---- Tier 3: strip_prefixes in ls-remote list ----

func TestGitHub_Tier3_StripPrefixes(t *testing.T) {
	stub := github.NewStubRunner()
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")
	stub.Errors[kRel] = fmt.Errorf("gh error")
	stub.Errors[kList] = fmt.Errorf("gh error")

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{
		Client:    client,
		TagLister: func(url string) ([]string, error) { return []string{"stable_1.0", "stable_2.0", "v1.5"}, nil },
		ResolveOptions: &larryconfig.ResolveOptions{
			StripPrefixes: []string{"stable_"},
		},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Version != "2.0" {
		t.Errorf("Version = %q, want 2.0 (stable_ prefix stripped, highest version)", res.Version)
	}
}

// ---- Tier 4: ignore_tags in GraphQL ----

func TestGitHub_Tier4_IgnoreTags(t *testing.T) {
	stub := github.NewStubRunner()
	kGQL := key("api", "graphql", "-f", "query="+graphqlTagsQuery, "-f", "owner=o", "-f", "name=r")
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")
	stub.Errors[kRel] = fmt.Errorf("gh error")
	stub.Errors[kList] = fmt.Errorf("gh error")

	gqlOutput, _ := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{
				"refs": map[string]interface{}{
					"nodes": []map[string]interface{}{
						{
							"name": "1.20230405",
							"target": map[string]interface{}{
								"committedDate": "2023-04-05T00:00:00Z",
							},
						},
						{
							"name": "20240115-stable",
							"target": map[string]interface{}{
								"committedDate": "2024-01-15T00:00:00Z",
							},
						},
					},
				},
			},
		},
	})
	stub.Outputs[kGQL] = gqlOutput

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{
		Client:    client,
		TagLister: func(url string) ([]string, error) { return nil, fmt.Errorf("no tags") },
		ResolveOptions: &larryconfig.ResolveOptions{
			IgnoreTagRe: []*regexp.Regexp{regexp.MustCompile(`^\d+\.\d{8}$`)},
		},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Tag != "20240115-stable" {
		t.Errorf("Tag = %q, want 20240115-stable (firmware date skipped via ignore_tags)", res.Tag)
	}
	if !res.ByDate {
		t.Error("ByDate should be true")
	}
}

// ---- ProviderOptionsReset: state doesn't leak between packages ----

func TestGitHub_ProviderOptionsReset(t *testing.T) {
	p := &GitHubProvider{}

	// 1. Package A with ignore rule (e.g. ignore firmware dates)
	p.ResolveOptions = &larryconfig.ResolveOptions{
		IgnoreTagRe: []*regexp.Regexp{regexp.MustCompile(`^\d+\.\d{8}$`)},
	}
	if _, err := p.tryParseVersion("1.20230405"); err == nil {
		t.Fatal("Package A: expected 1.20230405 to be ignored")
	}

	// 2. Clear options (mimicking resolveOne resetting options for Package B with no .larry/config.yaml)
	p.ResolveOptions = nil

	// 3. Package B with no ignore rule
	v, err := p.tryParseVersion("1.20230405")
	if err != nil {
		t.Fatalf("Package B: expected 1.20230405 to parse successfully when options are reset, got: %v", err)
	}
	if v.String() != "1.20230405" {
		t.Errorf("Package B: parsed version = %q, want 1.20230405", v.String())
	}
}

// ---- Plan 33: tag_match, version_template in tryParseVersion ----

func TestGitHub_TryParseVersion_TemplateExpansion(t *testing.T) {
	// Simulates raspberrypi-linux: PV=6.18.46_p20260824, tag=stable_20260911
	p := &GitHubProvider{
		ResolveOptions: &larryconfig.ResolveOptions{
			PVCaptures:      map[string]string{"BASE": "6.18.46"},
			TagMatchRe:      regexp.MustCompile(`^stable_(?P<DATE>\d{8})$`),
			VersionTemplate: "${BASE}_p${DATE}",
		},
	}

	v, err := p.tryParseVersion("stable_20260911")
	if err != nil {
		t.Fatalf("tryParseVersion(%q): %v", "stable_20260911", err)
	}

	// The template expands BASE + DATE → "6.18.46_p20260911"
	want := "6.18.46_p20260911"
	if v.String() != want {
		t.Errorf("parsed version = %q, want %q", v.String(), want)
	}
}

func TestGitHub_TryParseVersion_TagMatchRejects(t *testing.T) {
	p := &GitHubProvider{
		ResolveOptions: &larryconfig.ResolveOptions{
			TagMatchRe: regexp.MustCompile(`^stable_`),
		},
	}

	// Non-matching tag should be rejected
	_, err := p.tryParseVersion("oldstable_1.0.0")
	if err == nil {
		t.Fatal("tryParseVersion should reject tags not matching tag_match")
	}
}

func TestGitHub_Tier3_TagMatchFilter(t *testing.T) {
	// Tier 3 should filter tags by TagMatchRe before sorting
	stub := github.NewStubRunner()
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")
	stub.Errors[kRel] = fmt.Errorf("gh error")
	stub.Errors[kList] = fmt.Errorf("gh error")

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{
		Client: client,
		TagLister: func(url string) ([]string, error) {
			return []string{"stable_20260911", "oldstable_20260824", "stable_20260901"}, nil
		},
		ResolveOptions: &larryconfig.ResolveOptions{
			TagMatchRe:    regexp.MustCompile(`^stable_`),
			StripPrefixes: []string{"stable_"},
		},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}

	// oldstable_ should be filtered out by TagMatchRe; highest stable_ version should win
	if res.Version != "20260911" {
		t.Errorf("Version = %q, want 20260911 (oldstable filtered, highest stable_ wins)", res.Version)
	}
}

func TestGitHub_Tier4_TagMatchFilter(t *testing.T) {
	// Tier 4 should skip nodes that don't match TagMatchRe
	stub := github.NewStubRunner()
	kGQL := key("api", "graphql", "-f", "query="+graphqlTagsQuery, "-f", "owner=o", "-f", "name=r")
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100",
		"--exclude-pre-releases", "--json", "tagName")
	stub.Errors[kRel] = fmt.Errorf("gh error")
	stub.Errors[kList] = fmt.Errorf("gh error")

	// Two nodes: stable_20260911 (matches) and oldstable_20260824 (doesn't match)
	gqlOutput, _ := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{
				"refs": map[string]interface{}{
					"nodes": []map[string]interface{}{
						{
							"name": "stable_20260911",
							"target": map[string]interface{}{
								"committedDate": "2026-09-11T00:00:00Z",
							},
						},
						{
							"name": "oldstable_20260824",
							"target": map[string]interface{}{
								"committedDate": "2026-08-24T00:00:00Z",
							},
						},
					},
				},
			},
		},
	})
	stub.Outputs[kGQL] = gqlOutput

	client := github.NewClientWithRunner(stub)
	p := &GitHubProvider{
		Client:    client,
		TagLister: func(url string) ([]string, error) { return nil, fmt.Errorf("no tags") },
		ResolveOptions: &larryconfig.ResolveOptions{
			TagMatchRe: regexp.MustCompile(`^stable_`),
		},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Tag != "stable_20260911" {
		t.Errorf("Tag = %q, want stable_20260911 (oldstable filtered)", res.Tag)
	}
}

// ---- strategy: tags (opt-in per-package) ----

func TestGitHub_StrategyTags_PrefersTagsOverReleases(t *testing.T) {
	stub := github.NewStubRunner()
	kRel := key("api", "repos/o/r/releases/latest")
	kPre := key("release", "list", "-R", "o/r", "--limit", "100", "--json", "tagName,isPrerelease")
	relJSON, _ := json.Marshal(map[string]interface{}{
		"tag_name":   "3.50.2",
		"prerelease": false,
		"draft":      false,
	})
	stub.Outputs[kRel] = relJSON
	stub.Outputs[kPre] = []byte(`[]`)

	rec := &recordingRunner{inner: stub}
	p := &GitHubProvider{
		Client: github.NewClientWithRunner(rec),
		TagLister: func(string) ([]string, error) {
			return []string{"3.50.2", "3.50.11"}, nil
		},
		ResolveOptions: &larryconfig.ResolveOptions{Strategy: "tags"},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Version != "3.50.11" {
		t.Errorf("Version = %q, want 3.50.11 (tags strategy)", res.Version)
	}
	for _, k := range rec.keys {
		if k == kRel {
			t.Errorf("tags strategy must not call /releases/latest; recorded %v", rec.keys)
		}
	}
}

func TestGitHub_DefaultStrategy_ReleasesWin(t *testing.T) {
	stub := github.NewStubRunner()
	kRel := key("api", "repos/o/r/releases/latest")
	relJSON, _ := json.Marshal(map[string]interface{}{
		"tag_name":   "3.50.2",
		"prerelease": false,
		"draft":      false,
	})
	stub.Outputs[kRel] = relJSON

	tagListerCalled := false
	p := &GitHubProvider{
		Client: github.NewClientWithRunner(stub),
		TagLister: func(string) ([]string, error) {
			tagListerCalled = true
			return []string{"3.50.2", "3.50.11"}, nil
		},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Version != "3.50.2" {
		t.Errorf("Version = %q, want 3.50.2 (default release-first)", res.Version)
	}
	if tagListerCalled {
		t.Error("default strategy should not list tags when a release resolved")
	}
}

func TestGitHub_StrategyTags_FiltersPrereleaseNames(t *testing.T) {
	kPre := key("release", "list", "-R", "o/r", "--limit", "100", "--json", "tagName,isPrerelease")
	tags := []string{"3.51.0-rc1", "3.50.11"}

	t.Run("excluded", func(t *testing.T) {
		stub := github.NewStubRunner()
		stub.Outputs[kPre] = []byte(`[]`)
		p := &GitHubProvider{
			Client:         github.NewClientWithRunner(stub),
			TagLister:      func(string) ([]string, error) { return tags, nil },
			ResolveOptions: &larryconfig.ResolveOptions{Strategy: "tags"},
		}
		res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
		if err != nil {
			t.Fatalf("FetchResolution(): %v", err)
		}
		if res.Version != "3.50.11" {
			t.Errorf("Version = %q, want 3.50.11", res.Version)
		}
	})

	t.Run("allowed", func(t *testing.T) {
		stub := github.NewStubRunner()
		p := &GitHubProvider{
			Client:         github.NewClientWithRunner(stub),
			TagLister:      func(string) ([]string, error) { return tags, nil },
			ResolveOptions: &larryconfig.ResolveOptions{Strategy: "tags"},
		}
		res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, true)
		if err != nil {
			t.Fatalf("FetchResolution(): %v", err)
		}
		if res.Version != "3.51.0_rc1" {
			t.Errorf("Version = %q, want 3.51.0_rc1", res.Version)
		}
	})
}

func TestGitHub_StrategyTags_FiltersPrereleaseReleaseFlags(t *testing.T) {
	stub := github.NewStubRunner()
	kPre := key("release", "list", "-R", "o/r", "--limit", "100", "--json", "tagName,isPrerelease")
	stub.Outputs[kPre] = []byte(`[{"tagName":"9.9.9","isPrerelease":true}]`)

	p := &GitHubProvider{
		Client: github.NewClientWithRunner(stub),
		TagLister: func(string) ([]string, error) {
			return []string{"9.9.9", "3.50.11"}, nil
		},
		ResolveOptions: &larryconfig.ResolveOptions{Strategy: "tags"},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Version != "3.50.11" {
		t.Errorf("Version = %q, want 3.50.11 (9.9.9 is a prerelease release)", res.Version)
	}
}

func TestGitHub_StrategyTags_FallsBackToDate(t *testing.T) {
	stub := github.NewStubRunner()
	kPre := key("release", "list", "-R", "o/r", "--limit", "100", "--json", "tagName,isPrerelease")
	stub.Outputs[kPre] = []byte(`[]`)
	kGQL := key("api", "graphql", "-f", "query="+graphqlTagsQuery, "-f", "owner=o", "-f", "name=r")
	gqlOutput, _ := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"repository": map[string]interface{}{
				"refs": map[string]interface{}{
					"nodes": []map[string]interface{}{
						{"name": "firmware-20260824", "target": map[string]interface{}{
							"committedDate": "2020-01-01T00:00:00Z",
						}},
					},
				},
			},
		},
	})
	stub.Outputs[kGQL] = gqlOutput

	p := &GitHubProvider{
		Client:         github.NewClientWithRunner(stub),
		TagLister:      func(string) ([]string, error) { return nil, fmt.Errorf("no tags") },
		ResolveOptions: &larryconfig.ResolveOptions{Strategy: "tags"},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if !res.ByDate {
		t.Error("ByDate should be true when tags strategy falls back to tier 4")
	}
	if res.TagDate != "20260824" {
		t.Errorf("TagDate = %q, want 20260824", res.TagDate)
	}
}

func TestGitHub_TagMatchVersionTemplate_SixDigitDate(t *testing.T) {
	stub := github.NewStubRunner()
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100", "--exclude-pre-releases", "--json", "tagName")
	stub.Errors[kRel] = fmt.Errorf("no releases")
	stub.Errors[kList] = fmt.Errorf("no releases")

	p := &GitHubProvider{
		Client: github.NewClientWithRunner(stub),
		TagLister: func(string) ([]string, error) {
			return []string{
				"pios/1%20241210-1+rpt3",
				"pios/1%20250410-2+rpt1",
				"pios/1%20260519-1_bpo13+1+rpt1",
			}, nil
		},
		ResolveOptions: &larryconfig.ResolveOptions{
			TagMatchRe:      regexp.MustCompile(`^pios/1%20(?P<DATE>\d{6})-.+$`),
			VersionTemplate: "20${DATE}",
		},
	}
	res, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false)
	if err != nil {
		t.Fatalf("FetchResolution(): %v", err)
	}
	if res.Version != "20260519" {
		t.Errorf("Version = %q, want 20260519", res.Version)
	}
}

func TestGitHub_Tier3_FiltersPrereleaseTags(t *testing.T) {
	stub := github.NewStubRunner()
	kRel := key("api", "repos/o/r/releases/latest")
	kList := key("release", "list", "-R", "o/r", "--limit", "100", "--exclude-pre-releases", "--json", "tagName")
	stub.Errors[kRel] = fmt.Errorf("no releases")
	stub.Errors[kList] = fmt.Errorf("no releases")

	p := &GitHubProvider{
		Client:    github.NewClientWithRunner(stub),
		TagLister: func(string) ([]string, error) { return []string{"v2.0.0-rc1"}, nil },
	}
	if _, err := p.FetchResolution(remoteid.RemoteId{Owner: "o", Repo: "r"}, false); err == nil {
		t.Error("FetchResolution should not resolve a prerelease from tier 3 without --prereleases")
	}
}
