package prepare

import (
	"os"
	"os/exec"
)

// manifestRunner returns the manifest command path (pkgdev or ebuild).
var manifestRunner = func() (string, error) {
	return findManifestRunner()
}

// manifestRunnerArgs builds the command arguments for manifest.
var manifestRunnerArgs = func(path, distdir string) []string {
	return manifestArgs(path, distdir)
}

// runManifest executes pkgdev manifest on the given ebuild path.
func runManifest(path, distdir string) error {
	binPath, err := manifestRunner()
	if err != nil {
		return err
	}

	args := manifestRunnerArgs(path, distdir)
	cmd := exec.Command(binPath, args...)

	// TUI stream isolation: pipe subprocess output to parent stdio
	if cmd.Stdout != nil {
		cmd.Stdout = os.Stdout
	}
	if cmd.Stderr != nil {
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

// runManifestCmd is an internal helper that runs manifest without TUI piping.
func runManifestCmd(args ...string) error {
	binPath, err := manifestRunner()
	if err != nil {
		return err
	}
	cmd := exec.Command(binPath, args...)
	return cmd.Run()
}

// FindManifestRunner is the exported version of findManifestRunner.
func FindManifestRunner() (string, error) {
	return findManifestRunner()
}

// GetManifestArgs is the exported version of manifestArgs.
func GetManifestArgs(path, distdir string) []string {
	return manifestArgs(path, distdir)
}

// RunManifestCommand is the exported version of runManifest.
func RunManifestCommand(path, distdir string) error {
	return runManifest(path, distdir)
}
