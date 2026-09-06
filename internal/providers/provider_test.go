package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bennypowers.dev/larry/internal/httpclient"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/ratelimit"
)

// helper: create an httpclient.Client pointing at the given httptest.Server.
func newTestClient(srv *httptest.Server) *httpclient.Client {
	return httpclient.NewClientWithTransport(srv.Client().Transport)
}

// ---- PyPIProvider (httptest) ----

func TestPyPIProvider_NotFound(t *testing.T) {
	// Test that a missing package name returns an error.
	rl := ratelimit.NewRateLimiter()
	prov := NewPyPIProviderWithClient(rl, httpclient.NewClient())
	v, err := prov.FetchLatestVersion(remoteid.RemoteId{Name: ""}, false)
	if err == nil {
		t.Fatal("expected error for empty package name")
	}
	if v != nil {
		t.Errorf("expected nil version, got %v", v)
	}
}

func TestPyPIProvider_FetchLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pypi/testpkg/json" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		resp := map[string]interface{}{
			"info": map[string]string{
				"version": "1.2.3",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Build a client that routes pypi.org requests through our mock server.
	// We use a transport that replaces the host.
	transport := &pypiReplacingTransport{srv: srv}
	client := httpclient.NewClientWithTransport(transport)

	rl := ratelimit.NewRateLimiter()
	prov := NewPyPIProviderWithClient(rl, client)

	v, err := prov.FetchLatestVersion(remoteid.RemoteId{Name: "testpkg"}, false)
	if err != nil {
		t.Fatalf("FetchLatestVersion(): %v", err)
	}
	if v.String() != "1.2.3" {
		t.Errorf("version = %q, want 1.2.3", v.String())
	}
}

func TestPyPIProvider_FetchLatest_Prerelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"info": map[string]string{
				"version": "2.0.0_p1",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	transport := &pypiReplacingTransport{srv: srv}
	client := httpclient.NewClientWithTransport(transport)

	rl := ratelimit.NewRateLimiter()
	prov := NewPyPIProviderWithClient(rl, client)

	// Without prerelease flag, should error
	_, err := prov.FetchLatestVersion(remoteid.RemoteId{Name: "testpkg"}, false)
	if err == nil {
		t.Fatal("expected prerelease error without allowPrereleases")
	}

	// With prerelease flag, should succeed
	v, err := prov.FetchLatestVersion(remoteid.RemoteId{Name: "testpkg"}, true)
	if err != nil {
		t.Fatalf("FetchLatestVersion(): %v", err)
	}
	if v.String() != "2.0.0_p1" {
		t.Errorf("version = %q, want 2.0.0_p1", v.String())
	}
}

// ---- NpmProvider (httptest) ----

func TestNpmProvider_FetchLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{
			"name":    "testpkg",
			"version": "3.0.0",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	transport := &npmReplacingTransport{srv: srv}
	client := httpclient.NewClientWithTransport(transport)

	rl := ratelimit.NewRateLimiter()
	prov := NewNpmProviderWithClient(rl, client)

	v, err := prov.FetchLatestVersion(remoteid.RemoteId{Name: "testpkg"}, false)
	if err != nil {
		t.Fatalf("FetchLatestVersion(): %v", err)
	}
	if v.String() != "3.0.0" {
		t.Errorf("version = %q, want 3.0.0", v.String())
	}
}

func TestNpmProvider_FetchLatest_Prerelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{
			"name":    "testpkg",
			"version": "1.0.0_p1",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	transport := &npmReplacingTransport{srv: srv}
	client := httpclient.NewClientWithTransport(transport)

	rl := ratelimit.NewRateLimiter()
	prov := NewNpmProviderWithClient(rl, client)

	_, err := prov.FetchLatestVersion(remoteid.RemoteId{Name: "testpkg"}, false)
	if err == nil {
		t.Fatal("expected prerelease error without allowPrereleases")
	}

	v, err := prov.FetchLatestVersion(remoteid.RemoteId{Name: "testpkg"}, true)
	if err != nil {
		t.Fatalf("FetchLatestVersion(): %v", err)
	}
	if v.String() != "1.0.0_p1" {
		t.Errorf("version = %q, want 1.0.0_p1", v.String())
	}
}

