package main

import (
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/upstream"
)

func TestUpstreamTUI_NonTTYFallsBack(t *testing.T) {
	ov := verbosityOverlay(t)
	stubUpstreamResults(t, []upstream.UpstreamResult{{
		Package: "dev-util/foo",
		Current: "1.0.0",
		Latest:  "2.0.0",
		Status:  upstream.StatusOutdated,
	}}, nil)

	stdout, stderr, err := execCmd(t, "upstream", "--overlay", ov, "--tui")
	if err != nil {
		t.Fatalf("execCmd: %v (stderr %q)", err, stderr)
	}
	if !strings.Contains(stdout, "dev-util/foo") {
		t.Errorf("stdout = %q, want the plain table", stdout)
	}
	if !strings.Contains(stderr, "falling back") {
		t.Errorf("stderr = %q, want a fallback warning", stderr)
	}
}

func TestUpstreamTUI_ColorNeverNoANSI(t *testing.T) {
	ov := verbosityOverlay(t)
	stubUpstreamResults(t, []upstream.UpstreamResult{{
		Package: "dev-util/foo",
		Status:  upstream.StatusOutdated,
	}}, nil)

	stdout, stderr, err := execCmd(t, "upstream", "--overlay", ov, "--tui", "--color", "never")
	if err != nil {
		t.Fatalf("execCmd: %v", err)
	}
	if strings.Contains(stdout, "\x1b[") || strings.Contains(stderr, "\x1b[") {
		t.Errorf("--color never emitted ANSI; stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestUpstreamTUI_CallsFetchOnce(t *testing.T) {
	ov := verbosityOverlay(t)
	calls := 0
	orig := runUpstreamWithTargets
	runUpstreamWithTargets = func(upstream.RunOptions) ([]upstream.UpstreamResult, error) {
		calls++
		return []upstream.UpstreamResult{{Package: "dev-util/foo", Status: upstream.StatusOutdated}}, nil
	}
	t.Cleanup(func() { runUpstreamWithTargets = orig })

	if _, stderr, err := execCmd(t, "upstream", "--overlay", ov, "--tui"); err != nil {
		t.Fatalf("execCmd: %v (stderr %q)", err, stderr)
	}
	if calls != 1 {
		t.Errorf("fetch called %d times, want 1", calls)
	}
}

func TestUpstreamTUI_AllRowsWithVerbose(t *testing.T) {
	ov := verbosityOverlay(t)
	stubUpstreamResults(t, []upstream.UpstreamResult{{
		Package: "dev-util/live",
		Current: "9999",
		Status:  upstream.StatusLive,
	}}, nil)

	stdout, _, err := execCmd(t, "upstream", "--overlay", ov, "--tui")
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if strings.Contains(stdout, "dev-util/live") {
		t.Errorf("default fallback showed a LIVE row: %q", stdout)
	}

	stdout, _, err = execCmd(t, "upstream", "--overlay", ov, "--tui", "-v")
	if err != nil {
		t.Fatalf("-v: %v", err)
	}
	if !strings.Contains(stdout, "dev-util/live") {
		t.Errorf("-v fallback hid the LIVE row: %q", stdout)
	}
}
