package test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocal_EmitsProgress(t *testing.T) {
	dir := t.TempDir()
	ebuildPath := filepath.Join(dir, "dev-util", "foo", "foo-1.0.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var events []ProgressEvent
	var out, errOut []byte
	cfg := &TestLocalConfig{
		Compile:  true,
		Log:      newTestLogger(t, &out, &errOut),
		Progress: func(ev ProgressEvent) { events = append(events, ev) },
	}
	if err := TestLocalWithConfig(ebuildPath, nil, &localStubRunner{}, false, cfg); err != nil {
		t.Fatalf("TestLocalWithConfig: %v", err)
	}

	if len(events) < 2 {
		t.Fatalf("events = %+v, want start and done", events)
	}
	start := events[0]
	if start.Kind != ProgressStart {
		t.Errorf("first event kind = %v, want ProgressStart", start.Kind)
	}
	wantSteps := []string{"clean", "fetch", "unpack", "compile"}
	if len(start.Steps) != len(wantSteps) {
		t.Fatalf("start steps = %v, want %v", start.Steps, wantSteps)
	}
	for i, want := range wantSteps {
		if start.Steps[i] != want {
			t.Errorf("start.Steps[%d] = %q, want %q", i, start.Steps[i], want)
		}
	}
	if last := events[len(events)-1]; last.Kind != ProgressDone {
		t.Errorf("last event kind = %v, want ProgressDone", last.Kind)
	}
}

func TestLocal_EmitsProgressError(t *testing.T) {
	dir := t.TempDir()
	ebuildPath := filepath.Join(dir, "dev-util", "foo", "foo-1.0.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var events []ProgressEvent
	var out, errOut []byte
	runner := &localStubRunner{Err: &ExitError{Code: 2}}
	cfg := &TestLocalConfig{
		Compile:  true,
		Log:      newTestLogger(t, &out, &errOut),
		Progress: func(ev ProgressEvent) { events = append(events, ev) },
	}
	if err := TestLocalWithConfig(ebuildPath, nil, runner, false, cfg); err == nil {
		t.Fatal("expected failure")
	}
	last := events[len(events)-1]
	if last.Kind != ProgressError || last.Err == nil {
		t.Errorf("last event = %+v, want ProgressError with Err", last)
	}
}

func TestContainer_EmitsProgressSteps(t *testing.T) {
	dir := t.TempDir()
	ebuildPath := filepath.Join(dir, "dev-util", "javy", "javy-9.1.0.ebuild")
	if err := os.MkdirAll(filepath.Dir(ebuildPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profiles", "repo_name"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Containerfile.larry-test"), []byte("FROM ubuntu\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var events []ProgressEvent
	cfg := &ContainerConfig{
		Overlay:  dir,
		CacheDir: filepath.Join(dir, "cache"),
		Progress: func(ev ProgressEvent) { events = append(events, ev) },
	}
	if err := TestContainer(ebuildPath, cfg, newRecRunner(), false); err != nil {
		t.Fatalf("TestContainer: %v", err)
	}

	if events[0].Kind != ProgressStart || len(events[0].Steps) != 3 {
		t.Fatalf("start event = %+v, want three steps", events[0])
	}
	done := map[string]bool{}
	for _, ev := range events {
		if ev.Kind == ProgressStep && ev.State == StepDone {
			done[ev.Step] = true
		}
	}
	for _, step := range events[0].Steps {
		if !done[step] {
			t.Errorf("step %q never reported done; events = %+v", step, events)
		}
	}
	if last := events[len(events)-1]; last.Kind != ProgressDone {
		t.Errorf("last event = %+v, want ProgressDone", last)
	}
}