// ---- GitLabProvider (httptest) ----

func TestGitLabProvider_FetchLatest_NoHost(t *testing.T) {
	rl := ratelimit.NewRateLimiter()
	prov := NewGitLabProvider(rl)
	_, err := prov.FetchLatestVersion(remoteid.RemoteId{}, false)
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestGitLabProvider_FetchLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/projects/owner/repo/releases" {
			releases := []map[string]interface{}{
				{"tag_name": "v1.0.0", "name": "Release 1.0.0"},
				{"tag_name": "v0.9.0", "name": "Release 0.9.0"},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(releases)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	transport := &gitlabReplacingTransport{srv: srv}
	client := httpclient.NewClientWithTransport(transport)

	rl := ratelimit.NewRateLimiter()
	prov := NewGitLabProviderWithClient(rl, client)

	v, err := prov.FetchLatestVersion(remoteid.RemoteId{
		Host: "gitlab.example.com",
		Path: "/owner/repo",
	}, false)
	if err != nil {
		t.Fatalf("FetchLatestVersion(): %v", err)
	}
	if v.String() != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", v.String())
	}
}

func TestGitLabProvider_RateLimited(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if called < 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		releases := []map[string]interface{}{
			{"tag_name": "v2.0.0", "name": "Release 2.0.0"},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(releases)
	}))
	defer srv.Close()

	transport := &gitlabReplacingTransport{srv: srv}
	client := httpclient.NewClientWithTransport(transport)

	rl := ratelimit.NewRateLimiter()
	prov := NewGitLabProviderWithClient(rl, client)

	v, err := prov.FetchLatestVersion(remoteid.RemoteId{
		Host: "gitlab.example.com",
		Path: "/owner/repo",
	}, false)
	if err != nil {
		t.Fatalf("FetchLatestVersion(): %v", err)
	}
	if v.String() != "2.0.0" {
		t.Errorf("version = %q, want 2.0.0", v.String())
	}
	if called != 2 {
		t.Errorf("expected 2 requests (1 retry + 1 success), got %d", called)
	}
}

// ---- CodebergProvider (httptest) ----

func TestCodebergProvider_FetchLatest_NoOwner(t *testing.T) {
	rl := ratelimit.NewRateLimiter()
	prov := NewCodebergProvider(rl)
	_, err := prov.FetchLatestVersion(remoteid.RemoteId{Repo: "test"}, false)
	if err == nil {
		t.Fatal("expected error for missing owner")
	}
}

func TestCodebergProvider_FetchLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/repos/testowner/testrepo/releases" {
			releases := []map[string]interface{}{
				{"tag_name": "v1.5.0", "name": "v1.5.0", "prerelease": false},
				{"tag_name": "v1.4.0-rc1", "name": "v1.4.0-rc1", "prerelease": true},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(releases)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	transport := &codebergReplacingTransport{srv: srv}
	client := httpclient.NewClientWithTransport(transport)

	rl := ratelimit.NewRateLimiter()
	prov := NewCodebergProviderWithClient(rl, client)

	v, err := prov.FetchLatestVersion(remoteid.RemoteId{
		Owner: "testowner",
		Repo:  "testrepo",
	}, false)
	if err != nil {
		t.Fatalf("FetchLatestVersion(): %v", err)
	}
	if v.String() != "1.5.0" {
		t.Errorf("version = %q, want 1.5.0", v.String())
	}
}

// ---- SourceHutProvider (httptest) ----

