package main

import (
	"bennypowers.dev/larry/internal/exit"
)

// ExitError wraps an error with a specific exit code. It is an alias of the
// single shared type in internal/exit (also aliased by internal/test), so
// subprocess exit codes propagate unchanged to os.Exit.
type ExitError = exit.ExitError

// exitCodeFor maps an error to a process exit code.
// nil → 0; *ExitError (found with errors.As, so wrapping works) → its Code;
// *exec.ExitError (unwrapped via ExitCode) → the subprocess exit code, so
// ebuild/podman codes reach os.Exit; anything else → 1.
func exitCodeFor(err error) int {
	return exit.CodeFor(err)
}
