package upstream

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDeprecatedPackages(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string // overlay-relative path -> content
		want  map[string]DeprecatedInfo
	}{
		{
			name: "package.deprecated atoms with comments",
			files: map[string]string{
				"profiles/package.deprecated": "# removed upstream\nmail-client/vireo\n\nsys-apps/gone\n",
			},
			want: map[string]DeprecatedInfo{
				"mail-client/vireo": {Package: "mail-client/vireo", Source: "deprecated"},
				"sys-apps/gone":     {Package: "sys-apps/gone", Source: "deprecated"},
			},
		},
		{
			name: "updates move and rename",
			files: map[string]string{
				"profiles/updates/1Q-2026": "# comment\nmove mail-client/vireo mail-client/hylki\nrename old-cat/old new-cat/new\n",
			},
			want: map[string]DeprecatedInfo{
				"mail-client/vireo": {Package: "mail-client/vireo", Target: "mail-client/hylki", Source: "renamed"},
				"old-cat/old":       {Package: "old-cat/old", Target: "new-cat/new", Source: "renamed"},
			},
		},
		{
			name: "short lines in updates ignored",
			files: map[string]string{
				"profiles/updates/2Q-2026": "move\nmove only-one\nrename one\n",
			},
			want: map[string]DeprecatedInfo{},
		},
		{
			name:  "missing files",
			files: map[string]string{},
			want:  map[string]DeprecatedInfo{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			overlay := t.TempDir()
			for rel, content := range tt.files {
				path := filepath.Join(overlay, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			}

			got := LoadDeprecatedPackages(overlay)
			if len(got) != len(tt.want) {
				t.Fatalf("LoadDeprecatedPackages returned %d entries (%v), want %d (%v)",
					len(got), got, len(tt.want), tt.want)
			}
			for atom, wantInfo := range tt.want {
				gotInfo, ok := got[atom]
				if !ok {
					t.Errorf("atom %q missing from result: %v", atom, got)
					continue
				}
				if gotInfo != wantInfo {
					t.Errorf("result[%q] = %+v, want %+v", atom, gotInfo, wantInfo)
				}
				if !gotInfo.IsDeprecated() {
					t.Errorf("IsDeprecated() = false for %+v, want true", gotInfo)
				}
			}
		})
	}
}

func TestDeprecatedInfo_IsDeprecated(t *testing.T) {
	// Nil and empty infos are not deprecated.
	var nilInfo *DeprecatedInfo
	if nilInfo.IsDeprecated() {
		t.Error("nil DeprecatedInfo.IsDeprecated() = true, want false")
	}
	if (&DeprecatedInfo{}).IsDeprecated() {
		t.Error("empty DeprecatedInfo.IsDeprecated() = true, want false")
	}
}
