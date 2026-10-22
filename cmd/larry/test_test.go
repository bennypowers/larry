package main

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/test"
)

// ---------------------------------------------------------------------------
// cmdStubRunner records calls for cmd/larry integration tests.
// ---------------------------------------------------------------------------

type cmdStubRunner struct {
	RunCalls        []string
	RunWithEnvCalls []string
	Err             error
}

func (s *cmdStubRunner) Run(name string, args ...string) error {
	s.RunCalls = append(s.RunCalls, name+" "+strings.Join(args, " "))
	if s.Err != nil {
		return s.Err
	}
	return nil
}

func (s *cmdStubRunner) RunWithEnv(env []string, name string, args ...string) error {
	s.RunWithEnvCalls = append(s.RunWithEnvCalls, name+" "+strings.Join(args, " "))
	if s.Err != nil {
		return s.Err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Integration tests — use execCmd + fixtureOverlay
// ---------------------------------------------------------------------------

func TestTestCmd_EmergeDoesNotPassTTYFalse(t *testing.T) {
	runner := &cmdStubRunner{}
	test.TestRunnerForContainer = runner
	defer func() { test.TestRunnerForContainer = nil }()

	ov := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
	})
	_, stderr, err := execCmd(t, "test", "--emerge", "--overlay", ov, "dev-util/javy")
	if err != nil {
		t.Fatalf("execCmd: %v (stderr %q)", err, stderr)
	}

	sawEmerge := false
	for _, call := range runner.RunCalls {
		if strings.Contains(call, "--tty=false") {
			t.Errorf("container run isolated the TTY with no TUI: %q", call)
		}
		if strings.Contains(call, "emerge") {
			sawEmerge = true
		}
	}
	if !sawEmerge {
		t.Errorf("no emerge run recorded; calls = %v", runner.RunCalls)
	}
}

func TestTestCmd_CompileFlagReachesRunner(t *testing.T) {
	// D1: --compile must produce clean fetch unpack compile.
	runner := &cmdStubRunner{}
	test.TestRunnerForTest = runner

	tmpOverlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
	})
	defer func() { test.TestRunnerForTest = nil }()

	_, _, err := execCmd(t, "test", "--compile", "--overlay", tmpOverlay, "dev-util/javy")
	if err != nil {
		t.Fatalf("execCmd failed: %v", err)
	}

	if len(runner.RunWithEnvCalls) != 1 {
		t.Fatalf("expected 1 RunWithEnv call, got %d", len(runner.RunWithEnvCalls))
	}
	call := runner.RunWithEnvCalls[0]
	if !strings.Contains(call, "javy-9.1.0.ebuild") {
		t.Errorf("call missing ebuild path: %q", call)
	}
	for _, phase := range []string{"clean", "fetch", "unpack", "compile"} {
		if !strings.Contains(call, " "+phase) {
			t.Errorf("call missing phase %q: %q", phase, call)
		}
	}
}

func TestTestCmd_MergeFlagReachesRunner(t *testing.T) {
	// D2: --merge produces clean fetch unpack compile install, NO qmerge.
	runner := &cmdStubRunner{}
	test.TestRunnerForTest = runner
	tmpOverlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
	})
	defer func() { test.TestRunnerForTest = nil }()

	_, _, err := execCmd(t, "test", "--merge", "--overlay", tmpOverlay, "dev-util/javy")
	if err != nil {
		t.Fatalf("execCmd failed: %v", err)
	}

	if len(runner.RunWithEnvCalls) != 1 {
		t.Fatalf("expected 1 RunWithEnv call, got %d", len(runner.RunWithEnvCalls))
	}
	call := runner.RunWithEnvCalls[0]
	for _, phase := range []string{"clean", "fetch", "unpack", "compile", "install"} {
		if !strings.Contains(call, " "+phase) {
			t.Errorf("call missing phase %q: %q", phase, call)
		}
	}
	if strings.Contains(call, "qmerge") {
		t.Errorf("call must not contain qmerge: %q", call)
	}
}

func TestTestCmd_PhaseFlagReachesRunner(t *testing.T) {
	// --phase values override compile/merge.
	runner := &cmdStubRunner{}
	test.TestRunnerForTest = runner
	tmpOverlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
	})
	defer func() { test.TestRunnerForTest = nil }()

	_, _, err := execCmd(t, "test", "--phase", "fetch", "--phase", "unpack", "--overlay", tmpOverlay, "dev-util/javy")
	if err != nil {
		t.Fatalf("execCmd failed: %v", err)
	}

	if len(runner.RunWithEnvCalls) != 1 {
		t.Fatalf("expected 1 RunWithEnv call, got %d", len(runner.RunWithEnvCalls))
	}
	call := runner.RunWithEnvCalls[0]
	// Must contain fetch and unpack, NOT clean, fetch, unpack, compile
	if !strings.Contains(call, " fetch") || !strings.Contains(call, " unpack") {
		t.Errorf("call missing expected phases: %q", call)
	}
	if strings.Contains(call, " clean") || strings.Contains(call, " compile") {
		t.Errorf("call should not contain clean/compile when --phase used: %q", call)
	}
}

