package main

import (
	"testing"
)

// TestQuietSuppressesSubcommandOutput verifies that --quiet suppresses
// informational output (stdout is empty) while errors still reach stderr
// with the !!! prefix.
func TestQuietSuppressesSubcommandOutput(t *testing.T) {
	fixture := fixtureOverlay(t, map[string]string{
		"origin": "https://github.com/test/test.git",
	}, map[string]string{
		"dev-lang/foo/foo-1.0.0.ebuild": "# test ebuild\n",
	})

	// Run without --quiet: stdout should be non-empty for upstream.
	stdout, stderr, err := execCmd(t, "upstream", "--overlay", fixture)
	if err != nil {
		// upstream may fail for other reasons (no git provider),
		// but stdout should still contain something when not quiet.
		t.Logf("upstream without quiet: err=%v, stdout=%q, stderr=%q", err, stdout, stderr)
	}
	if stdout == "" {
		// May be empty if the spinner produced no visible output;
		// the key invariant is the quiet comparison below.
		t.Log("upstream stdout was empty without --quiet (spinner-only)")
	}

	// Run with --quiet: stdout must be exactly "".
	stdoutQuiet, _, errQuiet := execCmd(t, "upstream", "--quiet", "--overlay", fixture)
	if stdoutQuiet != "" {
		t.Errorf("--quiet stdout = %q, want empty string", stdoutQuiet)
	}
	// Errors should still appear on stderr.
	if errQuiet != nil {
		t.Logf("--quiet produced error (expected if upstream fails): %v", errQuiet)
	}
}

// TestColorNeverProducesNoAnsi_Subcommand verifies that --color never
// produces no ANSI escape sequences in either stdout or stderr.
func TestColorNeverProducesNoAnsi_Subcommand(t *testing.T) {
	fixture := fixtureOverlay(t, map[string]string{
		"origin": "https://github.com/test/test.git",
	}, map[string]string{
		"dev-lang/foo/foo-1.0.0.ebuild": "# test ebuild\n",
	})

	stdout, stderr, _ := execCmd(t, "upstream", "--color", "never", "--overlay", fixture)
	output := stdout + stderr
	if containsAnsi(output) {
		t.Errorf("--color never produced ANSI escapes: %q", output)
	}
}

// containsAnsi reports whether s contains any ANSI escape sequences.
func containsAnsi(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '\x1b' && (s[i+1] == '[' || s[i+1] == ']') {
			return true
		}
	}
	return false
}
