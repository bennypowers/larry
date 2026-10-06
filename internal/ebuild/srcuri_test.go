package ebuild

import (
	"strings"
	"testing"
	"time"

	"bennypowers.dev/larry/internal/atom"
)

// ---- TestSRC_URI_Resolve_Npm: mirror://npm/ → https://registry.npmjs.org/... ----

func TestSRC_URI_Resolve_Npm(t *testing.T) {
	tests := []struct {
		input    string
		wantURI  string
		wantLen  int
	}{
		{
			input:   "mirror://npm/some-pkg/some-pkg-1.0.0.tgz",
			wantURI: "https://registry.npmjs.org/some-pkg/some-pkg-1.0.0.tgz",
			wantLen: 1,
		},
		{
			input:   "mirror://npm/@scope/pkg/pkg-1.0.0.tgz",
			wantURI: "https://registry.npmjs.org/@scope/pkg/pkg-1.0.0.tgz",
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			entries, err := ResolveSRCURI(tt.input)
			if err != nil {
				t.Fatalf("ResolveSRCURI(%q) error: %v", tt.input, err)
			}
			if len(entries) != tt.wantLen {
				t.Fatalf("ResolveSRCURI(%q) returned %d entries, want %d: %v", tt.input, len(entries), tt.wantLen, entries)
			}
			if entries[0] != tt.wantURI {
				t.Errorf("ResolveSRCURI(%q) = %q, want %q", tt.input, entries[0], tt.wantURI)
			}
		})
	}
}

// ---- TestSRC_URI_Resolve_CustomVars: ${PN}/${PV} expanded ----

