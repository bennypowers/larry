package ebuild

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindFilesdirVersionPins(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{"unversioned inside quotes", `newinitd "${FILESDIR}/netbird.initd" netbird`, nil},
		{"unversioned after quotes", `newinitd "${FILESDIR}"/netbird.initd netbird`, nil},
		{"PN only is fine", `systemd_dounit "${FILESDIR}/${PN}.service"`, nil},
		{"PV pin", `newinitd "${FILESDIR}/${PN}-${PV}-r1.initd" netbird`, []string{"${PN}-${PV}-r1.initd"}},
		{"P pin after quotes", `PATCHES=( "${FILESDIR}"/${P}-fix.patch )`, []string{"${P}-fix.patch"}},
		{"PVR pin", `eapply "${FILESDIR}/${PVR}-revert.patch"`, []string{"${PVR}-revert.patch"}},
		{"PF pin", `eapply "${FILESDIR}/${PF}-gnu-install.patch"`, []string{"${PF}-gnu-install.patch"}},
		{"multiple refs one line", `PATCHES=( "${FILESDIR}/a-${PV}.patch" "${FILESDIR}/b.patch" )`, []string{"a-${PV}.patch"}},
		{"no filesdir", `eapply "${WORKDIR}/x.patch"`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var refs []string
			for _, p := range FindFilesdirVersionPins("EAPI=8\n" + tt.line + "\n") {
				refs = append(refs, p.Ref)
			}
			if len(refs) != len(tt.want) {
				t.Fatalf("refs = %v, want %v", refs, tt.want)
			}
			for i := range refs {
				if refs[i] != tt.want[i] {
					t.Errorf("ref[%d] = %q, want %q", i, refs[i], tt.want[i])
				}
			}
		})
	}
}

func TestCheckFilesdirPins(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "dev-util", "my-pkg")
	filesDir := filepath.Join(pkgDir, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ebuildPath := filepath.Join(pkgDir, "my-pkg-2.0.0.ebuild")
	raw := "EAPI=8\nPATCHES=( \"${FILESDIR}/${P}-fix.patch\" )\n"

	// File present for the new version: pinned, but not missing.
	present := filepath.Join(filesDir, "my-pkg-2.0.0-fix.patch")
	if err := os.WriteFile(present, []byte("patch"), 0o644); err != nil {
		t.Fatal(err)
	}
	warnings := CheckFilesdirPins(ebuildPath, raw)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %+v, want exactly one", warnings)
	}
	w := warnings[0]
	if w.Missing {
		t.Error("Missing = true, want false while the file exists")
	}
	if w.Name != "my-pkg-2.0.0-fix.patch" {
		t.Errorf("Name = %q, want my-pkg-2.0.0-fix.patch", w.Name)
	}
	if w.Line != 2 {
		t.Errorf("Line = %d, want 2", w.Line)
	}
	if !strings.Contains(w.Message, "my-pkg-2.0.0-fix.patch") {
		t.Errorf("Message = %q, want it to name the file", w.Message)
	}

	// File gone: pin is now missing.
	if err := os.Remove(present); err != nil {
		t.Fatal(err)
	}
	warnings = CheckFilesdirPins(ebuildPath, raw)
	if len(warnings) != 1 || !warnings[0].Missing {
		t.Fatalf("warnings = %+v, want one missing pin", warnings)
	}
	if !strings.Contains(warnings[0].Message, "does not exist") {
		t.Errorf("Message = %q, want it to say the file does not exist", warnings[0].Message)
	}

	// Unversioned reference: nothing to report.
	if err := os.WriteFile(filepath.Join(filesDir, "fix.patch"), []byte("patch"), 0o644); err != nil {
		t.Fatal(err)
	}
	rawOK := "EAPI=8\nPATCHES=( \"${FILESDIR}/fix.patch\" )\n"
	if warnings := CheckFilesdirPins(ebuildPath, rawOK); len(warnings) != 0 {
		t.Fatalf("warnings = %+v, want none for unversioned ref", warnings)
	}
}
