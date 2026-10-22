package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/upstream"
)

func stubExecutable(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	path := filepath.Join(t.TempDir(), "larry-stub")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := larryExecutable
	larryExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { larryExecutable = orig })
}

func TestRunLarryChildStreamsOutput(t *testing.T) {
	stubExecutable(t, "#!/bin/sh\necho first\necho second\n")
	var lines []string
	if err := runLarryChild(context.Background(), []string{"x"}, func(line string) {
		lines = append(lines, line)
	}); err != nil {
		t.Fatalf("runLarryChild: %v", err)
	}
	if len(lines) != 2 || lines[0] != "first" || lines[1] != "second" {
		t.Errorf("lines = %v, want [first second]", lines)
	}
}

func TestRunLarryChildPropagatesError(t *testing.T) {
	stubExecutable(t, "#!/bin/sh\necho nope >&2\nexit 3\n")
	var lines []string
	err := runLarryChild(context.Background(), []string{"x"}, func(line string) {
		lines = append(lines, line)
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(lines) == 0 || !strings.Contains(strings.Join(lines, "\n"), "nope") {
		t.Errorf("stderr was not streamed: %v", lines)
	}
}

func TestBuildUpstreamActionsArgs(t *testing.T) {
	stubExecutable(t, "#!/bin/sh\necho \"$@\"\n")

	var lines []string
	actions := buildUpstreamActions("/tmp/overlay")
	if err := actions.Bump(context.Background(), "dev-util/foo", func(line string) {
		lines = append(lines, line)
	}); err != nil {
		t.Fatalf("Bump: %v", err)
	}
	got := strings.Join(lines, " ")
	for _, want := range []string{"bump", "dev-util/foo", "--overlay /tmp/overlay", "--color never"} {
		if !strings.Contains(got, want) {
			t.Errorf("child args = %q, want %q", got, want)
		}
	}
}

func TestBuildUpstreamActionsTestUsesCompile(t *testing.T) {
	stubExecutable(t, "#!/bin/sh\necho \"$@\"\n")
	var lines []string
	if err := buildUpstreamActions("/tmp/overlay").Test(context.Background(), "dev-util/foo", func(line string) {
		lines = append(lines, line)
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(lines, " "), "--compile") {
		t.Errorf("test action args = %v, want --compile", lines)
	}
}

func TestShouldUseTUI(t *testing.T) {
	tests := []struct {
		name                string
		noTUI               bool
		output              string
		quiet               bool
		stdoutTTY, stdinTTY bool
		want                bool
	}{
		{"auto both ttys", false, "text", false, true, true, true},
		{"auto stdout not tty", false, "text", false, false, true, false},
		{"auto stdin not tty", false, "text", false, true, false, false},
		{"json is plain", false, "json", false, true, true, false},
		{"quiet is plain", false, "text", true, true, true, false},
		{"no-tui wins", true, "text", false, true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUseTUI(tt.noTUI, tt.output, tt.quiet, tt.stdoutTTY, tt.stdinTTY); got != tt.want {
				t.Errorf("shouldUseTUI = %v, want %v", got, tt.want)
			}
		})
	}
}

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
