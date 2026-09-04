package version

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		input   string
		wantMajor int
		wantMinor int
		wantPatch int
		wantErr bool
	}{
		{"1.2.3", 1, 2, 3, false},
		{"0.0.1", 0, 0, 1, false},
		{"10.20.30", 10, 20, 30, false},
		{"invalid", 0, 0, 0, true},
		{"1.2", 0, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, err := Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && v != nil {
				if v.Major != tt.wantMajor || v.Minor != tt.wantMinor || v.Patch != tt.wantPatch {
					t.Errorf("Parse(%q) = %v, want major=%d minor=%d patch=%d",
						tt.input, v, tt.wantMajor, tt.wantMinor, tt.wantPatch)
				}
			}
		})
	}
}

func TestVersion_String(t *testing.T) {
	v := &Version{1, 2, 3, ""}
	if got := v.String(); got != "1.2.3" {
		t.Errorf("Version.String() = %q, want %q", got, "1.2.3")
	}
}

func TestVersion_Compare(t *testing.T) {
	tests := []struct {
		a    *Version
		b    *Version
		want int
	}{
		{&Version{1, 0, 0, ""}, &Version{1, 0, 0, ""}, 0},
		{&Version{1, 0, 0, ""}, &Version{2, 0, 0, ""}, -1},
		{&Version{2, 0, 0, ""}, &Version{1, 0, 0, ""}, 1},
		{&Version{1, 2, 0, ""}, &Version{1, 1, 0, ""}, 1},
		{&Version{1, 0, 1, ""}, &Version{1, 0, 0, ""}, 1},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			if got := tt.a.Compare(tt.b); got != tt.want {
				t.Errorf("Compare(%v, %v) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