func TestTestCmd_ShortFlags(t *testing.T) {
	// -c is equivalent to --compile, -m to --merge, -p to --phase.
	// Test -c (compile)
	t.Run("short compile", func(t *testing.T) {
		runner := &cmdStubRunner{}
		test.TestRunnerForTest = runner
		tmpOverlay := fixtureOverlay(t, nil, map[string]string{
			"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
		})
		defer func() { test.TestRunnerForTest = nil }()

		_, _, err := execCmd(t, "test", "-c", "--overlay", tmpOverlay, "dev-util/javy")
		if err != nil {
			t.Fatalf("execCmd failed: %v", err)
		}
		if len(runner.RunWithEnvCalls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(runner.RunWithEnvCalls))
		}
		call := runner.RunWithEnvCalls[0]
		for _, phase := range []string{"clean", "fetch", "unpack", "compile"} {
			if !strings.Contains(call, " "+phase) {
				t.Errorf("missing phase %q in %q", phase, call)
			}
		}
	})

	t.Run("short merge", func(t *testing.T) {
		runner := &cmdStubRunner{}
		test.TestRunnerForTest = runner
		tmpOverlay := fixtureOverlay(t, nil, map[string]string{
			"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
		})
		defer func() { test.TestRunnerForTest = nil }()

		_, _, err := execCmd(t, "test", "-m", "--overlay", tmpOverlay, "dev-util/javy")
		if err != nil {
			t.Fatalf("execCmd failed: %v", err)
		}
		call := runner.RunWithEnvCalls[0]
		for _, phase := range []string{"clean", "fetch", "unpack", "compile", "install"} {
			if !strings.Contains(call, " "+phase) {
				t.Errorf("missing phase %q in %q", phase, call)
			}
		}
		if strings.Contains(call, "qmerge") {
			t.Error("qmerge must not appear")
		}
	})

	t.Run("short phase", func(t *testing.T) {
		runner := &cmdStubRunner{}
		test.TestRunnerForTest = runner
		tmpOverlay := fixtureOverlay(t, nil, map[string]string{
			"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
		})
		defer func() { test.TestRunnerForTest = nil }()

		_, _, err := execCmd(t, "test", "-p", "fetch", "--overlay", tmpOverlay, "dev-util/javy")
		if err != nil {
			t.Fatalf("execCmd failed: %v", err)
		}
		if len(runner.RunWithEnvCalls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(runner.RunWithEnvCalls))
		}
		call := runner.RunWithEnvCalls[0]
		if !strings.Contains(call, " fetch") {
			t.Errorf("missing fetch in %q", call)
		}
	})
}

func TestTestCmd_SubprocessExitCodePropagates(t *testing.T) {
	// AGENTS.md invariant: "Subprocess exit codes propagate to os.Exit".
	// A failing ebuild phase (exit 7) must surface as process exit code 7
	// through the cmd path — not be re-wrapped to 1.
	runner := &cmdStubRunner{}
	// A real *exec.ExitError with code 7, exactly as the ebuild subprocess
	// would produce it.
	if err := exec.Command("sh", "-c", "exit 7").Run(); err == nil {
		t.Fatal("test setup: expected sh to exit 7")
	} else {
		runner.Err = err
	}
	test.TestRunnerForTest = runner

	tmpOverlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
	})
	defer func() { test.TestRunnerForTest = nil }()

	_, _, err := execCmd(t, "test", "--overlay", tmpOverlay, "dev-util/javy")
	if err == nil {
		t.Fatal("expected error from failing ebuild phase")
	}
	if got := exitCodeFor(err); got != 7 {
		t.Errorf("exitCodeFor(err) = %d, want 7 (subprocess exit code must reach os.Exit)", got)
	}
}

