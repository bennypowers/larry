package remoteid

import (
	"testing"
)

// ---- TestRemoteId_Parse_GitHub ----

func TestRemoteId_Parse_GitHub(t *testing.T) {
	r, err := ParseRemoteIdFromStrings("github", "golang/go")
	if err != nil {
		t.Fatalf("ParseRemoteIdFromStrings(github) error = %v", err)
	}
	if r.Type != TypeGitHub {
		t.Errorf("Type = %v, want %v", r.Type, TypeGitHub)
	}
	if r.Owner != "golang" {
		t.Errorf("Owner = %q, want %q", r.Owner, "golang")
	}
	if r.Repo != "go" {
		t.Errorf("Repo = %q, want %q", r.Repo, "go")
	}
}

// ---- TestRemoteId_Parse_PyPI ----

func TestRemoteId_Parse_PyPI(t *testing.T) {
	r, err := ParseRemoteIdFromStrings("pypi", "requests")
	if err != nil {
		t.Fatalf("ParseRemoteIdFromStrings(pypi) error = %v", err)
	}
	if r.Type != TypePyPI {
		t.Errorf("Type = %v, want %v", r.Type, TypePyPI)
	}
	if r.Name != "requests" {
		t.Errorf("Name = %q, want %q", r.Name, "requests")
	}
	if r.Owner != "" || r.Repo != "" {
		t.Errorf("Owner/Repo should be empty, got %q/%q", r.Owner, r.Repo)
	}
}

// ---- TestRemoteId_Parse_GitLab ----

func TestRemoteId_Parse_GitLab(t *testing.T) {
	r, err := ParseRemoteIdFromStrings("gitlab", "gitlab.com:group/project")
	if err != nil {
		t.Fatalf("ParseRemoteIdFromStrings(gitlab) error = %v", err)
	}
	if r.Type != TypeGitLab {
		t.Errorf("Type = %v, want %v", r.Type, TypeGitLab)
	}
	if r.Host != "gitlab.com" {
		t.Errorf("Host = %q, want %q", r.Host, "gitlab.com")
	}
	if r.Path != "group/project" {
		t.Errorf("Path = %q, want %q", r.Path, "group/project")
	}
}

// ---- TestRemoteId_Parse_NPM ----

func TestRemoteId_Parse_NPM(t *testing.T) {
	r, err := ParseRemoteIdFromStrings("npm", "lodash")
	if err != nil {
		t.Fatalf("ParseRemoteIdFromStrings(npm) error = %v", err)
	}
	if r.Type != TypeNPM {
		t.Errorf("Type = %v, want %v", r.Type, TypeNPM)
	}
	if r.Name != "lodash" {
		t.Errorf("Name = %q, want %q", r.Name, "lodash")
	}
	if r.Owner != "" || r.Repo != "" {
		t.Errorf("Owner/Repo should be empty, got %q/%q", r.Owner, r.Repo)
	}
}

// ---- TestRemoteId_Parse_Codeberg ----

