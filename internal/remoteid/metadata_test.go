package remoteid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMetadata(t *testing.T) {
	xmlContent := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkgmetadata SYSTEM "http://www.gentoo.org/dtd/metadata.dtd">
<pkgmetadata>
  <longdescription>This is a test package for Gentoo Linux.</longdescription>
  <maintainer>
    <email>test@gentoo.org</email>
    <name>Test Maintainer</name>
  </maintainer>
</pkgmetadata>
`)

	m, err := ParseMetadata(xmlContent)
	if err != nil {
		t.Fatalf("ParseMetadata() error = %v", err)
	}

	if m.Longdesc != "This is a test package for Gentoo Linux." {
		t.Errorf("Longdesc = %q, want %q", m.Longdesc, "This is a test package for Gentoo Linux.")
	}

	if len(m.Maintainers) != 1 {
		t.Fatalf("Maintainers count = %d, want 1", len(m.Maintainers))
	}
	if m.Maintainers[0].Email != "test@gentoo.org" {
		t.Errorf("Email = %q, want %q", m.Maintainers[0].Email, "test@gentoo.org")
	}
	if m.Maintainers[0].Name != "Test Maintainer" {
		t.Errorf("Name = %q, want %q", m.Maintainers[0].Name, "Test Maintainer")
	}
}

func TestParseMetadata_Invalid(t *testing.T) {
	_, err := ParseMetadata([]byte("not valid xml"))
	if err == nil {
		t.Error("ParseMetadata() should error on invalid XML")
	}
}

func TestParseMetadataFile(t *testing.T) {
	dir := t.TempDir()
	xmlContent := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<pkgmetadata>
  <longdescription>File test</longdescription>
</pkgmetadata>
`)
	path := filepath.Join(dir, "metadata.xml")
	if err := os.WriteFile(path, xmlContent, 0644); err != nil {
		t.Fatal(err)
	}

	m, err := ParseMetadataFile(path)
	if err != nil {
		t.Fatalf("ParseMetadataFile() error = %v", err)
	}

	if m.Longdesc != "File test" {
		t.Errorf("Longdesc = %q, want %q", m.Longdesc, "File test")
	}
}

func TestParseMetadataFile_NotFound(t *testing.T) {
	_, err := ParseMetadataFile("/nonexistent/metadata.xml")
	if err == nil {
		t.Error("ParseMetadataFile() should error on missing file")
	}
}

func TestFindUpstreamURL(t *testing.T) {
	m := &Metadata{
		Longdesc: `This is a test package.

Homepage: https://example.com
See also: http://test.org`,
	}

	url := m.FindUpstreamURL()
	if url != "https://example.com" {
		t.Errorf("FindUpstreamURL() = %q, want %q", url, "https://example.com")
	}
}

func TestFindUpstreamURL_NoURL(t *testing.T) {
	m := &Metadata{
		Longdesc: "No URL in this description",
	}

	url := m.FindUpstreamURL()
	if url != "" {
		t.Errorf("FindUpstreamURL() = %q, want empty", url)
	}
}

func TestFindUpstreamURL_Empty(t *testing.T) {
	m := &Metadata{}

	url := m.FindUpstreamURL()
	if url != "" {
		t.Errorf("FindUpstreamURL() = %q, want empty", url)
	}
}

// ---- TestMetadataXML_Parse: full metadata.xml ----

