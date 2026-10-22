package exit

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

func TestExitError_Error(t *testing.T) {
	e := &ExitError{Code: 7, Err: errors.New("boom")}
	if got := e.Error(); got != "boom" {
		t.Errorf("ExitError.Error() = %q, want %q", got, "boom")
	}
	if got := (&ExitError{Code: 7}).Error(); got != "exit code 7" {
		t.Errorf("ExitError.Error() without Err = %q, want %q", got, "exit code 7")
	}
}

func TestExitError_Unwrap(t *testing.T) {
	inner := errors.New("boom")
	if errors.Unwrap(&ExitError{Code: 7, Err: inner}) != inner {
		t.Error("ExitError.Unwrap() should return the wrapped error")
	}
}

func TestCodeFor(t *testing.T) {
	if got := CodeFor(nil); got != 0 {
		t.Errorf("CodeFor(nil) = %d, want 0", got)
	}
	if got := CodeFor(errors.New("x")); got != 1 {
		t.Errorf("CodeFor(plain error) = %d, want 1", got)
	}
	if got := CodeFor(&ExitError{Code: 7}); got != 7 {
		t.Errorf("CodeFor(&ExitError{7}) = %d, want 7", got)
	}
	wrapped := fmt.Errorf("prepare failed: %w", &ExitError{Code: 3})
	if got := CodeFor(wrapped); got != 3 {
		t.Errorf("CodeFor(wrapped ExitError) = %d, want 3", got)
	}
}

func TestCodeFor_ExecExitError(t *testing.T) {
	// A raw subprocess exit error (ebuild/podman) must reach os.Exit intact.
	if err := exec.Command("sh", "-c", "exit 7").Run(); err == nil {
		t.Fatal("test setup: expected sh to exit 7")
	} else if got := CodeFor(err); got != 7 {
		t.Errorf("CodeFor(*exec.ExitError) = %d, want 7", got)
	}
}

func TestCodeFor_SignaledProcess(t *testing.T) {
	// A signaled process has no exit code (ExitCode() < 0) — fall back to 1.
	var execErr *exec.ExitError
	if err := exec.Command("sh", "-c", "kill -9 $$").Run(); errors.As(err, &execErr) {
		if got := CodeFor(err); got != 1 {
			t.Errorf("CodeFor(signaled process) = %d, want 1", got)
		}
	} else {
		t.Fatalf("test setup: expected *exec.ExitError, got %v", err)
	}
}
