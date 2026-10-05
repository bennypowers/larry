package upstream

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/remoteid"
	"github.com/lrstanley/go-nf/glyphs/dev"
)

// failWriter returns 0 bytes and an error on every Write.
type failWriter struct{}

func (failWriter) Write(p []byte) (int, error) { return 0, errors.New("boom") }

// ---------------------------------------------------------------------------
// TestSourceURL_GitLabSelfHosted — D1
// A self-hosted GitLab must use rid.Host, not gitlab.com.
// ---------------------------------------------------------------------------
func TestSourceURL_GitLabSelfHosted(t *testing.T) {
	rid := &remoteid.RemoteId{Type: remoteid.TypeGitLab, Host: "gitlab.gnome.org", Path: "GNOME/geary"}
	info := resolveSourceInfo(rid)
	wantURL := "https://gitlab.gnome.org/GNOME/geary"
	wantLabel := "GitLab:gitlab.gnome.org/GNOME/geary"
	if info.URL != wantURL {
		t.Errorf("URL = %q, want %q", info.URL, wantURL)
	}
	if info.Label != wantLabel {
		t.Errorf("Label = %q, want %q", info.Label, wantLabel)
	}
}

func TestSourceURL_GitLabDotCom(t *testing.T) {
	rid := &remoteid.RemoteId{Type: remoteid.TypeGitLab, Host: "gitlab.com", Path: "a/b"}
	info := resolveSourceInfo(rid)
	wantURL := "https://gitlab.com/a/b"
	if info.URL != wantURL {
		t.Errorf("URL = %q, want %q", info.URL, wantURL)
	}
	// Exactly two slashes after the scheme — no triple-slash.
	if strings.Contains(info.URL, "//a/b") {
		t.Errorf("URL contains double-slash after scheme: %q", info.URL)
	}
}

// ---------------------------------------------------------------------------
// TestSourceLabel_SourceHutCasing — D2
// The label must use "SourceHut:" (capital H), matching the constant.
// ---------------------------------------------------------------------------
func TestSourceLabel_SourceHutCasing(t *testing.T) {
	rid := &remoteid.RemoteId{Type: remoteid.TypeSourceHut, Host: "git.sr.ht", Path: "~tsdh/nirius"}
	info := resolveSourceInfo(rid)
	wantLabel := LabelSourceHut + "git.sr.ht/~tsdh/nirius"
	wantURL := "https://git.sr.ht/~tsdh/nirius"
	if info.Label != wantLabel {
		t.Errorf("Label = %q, want %q", info.Label, wantLabel)
	}
	if info.URL != wantURL {
		t.Errorf("URL = %q, want %q", info.URL, wantURL)
	}
	if info.Glyph == "" {
		t.Error("Glyph must not be empty for SourceHut")
	}
}

// ---------------------------------------------------------------------------
// TestSourceCell_UsesPrecomputedURL — D3
// SourceCell must use r.Source.URL directly, never re-derive from the label.
// ---------------------------------------------------------------------------
func TestSourceCell_UsesPrecomputedURL(t *testing.T) {
	r := UpstreamResult{
		Package: "dev-util/test",
		Source: SourceInfo{
			Label: "GitHub:a/b",
			URL:   "https://example.test/x",
			Glyph: "G",
		},
	}
	cell := SourceCell(r, true)
	if !strings.Contains(cell, "https://example.test/x") {
		t.Errorf("SourceCell must use pre-computed URL; got: %s", cell)
	}
	// Must NOT re-derive from label.
	if strings.Contains(cell, "https://github.com/a/b") {
		t.Error("SourceCell must NOT re-derive URL from label")
	}
}