func TestMetadataXML_Parse(t *testing.T) {
	xmlContent := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkgmetadata SYSTEM "http://www.gentoo.org/dtd/metadata.dtd">
<pkgmetadata>
  <longdescription>This is a test package for Gentoo Linux.</longdescription>
  <maintainer>
    <email>test@gentoo.org</email>
    <name>Test Maintainer</name>
  </maintainer>
  <upstream>
    <remote-id type="github">golang/go</remote-id>
    <remote-id type="pypi">requests</remote-id>
  </upstream>
</pkgmetadata>`)

	m, err := ParseMetadata(xmlContent)
	if err != nil {
		t.Fatalf("ParseMetadata() error = %v", err)
	}

	if m.Longdesc != "This is a test package for Gentoo Linux." {
		t.Errorf("Longdesc = %q, want %q", m.Longdesc, "This is a test package for Gentoo Linux.")
	}

	if len(m.Maintainers) != 1 {
		t.Fatalf("Maintainers count = %d, want 1", len(m.Maintainers))
	}

	if len(m.Remotes) != 2 {
		t.Fatalf("Remotes count = %d, want 2", len(m.Remotes))
	}

	if m.Remotes[0].Type != TypeGitHub || m.Remotes[0].Owner != "golang" || m.Remotes[0].Repo != "go" {
		t.Errorf("Remote[0] = %+v, want github:golang/go", m.Remotes[0])
	}
	if m.Remotes[1].Type != TypePyPI || m.Remotes[1].Name != "requests" {
		t.Errorf("Remote[1] = %+v, want pypi:requests", m.Remotes[1])
	}

	if !m.HasRemoteIds() {
		t.Error("HasRemoteIds() should be true")
	}
}

// ---- TestMetadataXML_MissingFile ----

func TestMetadataXML_MissingFile(t *testing.T) {
	_, err := ParseMetadataFile("/nonexistent/path/metadata.xml")
	if err == nil {
		t.Error("ParseMetadataFile() should error on missing file")
	}
}

// ---- TestRemoteId_Multiple: multiple <remote-id> tags ----

func TestRemoteId_MultipleTags(t *testing.T) {
	xmlContent := []byte(`
<pkgmetadata>
  <upstream>
    <remote-id type="github">owner/repo1</remote-id>
    <remote-id type="github">owner/repo2</remote-id>
    <remote-id type="pypi">package-a</remote-id>
    <remote-id type="npm">lodash</remote-id>
  </upstream>
</pkgmetadata>`)

	m, err := ParseMetadata(xmlContent)
	if err != nil {
		t.Fatalf("ParseMetadata() error = %v", err)
	}

	if len(m.Remotes) != 4 {
		t.Fatalf("Got %d remote-ids, want 4", len(m.Remotes))
	}

	expecteds := []struct {
		ty     RemoteIdType
		owner  string
		repo   string
		name   string
	}{
		{TypeGitHub, "owner", "repo1", ""},
		{TypeGitHub, "owner", "repo2", ""},
		{TypePyPI, "", "", "package-a"},
		{TypeNPM, "", "", "lodash"},
	}

	for i, exp := range expecteds {
		r := m.Remotes[i]
		if r.Type != exp.ty {
			t.Errorf("Remote[%d].Type = %v, want %v", i, r.Type, exp.ty)
		}
		if r.Owner != exp.owner {
			t.Errorf("Remote[%d].Owner = %q, want %q", i, r.Owner, exp.owner)
		}
		if r.Repo != exp.repo {
			t.Errorf("Remote[%d].Repo = %q, want %q", i, r.Repo, exp.repo)
		}
		if r.Name != exp.name {
			t.Errorf("Remote[%d].Name = %q, want %q", i, r.Name, exp.name)
		}
	}
}

// ---- TestMetadataXML_Absent: no <remote-id> ----

func TestMetadataXML_Absent(t *testing.T) {
	xmlContent := []byte(`
<pkgmetadata>
  <longdescription>No remote ids here.</longdescription>
</pkgmetadata>`)

	m, err := ParseMetadata(xmlContent)
	if err != nil {
		t.Fatalf("ParseMetadata() error = %v", err)
	}

	if m.HasRemoteIds() {
		t.Error("HasRemoteIds() should be false when no <remote-id> tags")
	}
	if len(m.Remotes) != 0 {
		t.Errorf("Remotes should be empty, got %d", len(m.Remotes))
	}
}

// ---- New tests per plan 15 ----

func TestParseMetadata_RealShape(t *testing.T) {
	xmlContent := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkgmetadata SYSTEM "https://www.gentoo.org/dtd/metadata.dtd">
<pkgmetadata>
  <maintainer type="person">
    <email>web@bennypowers.com</email>
    <name>Benny Powers</name>
  </maintainer>
  <upstream>
    <remote-id type="github">owner/repo</remote-id>
    <remote-id type="sourcehut">~tsdh/nirius</remote-id>
    <remote-id type="gitlab">gitlab.com:GNOME/geary</remote-id>
    <remote-id type="pypi">requests</remote-id>
    <bugs-to>https://todo.sr.ht/~tsdh/nirius</bugs-to>
  </upstream>
  <longdescription>
    Nirius provides utility commands for the niri wayland compositor.
  </longdescription>
</pkgmetadata>`)

	m, err := ParseMetadata(xmlContent)
	if err != nil {
		t.Fatalf("ParseMetadata() error = %v", err)
	}

	if len(m.Remotes) != 4 {
		t.Fatalf("Got %d remote-ids, want 4", len(m.Remotes))
	}

	// github
	if m.Remotes[0].Type != TypeGitHub || m.Remotes[0].Owner != "owner" || m.Remotes[0].Repo != "repo" {
		t.Errorf("Remote[0] = %+v, want github:owner/repo", m.Remotes[0])
	}
	// sourcehut
	if m.Remotes[1].Type != TypeSourceHut || m.Remotes[1].Host != "git.sr.ht" || m.Remotes[1].Path != "~tsdh/nirius" {
		t.Errorf("Remote[1] = %+v, want sourcehut:git.sr.ht/~tsdh/nirius", m.Remotes[1])
	}
	// gitlab
	if m.Remotes[2].Type != TypeGitLab || m.Remotes[2].Host != "gitlab.com" || m.Remotes[2].Path != "GNOME/geary" {
		t.Errorf("Remote[2] = %+v, want gitlab:gitlab.com/GNOME/geary", m.Remotes[2])
	}
	// pypi
	if m.Remotes[3].Type != TypePyPI || m.Remotes[3].Name != "requests" {
		t.Errorf("Remote[3] = %+v, want pypi:requests", m.Remotes[3])
	}
}

