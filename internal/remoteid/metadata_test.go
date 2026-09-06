package remoteid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMetadata(t *testing.T) {
	xmlContent := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkg-metadata SYSTEM "http://www.gentoo.org/dtd/metadata.dtd">
<pkg-metadata>
  <longdescription>This is a test package for Gentoo Linux.</longdescription>
  <maintainer>
    <email>test@gentoo.org</email>
    <name>Test Maintainer</name>
  </maintainer>
</pkg-metadata>
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
<pkg-metadata>
  <longdescription>File test</longdescription>
</pkg-metadata>
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
<!DOCTYPE pkg-metadata SYSTEM "http://www.gentoo.org/dtd/metadata.dtd">
<pkg-metadata>
  <longdescription>This is a test package for Gentoo Linux.</longdescription>
  <maintainer>
    <email>test@gentoo.org</email>
    <name>Test Maintainer</name>
  </maintainer>
  <remote-id type="github">golang/go</remote-id>
  <remote-id type="pypi">requests</remote-id>
</pkg-metadata>`)

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
<pkg-metadata>
  <remote-id type="github">owner/repo1</remote-id>
  <remote-id type="github">owner/repo2</remote-id>
  <remote-id type="pypi">package-a</remote-id>
  <remote-id type="npm">lodash</remote-id>
</pkg-metadata>`)

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
<pkg-metadata>
  <longdescription>No remote ids here.</longdescription>
</pkg-metadata>`)

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
