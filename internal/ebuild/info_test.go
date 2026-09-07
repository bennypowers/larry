package ebuild

import (
	"testing"
)

// ---- TestParseEbuildInfo_EmptyPath: rejects empty path ----

func TestParseEbuildInfo_EmptyPath(t *testing.T) {
	_, err := ParseEbuildInfo("")
	if err == nil {
		t.Fatal("ParseEbuildInfo(\"\") should return error")
	}
	if !containsString(err.Error(), "empty") {
		t.Errorf("Error should mention empty path: %v", err)
	}
}

// ---- TestParseEbuildInfo_NonExistent: returns error for missing file ----

func TestParseEbuildInfo_NonExistent(t *testing.T) {
	_, err := ParseEbuildInfo("/nonexistent/cat/pkg-1.0.ebuild")
	if err == nil {
		t.Fatal("ParseEbuildInfo(non-existent) should return error")
	}
	_ = err // file not found is expected
}

// ---- TestEbuildInfo_Parse: ebuild info output → struct fields ----

func TestEbuildInfo_Parse(t *testing.T) {
	// Simulated `ebuild info` output
	raw := `
CATEGORY="dev-util/my-tool"
PN="my-tool"
PV="1.2.3"
P="my-tool-1.2.3"
SRC_URI="mirror://github/my-owner/my-tool/my-tool-1.2.3.tar.gz"
DEPEND="dev-lang/python"
RDEPEND="dev-lang/python"
HOMEPAGE="https://example.com/my-tool"
INHERITED="cargo git-r3"
`
	info, err := parseInfoOutput("/fake/path", raw)
	if err != nil {
		t.Fatalf("parseInfoOutput() error: %v", err)
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"Path", info.Path, "/fake/path"},
		{"Category", info.Category, "dev-util/my-tool"},
		{"PN", info.Pkg, "my-tool"},
		{"PV", info.PVVal, "1.2.3"},
		{"P", info.PVal, "my-tool-1.2.3"},
		{"SRC_URI", info.SRC_URI, "mirror://github/my-owner/my-tool/my-tool-1.2.3.tar.gz"},
		{"DEPEND", info.DEPEND, "dev-lang/python"},
		{"RDEPEND", info.RDEPEND, "dev-lang/python"},
		{"HOMEPAGE", info.HOMEPAGE, "https://example.com/my-tool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}

	// Verify Inherit
	if len(info.Inherit) != 2 {
		t.Errorf("Inherit length = %d, want 2", len(info.Inherit))
	}
	if info.Inherit[0] != "cargo" || info.Inherit[1] != "git-r3" {
		t.Errorf("Inherit = %v, want [cargo git-r3]", info.Inherit)
	}
}

// ---- TestEbuildInfo_Parse_SingleQuoted: handles single-quoted values ----

func TestEbuildInfo_Parse_SingleQuoted(t *testing.T) {
	raw := `CATEGORY='dev-util/single'
PN='my-tool'
PV='2.0.0'
SRC_URI='mirror://github/owner/pkg'
INHERITED='cargo'
`
	info, err := parseInfoOutput("/fake", raw)
	if err != nil {
		t.Fatalf("parseInfoOutput() error: %v", err)
	}
	if info.Pkg != "my-tool" {
		t.Errorf("PN = %q, want %q", info.Pkg, "my-tool")
	}
	if info.PVVal != "2.0.0" {
		t.Errorf("PV = %q, want %q", info.PVVal, "2.0.0")
	}
}

// ---- TestEbuildInfo_Parse_MissingFields: handles partial output ----

func TestEbuildInfo_Parse_MissingFields(t *testing.T) {
	raw := `CATEGORY="dev-util/minimal"
PV="1.0"
`
	info, err := parseInfoOutput("/fake", raw)
	if err != nil {
		t.Fatalf("parseInfoOutput() error: %v", err)
	}
	if info.Category != "dev-util/minimal" {
		t.Errorf("Category = %q, want %q", info.Category, "dev-util/minimal")
	}
	if info.PVVal != "1.0" {
		t.Errorf("PV = %q, want %q", info.PVVal, "1.0")
	}
	// Missing fields should be empty
	if info.SRC_URI != "" {
		t.Errorf("SRC_URI = %q, want empty", info.SRC_URI)
	}
	if info.HOMEPAGE != "" {
		t.Errorf("HOMEPAGE = %q, want empty", info.HOMEPAGE)
	}
}

// ---- TestEbuildInfo_PN: accessor returns package name ----

func TestEbuildInfo_PN(t *testing.T) {
	info := &EbuildInfo{Pkg: "test-pkg"}
	if got := info.PN(); got != "test-pkg" {
		t.Errorf("PN() = %q, want %q", got, "test-pkg")
	}
}

// ---- TestEbuildInfo_PV: accessor returns version ----

func TestEbuildInfo_PV(t *testing.T) {
	info := &EbuildInfo{PVVal: "1.2.3"}
	if got := info.PV(); got != "1.2.3" {
		t.Errorf("PV() = %q, want %q", got, "1.2.3")
	}
}

// ---- TestEbuildInfo_P: accessor returns PN-PV ----

func TestEbuildInfo_P(t *testing.T) {
	t.Run("explicit P", func(t *testing.T) {
		info := &EbuildInfo{PVal: "custom-p-1.0"}
		if got := info.P(); got != "custom-p-1.0" {
			t.Errorf("P() = %q, want %q", got, "custom-p-1.0")
		}
	})
	t.Run("constructed from PN+PV", func(t *testing.T) {
		info := &EbuildInfo{Pkg: "my-pkg", PVVal: "1.0"}
		if got := info.P(); got != "my-pkg-1.0" {
			t.Errorf("P() = %q, want %q", got, "my-pkg-1.0")
		}
	})
	t.Run("empty components", func(t *testing.T) {
		info := &EbuildInfo{}
		if got := info.P(); got != "" {
			t.Errorf("P() = %q, want empty", got)
		}
	})
}

// ---- TestParseInfoOutput_Empty: handles empty input ----

func TestParseInfoOutput_Empty(t *testing.T) {
	info, err := parseInfoOutput("/fake", "")
	if err != nil {
		t.Fatalf("parseInfoOutput(\"\") error: %v", err)
	}
	if info == nil {
		t.Fatal("parseInfoOutput(\"\") returned nil")
	}
	if info.Path != "/fake" {
		t.Errorf("Path = %q, want %q", info.Path, "/fake")
	}
}

// ---- TestParseInfoOutput_BlankLines: handles blank lines and whitespace ----

func TestParseInfoOutput_BlankLines(t *testing.T) {
	raw := `

CATEGORY="dev-util/test"

PN="pkg"

PV="1.0"

`
	info, err := parseInfoOutput("/fake", raw)
	if err != nil {
		t.Fatalf("parseInfoOutput() error: %v", err)
	}
	if info.Category != "dev-util/test" {
		t.Errorf("Category = %q, want %q", info.Category, "dev-util/test")
	}
	if info.Pkg != "pkg" {
		t.Errorf("PN = %q, want %q", info.Pkg, "pkg")
	}
	if info.PVVal != "1.0" {
		t.Errorf("PV = %q, want %q", info.PVVal, "1.0")
	}
}

// ---- TestParseInfoOutput_InheritEmpty: handles empty INHERITED ----

func TestParseInfoOutput_InheritEmpty(t *testing.T) {
	raw := `CATEGORY="cat/pkg"
PN="pkg"
PV="1.0"
INHERITED=""
`
	info, err := parseInfoOutput("/fake", raw)
	if err != nil {
		t.Fatalf("parseInfoOutput() error: %v", err)
	}
	if info.Inherit != nil && len(info.Inherit) != 0 {
		t.Errorf("Inherit = %v, want nil/empty", info.Inherit)
	}
}

// ---- TestParseInfoOutput_InheritWhitespace: INHERITED with spaces ----

func TestParseInfoOutput_InheritWhitespace(t *testing.T) {
	raw := `CATEGORY="cat/pkg"
PN="pkg"
PV="1.0"
INHERITED="  cargo  git-r3  npm "
`
	info, err := parseInfoOutput("/fake", raw)
	if err != nil {
		t.Fatalf("parseInfoOutput() error: %v", err)
	}
	if len(info.Inherit) != 3 {
		t.Errorf("Inherit length = %d, want 3", len(info.Inherit))
	}
	if info.Inherit[0] != "cargo" || info.Inherit[1] != "git-r3" || info.Inherit[2] != "npm" {
		t.Errorf("Inherit = %v, want [cargo git-r3 npm]", info.Inherit)
	}
}

// ---- TestParseInfoOutput_WhitespaceInValues: handles values with whitespace ----

func TestParseInfoOutput_WhitespaceInValues(t *testing.T) {
	raw := `SRC_URI="  mirror://github/owner/pkg.tar.gz  "
HOMEPAGE="  https://example.com  "
`
	info, err := parseInfoOutput("/fake", raw)
	if err != nil {
		t.Fatalf("parseInfoOutput() error: %v", err)
	}
	if info.SRC_URI != "mirror://github/owner/pkg.tar.gz" {
		t.Errorf("SRC_URI = %q, want %q", info.SRC_URI, "mirror://github/owner/pkg.tar.gz")
	}
	if info.HOMEPAGE != "https://example.com" {
		t.Errorf("HOMEPAGE = %q, want %q", info.HOMEPAGE, "https://example.com")
	}
}

// ---- TestParseInfoOutput_CaseSensitiveKeys: ignores non-UPPER keys ----

func TestParseInfoOutput_CaseSensitiveKeys(t *testing.T) {
	raw := `CATEGORY="cat/pkg"
pn="should-be-ignored"
pv="1.0"
PN="actual-pn"
`
	info, err := parseInfoOutput("/fake", raw)
	if err != nil {
		t.Fatalf("parseInfoOutput() error: %v", err)
	}
	if info.Pkg != "actual-pn" {
		t.Errorf("PN = %q, want %q", info.Pkg, "actual-pn")
	}
}

// ---- TestParseInfoOutput_CompleteFlow: realistic ebuild info ----

func TestParseInfoOutput_CompleteFlow(t *testing.T) {
	raw := `CATEGORY="dev-util/lsp-server"
PN="lsp-server"
PV="1.5.0"
P="lsp-server-1.5.0"
SRC_URI="mirror://github/owner/lsp-server/lsp-server-1.5.0.tar.gz"
DEPEND="dev-lang/rust"
RDEPEND="dev-lang/rust"
HOMEPAGE="https://example.com/lsp-server"
INHERITED="cargo git-r3"
`
	info, err := parseInfoOutput("/fake", raw)
	if err != nil {
		t.Fatalf("parseInfoOutput() error: %v", err)
	}

	if info.PN() != "lsp-server" {
		t.Errorf("PN() = %q, want %q", info.PN(), "lsp-server")
	}
	if info.PV() != "1.5.0" {
		t.Errorf("PV() = %q, want %q", info.PV(), "1.5.0")
	}
	if info.P() != "lsp-server-1.5.0" {
		t.Errorf("P() = %q, want %q", info.P(), "lsp-server-1.5.0")
	}
	if info.HOMEPAGE != "https://example.com/lsp-server" {
		t.Errorf("HOMEPAGE = %q, want %q", info.HOMEPAGE, "https://example.com/lsp-server")
	}
}

// ---- TestUnquote: removes surrounding quotes ----

func TestUnquote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`"hello"`, "hello"},
		{`'hello'`, "hello"}, // unquote only strips double quotes
		{`hello`, "hello"},
		{`""`, ""},
		{`  "hello"  `, "hello"}, // trims first
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := unquote(tt.input)
			if got != tt.want {
				t.Errorf("unquote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---- TestParseInherited: splits INHERITED value ----

func TestParseInherited(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"cargo git-r3", []string{"cargo", "git-r3"}},
		{"go-module", []string{"go-module"}},
		{"  cargo  npm  git-r3  ", []string{"cargo", "npm", "git-r3"}},
		{"", nil},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseInherited(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("parseInherited(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseInherited(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// Helper: check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