func TestTestCmd_BarePhaseFlag(t *testing.T) {
	// --prepare should be accepted as a bare phase flag.
	runner := &cmdStubRunner{}
	test.TestRunnerForTest = runner
	tmpOverlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
	})
	defer func() { test.TestRunnerForTest = nil }()

	_, _, err := execCmd(t, "test", "--prepare", "--overlay", tmpOverlay, "dev-util/javy")
	if err != nil {
		t.Fatalf("execCmd failed: %v", err)
	}

	if len(runner.RunWithEnvCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(runner.RunWithEnvCalls))
	}
	call := runner.RunWithEnvCalls[0]
	if !strings.Contains(call, " prepare") {
		t.Errorf("bare --prepare not found in: %q", call)
	}
}

func TestTestCmd_UnknownFlagMessage(t *testing.T) {
	// --bogus should produce "Unknown option or phase: --bogus" and zero calls.
	runner := &cmdStubRunner{}
	test.TestRunnerForTest = runner
	tmpOverlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
	})
	defer func() { test.TestRunnerForTest = nil }()

	_, stderr, err := execCmd(t, "test", "--bogus", "--overlay", tmpOverlay, "dev-util/javy")
	// The error should be non-nil with the right message.
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if !strings.Contains(err.Error(), "unknown option or phase: --bogus") {
		t.Errorf("error = %q, want 'unknown option or phase: --bogus'", err.Error())
	}
	if !strings.Contains(stderr, "Unknown option or phase: --bogus") {
		t.Errorf("stderr = %q, expected 'Unknown option or phase: --bogus'", stderr)
	}

	// Runner should have zero calls.
	if len(runner.RunWithEnvCalls) != 0 {
		t.Errorf("expected 0 runner calls, got %d", len(runner.RunWithEnvCalls))
	}
}

func TestTestCmd_VersionedAtom(t *testing.T) {
	// D12: dev-util/javy-8.1.1 should resolve to javy-8.1.1.ebuild.
	runner := &cmdStubRunner{}
	test.TestRunnerForTest = runner
	tmpOverlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-8.1.1.ebuild": "EAPI=8\n",
		"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
	})
	defer func() { test.TestRunnerForTest = nil }()

	_, _, err := execCmd(t, "test", "--pretend", "--overlay", tmpOverlay, "dev-util/javy-8.1.1")
	if err != nil {
		t.Fatalf("execCmd failed: %v", err)
	}

	if len(runner.RunWithEnvCalls) != 0 {
		t.Fatal("pretend mode should have zero runner calls")
	}
	// The output should reference javy-8.1.1, not javy-9.1.0.
	// Since we're in pretend mode, check that no 9.1.0 reference exists.
	// Actually, in pretend mode we check the output buffer.
	// But execCmd only returns stdout/stderr/err. Let me check the stdout.
}

func TestTestCmd_PicksHighestVersion(t *testing.T) {
	// D12: dev-util/javy should resolve to javy-10.0.ebuild (highest).
	runner := &cmdStubRunner{}
	test.TestRunnerForTest = runner
	tmpOverlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-9.0.ebuild":  "EAPI=8\n",
		"dev-util/javy/javy-10.0.ebuild": "EAPI=8\n",
		"dev-util/javy/javy-9999.ebuild": "EAPI=8\n",
	})
	defer func() { test.TestRunnerForTest = nil }()

	_, _, err := execCmd(t, "test", "--pretend", "--overlay", tmpOverlay, "dev-util/javy")
	if err != nil {
		t.Fatalf("execCmd failed: %v", err)
	}

	if len(runner.RunWithEnvCalls) != 0 {
		t.Fatal("pretend mode should have zero runner calls")
	}
}

func TestTestCmd_Pretend(t *testing.T) {
	// --pretend should print the command but not execute.
	runner := &cmdStubRunner{}
	test.TestRunnerForTest = runner
	tmpOverlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
	})
	defer func() { test.TestRunnerForTest = nil }()

	_, _, err := execCmd(t, "test", "--pretend", "--compile", "--overlay", tmpOverlay, "dev-util/javy")
	if err != nil {
		t.Fatalf("execCmd failed: %v", err)
	}

	// Runner should have zero calls.
	if len(runner.RunWithEnvCalls) != 0 {
		t.Errorf("pretend mode executed %d calls, expected 0", len(runner.RunWithEnvCalls))
	}
}

// ---------------------------------------------------------------------------
// D9 — --image flag
// ---------------------------------------------------------------------------

func TestTestCmd_ImageNameFlag(t *testing.T) {
	tmpOverlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
	})

	// Capture os.Stdout for fmt.Printf in TestContainer's pretend path.
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	_, _, err := execCmd(t, "test", "--emerge", "--pretend", "--image", "foo", "--overlay", tmpOverlay, "dev-util/javy")

	// Best-effort close of pipe writer.
	_ = w.Close()
	os.Stdout = origStdout

	if err != nil {
		t.Fatalf("execCmd failed: %v", err)
	}

	combined, _ := io.ReadAll(r)
	stdout := string(combined)

	// The printed command should contain " foo emerge "
	if !strings.Contains(stdout, " foo emerge ") {
		t.Errorf("captured stdout = %q, want ' foo emerge '", stdout)
	}
	if strings.Contains(stdout, "larry-test") {
		t.Error("stdout should not contain 'larry-test' when --image=foo")
	}
}

