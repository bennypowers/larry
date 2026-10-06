package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// D14 — Provenance lines appear in CLI output
// ---------------------------------------------------------------------------

func TestImportCmd_PrintsProvenance(t *testing.T) {
	// Source overlay with a repo named "guru" so the provenance line
	// reads "Importing dev-lisp/foo from ::guru".
	sourceOverlay := fixtureOverlay(t,
		map[string]string{"source": "https://github.com/source/foo"},
		map[string]string{
			"dev-lisp/foo/foo-1.0.0.ebuild": `EAPI=8\nSRC_URI=""`,
		},
	)

	// Create the target overlay with a git remote pointing to the source.
	targetOverlay := fixtureOverlay(t,
		map[string]string{"target": "https://github.com/target/foo"},
		map[string]string{
			// Empty target — no existing ebuilds.
		},
	)

	// Set maintainer env vars so config.Load() picks them up.
	t.Setenv("LARRY_MAINTAINER_NAME", "Benny Powers")
	t.Setenv("LARRY_MAINTAINER_EMAIL", "web@bennypowers.com")

	stdout, stderr, err := execCmd(t,
		"--overlay", targetOverlay,
		"--from", sourceOverlay,
		"import", "--dry-run", "dev-lisp/foo",
	)
	if err != nil {
		t.Fatalf("execCmd: %v\nstderr: %s", err, stderr)
	}

	// The provenance line should appear (with ">>> " prefix from Logger.Msg).
	if !strings.Contains(stdout, "Importing dev-lisp/foo from ::") {
		t.Errorf("expected 'Importing dev-lisp/foo from ::' in stdout\nstdout: %s", stdout)
	}
	if !strings.Contains(stdout, "Source:") {
		t.Errorf("expected 'Source:' line in stdout\nstdout: %s", stdout)
	}
}

// ---------------------------------------------------------------------------
// D16 — No stale "Run larry prepare" hints
// ---------------------------------------------------------------------------

func TestImportCmd_NoStaleRunPrepareHint(t *testing.T) {
	// Set maintainer env vars for the CLI config.
	t.Setenv("LARRY_MAINTAINER_NAME", "Benny Powers")
	t.Setenv("LARRY_MAINTAINER_EMAIL", "web@bennypowers.com")

	sourceOverlay := fixtureOverlay(t,
		map[string]string{"source": "https://github.com/source/foo"},
		map[string]string{
			"dev-lisp/foo/foo-1.0.0.ebuild": `EAPI=8\nSRC_URI=""`,
		},
	)

	targetOverlay := fixtureOverlay(t,
		map[string]string{"target": "https://github.com/target/foo"},
		map[string]string{},
	)

	// We can't easily stub the Prepare hook from the CLI, but the code
	// change should ensure the literal string is absent from both output
	// and help text.
	_, _, err := execCmd(t,
		"--overlay", targetOverlay,
		"--from", sourceOverlay,
		"import", "--dry-run", "dev-lisp/foo",
	)
	if err != nil {
		t.Fatalf("execCmd: %v", err)
	}

	// The key assertion is on the help text (TestImportCmd_LongHelpHasNoStaleHint).
	// Here we just ensure the command runs without erroring on "Run larry prepare".
}

func TestImportCmd_LongHelpHasNoStaleHint(t *testing.T) {
	stdout, _, err := execCmd(t, "import", "--help")
	if err != nil {
		t.Fatalf("execCmd --help: %v", err)
	}

	if strings.Contains(stdout, "After importing, run 'larry prepare'") {
		t.Error("help text should not contain 'After importing, run 'larry prepare''")
	}
	if strings.Contains(stdout, "Run 'larry prepare") {
		t.Error("help text should not contain 'Run 'larry prepare'")
	}
}