// ---------------------------------------------------------------------------
// TestSourceCell_NoHyperlinkWhenNotTTY — D4
// hyperlinks==false must produce plain text, no OSC-8 escapes.
// ---------------------------------------------------------------------------
func TestSourceCell_NoHyperlinkWhenNotTTY(t *testing.T) {
	r := UpstreamResult{
		Package: "dev-util/test",
		Source: SourceInfo{
			Label: "GitHub:a/b",
			URL:   "https://example.test/x",
			Glyph: "G",
		},
	}
	cell := SourceCell(r, false)
	if strings.Contains(cell, "\033]8;;") {
		t.Errorf("SourceCell(r, false) must not contain OSC-8 escape; got: %s", cell)
	}
	// Must still show the short label at minimum.
	if !strings.Contains(cell, "a/b") {
		t.Errorf("SourceCell(r, false) must still show short label; got: %s", cell)
	}
}

func TestSourceCell_HyperlinkWhenColorAlways(t *testing.T) {
	r := UpstreamResult{
		Package: "dev-util/test",
		Source: SourceInfo{
			Label: "GitHub:a/b",
			URL:   "https://example.test/x",
			Glyph: "G",
		},
	}
	cell := SourceCell(r, true)
	if !strings.Contains(cell, "\033]8;;https://example.test/x") {
		t.Errorf("SourceCell(r, true) must contain OSC-8 hyperlink; got: %s", cell)
	}
}

func TestSourceCell_Deprecated(t *testing.T) {
	r := UpstreamResult{
		Package: "dev-util/foo",
		Status:  StatusDeprecated,
		Source:  SourceInfo{Label: "deprecated"},
		Target:  "dev-util/newname",
	}
	cell := SourceCell(r, true)
	if !strings.Contains(cell, "deprecated → dev-util/newname") {
		t.Errorf("Deprecated with target; got: %q", cell)
	}
	if strings.Contains(cell, "\033]8;;") {
		t.Error("Deprecated must not have hyperlink")
	}
}

func TestSourceCell_DeprecatedNoTarget(t *testing.T) {
	r := UpstreamResult{
		Package: "dev-util/foo",
		Status:  StatusDeprecated,
		Source:  SourceInfo{Label: "deprecated"},
		Target:  "",
	}
	cell := SourceCell(r, true)
	if cell != "deprecated" {
		t.Errorf("Deprecated without target; got: %q, want 'deprecated'", cell)
	}
}

// ---------------------------------------------------------------------------
// TestHyperlinksEnabled — environment gate
// ---------------------------------------------------------------------------
func TestHyperlinksEnabled(t *testing.T) {
	t.Setenv("LARRY_COLOR", "always")
	if !HyperlinksEnabled() {
		t.Error("LARRY_COLOR=always → HyperlinksEnabled() should be true")
	}
	t.Setenv("LARRY_COLOR", "never")
	if HyperlinksEnabled() {
		t.Error("LARRY_COLOR=never → HyperlinksEnabled() should be false")
	}
}

