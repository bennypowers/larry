package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bennypowers.dev/larry/internal/cli"
	"bennypowers.dev/larry/internal/prepare"
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
	}, nil); err != nil {
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

func TestRunLarryChild_LongLineDoesNotHang(t *testing.T) {
	// A single 2 MB line exceeds the scanner's token limit. The reader must
	// drain the pipe afterwards or the child blocks forever writing.
	stubExecutable(t, "#!/bin/sh\nhead -c 2000000 /dev/zero | tr '\\0' x\n")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runLarryChild(ctx, []string{"x"}, func(string) {})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runLarryChild: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("runLarryChild hung after an over-long output line")
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("runLarryChild did not finish before the deadline")
	}
}

func TestProgressEmitterWritesWire(t *testing.T) {
	var buf bytes.Buffer
	emitter := progressEmitter{enabled: true, w: &buf}
	emitter.start("bump", "prepare")
	emitter.step("bump", stepDone)
	emitter.done()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("wrote %d lines, want 3:\n%s", len(lines), buf.String())
	}
	progress, ok := parseTestProgress(lines[0])
	if !ok {
		t.Fatalf("start line did not parse: %q", lines[0])
	}
	if strings.Join(progress.Steps, ",") != "bump,prepare" {
		t.Errorf("steps = %v", progress.Steps)
	}
	if last, ok := parseTestProgress(lines[2]); !ok || !last.Done {
		t.Errorf("done line = %+v ok=%v", last, ok)
	}
}

func TestPrepareProgressMapping(t *testing.T) {
	var buf bytes.Buffer
	emitter := progressEmitter{enabled: true, w: &buf}
	mapPrepareProgress(emitter, prepare.ProgressEvent{Kind: prepare.ProgressStep, Step: "prepare"})
	mapPrepareProgress(emitter, prepare.ProgressEvent{Kind: prepare.ProgressStep, Step: "manifest"})
	mapPrepareProgress(emitter, prepare.ProgressEvent{Kind: prepare.ProgressDone})

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("wrote %d lines, want 3:\n%s", len(lines), buf.String())
	}
	first, ok := parseTestProgress(lines[0])
	if !ok || first.Step != "prepare" || first.State != cli.ActionStepRunning {
		t.Errorf("first = %+v ok=%v, want prepare running", first, ok)
	}
	last, ok := parseTestProgress(lines[2])
	if !ok || !last.Done {
		t.Errorf("last = %+v ok=%v, want done", last, ok)
	}
}

func TestProgressDetailCarriesNoStep(t *testing.T) {
	var buf bytes.Buffer
	emitter := progressEmitter{enabled: true, w: &buf}
	mapPrepareSubstep(emitter, prepare.ProgressEvent{Kind: prepare.ProgressStep, Step: "manifest"})

	progress, ok := parseTestProgress(strings.TrimSpace(buf.String()))
	if !ok {
		t.Fatalf("detail line did not parse: %q", buf.String())
	}
	if progress.Step != "" || progress.Message != "manifest" {
		t.Errorf("progress = %+v, want detail manifest with no step row", progress)
	}
}

func TestBumpProgressPlumbing(t *testing.T) {
	stubExecutable(t, `#!/bin/sh
echo 'larry-progress:{"kind":0,"steps":["bump","prepare","commit"]}'
echo 'larry-progress:{"kind":1,"step":"bump","state":1}'
echo 'larry-progress:{"kind":1,"step":"bump","state":2}'
echo 'Bumped: something'
echo 'larry-progress:{"kind":1,"step":"prepare","state":1}'
echo 'larry-progress:{"kind":1,"state":1,"message":"manifest"}'
sleep 1
`)

	type event struct {
		step    string
		state   cli.ActionStepState
		message string
	}
	var events []event
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := buildUpstreamActions("/tmp/overlay").Bump(ctx, "dev-util/foo",
		func(string) {}, func(p cli.ActionProgress) {
			events = append(events, event{step: p.Step, state: p.State, message: p.Message})
		})
	if err != nil && ctx.Err() == nil {
		t.Fatalf("Bump: %v", err)
	}

	sawPrepareRunning := false
	sawDetail := false
	for _, ev := range events {
		if ev.step == "prepare" && ev.state == cli.ActionStepRunning {
			sawPrepareRunning = true
		}
		if ev.message == "manifest" {
			sawDetail = true
		}
	}
	if !sawPrepareRunning {
		t.Errorf("prepare running event did not reach the pane: %+v", events)
	}
	if !sawDetail {
		t.Errorf("prepare detail did not reach the pane: %+v", events)
	}
}

func TestRedactOverlayPath(t *testing.T) {
	overlay := "/home/someone/Developer/gentoo-overlay"
	tests := map[string]string{
		overlay + "/app-admin/cockpit/cockpit-370.ebuild":     "app-admin/cockpit/cockpit-370.ebuild",
		"Bumped: " + overlay + "/dev-util/foo/foo-2.0.ebuild": "Bumped: dev-util/foo/foo-2.0.ebuild",
		"no path here": "no path here",
		overlay:        ".",
	}
	for in, want := range tests {
		if got := redactOverlayPath(in, overlay); got != want {
			t.Errorf("redactOverlayPath(%q) = %q, want %q", in, got, want)
		}
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
