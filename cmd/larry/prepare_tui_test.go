package main

import (
	"path/filepath"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/prepare"
)

func TestPrepareTUI_FlagRegistered(t *testing.T) {
	if prepareCmd.Flags().Lookup("tui") == nil {
		t.Error("prepare does not register --tui")
	}
}

func TestPrepareTUI_NonTTYFallsBack(t *testing.T) {
	ov := fixtureOverlay(t, map[string]string{"origin": "https://example.com/test.git"}, map[string]string{
		"dev-util/foo/foo-1.0.ebuild": "EAPI=8\n",
	})
	dd := t.TempDir()
	target := filepath.Join(ov, "dev-util", "foo")

	calls := 0
	orig := prepare.PrepareFile
	prepare.PrepareFile = func(string, *prepare.PrepareConfig) error {
		calls++
		return nil
	}
	t.Cleanup(func() { prepare.PrepareFile = orig })

	_, stderr, err := execCmd(t, "prepare", "--tui", "--overlay", ov, "--distdir", dd, target)
	if err != nil {
		t.Fatalf("execCmd: %v (stderr %q)", err, stderr)
	}
	if calls != 1 {
		t.Errorf("pipeline ran %d times, want 1", calls)
	}
	if !strings.Contains(stderr, "falling back") {
		t.Errorf("stderr = %q, want a fallback warning", stderr)
	}
}