// ---------------------------------------------------------------------------
// TestRenderTable_ErrorPropagates — D5
// A failing writer must surface the error.
// ---------------------------------------------------------------------------
func TestRenderTable_ErrorPropagates(t *testing.T) {
	err := RenderTable(failWriter{}, []UpstreamResult{{Package: "a/b"}}, false)
	if err == nil {
		t.Fatal("expected error from failing writer, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error should contain 'boom'; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Golden file test — all providers, all statuses, includes depth-1 row
// ---------------------------------------------------------------------------
var updateGolden = flag.Bool("update", false, "update golden files")

func TestRenderTable_AllProviders(t *testing.T) {
	results := []UpstreamResult{
		{Package: "dev-util/alpha", Current: "1.0.0", Latest: "1.1.0", Status: StatusOutdated, Source: SourceInfo{Label: "GitHub:acme/alpha", URL: "https://github.com/acme/alpha", Glyph: string(dev.Github)}, Depth: 0},
		{Package: "dev-util/beta", Current: "2.0.0", Latest: "2.0.0", Status: StatusUpToDate, Source: SourceInfo{Label: "GitLab:gitlab.gnome.org/g/beta", URL: "https://gitlab.gnome.org/g/beta", Glyph: string(dev.Gitlab)}, Depth: 1},
		{Package: "dev-util/gamma", Current: "3.0.0", Latest: "2.9.0", Status: StatusAhead, Source: SourceInfo{Label: "Codeberg:acme/gamma", URL: "https://codeberg.org/acme/gamma", Glyph: "\uf330"}, Depth: 0},
		{Package: "dev-util/delta", Current: "4.0.0", Latest: "4.1.0", Status: StatusNewerTag, Source: SourceInfo{Label: "SourceHut:git.sr.ht/~u/delta", URL: "https://git.sr.ht/~u/delta", Glyph: "\ue702"}, Depth: 0},
		{Package: "dev-python/epsilon", Current: "5.0.0", Latest: "5.0.0", Status: StatusLive, Source: SourceInfo{Label: "PyPI:epsilon", URL: "https://pypi.org/project/epsilon/", Glyph: "\ue73c"}, Depth: 0},
		{Package: "dev-util/zeta", Current: "9999 (live)", Latest: "6.0.0", Status: StatusHasRelease, Source: SourceInfo{Label: "npm:@acme/zeta", URL: "https://www.npmjs.com/package/@acme/zeta", Glyph: string(dev.Npm)}, Depth: 0},
		{Package: "dev-util/eta", Current: "7.0.0", Latest: "nightly", Status: StatusCheckTag, Source: SourceInfo{Label: "GitHub:acme/eta", URL: "https://github.com/acme/eta", Glyph: string(dev.Github)}, Depth: 0},
		{Package: "dev-util/theta", Current: "8.0.0", Latest: "", Status: StatusDeprecated, Source: SourceInfo{Label: "deprecated"}, Target: "dev-util/iota", Depth: 0},
		{Package: "dev-util/kappa", Current: "9.0.0", Latest: "", Status: StatusUnknown, Source: SourceInfo{Label: "", URL: "", Glyph: ""}, Error: "no provider match", Depth: 0},
	}

	t.Setenv("LARRY_COLOR", "never")
	var buf bytes.Buffer
	if err := RenderTable(&buf, results, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	goldenPath := filepath.Join("testdata", "table.golden")

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote golden file %s", goldenPath)
		return
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	// Normalise trailing whitespace for stable comparison.
	normalize := func(s string) string {
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			lines[i] = strings.TrimRight(l, " \t")
		}
		return strings.Join(lines, "\n")
	}

	got := normalize(out)
	want := normalize(string(golden))
	if got != want {
		// Write diff to a temp file for debugging.
		tmp := filepath.Join(t.TempDir(), "diff.golden")
		os.WriteFile(tmp, []byte(got), 0o644)
		cmd := exec.Command("diff", "-u", string(goldenPath), tmp)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("golden mismatch (diff exit=%v):\n%s", err, out)
		} else {
			t.Errorf("golden mismatch:\n%s", out)
		}
	}
}

// ---------------------------------------------------------------------------
// Legacy tests — updated to use SourceInfo
// ---------------------------------------------------------------------------
func TestRenderTable_Outdated(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "dev-python/requests", Current: "1.0.0", Latest: "2.0.0", Status: StatusOutdated, Source: SourceInfo{URL: "https://pypi.org/project/requests/"}},
	}
	if err := RenderTable(&buf, results, false); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "dev-python/requests") {
		t.Fatalf("output should contain package name, got: %s", output)
	}
	if !strings.Contains(output, "1.0.0") {
		t.Fatalf("output should contain current version, got: %s", output)
	}
	if !strings.Contains(output, "2.0.0") {
		t.Fatalf("output should contain latest version, got: %s", output)
	}
}

func TestRenderTable_UpToDate(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "dev-python/requests", Current: "2.0.0", Latest: "2.0.0", Status: StatusUpToDate},
	}
	if err := RenderTable(&buf, results, false); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "dev-python/requests") {
		t.Fatalf("output should contain package name, got: %s", output)
	}
}

func TestRenderTable_Ahead(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "dev-python/requests", Current: "3.0.0", Latest: "2.0.0", Status: StatusAhead},
	}
	if err := RenderTable(&buf, results, false); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "dev-python/requests") {
		t.Fatalf("output should contain package name, got: %s", output)
	}
}