func TestParseMetadata_NoUpstreamBlock(t *testing.T) {
	xmlContent := []byte(`
<pkgmetadata>
  <maintainer type="person">
    <email>test@example.com</email>
    <name>Test</name>
  </maintainer>
</pkgmetadata>`)

	m, err := ParseMetadata(xmlContent)
	if err != nil {
		t.Fatalf("ParseMetadata() error = %v", err)
	}
	if len(m.Remotes) != 0 {
		t.Fatalf("Expected 0 remotes, got %d", len(m.Remotes))
	}
}

func TestParseMetadata_Malformed(t *testing.T) {
	_, err := ParseMetadata([]byte("not valid xml"))
	if err == nil {
		t.Error("ParseMetadata() should error on invalid XML")
	}
}

func TestParseMetadataFile_Nirius(t *testing.T) {
	fixturePath := filepath.Join("testdata", "metadata", "nirius.xml")
	m, err := ParseMetadataFile(fixturePath)
	if err != nil {
		t.Fatalf("ParseMetadataFile() error = %v", err)
	}
	if len(m.Remotes) != 1 {
		t.Fatalf("Got %d remote-ids, want 1", len(m.Remotes))
	}
	r := m.Remotes[0]
	if r.Type != TypeSourceHut {
		t.Errorf("Type = %v, want %v", r.Type, TypeSourceHut)
	}
	if r.Host != "git.sr.ht" {
		t.Errorf("Host = %q, want %q", r.Host, "git.sr.ht")
	}
	if r.Path != "~tsdh/nirius" {
		t.Errorf("Path = %q, want %q", r.Path, "~tsdh/nirius")
	}
}

func TestRemoteId_SourceHutString(t *testing.T) {
	r, err := ParseRemoteIdFromStrings("sourcehut", "~tsdh/nirius")
	if err != nil {
		t.Fatalf("ParseRemoteIdFromStrings(sourcehut) error = %v", err)
	}
	if r.Host != "git.sr.ht" {
		t.Errorf("Host = %q, want %q", r.Host, "git.sr.ht")
	}
	if r.Path != "~tsdh/nirius" {
		t.Errorf("Path = %q, want %q", r.Path, "~tsdh/nirius")
	}
	want := "sourcehut:git.sr.ht/~tsdh/nirius"
	if got := r.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
