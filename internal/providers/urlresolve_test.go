package providers

import (
	"testing"

	"bennypowers.dev/larry/internal/remoteid"
)

// ---- TestResolveProviderFromURL_GitHub ----

func TestResolveProviderFromURL_GitHub(t *testing.T) {
	tests := []string{
		"https://github.com/golang/go",
		"http://github.com/owner/repo",
		"git@github.com:owner/repo.git",
		"https://github.com/torvalds/linux",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			rtype := ResolveProviderFromURL(url)
			if rtype != remoteid.TypeGitHub {
				t.Errorf("ResolveProviderFromURL(%q) = %v, want %v", url, rtype, remoteid.TypeGitHub)
			}
		})
	}
}

// ---- TestResolveProviderFromURL_PyPI ----

func TestResolveProviderFromURL_PyPI(t *testing.T) {
	tests := []string{
		"https://pypi.org/project/requests",
		"https://pypi.org/project/numpy/#files",
		"https://files.pythonhosted.org/packages/requests-2.28.0.tar.gz",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			rtype := ResolveProviderFromURL(url)
			if rtype != remoteid.TypePyPI {
				t.Errorf("ResolveProviderFromURL(%q) = %v, want %v", url, rtype, remoteid.TypePyPI)
			}
		})
	}
}

// ---- TestResolveProviderFromURL_NPM ----

func TestResolveProviderFromURL_NPM(t *testing.T) {
	tests := []string{
		"https://registry.npmjs.org/lodash",
		"https://npmjs.com/package/lodash",
		"https://www.npmjs.com/package/react",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			rtype := ResolveProviderFromURL(url)
			if rtype != remoteid.TypeNPM {
				t.Errorf("ResolveProviderFromURL(%q) = %v, want %v", url, rtype, remoteid.TypeNPM)
			}
		})
	}
}

// ---- TestResolveProviderFromURL_GitLab ----

func TestResolveProviderFromURL_GitLab(t *testing.T) {
	tests := []string{
		"https://gitlab.com/groups/project",
		"https://gitlab.com/mygroup/myproject",
		"https://gitlab.com/freeipa/freeipa",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			rtype := ResolveProviderFromURL(url)
			if rtype != remoteid.TypeGitLab {
				t.Errorf("ResolveProviderFromURL(%q) = %v, want %v", url, rtype, remoteid.TypeGitLab)
			}
		})
	}
}

// ---- TestResolveProviderFromURL_Codeberg ----

func TestResolveProviderFromURL_Codeberg(t *testing.T) {
	tests := []string{
		"https://codeberg.org/owner/repo",
		"http://codeberg.org/user/project",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			rtype := ResolveProviderFromURL(url)
			if rtype != remoteid.TypeCodeberg {
				t.Errorf("ResolveProviderFromURL(%q) = %v, want %v", url, rtype, remoteid.TypeCodeberg)
			}
		})
	}
}

// ---- TestResolveProviderFromURL_SourceHut ----

func TestResolveProviderFromURL_SourceHut(t *testing.T) {
	tests := []string{
		"https://git.sr.ht/~user/project",
		"git://git.sr.ht/~user/project",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			rtype := ResolveProviderFromURL(url)
			if rtype != remoteid.TypeSourceHut {
				t.Errorf("ResolveProviderFromURL(%q) = %v, want %v", url, rtype, remoteid.TypeSourceHut)
			}
		})
	}
}

// ---- TestResolveProviderFromURL_Unknown ----

func TestResolveProviderFromURL_Unknown(t *testing.T) {
	tests := []string{
		"https://example.com/something",
		"https://bitbucket.org/owner/repo",
		"https://raw.githubusercontent.com/owner/repo/main/README.md",
		"",
		"  ",
		"not-a-url",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			rtype := ResolveProviderFromURL(url)
			if rtype != remoteid.TypeUnknown {
				t.Errorf("ResolveProviderFromURL(%q) = %v, want %v", url, rtype, remoteid.TypeUnknown)
			}
		})
	}
}

// ---- TestResolveProviderFromURL_DistinctProviders ----

