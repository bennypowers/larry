package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/cli"
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
	err := buildUpstreamActions("/tmp/overlay").Test(context.Background(), "dev-util/foo",
		cli.TestOptions{Mode: "compile"},
		func(line string) { lines = append(lines, line) }, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(lines, " ")
	for _, want := range []string{"--compile", "--progress json", "--overlay /tmp/overlay", "dev-util/foo"} {
		if !strings.Contains(got, want) {
			t.Errorf("test action args = %q, want %q", got, want)
		}
	}
}

func TestBuildUpstreamActionsTestModes(t *testing.T) {
	stubExecutable(t, "#!/bin/sh\necho \"$@\"\n")
	action := buildUpstreamActions("/tmp/overlay").Test

	var lines []string
	if err := action(context.Background(), "dev-util/foo",
		cli.TestOptions{Mode: "merge", Use: "cuda rocm"},
		func(line string) { lines = append(lines, line) }, nil); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(lines, " ")
	if !strings.Contains(got, "--merge") || !strings.Contains(got, "--use cuda rocm") {
		t.Errorf("merge args = %q", got)
	}

	lines = nil
	if err := action(context.Background(), "dev-util/foo",
		cli.TestOptions{Mode: "phases", Phases: []string{"fetch", "unpack"}},
		func(line string) { lines = append(lines, line) }, nil); err != nil {
		t.Fatal(err)
	}
	got = strings.Join(lines, " ")
	if !strings.Contains(got, "--phase fetch --phase unpack") {
		t.Errorf("phase args = %q", got)
	}
}

func TestParseTestProgress(t *testing.T) {
	line := `larry-progress:{"kind":1,"step":"image","state":1}`
	got, ok := parseTestProgress(line)
	if !ok {
		t.Fatalf("parseTestProgress(%q) did not parse", line)
	}
	if got.Step != "image" || got.State != cli.ActionStepRunning {
		t.Errorf("progress = %+v, want image running", got)
	}

	line = `larry-progress:{"kind":2}`
	got, ok = parseTestProgress(line)
	if !ok || !got.Done {
		t.Errorf("done event = %+v, ok=%v", got, ok)
	}

	if _, ok := parseTestProgress("ordinary output"); ok {
		t.Error("ordinary line parsed as progress")
	}
}

func TestManageRequiresTTY(t *testing.T) {
	ov := verbosityOverlay(t)
	_, _, err := execCmd(t, "manage", "--overlay", ov)
	if err == nil {
		t.Fatal("expected manage to reject a non-terminal")
	}
	if !strings.Contains(err.Error(), "requires a terminal") {
		t.Errorf("error = %v, want a terminal requirement", err)
	}
}

func TestUpstreamHasNoTUIFlags(t *testing.T) {
	for _, name := range []string{"tui", "no-tui"} {
		if upstreamCmd.Flags().Lookup(name) != nil {
			t.Errorf("upstream still defines --%s", name)
		}
	}
}

func TestUpstreamStaysPlainInTTY(t *testing.T) {
	ov := verbosityOverlay(t)
	stubUpstreamResults(t, []upstream.UpstreamResult{{
		Package: "dev-util/foo",
		Current: "1.0.0",
		Latest:  "2.0.0",
		Status:  upstream.StatusOutdated,
	}}, nil)

	stdout, stderr, err := execCmd(t, "upstream", "--overlay", ov)
	if err != nil {
		t.Fatalf("execCmd: %v (stderr %q)", err, stderr)
	}
	if !strings.Contains(stdout, "dev-util/foo") {
		t.Errorf("stdout = %q, want the table", stdout)
	}
	if strings.Contains(stderr, "falling back") {
		t.Errorf("upstream emitted a TUI fallback warning: %q", stderr)
	}
}
