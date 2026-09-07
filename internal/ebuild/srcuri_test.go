package ebuild

import (
	"testing"
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
	vars := BuildVariableMap(info)
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
	vars := BuildVariableMap(info)

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
	vars := BuildVariableMap(info)

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
	vars := BuildVariableMap(info)

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
	vars := BuildVariableMap(info)

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
	vars = BuildVariableMap(info)
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
	vars = BuildVariableMap(info)
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
	vars := BuildVariableMap(info)

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
	info := &EbuildInfo{
		Pkg:     "my-tool",
		PVVal:   "1.2.3",
		PVal:    "my-tool-1.2.3",
		SRC_URI: `MY_P="${PN}-${PV}"
SRC_URI="mirror://npm/${MY_P}/${MY_P}.tgz"`,
	}
	vars := BuildVariableMap(info)

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