func TestResolveProviderFromURL_DistinctProviders(t *testing.T) {
	tests := []struct {
		url  string
		want remoteid.RemoteIdType
	}{
		{"https://github.com/golang/go", remoteid.TypeGitHub},
		{"https://pypi.org/project/requests", remoteid.TypePyPI},
		{"https://registry.npmjs.org/lodash", remoteid.TypeNPM},
		{"https://gitlab.com/groups/project", remoteid.TypeGitLab},
		{"https://codeberg.org/owner/repo", remoteid.TypeCodeberg},
		{"https://git.sr.ht/~user/project", remoteid.TypeSourceHut},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := ResolveProviderFromURL(tt.url)
			if got != tt.want {
				t.Errorf("ResolveProviderFromURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

// ---- TestResolveRemoteIdFromURL_GitHub ----

func TestResolveRemoteIdFromURL_GitHub(t *testing.T) {
	rid, err := ResolveRemoteIdFromURL("https://github.com/golang/go")
	if err != nil {
		t.Fatalf("ResolveRemoteIdFromURL error = %v", err)
	}
	if rid == nil {
		t.Fatal("ResolveRemoteIdFromURL returned nil")
	}
	if rid.Type != remoteid.TypeGitHub {
		t.Errorf("Type = %v, want %v", rid.Type, remoteid.TypeGitHub)
	}
	if rid.Owner != "golang" {
		t.Errorf("Owner = %q, want %q", rid.Owner, "golang")
	}
	if rid.Repo != "go" {
		t.Errorf("Repo = %q, want %q", rid.Repo, "go")
	}
}

// ---- TestResolveRemoteIdFromURL_PyPI ----

func TestResolveRemoteIdFromURL_PyPI(t *testing.T) {
	rid, err := ResolveRemoteIdFromURL("https://pypi.org/project/requests")
	if err != nil {
		t.Fatalf("ResolveRemoteIdFromURL error = %v", err)
	}
	if rid == nil {
		t.Fatal("ResolveRemoteIdFromURL returned nil")
	}
	if rid.Type != remoteid.TypePyPI {
		t.Errorf("Type = %v, want %v", rid.Type, remoteid.TypePyPI)
	}
	if rid.Name != "requests" {
		t.Errorf("Name = %q, want %q", rid.Name, "requests")
	}
}

// ---- TestResolveRemoteIdFromURL_NPM ----

func TestResolveRemoteIdFromURL_NPM(t *testing.T) {
	rid, err := ResolveRemoteIdFromURL("https://npmjs.com/package/lodash")
	if err != nil {
		t.Fatalf("ResolveRemoteIdFromURL error = %v", err)
	}
	if rid == nil {
		t.Fatal("ResolveRemoteIdFromURL returned nil")
	}
	if rid.Type != remoteid.TypeNPM {
		t.Errorf("Type = %v, want %v", rid.Type, remoteid.TypeNPM)
	}
	if rid.Name != "lodash" {
		t.Errorf("Name = %q, want %q", rid.Name, "lodash")
	}
}

// ---- TestResolveRemoteIdFromURL_Unknown ----

func TestResolveRemoteIdFromURL_Unknown(t *testing.T) {
	rid, err := ResolveRemoteIdFromURL("https://example.com/something")
	if err != nil {
		t.Fatalf("ResolveRemoteIdFromURL error = %v", err)
	}
	if rid != nil {
		t.Errorf("expected nil for unknown URL, got %+v", rid)
	}
}

// ---- TestResolveProviderFromURL_NPM_Scoped ----

func TestResolveProviderFromURL_NPM_Scoped(t *testing.T) {
	tests := []string{
		"https://registry.npmjs.org/@angular/core",
		"https://npmjs.com/package/@angular/core",
		"https://www.npmjs.com/package/@types/node",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			rtype := ResolveProviderFromURL(url)
			if rtype != remoteid.TypeNPM {
				t.Errorf("ResolveProviderFromURL(%q) = %v, want %v", url, rtype, remoteid.TypeNPM)
			}
		})
	}
}

// ---- TestResolveProviderFromURL_PyPI_Simple ----

func TestResolveProviderFromURL_PyPI_Simple(t *testing.T) {
	tests := []string{
		"https://pypi.org/simple/requests/",
		"https://pypi.org/simple/requests",
		"https://pypi.org/simple/numpy/",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			rtype := ResolveProviderFromURL(url)
			if rtype != remoteid.TypePyPI {
				t.Errorf("ResolveProviderFromURL(%q) = %v, want %v", url, rtype, remoteid.TypePyPI)
			}
		})
	}
}