func TestRenderTable_Live(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "dev-python/requests", Current: "9999", Latest: "", Status: StatusLive},
	}
	if err := RenderTable(&buf, results, false); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "dev-python/requests") {
		t.Fatalf("output should contain package name, got: %s", output)
	}
}

func TestRenderTable_Unknown(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "dev-python/requests", Current: "1.0.0", Latest: "", Status: StatusUnknown},
	}
	if err := RenderTable(&buf, results, false); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "dev-python/requests") {
		t.Fatalf("output should contain package name, got: %s", output)
	}
}

func TestRenderTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{}
	if err := RenderTable(&buf, results, false); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if output == "" {
		t.Fatal("output should not be empty")
	}
}

func TestRenderTable_MultipleResults(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "dev-python/requests", Current: "1.0.0", Latest: "2.0.0", Status: StatusOutdated},
		{Package: "dev-python/flask", Current: "2.0.0", Latest: "2.0.0", Status: StatusUpToDate},
		{Package: "dev-python/django", Current: "5.0.0", Latest: "4.0.0", Status: StatusAhead},
	}
	if err := RenderTable(&buf, results, false); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "dev-python/requests") {
		t.Fatal("output should contain dev-python/requests")
	}
	if !strings.Contains(output, "dev-python/flask") {
		t.Fatal("output should contain dev-python/flask")
	}
	if !strings.Contains(output, "dev-python/django") {
		t.Fatal("output should contain dev-python/django")
	}
}

func TestStatusString(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusOutdated, "OUTDATED"},
		{StatusUpToDate, "UP-TO-DATE"},
		{StatusAhead, "AHEAD"},
		{StatusLive, "LIVE"},
		{StatusUnknown, "UNKNOWN"},
		{StatusCheckTag, "CHECK-TAG"},
	}
	for _, tt := range tests {
		got := tt.status.String()
		if got != tt.want {
			t.Fatalf("status %d = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestStatusColor_CheckTag(t *testing.T) {
	t.Setenv("LARRY_COLOR", "never")
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "dev-foo/bar", Current: "1.0", Latest: "nightly", Status: StatusCheckTag, Source: SourceInfo{URL: "https://example.com"}},
	}
	if err := RenderTable(&buf, results, true); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "\x1b[") {
		t.Errorf("RenderTable with LARRY_COLOR=never emitted CSI; output = %q", out)
	}
	if !strings.Contains(out, "CHECK-TAG") {
		t.Errorf("RenderTable did not render CHECK-TAG; output = %q", out)
	}

	t.Setenv("LARRY_COLOR", "always")
	var buf2 bytes.Buffer
	if err := RenderTable(&buf2, results, true); err != nil {
		t.Fatal(err)
	}
	out2 := buf2.String()
	if !strings.Contains(out2, "CHECK-TAG") {
		t.Errorf("RenderTable with LARRY_COLOR=always did not render CHECK-TAG; output = %q", out2)
	}
}

func TestCheckTag_IsSignificant(t *testing.T) {
	if !StatusCheckTag.IsSignificant() {
		t.Error("StatusCheckTag.IsSignificant() should be true")
	}
}

func TestRenderTable_Indentation(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "p/a", Current: "1.0", Latest: "1.0", Status: StatusUpToDate, Depth: 0},
		{Package: "p/b", Current: "1.0", Latest: "1.0", Status: StatusUpToDate, Depth: 1},
		{Package: "p/c", Current: "1.0", Latest: "1.0", Status: StatusUpToDate, Depth: 2},
	}
	t.Setenv("LARRY_COLOR", "never")
	if err := RenderTable(&buf, results, false); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "p/a") {
		t.Errorf("expected 'p/a' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "  p/b") {
		t.Errorf("expected '  p/b' (indented) in output, got:\n%s", output)
	}
	if !strings.Contains(output, "    p/c") {
		t.Errorf("expected '    p/c' (double-indented) in output, got:\n%s", output)
	}
}
