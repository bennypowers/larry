package prepare

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bennypowers.dev/larry/internal/ebuild"
)

func collectEvents() (*[]ProgressEvent, ProgressFunc) {
	var events []ProgressEvent
	return &events, func(ev ProgressEvent) { events = append(events, ev) }
}

func progressFixture(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("EAPI=8\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestPrepareDir_EmitsStartStepDone(t *testing.T) {
	dir := progressFixture(t, "foo-1.0.ebuild", "foo-2.0.ebuild")
	events, progress := collectEvents()

	orig := PrepareFile
	PrepareFile = func(string, *PrepareConfig) error { return nil }
	t.Cleanup(func() { PrepareFile = orig })

	if err := PrepareDir(dir, &PrepareConfig{Progress: progress}); err != nil {
		t.Fatalf("PrepareDir: %v", err)
	}
	if len(*events) == 0 {
		t.Fatal("no progress events")
	}
	var last ProgressEvent
	for _, ev := range *events {
		if ev.Kind == ProgressStart && ev.Total != 2 {
			t.Errorf("start Total = %d, want 2", ev.Total)
		}
		last = ev
	}
	if last.Kind != ProgressDone || last.Done != 2 || last.Total != 2 {
		t.Errorf("last event = %+v, want Done 2/2", last)
	}
}

func TestPrepareDir_EmitsErrorAndContinues(t *testing.T) {
	dir := progressFixture(t, "bad-1.0.ebuild", "good-1.0.ebuild")
	events, progress := collectEvents()

	orig := PrepareFile
	PrepareFile = func(path string, _ *PrepareConfig) error {
		if strings.Contains(path, "bad-") {
			return errors.New("prepare boom")
		}
		return nil
	}
	t.Cleanup(func() { PrepareFile = orig })

	err := PrepareDir(dir, &PrepareConfig{Progress: progress})
	if err == nil {
		t.Fatal("expected an aggregate failure")
	}
	var gotErr bool
	for _, ev := range *events {
		if ev.Kind == ProgressError && strings.Contains(ev.Message, "prepare boom") {
			gotErr = true
		}
	}
	if !gotErr {
		t.Errorf("no ProgressError event in %+v", *events)
	}
}

func TestPrepareManifestAllWithConfig_Progress(t *testing.T) {
	overlay := progressFixture(t,
		"dev-util/a/a-1.0.ebuild",
		"dev-util/b/b-1.0.ebuild",
	)
	events, progress := collectEvents()

	orig := ManifestRunner
	ManifestRunner = func(string, []string, string, []string) error { return nil }
	t.Cleanup(func() { ManifestRunner = orig })

	cfg := &PrepareConfig{Overlay: overlay, Distdir: t.TempDir(), Progress: progress}
	if err := PrepareManifestAllWithConfig(cfg); err != nil {
		t.Fatalf("PrepareManifestAllWithConfig: %v", err)
	}
	var steps, done int
	for _, ev := range *events {
		switch ev.Kind {
		case ProgressStep:
			steps++
			if ev.Total != 2 {
				t.Errorf("step Total = %d, want 2", ev.Total)
			}
		case ProgressDone:
			done++
		}
	}
	if steps != 2 || done != 1 {
		t.Errorf("steps=%d done=%d, want 2 and 1", steps, done)
	}
}

func TestPrepare_ContextCanceled(t *testing.T) {
	dir := progressFixture(t, "foo-1.0.ebuild")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	orig := PrepareFile
	PrepareFile = func(string, *PrepareConfig) error { called = true; return nil }
	t.Cleanup(func() { PrepareFile = orig })

	err := PrepareDir(dir, &PrepareConfig{Context: ctx})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if called {
		t.Error("PrepareFile ran after cancellation")
	}
}

func TestPrepare_TUILongScriptLineDoesNotHang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	overlay := progressFixture(t, "dev-util/foo/foo-1.0.ebuild")
	pkgDir := filepath.Join(overlay, "dev-util", "foo")
	larryDir := filepath.Join(pkgDir, ".larry")
	if err := os.MkdirAll(larryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(larryDir, "prepare.sh")
	body := "#!/bin/sh\nhead -c 200000 /dev/zero | tr '\\0' x\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	orig := ManifestRunner
	ManifestRunner = func(string, []string, string, []string) error { return nil }
	t.Cleanup(func() { ManifestRunner = orig })

	ei := &ebuild.EbuildInfo{
		Path:     filepath.Join(pkgDir, "foo-1.0.ebuild"),
		Category: "dev-util",
		Pkg:      "foo",
		PVVal:    "1.0",
		Raw:      "EAPI=8\n",
	}
	cfg := &PrepareConfig{
		Distdir:  t.TempDir(),
		Overlay:  overlay,
		UseTUI:   true,
		Progress: func(ProgressEvent) {},
	}

	done := make(chan error, 1)
	go func() { done <- runCustomPrepare(ei, script, cfg) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runCustomPrepare: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runCustomPrepare hung on an over-long script line")
	}
}

func TestPrepare_TUIPipesCustomScriptOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	overlay := progressFixture(t, "dev-util/foo/foo-1.0.ebuild")
	pkgDir := filepath.Join(overlay, "dev-util", "foo")
	larryDir := filepath.Join(pkgDir, ".larry")
	if err := os.MkdirAll(larryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(larryDir, "prepare.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hello-from-script\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	events, progress := collectEvents()
	orig := ManifestRunner
	ManifestRunner = func(string, []string, string, []string) error { return nil }
	t.Cleanup(func() { ManifestRunner = orig })

	ebuildPath := filepath.Join(pkgDir, "foo-1.0.ebuild")
	ei := &ebuild.EbuildInfo{
		Path:     ebuildPath,
		Category: "dev-util",
		Pkg:      "foo",
		PVVal:    "1.0",
		Raw:      "EAPI=8\n",
	}
	cfg := &PrepareConfig{
		Distdir:  t.TempDir(),
		Overlay:  overlay,
		UseTUI:   true,
		Progress: progress,
	}
	if err := runCustomPrepare(ei, script, cfg); err != nil {
		t.Fatalf("runCustomPrepare: %v", err)
	}

	var sawLog bool
	for _, ev := range *events {
		if ev.Kind == ProgressLog && strings.Contains(ev.Message, "hello-from-script") {
			sawLog = true
		}
	}
	if !sawLog {
		t.Errorf("no ProgressLog for script output: %+v", *events)
	}
}

func TestPrepare_TUIGrandchildHoldingPipeDoesNotHang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	overlay := progressFixture(t, "dev-util/foo/foo-1.0.ebuild")
	pkgDir := filepath.Join(overlay, "dev-util", "foo")
	larryDir := filepath.Join(pkgDir, ".larry")
	if err := os.MkdirAll(larryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(larryDir, "prepare.sh")
	// The background child inherits stdout and outlives the script. Reading
	// until EOF would block forever; runCustomPrepare must force the pipe
	// closed after a short drain.
	body := "#!/bin/sh\n( sleep 30 ) &\necho done\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	orig := ManifestRunner
	ManifestRunner = func(string, []string, string, []string) error { return nil }
	t.Cleanup(func() { ManifestRunner = orig })

	ei := &ebuild.EbuildInfo{
		Path:     filepath.Join(pkgDir, "foo-1.0.ebuild"),
		Category: "dev-util",
		Pkg:      "foo",
		PVVal:    "1.0",
		Raw:      "EAPI=8\n",
	}
	cfg := &PrepareConfig{
		Distdir:  t.TempDir(),
		Overlay:  overlay,
		UseTUI:   true,
		Progress: func(ProgressEvent) {},
	}

	done := make(chan error, 1)
	go func() { done <- runCustomPrepare(ei, script, cfg) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runCustomPrepare: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("runCustomPrepare hung on a grandchild holding the pipe")
	}
}