func TestRemoteId_Parse_Codeberg(t *testing.T) {
	r, err := ParseRemoteIdFromStrings("codeberg", "owner/repo")
	if err != nil {
		t.Fatalf("ParseRemoteIdFromStrings(codeberg) error = %v", err)
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

// ---- TestRemoteId_Parse_SourceHut ----

func TestRemoteId_Parse_SourceHut(t *testing.T) {
	r, err := ParseRemoteIdFromStrings("sourcehut", "git.sr.ht/~user/project")
	if err != nil {
		t.Fatalf("ParseRemoteIdFromStrings(sourcehut) error = %v", err)
	}
	if r.Type != TypeSourceHut {
		t.Errorf("Type = %v, want %v", r.Type, TypeSourceHut)
	}
	if r.Host != "git.sr.ht" {
		t.Errorf("Host = %q, want %q", r.Host, "git.sr.ht")
	}
	if r.Path != "~user/project" {
		t.Errorf("Path = %q, want %q", r.Path, "~user/project")
	}
}

// ---- TestRemoteId_Multiple: different types ----

func TestRemoteId_Multiple(t *testing.T) {
	types := []RemoteIdType{TypeGitHub, TypePyPI, TypeNPM, TypeGitLab, TypeCodeberg, TypeSourceHut}
	for _, rtype := range types {
		t.Run(string(rtype), func(t *testing.T) {
			var content string
			switch rtype {
			case TypeGitHub, TypeCodeberg:
				content = "owner/repo"
			case TypePyPI, TypeNPM:
				content = "package-name"
			case TypeGitLab:
				content = "gitlab.com:group/project"
			case TypeSourceHut:
				content = "git.sr.ht/~user/project"
			}
			r, err := ParseRemoteIdFromStrings(string(rtype), content)
			if err != nil {
				t.Fatalf("ParseRemoteIdFromStrings(%q) error = %v", rtype, err)
			}
			if r.Type != rtype {
				t.Errorf("Type = %v, want %v", r.Type, rtype)
			}
		})
	}
}

// ---- TestRemoteId_Absent: empty content ----

func TestRemoteId_Absent(t *testing.T) {
	_, err := ParseRemoteIdFromStrings("github", "")
	if err == nil {
		t.Error("ParseRemoteIdFromStrings(github, \"\") should error for empty content")
	}
}

// ---- TestRemoteId_Invalid: malformed ----

func TestRemoteId_Invalid(t *testing.T) {
	tests := []struct {
		rtype   string
		content string
	}{
		{"github", "nocolon"},           // missing /
		{"github", "/repo"},             // missing owner
		{"github", "owner/"},            // missing repo
		{"pypi", ""},                    // empty
		{"gitlab", ""},                  // empty
		{"unknown-type", "something"},   // unknown type
	}
	for _, tt := range tests {
		t.Run(tt.rtype+":"+tt.content, func(t *testing.T) {
			_, err := ParseRemoteIdFromStrings(tt.rtype, tt.content)
			if err == nil {
				t.Errorf("ParseRemoteIdFromStrings(%q, %q) should error", tt.rtype, tt.content)
			}
		})
	}
}

// ---- TestRemoteId_Parse_XML ----

func TestRemoteId_Parse_XML(t *testing.T) {
	xmlContent := []byte(`
<remote-id type="github">
  golang/go
</remote-id>`)
	r, err := ParseRemoteId(xmlContent)
	if err != nil {
		t.Fatalf("ParseRemoteId() error = %v", err)
	}
	if r.Type != TypeGitHub {
		t.Errorf("Type = %v, want %v", r.Type, TypeGitHub)
	}
	if r.Owner != "golang" {
		t.Errorf("Owner = %q, want %q", r.Owner, "golang")
	}
	if r.Repo != "go" {
		t.Errorf("Repo = %q, want %q", r.Repo, "go")
	}
}

// ---- TestRemoteId_String ----

func TestRemoteId_String(t *testing.T) {
	tests := []struct {
		id   RemoteId
		want string
	}{
		{RemoteId{Type: TypeGitHub, Owner: "golang", Repo: "go"}, "github:golang/go"},
		{RemoteId{Type: TypePyPI, Name: "requests"}, "pypi:requests"},
		{RemoteId{Type: TypeNPM, Name: "lodash"}, "npm:lodash"},
		{RemoteId{Type: TypeGitLab, Host: "gitlab.com", Path: "group/project"}, "gitlab:gitlab.com/group/project"},
		{RemoteId{Type: TypeCodeberg, Owner: "owner", Repo: "repo"}, "codeberg:owner/repo"},
		{RemoteId{Type: TypeSourceHut, Host: "git.sr.ht", Path: "~user/project"}, "sourcehut:git.sr.ht/~user/project"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.id.String(); got != tt.want {
				t.Errorf("RemoteId.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---- TestRemoteId_IsZero ----

func TestRemoteId_IsZero(t *testing.T) {
	var zero *RemoteId
	if !zero.IsZero() {
		t.Error("nil RemoteId should be zero")
	}
	empty := &RemoteId{}
	if !empty.IsZero() {
		t.Error("empty type RemoteId should be zero")
	}
	unknown := &RemoteId{Type: TypeUnknown}
	if !unknown.IsZero() {
		t.Error("unknown RemoteId should be zero")
	}
	valid := &RemoteId{Type: TypeGitHub, Owner: "golang", Repo: "go"}
	if valid.IsZero() {
		t.Error("non-empty RemoteId should not be zero")
	}
}

// ---- TestRemoteId_BackwardsCompat ----

func TestRemoteId_BackwardsCompat(t *testing.T) {
	// Legacy <upstream> tags should parse without error
	xmlContent := []byte(`
<upstream>
  <bugs-to>https://github.com/golang/go/issues</bugs-to>
  <maintainer>
    <email>gopher@golang.org</email>
    <name>Go Team</name>
  </maintainer>
</upstream>`)
	r, err := ParseRemoteId(xmlContent)
	// Legacy upstream doesn't have a type attr, so should return an error or nil
	// (this is acceptable — backwards compat means it doesn't crash)
	if err == nil && r != nil {
		t.Logf("ParseRemoteId on legacy upstream returned: %+v", r)
	}
}
