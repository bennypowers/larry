package upstream

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusJSONName(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusOutdated, "outdated"},
		{StatusUpToDate, "up_to_date"},
		{StatusAhead, "ahead"},
		{StatusNewerTag, "newer_tag"},
		{StatusLive, "live"},
		{StatusDeprecated, "deprecated"},
		{StatusUnknown, "unknown"},
		{StatusHasRelease, "has_release"},
		{StatusCheckTag, "check_tag"},
		{StatusManual, "manual"},
		{Status(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.status.JSONName(); got != tt.want {
			t.Errorf("Status(%d).JSONName() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestNewJSONOutput_Shape(t *testing.T) {
	results := []UpstreamResult{
		{
			Package: "dev-util/alpha",
			Current: "1.0.0",
			Latest:  "1.1.0",
			Status:  StatusOutdated,
			Source:  SourceInfo{Label: "GitHub:acme/alpha", URL: "https://github.com/acme/alpha", Glyph: ""},
			Depth:   0,
		},
		{
			Package: "dev-util/old",
			Current: "1.0.0",
			Status:  StatusDeprecated,
			Source:  SourceInfo{Label: "deprecated"},
			Target:  "dev-util/new",
			Error:   "renamed",
			Depth:   1,
		},
	}

	out := NewJSONOutput(results)
	if out.SchemaVersion != JSONSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", out.SchemaVersion, JSONSchemaVersion)
	}
	if len(out.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(out.Results))
	}
	first := out.Results[0]
	if first.Status != "outdated" || first.Source.Label != "GitHub:acme/alpha" || first.Source.URL == "" {
		t.Errorf("first result = %+v", first)
	}
	second := out.Results[1]
	if second.Target != "dev-util/new" || second.Error != "renamed" || second.Depth != 1 {
		t.Errorf("second result = %+v", second)
	}
}

func TestRenderJSON_DecodesAndOmitsGlyph(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, []UpstreamResult{{
		Package: "dev-util/alpha",
		Current: "1.0.0",
		Status:  StatusManual,
		Source:  SourceInfo{Label: "vendor", Glyph: ""},
	}}); err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if decoded["schema_version"] != float64(1) {
		t.Errorf("schema_version = %v, want 1", decoded["schema_version"])
	}
	rows, ok := decoded["results"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("results = %#v", decoded["results"])
	}
	src := rows[0].(map[string]any)["source"].(map[string]any)
	if _, has := src["glyph"]; has {
		t.Errorf("JSON source contains glyph: %#v", src)
	}
	if _, has := rows[0].(map[string]any)["target"]; has {
		t.Errorf("empty target was not omitted: %#v", rows[0])
	}
	if !strings.Contains(buf.String(), `"status": "manual"`) {
		t.Errorf("status is not the string slug: %s", buf.String())
	}
}

func TestRenderJSON_IncludesInsignificantRows(t *testing.T) {
	results := []UpstreamResult{
		{Package: "a/up", Status: StatusUpToDate},
		{Package: "b/live", Status: StatusLive},
		{Package: "c/out", Status: StatusOutdated},
	}
	out := NewJSONOutput(results)
	if len(out.Results) != len(results) {
		t.Errorf("NewJSONOutput dropped rows: got %d, want %d", len(out.Results), len(results))
	}
}

func TestRenderJSON_Golden(t *testing.T) {
	results := []UpstreamResult{
		{Package: "dev-util/alpha", Current: "1.0.0", Latest: "1.1.0", Status: StatusOutdated, Source: SourceInfo{Label: "GitHub:acme/alpha", URL: "https://github.com/acme/alpha", Glyph: ""}, Depth: 0},
		{Package: "dev-util/beta", Current: "2.0.0", Latest: "2.0.0", Status: StatusUpToDate, Source: SourceInfo{Label: "GitLab:gitlab.gnome.org/g/beta", URL: "https://gitlab.gnome.org/g/beta", Glyph: ""}, Depth: 1},
		{Package: "dev-util/gamma", Current: "3.0.0", Latest: "2.9.0", Status: StatusAhead, Source: SourceInfo{Label: "Codeberg:acme/gamma", URL: "https://codeberg.org/acme/gamma"}, Depth: 0},
		{Package: "dev-util/zeta", Current: "9999 (live)", Latest: "6.0.0", Status: StatusHasRelease, Source: SourceInfo{Label: "npm:@acme/zeta", URL: "https://www.npmjs.com/package/@acme/zeta"}, Depth: 0},
		{Package: "dev-util/theta", Current: "8.0.0", Status: StatusDeprecated, Source: SourceInfo{Label: "deprecated"}, Target: "dev-util/iota", Depth: 0},
		{Package: "dev-util/kappa", Current: "9.0.0", Status: StatusUnknown, Source: SourceInfo{}, Error: "no provider match", Depth: 0},
	}

	var buf bytes.Buffer
	if err := RenderJSON(&buf, results); err != nil {
		t.Fatal(err)
	}

	goldenPath := filepath.Join("testdata", "upstream.json.golden")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote golden file %s", goldenPath)
		return
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), golden) {
		t.Errorf("JSON golden mismatch:\n--- got ---\n%s\n--- want ---\n%s", buf.String(), golden)
	}
}

func TestRenderJSON_NoANSI(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, []UpstreamResult{{Package: "a/b", Status: StatusOutdated}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "\x1b") {
		t.Errorf("JSON contains ANSI: %q", buf.String())
	}
}
