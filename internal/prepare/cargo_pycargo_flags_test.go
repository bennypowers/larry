package prepare

import (
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// TestRunPycargoebuild_UsesFillCratesNoLicense pins the flags larry must pass.
//
// pycargoebuild's in-place mode rewrites both CRATES and the crate-license
// block. The license rewrite requires an exact `# Dependent crate licenses`
// marker in the ebuild and aborts with a RuntimeError when it is absent, which
// is common for hand-maintained ebuilds (e.g. mail-client/hylki). larry only
// owns CRATES, so it must select fill-crates mode (-C) and disable the license
// rewrite (-L). It must also stop relying on the deprecated
// --no-write-crate-tarball flag, which only implies a mode and triggers
// pycargoebuild's deprecation warning.
func TestRunPycargoebuild_UsesFillCratesNoLicense(t *testing.T) {
	var got []string
	runner := &mockCmdRunner{
		runFn: func(cmd *exec.Cmd) error {
			got = cmd.Args
			return nil
		},
	}

	if err := runPycargoebuild(t.TempDir(), "/tmp/pkg-1.0.0.ebuild", runner); err != nil {
		t.Fatalf("runPycargoebuild returned error: %v", err)
	}

	for _, want := range []string{"-C", "-L", "--no-manifest", "--inplace"} {
		if !slices.Contains(got, want) {
			t.Errorf("args %v missing %q", got, want)
		}
	}
	if slices.Contains(got, "--no-write-crate-tarball") {
		t.Errorf("args %v still use deprecated --no-write-crate-tarball", got)
	}
}

// pycargoebuild runs with cmd.Dir set to the temp work dir, so a relative
// ebuild path cannot be opened: it resolves against the work dir instead of
// larry's cwd. runPycargoebuild must hand it an absolute path.
func TestRunPycargoebuild_AbsolutizesEbuildPath(t *testing.T) {
	var got []string
	runner := &mockCmdRunner{
		runFn: func(cmd *exec.Cmd) error {
			got = cmd.Args
			return nil
		},
	}

	if err := runPycargoebuild(t.TempDir(), "dev-util/hylki/hylki-1.45.1.ebuild", runner); err != nil {
		t.Fatalf("runPycargoebuild returned error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("runner was never invoked")
	}
	arg := got[len(got)-1]
	if !filepath.IsAbs(arg) {
		t.Errorf("ebuild argument = %q, want an absolute path (cmd.Dir is the temp dir)", arg)
	}
}
