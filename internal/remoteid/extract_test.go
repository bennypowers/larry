package remoteid

import (
	"testing"
)

// ---- TestExtractFromURLs_GitHub ----

func TestExtractFromURLs_GitHub(t *testing.T) {
	r := ExtractFromURLs(TypeGitHub,
		"https://github.com/owner/repo/archive/v1.0.tar.gz", "")
	if r == nil {
		t.Fatal("expected match, got nil")
	}
	if r.Type != TypeGitHub {
		t.Errorf("Type = %v, want %v", r.Type, TypeGitHub)
	}
	if r.Owner != "owner" {
		t.Errorf("Owner = %q, want %q", r.Owner, "owner")
	}
	if r.Repo != "repo" {
		t.Errorf("Repo = %q, want %q", r.Repo, "repo")
	}
}

// ---- TestExtractFromURLs_StripsDotGit ----

func TestExtractFromURLs_StripsDotGit(t *testing.T) {
	r := ExtractFromURLs(TypeGitHub,
		"https://github.com/owner/repo.git", "")
	if r == nil {
		t.Fatal("expected match, got nil")
	}
	if r.Repo != "repo" {
		t.Errorf("Repo = %q, want %q", r.Repo, "repo")
	}
}

// ---- TestExtractFromURLs_GitLabDotCom ----

func TestExtractFromURLs_GitLabDotCom(t *testing.T) {
	r := ExtractFromURLs(TypeGitLab,
		"https://gitlab.com/GNOME/geary/-/archive/44.0/geary-44.0.tar.gz", "")
	if r == nil {
		t.Fatal("expected match, got nil")
	}
	if r.Type != TypeGitLab {
		t.Errorf("Type = %v, want %v", r.Type, TypeGitLab)
	}
	if r.Host != "gitlab.com" {
		t.Errorf("Host = %q, want %q", r.Host, "gitlab.com")
	}
	if r.Path != "GNOME/geary" {
		t.Errorf("Path = %q, want %q", r.Path, "GNOME/geary")
	}
}

// ---- TestExtractFromURLs_GitLabSelfHosted ----

func TestExtractFromURLs_GitLabSelfHosted(t *testing.T) {
	r := ExtractFromURLs(TypeGitLab,
		"https://gitlab.gnome.org/GNOME/geary", "")
	if r == nil {
		// No /-/ in URL, so no match — that's expected
		t.Log("no match for self-hosted without /-/ (expected)")
		return
	}
	if r.Host != "gitlab.gnome.org" {
		t.Errorf("Host = %q, want %q", r.Host, "gitlab.gnome.org")
	}
	if r.Path != "GNOME/geary" {
		t.Errorf("Path = %q, want %q", r.Path, "GNOME/geary")
	}
}

// ---- TestExtractFromURLs_GitLabNested ----

func TestExtractFromURLs_GitLabNested(t *testing.T) {
	r := ExtractFromURLs(TypeGitLab,
		"https://gitlab.com/a/b/c/-/archive/1/archive.tar.gz", "")
	if r == nil {
		t.Fatal("expected match, got nil")
	}
	if r.Host != "gitlab.com" {
		t.Errorf("Host = %q, want %q", r.Host, "gitlab.com")
	}
	if r.Path != "a/b/c" {
		t.Errorf("Path = %q, want %q", r.Path, "a/b/c")
	}
}

// ---- TestExtractFromURLs_SourceHut ----

func TestExtractFromURLs_SourceHut(t *testing.T) {
	tests := []struct {
		url  string
		host string
		path string
	}{
		{"https://git.sr.ht/~sircmpwn/scdoc", "git.sr.ht", "~sircmpwn/scdoc"},
		{"https://sr.ht/~tsdh/nirius", "git.sr.ht", "~tsdh/nirius"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			r := ExtractFromURLs(TypeSourceHut, tt.url, "")
			if r == nil {
				t.Fatalf("expected match for %q, got nil", tt.url)
			}
			if r.Type != TypeSourceHut {
				t.Errorf("Type = %v, want %v", r.Type, TypeSourceHut)
			}
			if r.Host != tt.host {
				t.Errorf("Host = %q, want %q", r.Host, tt.host)
			}
			if r.Path != tt.path {
				t.Errorf("Path = %q, want %q", r.Path, tt.path)
			}
		})
	}
}

// ---- TestExtractFromURLs_Codeberg ----

func TestExtractFromURLs_Codeberg(t *testing.T) {
	r := ExtractFromURLs(TypeCodeberg,
		"https://codeberg.org/owner/repo/archive/v1.tar.gz", "")
	if r == nil {
		t.Fatal("expected match, got nil")
	}
	if r.Type != TypeCodeberg {
		t.Errorf("Type = %v, want %v", r.Type, TypeCodeberg)
	}
	if r.Owner != "owner" {
		t.Errorf("Owner = %q, want %q", r.Owner, "owner")
	}
	if r.Repo != "repo" {
		t.Errorf("Repo = %q, want %q", r.Repo, "repo")
	}
}

