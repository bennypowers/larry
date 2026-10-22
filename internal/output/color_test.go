package output

import (
	"testing"

	"github.com/muesli/termenv"
)

func TestColorHelper_Profile(t *testing.T) {
	always, err := NewColorHelper("always")
	if err != nil {
		t.Fatal(err)
	}
	if !always.Enabled() || always.Profile() == termenv.Ascii {
		t.Errorf("always: enabled=%v profile=%v, want enabled and non-Ascii",
			always.Enabled(), always.Profile())
	}

	never, err := NewColorHelper("never")
	if err != nil {
		t.Fatal(err)
	}
	if never.Enabled() || never.Profile() != termenv.Ascii {
		t.Errorf("never: enabled=%v profile=%v, want disabled and Ascii",
			never.Enabled(), never.Profile())
	}
}

func TestColor_NewColorHelper(t *testing.T) {
	tests := []struct {
		mode    string
		wantErr bool
	}{
		{"auto", false},
		{"always", false},
		{"never", false},
		{"invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			c, err := NewColorHelper(tt.mode)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewColorHelper(%q) error = %v, wantErr %v", tt.mode, err, tt.wantErr)
				return
			}
			if !tt.wantErr && c.mode != tt.mode {
				t.Errorf("mode = %q, want %q", c.mode, tt.mode)
			}
		})
	}
}

func TestColor_AutoDetect(t *testing.T) {
	c, err := NewColorHelper("auto")
	if err != nil {
		t.Fatal(err)
	}

	err = c.Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	// Just verify it doesn't panic and mode is set
	_ = c.Enabled()
	_ = c.Mode()
}

func TestColor_Override(t *testing.T) {
	c, err := NewColorHelper("never")
	if err != nil {
		t.Fatal(err)
	}

	if c.Enabled() {
		t.Error("Color should be disabled when mode=never")
	}
}

func TestColor_NoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	c, err := NewColorHelper("auto")
	if err != nil {
		t.Fatal(err)
	}

	err = c.Detect()
	if err != nil {
		t.Fatal(err)
	}

	if c.Enabled() {
		t.Error("NO_COLOR should disable colors")
	}
}

func TestColor_Helpers(t *testing.T) {
	c, err := NewColorHelper("never")
	if err != nil {
		t.Fatal(err)
	}

	input := "hello"
	if got := c.Red(input); got != input {
		t.Errorf("Red() = %q, want %q (color disabled)", got, input)
	}
	if got := c.Green(input); got != input {
		t.Errorf("Green() = %q, want %q", got, input)
	}
	if got := c.Yellow(input); got != input {
		t.Errorf("Yellow() = %q, want %q", got, input)
	}
	if got := c.Blue(input); got != input {
		t.Errorf("Blue() = %q, want %q", got, input)
	}
	if got := c.Dim(input); got != input {
		t.Errorf("Dim() = %q, want %q", got, input)
	}
	if got := c.Hyperlink(input, "http://example.com"); got != input {
		t.Errorf("Hyperlink() = %q, want %q", got, input)
	}
}
