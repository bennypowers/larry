package main

import (
	"errors"
	"fmt"
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

// exitCodeFor maps an error to a process exit code.
// nil → 0; *ExitError (found with errors.As, so wrapping works) → its Code;
// anything else → 1.
func exitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return 1
}
