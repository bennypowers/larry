package version

import "testing"

func TestVersion_GoldenCompare(t *testing.T) {
	// Test cases sourced from Gentoo portage tree version comparison behavior
	golden := []struct {
		a, b   string
		want   int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.3", "1.2.4", -1},
		{"1.2.4", "1.2.3", 1},
		{"2.0.0", "1.9.9", 1},
		{"1.9.9", "2.0.0", -1},
	}

	for _, g := range golden {
		t.Run(g.a+"_vs_"+g.b, func(t *testing.T) {
			a, errA := Parse(g.a)
			b, errB := Parse(g.b)
			if errA != nil || errB != nil {
				t.Fatal("failed to parse golden inputs")
			}
			if got := a.Compare(b); got != g.want {
				t.Errorf("Compare(%s, %s) = %d, want %d", g.a, g.b, got, g.want)
			}
		})
	}
}