// ---- TestResolveProviderFromURL_GitHub_Clone ----

func TestResolveProviderFromURL_GitHub_Clone(t *testing.T) {
	tests := []string{
		"git+ssh://git@github.com/owner/repo.git",
		"git@github.com:owner/repo.git",
		"https://github.com/owner/repo.git",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			rtype := ResolveProviderFromURL(url)
			if rtype != remoteid.TypeGitHub {
				t.Errorf("ResolveProviderFromURL(%q) = %v, want %v", url, rtype, remoteid.TypeGitHub)
			}
		})
	}
}

// ---- TestResolveRemoteIdFromURL_GitLab_Group ----

func TestResolveRemoteIdFromURL_GitLab_Group(t *testing.T) {
	tests := []struct {
		url  string
		host string
		path string
	}{
		{"https://gitlab.com/g1/g2/project", "gitlab.com", "/g1/g2/project"},
		{"https://gitlab.com/group/subgroup/subsubgroup/project", "gitlab.com", "/group/subgroup/subsubgroup/project"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			rid, err := ResolveRemoteIdFromURL(tt.url)
			if err != nil {
				t.Fatalf("ResolveRemoteIdFromURL error = %v", err)
			}
			if rid == nil {
				t.Fatal("ResolveRemoteIdFromURL returned nil")
			}
			if rid.Type != remoteid.TypeGitLab {
				t.Errorf("Type = %v, want %v", rid.Type, remoteid.TypeGitLab)
			}
			if rid.Host != tt.host {
				t.Errorf("Host = %q, want %q", rid.Host, tt.host)
			}
			if rid.Path != tt.path {
				t.Errorf("Path = %q, want %q", rid.Path, tt.path)
			}
		})
	}
}

// ---- TestResolveRemoteIdFromURL_NPM_Scoped ----

func TestResolveRemoteIdFromURL_NPM_Scoped(t *testing.T) {
	tests := []struct {
		url    string
		name   string
	}{
		{"https://registry.npmjs.org/@angular/core", "@angular/core"},
		{"https://npmjs.com/package/@types/node", "@types/node"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			rid, err := ResolveRemoteIdFromURL(tt.url)
			if err != nil {
				t.Fatalf("ResolveRemoteIdFromURL error = %v", err)
			}
			if rid == nil {
				t.Fatal("ResolveRemoteIdFromURL returned nil")
			}
			if rid.Type != remoteid.TypeNPM {
				t.Errorf("Type = %v, want %v", rid.Type, remoteid.TypeNPM)
			}
			if rid.Name != tt.name {
				t.Errorf("Name = %q, want %q", rid.Name, tt.name)
			}
		})
	}
}

// ---- TestResolveRemoteIdFromURL_PyPI_Simple ----

func TestResolveRemoteIdFromURL_PyPI_Simple(t *testing.T) {
	tests := []struct {
		url  string
		name string
	}{
		{"https://pypi.org/simple/requests/", "requests"},
		{"https://pypi.org/simple/requests", "requests"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			rid, err := ResolveRemoteIdFromURL(tt.url)
			if err != nil {
				t.Fatalf("ResolveRemoteIdFromURL error = %v", err)
			}
			if rid == nil {
				t.Fatal("ResolveRemoteIdFromURL returned nil")
			}
			if rid.Type != remoteid.TypePyPI {
				t.Errorf("Type = %v, want %v", rid.Type, remoteid.TypePyPI)
			}
			if rid.Name != tt.name {
				t.Errorf("Name = %q, want %q", rid.Name, tt.name)
			}
		})
	}
}

// ---- TestProviderTypes ----

func TestProviderTypes(t *testing.T) {
	types := ProviderTypes()
	expected := []remoteid.RemoteIdType{
		remoteid.TypeGitHub,
		remoteid.TypePyPI,
		remoteid.TypeNPM,
		remoteid.TypeGitLab,
		remoteid.TypeCodeberg,
		remoteid.TypeSourceHut,
	}
	if len(types) != len(expected) {
		t.Fatalf("len(ProviderTypes()) = %d, want %d", len(types), len(expected))
	}
	for i, want := range expected {
		if types[i] != want {
			t.Errorf("ProviderTypes()[%d] = %v, want %v", i, types[i], want)
		}
	}
}
