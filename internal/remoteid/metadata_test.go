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