// ---- TestExtractFromURLs_SkipsUnexpandedVars ----

func TestExtractFromURLs_SkipsUnexpandedVars(t *testing.T) {
	tests := []struct {
		name   string
		forge  RemoteIdType
		srcURI string
		owner  string
		repo   string
		path   string
	}{
		{
			"GitHub", TypeGitHub,
			"https://github.com/${PN}/${PN}/archive/v1.tar.gz https://github.com/owner/repo/archive/v1.tar.gz",
			"owner", "repo", "",
		},
		{
			"Codeberg", TypeCodeberg,
			"https://codeberg.org/${PN}/${PN} https://codeberg.org/owner/repo",
			"owner", "repo", "",
		},
		{
			"GitLab", TypeGitLab,
			"https://gitlab.com/${PN}/x/-/archive/1/x.tar.gz https://gitlab.com/group/project/-/archive/1/x.tar.gz",
			"", "", "group/project",
		},
		{
			"SourceHut", TypeSourceHut,
			"https://git.sr.ht/~${PN}/x https://git.sr.ht/~user/project",
			"", "", "~user/project",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := ExtractFromURLs(tt.forge, tt.srcURI, "")
			if r == nil {
				t.Fatal("expected match, got nil")
			}
			if tt.owner != "" && r.Owner != tt.owner {
				t.Errorf("Owner = %q, want %q", r.Owner, tt.owner)
			}
			if tt.repo != "" && r.Repo != tt.repo {
				t.Errorf("Repo = %q, want %q", r.Repo, tt.repo)
			}
			if tt.path != "" && r.Path != tt.path {
				t.Errorf("Path = %q, want %q", r.Path, tt.path)
			}
		})
	}
}

// ---- TestExtractFromURLs_KeepsURLsWithEmbeddedVars ----
// URLs where owner/repo are concrete but the path contains ${PV} etc.
// must still resolve — the ${} is in the version part, not the repo.
func TestExtractFromURLs_KeepsURLsWithEmbeddedVars(t *testing.T) {
	// hylki: https://github.com/hyprlab/hylki/archive/.../${PV}...
	r := ExtractFromURLs(TypeGitHub,
		"https://github.com/hyprlab/hylki/archive/refs/tags/v${PV}.tar.gz",
		"")
	if r == nil {
		t.Fatal("expected RemoteId for hylki-style GitHub URL")
	}
	if r.Owner != "hyprlab" || r.Repo != "hylki" {
		t.Errorf("Owner=%q Repo=%q, want hyprlab hylki", r.Owner, r.Repo)
	}

	// Codeberg with ${PV} in path
	r2 := ExtractFromURLs(TypeCodeberg,
		"https://codeberg.org/acme/widget/archive/v${PV}.tar.gz",
		"")
	if r2 == nil {
		t.Fatal("expected RemoteId for codeberg-style URL with ${PV}")
	}
	if r2.Owner != "acme" || r2.Repo != "widget" {
		t.Errorf("Owner=%q Repo=%q, want acme widget", r2.Owner, r2.Repo)
	}

	// SourceHut with ${PV} in path
	r3 := ExtractFromURLs(TypeSourceHut,
		"https://git.sr.ht/~tsdh/scdoc/archive/v${PV}.tar.gz",
		"")
	if r3 == nil {
		t.Fatal("expected RemoteId for sourcehut-style URL with ${PV}")
	}
	if r3.Path != "~tsdh/scdoc" {
		t.Errorf("Path=%q, want ~tsdh/scdoc", r3.Path)
	}

	// GitLab with ${PV} in path (GitLab uses /-/archive/ format)
	r4 := ExtractFromURLs(TypeGitLab,
		"https://gitlab.gnome.org/GNOME/geary/-/archive/v${PV}/geary-${PV}.tar.gz",
		"")
	if r4 == nil {
		t.Fatal("expected RemoteId for gitlab-style URL with ${PV}")
	}
	if r4.Path != "GNOME/geary" {
		t.Errorf("Path=%q, want GNOME/geary", r4.Path)
	}
}

// ---- TestExtractFromURLs_IgnoresAboutGitlabCom ----

func TestExtractFromURLs_IgnoresAboutGitlabCom(t *testing.T) {
	r := ExtractFromURLs(TypeGitLab, "", "https://about.gitlab.com")
	if r != nil {
		t.Errorf("expected nil for about.gitlab.com, got %+v", r)
	}
}

// ---- TestExtractFromURLs_NoMatch ----

func TestExtractFromURLs_NoMatch(t *testing.T) {
	r := ExtractFromURLs(TypeGitHub, "https://example.com/some/repo", "")
	if r != nil {
		t.Errorf("expected nil, got %+v", r)
	}
}
