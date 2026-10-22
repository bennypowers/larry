package test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/config"
	"bennypowers.dev/larry/internal/output"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// localStubRunner records Run and RunWithEnv calls for inspection.
type localStubRunner struct {
	RunCalls        []stubCall
	RunWithEnvCalls []stubRunWithEnvCall
	Err             error
}

type stubCall struct {
	Name string
	Args []string
}

type stubRunWithEnvCall struct {
	Env  []string
	Name string
	Args []string
}

func (s *localStubRunner) Run(name string, args ...string) error {
	s.RunCalls = append(s.RunCalls, stubCall{Name: name, Args: args})
	if s.Err != nil {
		return s.Err
	}
	return nil
}

func (s *localStubRunner) RunWithEnv(env []string, name string, args ...string) error {
	s.RunWithEnvCalls = append(s.RunWithEnvCalls, stubRunWithEnvCall{Env: env, Name: name, Args: args})
	if s.Err != nil {
		return s.Err
	}
	return nil
}

// newTestLogger creates a logger with injectable output writers.
func newTestLogger(t *testing.T, outBuf, errBuf *[]byte) *output.Logger {
	t.Helper()
	log, err := output.NewLogger(0, false, nil)
	if err != nil {
		t.Fatalf("creating logger: %v", err)
	}
	log.SetOut(&captureWriter{buf: outBuf})
	log.SetErr(&captureWriter{buf: errBuf})
	return log
}

// captureWriter implements io.Writer, appending to a byte slice.
type captureWriter struct {
	buf *[]byte
}

func (w *captureWriter) Write(p []byte) (n int, err error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}

// ---------------------------------------------------------------------------
// Phase resolution helpers
// ---------------------------------------------------------------------------

func TestResolveEbuild_ExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	path, err := resolveEbuild(ebuildPath)
	if err != nil {
		t.Fatalf("resolveEbuild(%q) = %v", ebuildPath, err)
	}
	if path != ebuildPath {
		t.Errorf("resolveEbuild = %q, want %q", path, ebuildPath)
	}
}