func TestSourceHutProvider_FetchLatest_MissingFields(t *testing.T) {
	rl := ratelimit.NewRateLimiter()
	prov := NewSourceHutProvider(rl)

	_, err := prov.FetchLatestVersion(remoteid.RemoteId{}, false)
	if err == nil {
		t.Fatal("expected error for missing host/path")
	}

	_, err = prov.FetchLatestVersion(remoteid.RemoteId{Host: "git.sr.ht"}, false)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestSourceHutProvider_FetchLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GraphQL endpoint
		if r.URL.Path == "/gitea" && r.Method == "POST" {
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"user": map[string]interface{}{
						"repositories": map[string]interface{}{
							"edges": []map[string]interface{}{
								{
									"node": map[string]interface{}{
										"name": "testproject",
										"refs": map[string]interface{}{
											"edges": []map[string]interface{}{
												{
													"node": map[string]interface{}{
														"name": "refs/tags/v2.0.0",
														"target": map[string]string{
															"oid": "abc123",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	transport := &sourceHutReplacingTransport{srv: srv}
	client := httpclient.NewClientWithTransport(transport)

	rl := ratelimit.NewRateLimiter()
	prov := NewSourceHutProviderWithClient(rl, client)

	v, err := prov.FetchLatestVersion(remoteid.RemoteId{
		Host: "git.sr.ht",
		Path: "/~testuser/testproject",
	}, false)
	if err != nil {
		t.Fatalf("FetchLatestVersion(): %v", err)
	}
	if v.String() != "2.0.0" {
		t.Errorf("version = %q, want 2.0.0", v.String())
	}
}

// ---- URL resolver (unit) ----

func TestResolveProviderFromURL_Providers(t *testing.T) {
	tests := []struct {
		url    string
		wantType remoteid.RemoteIdType
	}{
		{"https://github.com/owner/repo", remoteid.TypeGitHub},
		{"https://pypi.org/project/requests/", remoteid.TypePyPI},
		{"https://registry.npmjs.org/express", remoteid.TypeNPM},
		{"https://gitlab.com/group/project", remoteid.TypeGitLab},
		{"https://codeberg.org/owner/repo", remoteid.TypeCodeberg},
		{"https://git.sr.ht/~user/project", remoteid.TypeSourceHut},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := ResolveProviderFromURL(tt.url)
			if got != tt.wantType {
				t.Errorf("ResolveProviderFromURL(%q) = %q, want %q", tt.url, got, tt.wantType)
			}
		})
	}
}

func TestResolveProviderFromURL_NotMatched(t *testing.T) {
	tests := []string{
		"https://unknown.org/owner/repo",
		"https://example.com",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			got := ResolveProviderFromURL(url)
			if got != remoteid.TypeUnknown {
				t.Errorf("ResolveProviderFromURL(%q) = %q, want TypeUnknown", url, got)
			}
		})
	}
}

// ---- httptest.Transport helpers ----

// pypiReplacingTransport routes pypi.org requests to the test server.
type pypiReplacingTransport struct {
	srv *httptest.Server
}

func (t *pypiReplacingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Host == "pypi.org" {
		req.URL.Host = t.srv.Listener.Addr().String()
		req.URL.Scheme = "http"
	}
	return http.DefaultTransport.RoundTrip(req)
}

// npmReplacingTransport routes registry.npmjs.org requests to the test server.
type npmReplacingTransport struct {
	srv *httptest.Server
}

func (t *npmReplacingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Host == "registry.npmjs.org" {
		req.URL.Host = t.srv.Listener.Addr().String()
		req.URL.Scheme = "http"
	}
	return http.DefaultTransport.RoundTrip(req)
}

// gitlabReplacingTransport routes the specified GitLab host to the test server.
type gitlabReplacingTransport struct {
	srv *httptest.Server
}

func (t *gitlabReplacingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Host == "gitlab.example.com" {
		req.URL.Host = t.srv.Listener.Addr().String()
		req.URL.Scheme = "http"
	}
	return http.DefaultTransport.RoundTrip(req)
}

// codebergReplacingTransport routes codeberg.org requests to the test server.
type codebergReplacingTransport struct {
	srv *httptest.Server
}

func (t *codebergReplacingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Host == "codeberg.org" {
		req.URL.Host = t.srv.Listener.Addr().String()
		req.URL.Scheme = "http"
	}
	return http.DefaultTransport.RoundTrip(req)
}

// sourceHutReplacingTransport routes api.sr.ht requests to the test server.
type sourceHutReplacingTransport struct {
	srv *httptest.Server
}

func (t *sourceHutReplacingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Host == "api.sr.ht" {
		req.URL.Host = t.srv.Listener.Addr().String()
		req.URL.Scheme = "http"
	}
	return http.DefaultTransport.RoundTrip(req)
}