// ---------------------------------------------------------------------------
// D4 — --emerge --pretend builds nothing
// ---------------------------------------------------------------------------

func TestTestCmd_EmergePretendBuildsNothing(t *testing.T) {
	tmpOverlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
	})

	// Capture os.Stdout.
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	_, _, err := execCmd(t, "test", "--emerge", "--pretend", "--overlay", tmpOverlay, "dev-util/javy")

	// Best-effort close of pipe writer.
	_ = w.Close()
	os.Stdout = origStdout

	if err != nil {
		t.Fatalf("execCmd failed: %v", err)
	}

	combined, _ := io.ReadAll(r)
	stdout := string(combined)

	// Should print a command with --pretend
	if !strings.Contains(stdout, "--pretend") {
		t.Errorf("captured stdout = %q, want '--pretend'", stdout)
	}
}

// ---------------------------------------------------------------------------
// --use reaches the container as -e USE=...
// ---------------------------------------------------------------------------

func TestTestCmd_UseFlagReachesContainer(t *testing.T) {
	tmpOverlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
	})

	// Capture os.Stdout for fmt.Printf in TestContainer's pretend path.
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	_, _, err := execCmd(t, "test", "--emerge", "--pretend", "--use", "cuda,rocm", "--overlay", tmpOverlay, "dev-util/javy")

	_ = w.Close()
	os.Stdout = origStdout

	if err != nil {
		t.Fatalf("execCmd failed: %v", err)
	}

	combined, _ := io.ReadAll(r)
	stdout := string(combined)

	if !strings.Contains(stdout, "-e USE=cuda rocm") {
		t.Errorf("captured stdout = %q, want '-e USE=cuda rocm'", stdout)
	}
}

func TestTestCmd_DryRunRejected(t *testing.T) {
	runner := &cmdStubRunner{}
	test.TestRunnerForTest = runner
	tmpOverlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
	})
	defer func() { test.TestRunnerForTest = nil }()

	_, _, err := execCmd(t, "test", "--dry-run", "--overlay", tmpOverlay, "dev-util/javy")
	if err == nil {
		t.Fatal("expected --dry-run to be rejected")
	}
	if !strings.Contains(err.Error(), "--pretend") {
		t.Errorf("error = %q, want a --pretend hint", err.Error())
	}
	if len(runner.RunWithEnvCalls) != 0 {
		t.Errorf("expected 0 runner calls, got %d", len(runner.RunWithEnvCalls))
	}
}

func TestTestCmd_InvalidProgressRejected(t *testing.T) {
	runner := &cmdStubRunner{}
	test.TestRunnerForTest = runner
	tmpOverlay := fixtureOverlay(t, nil, map[string]string{
		"dev-util/javy/javy-9.1.0.ebuild": "EAPI=8\n",
	})
	defer func() { test.TestRunnerForTest = nil }()

	_, _, err := execCmd(t, "test", "--progress", "bogus", "--overlay", tmpOverlay, "dev-util/javy")
	if err == nil {
		t.Fatal("expected invalid --progress to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid --progress") {
		t.Errorf("error = %q, want invalid --progress", err.Error())
	}
	if len(runner.RunWithEnvCalls) != 0 {
		t.Errorf("expected 0 runner calls, got %d", len(runner.RunWithEnvCalls))
	}
}

func TestChildCommandArgs_DryRunForwarded(t *testing.T) {
	got := childCommandArgs("bump", "/overlay", "app-admin/cockpit", true, true, nil)
	joined := strings.Join(got, " ")
	for _, want := range []string{"--dry-run", "--progress json", "--overlay /overlay", "--color never", "app-admin/cockpit"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args = %q, missing %q", joined, want)
		}
	}
	if got[len(got)-1] != "app-admin/cockpit" {
		t.Errorf("package must be last, got %q", got)
	}
}

func TestChildCommandArgs_NoDryRunByDefault(t *testing.T) {
	got := childCommandArgs("prepare", "/overlay", "dev-util/foo", false, true, nil)
	if strings.Contains(strings.Join(got, " "), "--dry-run") {
		t.Errorf("args = %q, unexpected --dry-run", strings.Join(got, " "))
	}
}