func TestResolveEbuild_Relative(t *testing.T) {
	// Verify that relative paths are converted to absolute.
	tmpDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.WriteFile("foo-1.0.0.ebuild", []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	path, err := resolveEbuild("foo-1.0.0.ebuild")
	if err != nil {
		t.Fatalf("resolveEbuild = %v", err)
	}
	abs, err := filepath.Abs("foo-1.0.0.ebuild")
	if err != nil {
		t.Fatalf("filepath.Abs = %v", err)
	}
	if path != abs {
		t.Errorf("resolveEbuild = %q, want %q", path, abs)
	}
}

func TestResolveEbuild_CatPkg(t *testing.T) {
	// When given a category/package string, find the latest ebuild
	tmpDir := t.TempDir()
	catDir := filepath.Join(tmpDir, "app-misc", "foo")
	if err := os.MkdirAll(catDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(catDir, "foo-1.0.0.ebuild"), []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(catDir, "foo-2.0.0.ebuild"), []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	path, err := resolveEbuildFromCatPkg("app-misc/foo", tmpDir)
	if err != nil {
		t.Fatalf("resolveEbuildFromCatPkg failed: %v", err)
	}
	if !strings.HasSuffix(path, "foo-2.0.0.ebuild") {
		t.Errorf("resolved = %q, want foo-2.0.0.ebuild", path)
	}
}

func TestResolveEbuild_CatPkgNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := resolveEbuildFromCatPkg("app-misc/nonexistent", tmpDir)
	if err == nil {
		t.Fatal("expected error for missing cat/pkg")
	}
}

// ---------------------------------------------------------------------------
// Phase validation
// ---------------------------------------------------------------------------

func TestValidatePhase_Valid(t *testing.T) {
	// The full 14-element set must all be recognized.
	expected := []string{"clean", "fetch", "unpack", "prepare", "configure",
		"compile", "install", "test", "package", "preinst", "postinst",
		"prerm", "postrm", "nofetch"}
	for _, phase := range expected {
		if !isValidPhase(phase) {
			t.Errorf("isValidPhase(%q) = false, want true", phase)
		}
	}
}

func TestValidatePhase_Invalid(t *testing.T) {
	invalid := []string{"build", "postunpack", "digest", "manifest", "qmerge",
		"run", "invalid", "clean-fetch"}
	for _, phase := range invalid {
		if isValidPhase(phase) {
			t.Errorf("isValidPhase(%q) = true, want false", phase)
		}
	}
}

func TestValidPhases_MatchesScript(t *testing.T) {
	// The key set must equal exactly the 14-element list.
	// No missing, no extra.
	expected := map[string]bool{
		"clean": true, "fetch": true, "unpack": true, "prepare": true,
		"configure": true, "compile": true, "install": true, "test": true,
		"package": true, "preinst": true, "postinst": true, "prerm": true,
		"postrm": true, "nofetch": true,
	}

	if len(validPhases) != len(expected) {
		t.Fatalf("validPhases has %d entries, want %d", len(validPhases), len(expected))
	}

	for k := range validPhases {
		if !expected[k] {
			t.Errorf("validPhases has unexpected key %q", k)
		}
	}
	for k := range expected {
		if !validPhases[k] {
			t.Errorf("validPhases is missing key %q", k)
		}
	}
}

// ---------------------------------------------------------------------------
// BuildPhaseList — assemble phases from flags
// ---------------------------------------------------------------------------

func TestBuildPhaseList_Default(t *testing.T) {
	phases := buildPhaseList(false, false, nil, nil)
	expected := []string{"clean", "fetch", "unpack"}
	if len(phases) != len(expected) {
		t.Fatalf("expected %d phases, got %d", len(expected), len(phases))
	}
	for i, exp := range expected {
		if phases[i] != exp {
			t.Errorf("phases[%d] = %q, want %q", i, phases[i], exp)
		}
	}
}

func TestBuildPhaseList_Compile(t *testing.T) {
	phases := buildPhaseList(true, false, nil, nil)
	expected := []string{"clean", "fetch", "unpack", "compile"}
	if len(phases) != len(expected) {
		t.Fatalf("expected %d phases, got %d", len(expected), len(phases))
	}
	for i, exp := range expected {
		if phases[i] != exp {
			t.Errorf("phases[%d] = %q, want %q", i, phases[i], exp)
		}
	}
}

func TestBuildPhaseList_Merge(t *testing.T) {
	phases := buildPhaseList(false, true, nil, nil)
	expected := []string{"clean", "fetch", "unpack", "compile", "install"}
	if len(phases) != len(expected) {
		t.Fatalf("expected %d phases, got %d", len(expected), len(phases))
	}
	for i, exp := range expected {
		if phases[i] != exp {
			t.Errorf("phases[%d] = %q, want %q", i, phases[i], exp)
		}
	}
	// Regression: no qmerge.
	for _, p := range phases {
		if p == "qmerge" {
			t.Errorf("phases must not contain qmerge")
		}
	}
}

func TestBuildPhaseList_MergeBeatsCompile(t *testing.T) {
	phases := buildPhaseList(true, true, nil, nil)
	expected := []string{"clean", "fetch", "unpack", "compile", "install"}
	if len(phases) != len(expected) {
		t.Fatalf("expected %d phases, got %d", len(expected), len(phases))
	}
	for i, exp := range expected {
		if phases[i] != exp {
			t.Errorf("phases[%d] = %q, want %q", i, phases[i], exp)
		}
	}
}

func TestBuildPhaseList_ExplicitPhasesOverride(t *testing.T) {
	phases := buildPhaseList(true, false, []string{"fetch", "unpack"}, nil)
	expected := []string{"fetch", "unpack"}
	if len(phases) != len(expected) {
		t.Fatalf("expected %d phases, got %d", len(expected), len(phases))
	}
	for i, exp := range expected {
		if phases[i] != exp {
			t.Errorf("phases[%d] = %q, want %q", i, phases[i], exp)
		}
	}
}

func TestBuildPhaseList_ExtraPhasesAppend(t *testing.T) {
	phases := buildPhaseList(true, false, nil, []string{"install", "test"})
	expected := []string{"clean", "fetch", "unpack", "compile", "install", "test"}
	if len(phases) != len(expected) {
		t.Fatalf("expected %d phases, got %d", len(expected), len(phases))
	}
	for i, exp := range expected {
		if phases[i] != exp {
			t.Errorf("phases[%d] = %q, want %q", i, phases[i], exp)
		}
	}
}

// ---------------------------------------------------------------------------
// TestLocal — main entry point (stub runner tests)
// ---------------------------------------------------------------------------

func TestTestLocal_SingleInvocation(t *testing.T) {
	// D7: exactly one invocation with all phases in one call.
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	runner := &localStubRunner{}
	cfg := &TestLocalConfig{
		Phases: []string{"clean", "fetch", "unpack"},
	}
	err := TestLocalWithConfig(ebuildPath, nil, runner, false, cfg)
	if err != nil {
		t.Fatalf("TestLocal failed: %v", err)
	}

	if len(runner.RunWithEnvCalls) != 1 {
		t.Fatalf("expected 1 RunWithEnv call, got %d", len(runner.RunWithEnvCalls))
	}
	call := runner.RunWithEnvCalls[0]
	if call.Name != "ebuild" {
		t.Errorf("name = %q, want ebuild", call.Name)
	}
	expectedArgs := []string{ebuildPath, "clean", "fetch", "unpack"}
	if len(call.Args) != len(expectedArgs) ||
		call.Args[0] != ebuildPath ||
		call.Args[1] != "clean" ||
		call.Args[2] != "fetch" ||
		call.Args[3] != "unpack" {
		t.Errorf("args = %v, want %v", call.Args, expectedArgs)
	}
	// Verify no per-phase calls.
	if len(runner.RunCalls) != 0 {
		t.Errorf("expected 0 Run calls, got %d", len(runner.RunCalls))
	}
}

func TestTestLocal_SetsSandboxEnv(t *testing.T) {
	// D5: env must contain the full sandbox FEATURES.
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	distdir := filepath.Join(tmpDir, "dist")
	ptmpdir := filepath.Join(tmpDir, "portage-tmp")

	runner := &localStubRunner{}
	cfg := &TestLocalConfig{
		Phases:        []string{"clean", "fetch", "unpack"},
		PortageTmpdir: ptmpdir,
		Distdir:       distdir,
	}
	err := TestLocalWithConfig(ebuildPath, nil, runner, false, cfg)
	if err != nil {
		t.Fatalf("TestLocal failed: %v", err)
	}

	call := runner.RunWithEnvCalls[0]
	found := false
	for _, e := range call.Env {
		if e == "FEATURES=-sandbox -usersandbox -network-sandbox -ipc-sandbox -pid-sandbox" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FEATURES env not set correctly in %d env entries", len(call.Env))
		for _, e := range call.Env {
			if strings.HasPrefix(e, "FEATURES=") {
				t.Logf("FEATURES = %q", e)
			}
		}
	}

	// Verify PORTAGE_TMPDIR.
	found = false
	for _, e := range call.Env {
		if e == "PORTAGE_TMPDIR="+ptmpdir {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("PORTAGE_TMPDIR not set to %q", ptmpdir)
		for _, e := range call.Env {
			if strings.HasPrefix(e, "PORTAGE_TMPDIR=") {
				t.Logf("PORTAGE_TMPDIR = %q", e)
			}
		}
	}

	// Verify DISTDIR.
	found = false
	for _, e := range call.Env {
		if e == "DISTDIR="+distdir {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("DISTDIR not set to %q", distdir)
		for _, e := range call.Env {
			if strings.HasPrefix(e, "DISTDIR=") {
				t.Logf("DISTDIR = %q", e)
			}
		}
	}

	// Verify GENTOO_MIRRORS= (empty value, entry present).
	found = false
	for _, e := range call.Env {
		if e == "GENTOO_MIRRORS=" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("GENTOO_MIRRORS= not present in env")
	}
}

func TestTestLocal_CreatesDirs(t *testing.T) {
	// D5: distdir and portage tmpdir are created via os.MkdirAll.
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	distdir := filepath.Join(tmpDir, "nonexistent-dist")
	ptmpdir := filepath.Join(tmpDir, "nonexistent-portage")

	runner := &localStubRunner{}
	cfg := &TestLocalConfig{
		Phases:        []string{"clean", "fetch", "unpack"},
		PortageTmpdir: ptmpdir,
	}
	// Pre-set Cfg's distdir resolution to point at our temp distdir.
	cfg.Cfg = &config.Config{}
	cfg.Cfg.Distdir = distdir

	err := TestLocalWithConfig(ebuildPath, nil, runner, false, cfg)
	if err != nil {
		t.Fatalf("TestLocal failed: %v", err)
	}

	// Both directories should exist now.
	for _, d := range []string{distdir, ptmpdir} {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("directory %q does not exist: %v", d, err)
		}
		if !info.IsDir() {
			t.Errorf("%q exists but is not a directory", d)
		}
	}
}

func TestTestLocal_PropagatesExitCode(t *testing.T) {
	// D8: runner error wrapped in ExitError.
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "foo-1.0.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Use the real ExitError type so errors.As can find it.
	runner := &localStubRunner{
		Err: &ExitError{Code: 7, Err: nil},
	}
	err := TestLocalWithConfig(ebuildPath, nil, runner, false, nil)
	if err == nil {
		t.Fatal("expected error from failed runner")
	}

	// The error should unwrap to an ExitError.
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error does not unwrap to *ExitError: %T", err)
	}
	if exitErr.Code != 7 {
		t.Errorf("ExitError.Code = %d, want 7", exitErr.Code)
	}
}

func TestTestLocal_RelativePath(t *testing.T) {
	// D11: relative paths are resolved to absolute.
	tmpDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.WriteFile("foo-1.0.0.ebuild", []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	absPath, err := filepath.Abs("foo-1.0.0.ebuild")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}

	runner := &localStubRunner{}
	cfg := &TestLocalConfig{
		Phases: []string{"clean", "fetch", "unpack"},
	}
	if err := TestLocalWithConfig("foo-1.0.0.ebuild", nil, runner, false, cfg); err != nil {
		t.Fatalf("TestLocal failed: %v", err)
	}

	if len(runner.RunWithEnvCalls) != 1 {
		t.Fatalf("expected 1 RunWithEnv call, got %d", len(runner.RunWithEnvCalls))
	}
	call := runner.RunWithEnvCalls[0]
	if call.Args[0] != absPath {
		t.Errorf("ebuild path = %q, want %q", call.Args[0], absPath)
	}
}

func TestTestLocal_Banner(t *testing.T) {
	// D8: banner output is correct.
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "javy-9.1.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var outBuf, errBuf []byte
	log := newTestLogger(t, &outBuf, &errBuf)

	runner := &localStubRunner{}
	cfg := &TestLocalConfig{
		Phases: []string{"clean", "fetch"},
		Log:    log,
		Cfg: &config.Config{
			Overlay: tmpDir,
		},
	}

	// We need to create a proper overlay structure for the banner to work.
	catDir := filepath.Join(tmpDir, "dev-util", "javy")
	if err := os.MkdirAll(catDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(catDir, "javy-9.1.0.ebuild"), []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Use the overlay path.
	cfg.Cfg.Overlay = tmpDir

	// Manually set the ebuild path to use the overlay-based one.
	ebuildPath = filepath.Join(catDir, "javy-9.1.0.ebuild")

	err := TestLocalWithConfig(ebuildPath, nil, runner, false, cfg)
	if err != nil {
		t.Fatalf("TestLocal failed: %v", err)
	}

	output := string(outBuf)
	if !strings.Contains(output, "Package: dev-util/javy-9.1.0") {
		t.Errorf("banner missing package line. Output:\n%s", output)
	}
	if !strings.Contains(output, "Ebuild:  "+ebuildPath) {
		t.Errorf("banner missing ebuild line. Output:\n%s", output)
	}
	if !strings.Contains(output, "Phases:  clean fetch") {
		t.Errorf("banner missing phases line. Output:\n%s", output)
	}
}

func TestTestLocal_SuccessSummary(t *testing.T) {
	// D8: success message printed on runner success.
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "javy-9.1.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var outBuf, errBuf []byte
	log := newTestLogger(t, &outBuf, &errBuf)

	runner := &localStubRunner{}
	cfg := &TestLocalConfig{
		Phases: []string{"clean", "fetch"},
		Log:    log,
		Cfg: &config.Config{
			Overlay: tmpDir,
		},
	}

	catDir := filepath.Join(tmpDir, "dev-util", "javy")
	if err := os.MkdirAll(catDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ebuildPath = filepath.Join(catDir, "javy-9.1.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg.Cfg.Overlay = tmpDir

	err := TestLocalWithConfig(ebuildPath, nil, runner, false, cfg)
	if err != nil {
		t.Fatalf("TestLocal failed: %v", err)
	}

	output := string(outBuf)
	if !strings.Contains(output, "Success: dev-util/javy-9.1.0 (clean fetch)") {
		t.Errorf("missing success summary. Output:\n%s", output)
	}
}

func TestTestLocal_FailureSummary(t *testing.T) {
	// D8: failure messages on stderr.
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "javy-9.1.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var outBuf, errBuf []byte
	log := newTestLogger(t, &outBuf, &errBuf)

	// Use the real ExitError type so the exit code propagates correctly.
	runner := &localStubRunner{
		Err: &ExitError{Code: 7, Err: nil},
	}
	cfg := &TestLocalConfig{
		Phases: []string{"clean", "fetch"},
		Log:    log,
		Cfg: &config.Config{
			Overlay: tmpDir,
		},
	}

	catDir := filepath.Join(tmpDir, "dev-util", "javy")
	if err := os.MkdirAll(catDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ebuildPath = filepath.Join(catDir, "javy-9.1.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg.Cfg.Overlay = tmpDir

	err := TestLocalWithConfig(ebuildPath, nil, runner, false, cfg)
	if err == nil {
		t.Fatal("expected error from failed runner")
	}

	errOutput := string(errBuf)
	if !strings.Contains(errOutput, "Failed: dev-util/javy-9.1.0 (clean fetch)") {
		t.Errorf("missing failure summary on stderr. Output:\n%s", errOutput)
	}
	if !strings.Contains(errOutput, "Exit code: 7") {
		t.Errorf("missing exit code on stderr. Output:\n%s", errOutput)
	}
}

func TestTestLocal_PretendPrintsCommand(t *testing.T) {
	// D7: pretend mode prints but does not run.
	tmpDir := t.TempDir()
	ebuildPath := filepath.Join(tmpDir, "javy-9.1.0.ebuild")
	if err := os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var outBuf, errBuf []byte
	log := newTestLogger(t, &outBuf, &errBuf)

	runner := &localStubRunner{}
	cfg := &TestLocalConfig{
		Phases: []string{"clean", "fetch"},
		Log:    log,
		Cfg:    &config.Config{},
	}

	err := TestLocalWithConfig(ebuildPath, nil, runner, true, cfg)
	if err != nil {
		t.Fatalf("TestLocal failed: %v", err)
	}

	// Runner should have zero calls.
	if len(runner.RunWithEnvCalls) != 0 {
		t.Errorf("pretend mode executed %d RunWithEnv calls, expected 0", len(runner.RunWithEnvCalls))
	}

	// Pretend warning goes to stderr (log.Warn).
	errOutput := string(errBuf)
	if !strings.Contains(errOutput, "Pretend mode: would run:") {
		t.Errorf("missing pretend warning in stderr. Output:\n%s", errOutput)
	}

	// Command line goes to stdout (log.Msg).
	outOutput := string(outBuf)
	if !strings.Contains(outOutput, "  ebuild "+ebuildPath+" clean fetch") {
		t.Errorf("missing ebuild command line in stdout. Output:\n%s", outOutput)
	}
}
