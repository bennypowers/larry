// Package exit defines larry's single exit-code-carrying error type.
//
// Exactly one ExitError type exists across the codebase so subprocess exit
// codes (ebuild, podman, gh) propagate intact to os.Exit: internal packages
// wrap failures in exit.ExitError, and the cmd/larry entrypoint maps any
// error to a process exit code with exit.CodeFor. The spec only defines
// exit status 0/1 plus subprocess propagation — no per-class codes.
package exit

import (
	"errors"
	"fmt"
	"os/exec"
)

// ExitError wraps an error with a specific exit code.
type ExitError struct {
	Code int
	Err  error
}

// Error implements error for ExitError.
func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit code %d", e.Code)
}

// Unwrap returns the wrapped error for errors.As compatibility.
func (e *ExitError) Unwrap() error {
	return e.Err
}

// CodeFor maps an error to a process exit code.
// nil → 0; *ExitError (found with errors.As, so wrapping works) → its Code;
// *exec.ExitError (unwrapped via ExitCode) → the subprocess exit code, so
// ebuild/podman codes reach os.Exit; anything else → 1.
func CodeFor(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	var execErr *exec.ExitError
	if errors.As(err, &execErr) {
		if code := execErr.ExitCode(); code >= 0 {
			return code
		}
	}
	return 1
}