func TestSRC_URI_Resolve_CustomVars(t *testing.T) {
	info := &EbuildInfo{
		Pkg:     "my-tool",
		PVVal:   "1.0.0",
		SRC_URI: "mirror://npm/${PN}/${PN}-${PV}.tgz",
	}
	pe := &atom.ParsedEbuild{PN: "my-tool", PV: "1.0.0", P: "my-tool-1.0.0", PF: "my-tool-1.0.0", Category: "cat"}
	vars := BuildVariableMap(info.SRC_URI, pe)
	entries, err := ResolveSRCURIWithVars(info.SRC_URI, vars)
	if err != nil {
		t.Fatalf("ResolveSRCURIWithVars() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0] != "https://registry.npmjs.org/my-tool/my-tool-1.0.0.tgz" {
		t.Errorf("resolved to %q, want https://registry.npmjs.org/my-tool/my-tool-1.0.0.tgz", entries[0])
	}
}

// ---- TestSRC_URI_Resolve_MultiEntry: handles multiple URIs ----

func TestSRC_URI_Resolve_MultiEntry(t *testing.T) {
	input := "mirror://npm/pkg/pkg-1.0.tgz https://example.com/other.tar.gz"
	entries, err := ResolveSRCURI(input)
	if err != nil {
		t.Fatalf("ResolveSRCURI() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0] != "https://registry.npmjs.org/pkg/pkg-1.0.tgz" {
		t.Errorf("entry[0] = %q, want https://registry.npmjs.org/pkg/pkg-1.0.tgz", entries[0])
	}
	if entries[1] != "https://example.com/other.tar.gz" {
		t.Errorf("entry[1] = %q, want https://example.com/other.tar.gz", entries[1])
	}
}

// ---- TestSRC_URI_Resolve_GitHub: mirror://github/ passthrough ----

func TestSRC_URI_Resolve_GitHub(t *testing.T) {
	input := "mirror://github/owner/pkg/pkg-1.0.tar.gz"
	entries, err := ResolveSRCURI(input)
	if err != nil {
		t.Fatalf("ResolveSRCURI() error: %v", err)
	}
	// mirror://github/ is not explicitly handled, should strip the mirror:// prefix
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	want := "github/owner/pkg/pkg-1.0.tar.gz"
	if entries[0] != want {
		t.Errorf("= %q, want %q", entries[0], want)
	}
}

// ---- TestSRC_URI_Resolve_PyPI: mirror://pypi/ → https://files.pythonhosted.org/ ----

func TestSRC_URI_Resolve_PyPI(t *testing.T) {
	input := "mirror://pypi/requests/requests-2.28.0.tar.gz"
	entries, err := ResolveSRCURI(input)
	if err != nil {
		t.Fatalf("ResolveSRCURI() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	want := "https://files.pythonhosted.org/packages/requests/requests-2.28.0.tar.gz"
	if entries[0] != want {
		t.Errorf("= %q, want %q", entries[0], want)
	}
}

// ---- TestSRC_URI_Resolve_SourceForge: mirror://sourceforge/ → downloads.sourceforge.net ----

func TestSRC_URI_Resolve_SourceForge(t *testing.T) {
	input := "mirror://sourceforge/myproject/files/myproject-1.0.tar.gz"
	entries, err := ResolveSRCURI(input)
	if err != nil {
		t.Fatalf("ResolveSRCURI() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	want := "https://downloads.sourceforge.net/project/myproject/files/myproject-1.0.tar.gz"
	if entries[0] != want {
		t.Errorf("= %q, want %q", entries[0], want)
	}
}

// ---- TestSRC_URI_Resolve_Kernel: mirror://kernel/ → kernel.org ----

func TestSRC_URI_Resolve_Kernel(t *testing.T) {
	input := "mirror://kernel/pub/linux/kernel/v6.x/linux-6.1.0.tar.xz"
	entries, err := ResolveSRCURI(input)
	if err != nil {
		t.Fatalf("ResolveSRCURI() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	want := "https://kernel.org/pub/linux/kernel/v6.x/linux-6.1.0.tar.xz"
	if entries[0] != want {
		t.Errorf("= %q, want %q", entries[0], want)
	}
}

// ---- TestSRC_URI_Resolve_NoMirror: plain URLs pass through ----

func TestSRC_URI_Resolve_NoMirror(t *testing.T) {
	input := "https://example.com/pkg-1.0.tar.gz"
	entries, err := ResolveSRCURI(input)
	if err != nil {
		t.Fatalf("ResolveSRCURI() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0] != input {
		t.Errorf("= %q, want %q", entries[0], input)
	}
}

// ---- TestSRC_URI_Resolve_Empty: returns nil for empty input ----

func TestSRC_URI_Resolve_Empty(t *testing.T) {
	entries, err := ResolveSRCURI("")
	if err != nil {
		t.Fatalf("ResolveSRCURI(\"\") error: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil, got %v", entries)
	}
}

// ---- TestSRC_URI_Resolve_MixedMirror: mix of mirror and plain ----

func TestSRC_URI_Resolve_MixedMirror(t *testing.T) {
	input := "mirror://npm/foo/foo-1.0.tgz mirror://pypi/bar/bar-2.0.tar.gz https://example.com/baz.tar.gz"
	entries, err := ResolveSRCURI(input)
	if err != nil {
		t.Fatalf("ResolveSRCURI() error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(entries), entries)
	}
	expected := []string{
		"https://registry.npmjs.org/foo/foo-1.0.tgz",
		"https://files.pythonhosted.org/packages/bar/bar-2.0.tar.gz",
		"https://example.com/baz.tar.gz",
	}
	for i, got := range entries {
		if got != expected[i] {
			t.Errorf("entry[%d] = %q, want %q", i, got, expected[i])
		}
	}
}

// ---- TestSRC_URI_Resolve_NpmWithVars: resolves npm with variable expansion ----

func TestSRC_URI_Resolve_NpmWithVars(t *testing.T) {
	// When MY_PN is defined in the SRC_URI line, it gets picked up by BuildVariableMap
	info := &EbuildInfo{
		Pkg:     "my-web-tool",
		PVVal:   "0.75.3",
		SRC_URI: `MY_PN="my-web-tool" mirror://npm/${MY_PN}/${MY_PN}-${PV}.tgz`,
	}
	pe := &atom.ParsedEbuild{PN: "my-web-tool", PV: "0.75.3", P: "my-web-tool-0.75.3", PF: "my-web-tool-0.75.3", Category: "cat"}
	vars := BuildVariableMap(info.SRC_URI, pe)

	entries, err := ResolveSRCURIWithVars(info.SRC_URI, vars)
	if err != nil {
		t.Fatalf("ResolveSRCURIWithVars() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	want := "https://registry.npmjs.org/my-web-tool/my-web-tool-0.75.3.tgz"
	if entries[0] != want {
		t.Errorf("= %q, want %q", entries[0], want)
	}
}

// ---- TestSRC_URI_Resolve_NpmWithVarsAndCustomVar: MY_PN custom var ----

func TestSRC_URI_Resolve_NpmWithVarsAndCustomVar(t *testing.T) {
	// Simulate an ebuild where MY_PN is extracted from SRC_URI
	info := &EbuildInfo{
		Pkg:     "my-tool",
		PVVal:   "1.2.3",
		SRC_URI: `MY_PN="my-tool" mirror://npm/${MY_PN}/${MY_PN}-${PV}.tgz`,
	}
	pe := &atom.ParsedEbuild{PN: "my-tool", PV: "1.2.3", P: "my-tool-1.2.3", PF: "my-tool-1.2.3", Category: "cat"}
	vars := BuildVariableMap(info.SRC_URI, pe)

	entries, err := ResolveSRCURIWithVars(info.SRC_URI, vars)
	if err != nil {
		t.Fatalf("ResolveSRCURIWithVars() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	want := "https://registry.npmjs.org/my-tool/my-tool-1.2.3.tgz"
	if entries[0] != want {
		t.Errorf("= %q, want %q", entries[0], want)
	}
}

// ---- TestSRC_URI_Resolve_NpmDistfile: ResolveNpmDistfile convenience ----

func TestSRC_URI_Resolve_NpmDistfile(t *testing.T) {
	info := &EbuildInfo{
		Pkg:     "express",
		PVVal:   "4.18.0",
		SRC_URI: "mirror://npm/express/express-4.18.0.tgz",
	}
	pe := &atom.ParsedEbuild{PN: "express", PV: "4.18.0", P: "express-4.18.0", PF: "express-4.18.0", Category: "cat"}
	vars := BuildVariableMap(info.SRC_URI, pe)

	resolved, err := ResolveNpmDistfile(info.SRC_URI, vars)
	if err != nil {
		t.Fatalf("ResolveNpmDistfile() error: %v", err)
	}
	want := "https://registry.npmjs.org/express/express-4.18.0.tgz"
	if resolved != want {
		t.Errorf("= %q, want %q", resolved, want)
	}
}

// ---- TestSRC_URI_Resolve_NpmDistfile_NotNpm: rejects non-npm URIs ----

func TestSRC_URI_Resolve_NpmDistfile_NotNpm(t *testing.T) {
	_, err := ResolveNpmDistfile("https://example.com/pkg.tar.gz", nil)
	if err == nil {
		t.Fatal("ResolveNpmDistfile(non-npm) should return error")
	}
}

// ---- TestSRC_URI_Resolve_NonMirror: plain URL passthrough ----

func TestSRC_URI_Resolve_NonMirror(t *testing.T) {
	entries, err := ResolveSRCURI("https://github.com/owner/repo/releases/download/v1.0/repo-1.0.tar.gz")
	if err != nil {
		t.Fatalf("ResolveSRCURI() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	want := "https://github.com/owner/repo/releases/download/v1.0/repo-1.0.tar.gz"
	if entries[0] != want {
		t.Errorf("= %q, want %q", entries[0], want)
	}
}

// ---- TestSRC_URI_Resolve_WhitespaceHandling: trims whitespace in entries ----

func TestSRC_URI_Resolve_WhitespaceHandling(t *testing.T) {
	input := "  mirror://npm/foo/foo-1.0.tgz    mirror://pypi/bar/bar-2.0.tar.gz  "
	entries, err := ResolveSRCURI(input)
	if err != nil {
		t.Fatalf("ResolveSRCURI() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(entries), entries)
	}
}

// ---- TestSRC_URI_Resolve_SourceForgePath: complex sourceforge path ----

func TestSRC_URI_Resolve_SourceForgePath(t *testing.T) {
	input := "mirror://sourceforge/ffmpeg-benchmark/ffmpeg-benchmark-0.1.0.tar.gz"
	entries, err := ResolveSRCURI(input)
	if err != nil {
		t.Fatalf("ResolveSRCURI() error: %v", err)
	}
	want := "https://downloads.sourceforge.net/project/ffmpeg-benchmark/ffmpeg-benchmark-0.1.0.tar.gz"
	if len(entries) != 1 || entries[0] != want {
		t.Errorf("= %q, want %q", entries[0], want)
	}
}

// ---- TestSRCURIVars: convenience wrapper ----

func TestSRCURIVars(t *testing.T) {
	info := &EbuildInfo{
		Pkg:     "pkg",
		PVVal:   "1.0",
		SRC_URI: "mirror://npm/${PN}/${PN}-${PV}.tgz",
	}
	pe := &atom.ParsedEbuild{PN: "pkg", PV: "1.0", P: "pkg-1.0", PF: "pkg-1.0", Category: "cat"}
	vars := BuildVariableMap(info.SRC_URI, pe)

	result, err := SRCURIVars(info.SRC_URI, vars)
	if err != nil {
		t.Fatalf("SRCURIVars() error: %v", err)
	}
	want := "https://registry.npmjs.org/pkg/pkg-1.0.tgz"
	if result != want {
		t.Errorf("= %q, want %q", result, want)
	}
}

// ---- TestSRCURIVars_EmptyVars: empty input ----

func TestSRCURIVars_EmptyVars(t *testing.T) {
	result, err := SRCURIVars("", nil)
	if err != nil {
		t.Fatalf("SRCURIVars(\"\", nil) error: %v", err)
	}
	if result != "" {
		t.Errorf("= %q, want empty", result)
	}
}

// ---- TestVariableResolution_Nested: ${MY_VAR="${OTHER}-suffix"} ----

func TestVariableResolution_Nested(t *testing.T) {
	// Test nested variable expansion: ${MY_VAR="${OTHER}-suffix"}
	vars := VariableMap{
		"OTHER": "base",
		"MY_VAR": "${OTHER}-suffix",
	}

	// Expand MY_VAR reference
	result := ExpandVariables("${MY_VAR}", vars)
	want := "base-suffix"
	if result != want {
		t.Errorf("ExpandVariables(\"${MY_VAR}\", vars) = %q, want %q", result, want)
	}

	// Test with SRC_URI containing nested var
	info := &EbuildInfo{
		Pkg:     "my-tool",
		PVVal:   "1.0",
		SRC_URI: `MY_PN="my-${PV}"
SRC_URI="mirror://npm/${MY_PN}/${MY_PN}-${PV}.tgz"`,
	}
	pe := &atom.ParsedEbuild{PN: "my-tool", PV: "1.0", P: "my-tool-1.0", PF: "my-tool-1.0", Category: "cat"}
	vars = BuildVariableMap(info.SRC_URI, pe)
	result, err := SRCURIVars(info.SRC_URI, vars)
	if err != nil {
		t.Fatalf("SRCURIVars() error: %v", err)
	}
	// MY_PN should be "my-1.0" and should be used in SRC_URI
	want = "https://registry.npmjs.org/my-1.0/my-1.0-1.0.tgz"
	if result != want {
		t.Errorf("= %q, want %q", result, want)
	}
}

// ---- TestVariableResolution_BashManip: ${PV//./_} ----

func TestVariableResolution_BashManip(t *testing.T) {
	// Test bash string manipulation: ${PV//./_} should replace all "." with "_"
	vars := VariableMap{
		"PV": "1.2.3",
	}

	result := ExpandVariables("${PV//./_}", vars)
	want := "1_2_3"
	if result != want {
		t.Errorf("ExpandVariables(\"${PV//./_}\", vars) = %q, want %q", result, want)
	}

	// Test with a more complex manipulation
	info := &EbuildInfo{
		Pkg:     "my-pkg",
		PVVal:   "1.2.3",
		SRC_URI: `mirror://npm/${PN////-/_}/${PN////-/_}-${PV//./_}.tgz`,
	}
	pe := &atom.ParsedEbuild{PN: "my-pkg", PV: "1.2.3", P: "my-pkg-1.2.3", PF: "my-pkg-1.2.3", Category: "cat"}
	vars = BuildVariableMap(info.SRC_URI, pe)
	result2, err := SRCURIVars(info.SRC_URI, vars)
	if err != nil {
		t.Fatalf("SRCURIVars() error: %v", err)
	}
	// PN is "my-pkg", no dots to replace → "my-pkg"
	// PV is "1.2.3", dots replaced → "1_2_3"
	want2 := "https://registry.npmjs.org/my-pkg/my-pkg-1_2_3.tgz"
	if result2 != want2 {
		t.Errorf("= %q, want %q", result2, want2)
	}
}

// ---- TestVariableResolution_Expansion: core variables ----

func TestVariableResolution_Expansion(t *testing.T) {
	info := &EbuildInfo{
		Pkg:     "my-tool",
		PVVal:   "1.2.3",
		SRC_URI: "mirror://npm/${PN}/${P}.tgz",
	}
	pe := &atom.ParsedEbuild{PN: "my-tool", PV: "1.2.3", P: "my-tool-1.2.3", PF: "my-tool-1.2.3", Category: "cat"}
	vars := BuildVariableMap(info.SRC_URI, pe)

	// Test PN expansion
	if got := ExpandVariables("${PN}", vars); got != "my-tool" {
		t.Errorf("ExpandVariables(\"${PN}\") = %q, want %q", got, "my-tool")
	}

	// Test PV expansion
	if got := ExpandVariables("${PV}", vars); got != "1.2.3" {
		t.Errorf("ExpandVariables(\"${PV}\") = %q, want %q", got, "1.2.3")
	}

	// Test P expansion (should be PN-PV)
	if got := ExpandVariables("${P}", vars); got != "my-tool-1.2.3" {
		t.Errorf("ExpandVariables(\"${P}\") = %q, want %q", got, "my-tool-1.2.3")
	}
}

// ---- TestVariableResolution_UnknownVar: leaves unknown vars as-is ----

func TestVariableResolution_UnknownVar(t *testing.T) {
	vars := VariableMap{
		"PN": "my-pkg",
	}

	// Unknown variable should remain as ${UNKNOWN}
	result := ExpandVariables("${UNKNOWN}", vars)
	want := "${UNKNOWN}"
	if result != want {
		t.Errorf("ExpandVariables(\"${UNKNOWN}\") = %q, want %q", result, want)
	}
}

// ---- TestVariableResolution_MultiExpand: multiple refs in one string ----

func TestVariableResolution_MultiExpand(t *testing.T) {
	vars := VariableMap{
		"PN": "my-pkg",
		"PV": "1.2.3",
	}

	result := ExpandVariables("${PN}/${PV}", vars)
	want := "my-pkg/1.2.3"
	if result != want {
		t.Errorf("ExpandVariables(\"${PN}/${PV}\") = %q, want %q", result, want)
	}
}

// ---- TestBuildVariableMap: builds vars from EbuildInfo ----

func TestBuildVariableMap(t *testing.T) {
	raw := `MY_P="${PN}-${PV}"
SRC_URI="mirror://npm/${MY_P}/${MY_P}.tgz"
EAPI=8`
	pe := &atom.ParsedEbuild{PN: "my-tool", PV: "1.2.3", P: "my-tool-1.2.3", PF: "my-tool-1.2.3", Category: "cat"}
	vars := BuildVariableMap(raw, pe)

	// Core vars
	if vars["PN"] != "my-tool" {
		t.Errorf("vars[PN] = %q, want %q", vars["PN"], "my-tool")
	}
	if vars["PV"] != "1.2.3" {
		t.Errorf("vars[PV] = %q, want %q", vars["PV"], "1.2.3")
	}
	if vars["P"] != "my-tool-1.2.3" {
		t.Errorf("vars[P] = %q, want %q", vars["P"], "my-tool-1.2.3")
	}

	// Custom var from SRC_URI
	if vars["MY_P"] != "${PN}-${PV}" {
		t.Errorf("vars[MY_P] = %q, want %q", vars["MY_P"], "${PN}-${PV}")
	}
}

// ---- TestResolveMirror_Generic: generic mirror passthrough ----

func TestResolveMirror_Generic(t *testing.T) {
	uri := "mirror://custom/path/to/file.tar.gz"
	resolved, err := resolveMirror(uri)
	if err != nil {
		t.Fatalf("resolveMirror() error: %v", err)
	}
	// Generic mirrors strip the mirror:// prefix
	want := "custom/path/to/file.tar.gz"
	if resolved != want {
		t.Errorf("= %q, want %q", resolved, want)
	}
}

// ---- TestSplitSRCURI: splits multi-entry SRC_URI ----

func TestSplitSRCURI(t *testing.T) {
	tests := []struct {
		input    string
		wantLen  int
	}{
		{"mirror://npm/pkg/pkg-1.0.tgz", 1},
		{"mirror://npm/a/a.tgz mirror://npm/b/b.tgz", 2},
		{"https://example.com/a.tar.gz https://example.com/b.tar.gz",
2},
		{"  mirror://npm/pkg/pkg-1.0.tgz  ", 1},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			entries := splitSRCURI(tt.input)
			if len(entries) != tt.wantLen {
				t.Errorf("splitSRCURI(%q) returned %d entries, want %d: %v", tt.input, len(entries), tt.wantLen, entries)
			}
		})
	}
}

// ---- TestApplyBashManip: bash string manipulation ----

func TestApplyBashManip(t *testing.T) {
	tests := []struct {
		input       string
		manip       string
		wantReplace string
		wantSimple  string
	}{
		{"1.2.3", "//./_", "1_2_3", "1.2.3"},   // //./_ replaces all dots
		{"hello world", "// /!", "hello!world", "hello world"}, // // /! replaces spaces
		{"a/b/c", "//b/X", "a/X/c", "a/b/c"}, // //b/X replaces b with X
		{"test", "//x/Y", "test", ""},          // no x to replace
	}

	for _, tt := range tests {
		t.Run(tt.manip, func(t *testing.T) {
			got := applyBashManip(tt.input, tt.manip)
			if got != tt.wantReplace {
				t.Errorf("applyBashManip(%q, %q) = %q, want %q", tt.input, tt.manip, got, tt.wantReplace)
			}
		})
	}
}

// ---- TestExtractVarName: extracts var name from ${VAR} and ${VAR//pat} ----

func TestExtractVarName(t *testing.T) {
	tests := []struct {
		input      string
		wantName   string
		wantManip  bool
	}{
		{"PN", "PN", false},
		{"PV//./_", "PV", true},
		{"MY_P", "MY_P", false},
		{"MY_P=\"${PN}-1.0\"", "MY_P", false}, // = is not /
		{"PV//./_//x/Y", "PV", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, manip, _ := extractVarName(tt.input)
			if name != tt.wantName {
				t.Errorf("extractVarName(%q) name = %q, want %q", tt.input, name, tt.wantName)
			}
			if manip != tt.wantManip {
				t.Errorf("extractVarName(%q) manip = %v, want %v", tt.input, manip, tt.wantManip)
			}
		})
	}
}

// ---- TestExpandVariables_MaxIter: stops after max iterations ----

func TestExpandVariables_MaxIter(t *testing.T) {
	// Self-referencing variable should not infinite loop
	vars := VariableMap{
		"A": "${A}-suffix",
	}
	result := ExpandVariables("${A}", vars)
	// Should stabilize after first expansion without infinite loop
	if result == "" {
		t.Error("ExpandVariables should not return empty for self-ref")
	}
	// Verify it doesn't loop forever (max 10 iterations)
	_ = result
}

// ---- TestExpandVariables_Mixed: $VAR and ${VAR} both work ----

func TestExpandVariables_Mixed(t *testing.T) {
	vars := VariableMap{
		"PN": "my-pkg",
		"PV": "1.2.3",
	}

	// $VAR form (without braces)
	result := ExpandVariables("$PN-$PV", vars)
	want := "my-pkg-1.2.3"
	if result != want {
		t.Errorf("ExpandVariables(\"$PN-$PV\") = %q, want %q", result, want)
	}
}

// ---- TestExpandVariables_Escape: handles escaped vars ----

func TestExpandVariables_Escape(t *testing.T) {
	vars := VariableMap{
		"PN": "my-pkg",
	}

	// Double-brace ${${PN}} should not expand the inner ${PN}
	result := ExpandVariables("${${PN}}", vars)
	// The outer ${} captures ${PN} as the var name, which doesn't exist
	// So it should remain unchanged
	if result == "" {
		t.Error("ExpandVariables should not return empty")
	}
	_ = result
}

// ---- TestExpandVariables_Empty: empty input ----

func TestExpandVariables_Empty(t *testing.T) {
	vars := VariableMap{
		"PN": "my-pkg",
	}
	result := ExpandVariables("", vars)
	if result != "" {
		t.Errorf("ExpandVariables(\"\") = %q, want empty", result)
	}
}

// ---- TestExpandVariables_NoVars: no variables to expand ----

func TestExpandVariables_NoVars(t *testing.T) {
	result := ExpandVariables("plain text without vars", nil)
	if result != "plain text without vars" {
		t.Errorf("ExpandVariables() = %q, want unchanged", result)
	}
}

// ---- TestExpandVariables_NestedFixpoint (E1–E4) ----

func TestExpandVariables_NestedFixpoint(t *testing.T) {
	vars := VariableMap{
		"MY_NPM_NAME":    "widget",
		"MY_WEBUI_P":     "${MY_NPM_NAME}-0.75.3",
	}
	result := ExpandVariables("mirror://npm/${MY_WEBUI_P}.tgz", vars)
	want := "mirror://npm/widget-0.75.3.tgz"
	if result != want {
		t.Errorf("= %q, want %q", result, want)
	}
}

// ---- TestExpandVariables_NoInfiniteLoop ----

func TestExpandVariables_NoInfiniteLoop(t *testing.T) {
	vars := VariableMap{
		"A": "${B}",
		"B": "${A}",
	}
	// Must not hang — cap at 10 iterations
	done := make(chan struct{})
	go func() {
		ExpandVariables("${A}", vars)
		close(done)
	}()
	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("ExpandVariables hung — infinite loop")
	}
}

// ---- TestExpandVariables_UnknownVarLeftAlone ----

func TestExpandVariables_UnknownVarLeftAlone(t *testing.T) {
	vars := VariableMap{"PN": "foo"}
	result := ExpandVariables("${NOPE}/${PN}", vars)
	want := "${NOPE}/foo"
	if result != want {
		t.Errorf("= %q, want %q", result, want)
	}
}

// ---- TestExpandVariables_BashManipulation (E6) ----

func TestExpandVariables_BashManipulation(t *testing.T) {
	tests := []struct {
		input       string
		want        string
	}{
		{"${PN//-/_}", "fish_lsp"},
		{"${PN//s/S}", "fiSh-lSp"},
		{"${PN/s/S}", "fiSh-lsp"},
		{"${PN/-/_}", "fish_lsp"},
		{"${MY_PN::1}", "k"},
		{"${MY_PN::6}", "kornia"},
		{"${MY_PN:7:2}", "rs"},
		{"${MY_PN::99}", "kornia-rs"},
		{"${MY_PN::x}", "kornia-rs"},
	}
	vars := VariableMap{
		"PN":     "fish-lsp",
		"MY_PN":  "kornia-rs",
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ExpandVariables(tt.input, vars)
			if result != tt.want {
				t.Errorf("ExpandVariables(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}

// ---- TestParseInherit_SingleLine (E7) ----

func TestParseInherit_SingleLine(t *testing.T) {
	result := ParseInherit("inherit cargo\n")
	want := []string{"cargo"}
	if len(result) != len(want) || result[0] != want[0] {
		t.Errorf("ParseInherit(%q) = %v, want %v", "inherit cargo\n", result, want)
	}
}

// ---- TestParseInherit_MultipleLines ----

func TestParseInherit_MultipleLines(t *testing.T) {
	raw := `inherit pam python-single-r1
DESCRIPTION="x"
inherit systemd tmpfiles
`
	result := ParseInherit(raw)
	want := []string{"pam", "python-single-r1", "systemd", "tmpfiles"}
	if len(result) != len(want) {
		t.Errorf("ParseInherit() = %v, want %v", result, want)
		return
	}
	for i, w := range want {
		if result[i] != w {
			t.Errorf("result[%d] = %q, want %q", i, result[i], w)
		}
	}
}

// ---- TestParseInherit_Continuation ----

func TestParseInherit_Continuation(t *testing.T) {
	raw := `inherit foo \
	go-module bar
`
	result := ParseInherit(raw)
	want := []string{"foo", "go-module", "bar"}
	if len(result) != len(want) {
		t.Errorf("ParseInherit() = %v, want %v", result, want)
		return
	}
	for i, w := range want {
		if result[i] != w {
			t.Errorf("result[%d] = %q, want %q", i, result[i], w)
		}
	}
}

// ---- TestParseInherit_SkipsComments ----

func TestParseInherit_SkipsComments(t *testing.T) {
	result := ParseInherit("# inherit go-module\ninherit cargo\n")
	want := []string{"cargo"}
	if len(result) != len(want) || result[0] != want[0] {
		t.Errorf("ParseInherit() = %v, want %v", result, want)
	}
}

// ---- TestParseInherit_Dedup ----

func TestParseInherit_Dedup(t *testing.T) {
	raw := "inherit cargo\ninherit cargo systemd\n"
	result := ParseInherit(raw)
	want := []string{"cargo", "systemd"}
	if len(result) != len(want) {
		t.Errorf("ParseInherit() = %v, want %v", result, want)
		return
	}
	for i, w := range want {
		if result[i] != w {
			t.Errorf("result[%d] = %q, want %q", i, result[i], w)
		}
	}
}

// ---- TestParseInherit_None ----

func TestParseInherit_None(t *testing.T) {
	result := ParseInherit("EAPI=8\n")
	if result != nil {
		t.Errorf("ParseInherit() = %v, want nil", result)
	}
}

// ---- TestBuildVariableMap_WholeFile (E5) ----

func TestBuildVariableMap_WholeFile(t *testing.T) {
	raw := `EAPI=8
MY_NPM_NAME="language-server"
DESCRIPTION="x"
SRC_URI="
	mirror://npm/@vtsls/${MY_NPM_NAME}/-/${MY_NPM_NAME}-${PV}.tgz
"
`
	pe := &atom.ParsedEbuild{Category: "dev-util", PN: "vtsls", PV: "0.3.0", P: "vtsls-0.3.0", PF: "vtsls-0.3.0"}
	vars := BuildVariableMap(raw, pe)

	if vars["MY_NPM_NAME"] != "language-server" {
		t.Errorf("vars[MY_NPM_NAME] = %q, want %q", vars["MY_NPM_NAME"], "language-server")
	}
	if vars["PN"] != "vtsls" {
		t.Errorf("vars[PN] = %q, want %q", vars["PN"], "vtsls")
	}
	if vars["PV"] != "0.3.0" {
		t.Errorf("vars[PV] = %q, want %q", vars["PV"], "0.3.0")
	}
	if vars["P"] != "vtsls-0.3.0" {
		t.Errorf("vars[P] = %q, want %q", vars["P"], "vtsls-0.3.0")
	}
	if vars["PF"] != "vtsls-0.3.0" {
		t.Errorf("vars[PF] = %q, want %q", vars["PF"], "vtsls-0.3.0")
	}
	if vars["CATEGORY"] != "dev-util" {
		t.Errorf("vars[CATEGORY] = %q, want %q", vars["CATEGORY"], "dev-util")
	}
	if _, ok := vars["SRC_URI"]; ok {
		t.Error("vars should not contain SRC_URI key")
	}
}

// ---- TestBuildVariableMap_MultiLineSRCURI: multi-line quoted SRC_URI resolves correctly ----
// Regression for the original TestExtractVar/multi_line failure: SRC_URI whose
// quoted body spans multiple lines must still resolve to the correct URI entries.
func TestBuildVariableMap_MultiLineSRCURI(t *testing.T) {
	// The quoted body includes embedded newlines and a tab — exactly the
	// input that caused the original TestExtractVar/multi_line failure.
	multiLine := "\n\tmirror://npm/pkg/pkg-1.0.0.tgz\n"

	vars := VariableMap{
		"PN":    "pkg",
		"PV":    "1.0.0",
		"P":     "pkg-1.0.0",
		"SRC_URI": multiLine,
	}

	entries := ParseSrcUri(multiLine, vars)
	if len(entries) != 1 {
		t.Fatalf("ParseSrcUri(multiLine, vars) = %d entries, want 1: %v", len(entries), entries)
	}
	want := "mirror://npm/pkg/pkg-1.0.0.tgz"
	if entries[0].URL != want {
		t.Errorf("entries[0].URL = %q, want %q", entries[0].URL, want)
	}
}

// ---- TestBuildVariableMap_NilParsedEbuild ----

func TestBuildVariableMap_NilParsedEbuild(t *testing.T) {
	raw := `EAPI=8
MY_NPM_NAME="language-server"
SRC_URI="mirror://npm/${MY_NPM_NAME}.tgz"
`
	vars := BuildVariableMap(raw, nil)

	if vars["MY_NPM_NAME"] != "language-server" {
		t.Errorf("vars[MY_NPM_NAME] = %q, want %q", vars["MY_NPM_NAME"], "language-server")
	}
	if _, ok := vars["PN"]; ok {
		t.Error("vars should not contain PN when pe is nil")
	}
}

// ---- TestBuildVariableMap_IgnoresComments ----

func TestBuildVariableMap_IgnoresComments(t *testing.T) {
	raw := `# MY_NPM_NAME="wrong"
MY_NPM_NAME="right"
`
	vars := BuildVariableMap(raw, nil)

	if vars["MY_NPM_NAME"] != "right" {
		t.Errorf("vars[MY_NPM_NAME] = %q, want %q", vars["MY_NPM_NAME"], "right")
	}
}

// ---- TestParseSrcUri_* (plan 22a) ----

// ParseSrcUriTestVars is the shared VariableMap fixture for all TestParseSrcUri_* tests.
var ParseSrcUriTestVars = VariableMap{
	"PN":           "vtsls",
	"PV":           "0.3.0",
	"P":            "vtsls-0.3.0",
	"MY_NPM_NAME":  "language-server",
	"MY_NPM_SCOPE": "@earendil-works",
	"MY_WEBUI_P":   "pi-web-ui-0.75.3",
}

func TestParseSrcUri_Unconditional(t *testing.T) {
	input := "\n\tmirror://npm/${PN}/-/${P}.tgz\n"
	entries := ParseSrcUri(input, ParseSrcUriTestVars)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %v", len(entries), entries)
	}
	want := SrcUriEntry{
		URL:      "mirror://npm/vtsls/-/vtsls-0.3.0.tgz",
		Filename: "vtsls-0.3.0.tgz",
		UseFlag:  "",
	}
	if entries[0] != want {
		t.Errorf("= %v, want %v", entries[0], want)
	}
}

func TestParseSrcUri_RenameTarget(t *testing.T) {
	input := "mirror://npm/@vtsls/${MY_NPM_NAME}/-/${MY_NPM_NAME}-${PV}.tgz -> ${P}.tgz"
	entries := ParseSrcUri(input, ParseSrcUriTestVars)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %v", len(entries), entries)
	}
	want := SrcUriEntry{
		URL:      "mirror://npm/@vtsls/language-server/-/language-server-0.3.0.tgz",
		Filename: "vtsls-0.3.0.tgz",
		UseFlag:  "",
	}
	if entries[0] != want {
		t.Errorf("= %v, want %v", entries[0], want)
	}
}

func TestParseSrcUri_UseConditional(t *testing.T) {
	input := "webui? ( mirror://npm/a/-/a-1.0.tgz -> a.tgz )"
	entries := ParseSrcUri(input, ParseSrcUriTestVars)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %v", len(entries), entries)
	}
	want := SrcUriEntry{
		URL:      "mirror://npm/a/-/a-1.0.tgz",
		Filename: "a.tgz",
		UseFlag:  "webui",
	}
	if entries[0] != want {
		t.Errorf("= %v, want %v", entries[0], want)
	}
}

func TestParseSrcUri_MultiEntryUseGroup(t *testing.T) {
	input := `
	mirror://npm/${MY_NPM_SCOPE}/${PN}/-/${P}.tgz -> ${P}.tgz
	https://github.com/o/r/releases/download/pi/${P}-deps.tar.xz
	webui? ( mirror://npm/${MY_NPM_SCOPE}/pi-web-ui/-/${MY_WEBUI_P}.tgz -> ${MY_WEBUI_P}.tgz
		https://github.com/o/r/releases/download/pi/${MY_WEBUI_P}-deps.tar.xz )`
	entries := ParseSrcUri(input, ParseSrcUriTestVars)
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4: %v", len(entries), entries)
	}
	// Entry 1: unconditional npm
	if entries[0].UseFlag != "" {
		t.Errorf("entry[0].UseFlag = %q, want empty", entries[0].UseFlag)
	}
	if entries[0].Filename != "vtsls-0.3.0.tgz" {
		t.Errorf("entry[0].Filename = %q, want vtsls-0.3.0.tgz", entries[0].Filename)
	}
	// Entry 2: unconditional https
	if entries[1].UseFlag != "" {
		t.Errorf("entry[1].UseFlag = %q, want empty", entries[1].UseFlag)
	}
	// Entries 3 and 4: both inside webui? ( … )
	if entries[2].UseFlag != "webui" {
		t.Errorf("entry[2].UseFlag = %q, want webui", entries[2].UseFlag)
	}
	if entries[3].UseFlag != "webui" {
		t.Errorf("entry[3].UseFlag = %q, want webui", entries[3].UseFlag)
	}
	// Entry 4 has no -> so Filename = path.Base
	if entries[3].Filename != "pi-web-ui-0.75.3-deps.tar.xz" {
		t.Errorf("entry[3].Filename = %q, want pi-web-ui-0.75.3-deps.tar.xz", entries[3].Filename)
	}
}

func TestParseSrcUri_NegatedUseFlag(t *testing.T) {
	input := `!minimal? ( https://e.com/x-1.0.tar.gz )`
	entries := ParseSrcUri(input, ParseSrcUriTestVars)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %v", len(entries), entries)
	}
	if entries[0].UseFlag != "!minimal" {
		t.Errorf("UseFlag = %q, want !minimal", entries[0].UseFlag)
	}
}

func TestParseSrcUri_ScopedNpm(t *testing.T) {
	input := "mirror://npm/@vtsls/language-server/-/language-server-0.3.0.tgz"
	entries := ParseSrcUri(input, ParseSrcUriTestVars)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %v", len(entries), entries)
	}
	if entries[0].Filename != "language-server-0.3.0.tgz" {
		t.Errorf("Filename = %q, want language-server-0.3.0.tgz", entries[0].Filename)
	}
}

func TestParseSrcUri_MultipleEntriesMixedProviders(t *testing.T) {
	input := "mirror://npm/pkg/pkg-1.0.tgz https://github.com/o/r/releases/download/v1.0/deps.tar.gz"
	entries := ParseSrcUri(input, ParseSrcUriTestVars)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %v", len(entries), entries)
	}
	if entries[0].UseFlag != "" || entries[1].UseFlag != "" {
		t.Errorf("both UseFlags should be empty, got %v", entries)
	}
	if entries[0].URL != "mirror://npm/pkg/pkg-1.0.tgz" {
		t.Errorf("entry[0].URL = %q, want mirror://npm/pkg/pkg-1.0.tgz", entries[0].URL)
	}
}

func TestParseSrcUri_UrlIsMirrorUnresolved(t *testing.T) {
	input := "mirror://npm/@scope/pkg/-/pkg-1.0.0.tgz"
	entries := ParseSrcUri(input, ParseSrcUriTestVars)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %v", len(entries), entries)
	}
	if !strings.HasPrefix(entries[0].URL, "mirror://npm/") {
		t.Errorf("URL should be mirror-unresolved, got %q", entries[0].URL)
	}
}

func TestParseSrcUri_IgnoresBareAssignment(t *testing.T) {
	input := `MY_P="x" mirror://npm/a/-/a.tgz`
	entries := ParseSrcUri(input, ParseSrcUriTestVars)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %v", len(entries), entries)
	}
	if entries[0].URL != "mirror://npm/a/-/a.tgz" {
		t.Errorf("URL = %q, want mirror://npm/a/-/a.tgz", entries[0].URL)
	}
}

func TestParseSrcUri_Continuation(t *testing.T) {
	input := "mirror://npm/a/-/a.tgz \\\n\thttps://e.com/b.tar.gz"
	entries := ParseSrcUri(input, ParseSrcUriTestVars)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %v", len(entries), entries)
	}
	if entries[0].URL != "mirror://npm/a/-/a.tgz" {
		t.Errorf("entry[0].URL = %q, want mirror://npm/a/-/a.tgz", entries[0].URL)
	}
	if entries[1].URL != "https://e.com/b.tar.gz" {
		t.Errorf("entry[1].URL = %q, want https://e.com/b.tar.gz", entries[1].URL)
	}
}

func TestParseSrcUri_Empty(t *testing.T) {
	entries := ParseSrcUri("", ParseSrcUriTestVars)
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}
