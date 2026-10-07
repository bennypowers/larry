package providers

import "testing"

// ---------------------------------------------------------------------------
// extractSourceHutTags — unit tests
// ---------------------------------------------------------------------------
func TestExtractSourceHutTags(t *testing.T) {
	// Real HTML from SourceHut refs page (tags on separate lines)
	html := `<a href="/~tsdh/nirius/refs/nirius-0.10.0">0.10.0</a>
<a href="/~tsdh/nirius/refs/nirius-0.9.0">0.9.0</a>
<pre class="pb-0">Release nirius-0.10.0
</pre>
<pre class="pb-0">Release nirius-0.8.0
</pre>
<a href="/~tsdh/nirius/refs">refs</a>
<a href="/~tsdh/nirius/refs/rss.xml">rss</a>
`
	tags := extractSourceHutTags(html)

	// Should extract: nirius-0.10.0, nirius-0.9.0, nirius-0.8.0
	// (nirius-0.10.0 from both patterns but deduplicated)
	if len(tags) < 3 {
		t.Fatalf("expected at least 3 tags, got %d: %v", len(tags), tags)
	}
	// First tag should be the most recent
	if tags[0] != "nirius-0.10.0" {
		t.Errorf("first tag = %q, want \"nirius-0.10.0\"", tags[0])
	}
}

func TestExtractSourceHutPkgName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"~tsdh/nirius", "nirius"},
		{"~user/project", "project"},
		{"~user/my-project", "my-project"},
	}
	for _, tt := range tests {
		got := extractSourceHutPkgName(tt.path)
		if got != tt.want {
			t.Errorf("extractSourceHutPkgName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestStripPkgNamePrefix(t *testing.T) {
	tests := []struct {
		tag     string
		pkgName string
		want    string
	}{
		{"nirius-0.10.0", "nirius", "0.10.0"},
		{"nirius-0.9.0", "nirius", "0.9.0"},
		{"nirius_1.0", "nirius", "1.0"},
		{"nirius-v0.10.0", "nirius", "v0.10.0"}, // stripPkgNamePrefix doesn't strip v; Normalize will handle it
		{"v0.10.0", "nirius", "v0.10.0"}, // no match — return as-is
	}
	for _, tt := range tests {
		got := stripPkgNamePrefix(tt.tag, tt.pkgName)
		if got != tt.want {
			t.Errorf("stripPkgNamePrefix(%q, %q) = %q, want %q", tt.tag, tt.pkgName, got, tt.want)
		}
	}
}
