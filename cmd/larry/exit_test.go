package main

import (
	"errors"
	"fmt"
	"testing"
)

func TestExitError_Error(t *testing.T) {
	e := &ExitError{Code: 7, Err: errors.New("boom")}
	got := e.Error()
	if got != "boom" {
		t.Errorf("ExitError.Error() = %q, want %q", got, "boom")
	}
}

func TestExitError_Unwrap(t *testing.T) {
	inner := errors.New("boom")
	e := &ExitError{Code: 7, Err: inner}
	if errors.Unwrap(e) != inner {
		t.Error("ExitError.Unwrap() should return the wrapped error")
	}
}

func TestExitCodeFor_ExitError(t *testing.T) {
	if got := exitCodeFor(&ExitError{Code: 7}); got != 7 {
		t.Errorf("exitCodeFor(&ExitError{Code: 7}) = %d, want 7", got)
	}
}

func TestExitCodeFor_PlainError(t *testing.T) {
	if got := exitCodeFor(errors.New("x")); got != 1 {
		t.Errorf("exitCodeFor(errors.New(\"x\")) = %d, want 1", got)
	}
}

func TestExitCodeFor_Nil(t *testing.T) {
	if got := exitCodeFor(nil); got != 0 {
		t.Errorf("exitCodeFor(nil) = %d, want 0", got)
	}
}

func TestExitCodeFor_WrappedExitError(t *testing.T) {
	inner := &ExitError{Code: 3}
	wrapped := fmt.Errorf("prepare failed: %w", inner)
	if got := exitCodeFor(wrapped); got != 3 {
		t.Errorf("exitCodeFor(wrapped) = %d, want 3", got)
	}
}
