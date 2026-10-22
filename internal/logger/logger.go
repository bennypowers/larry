package logger

import (
	"fmt"
	"io"
	"os"
)

const prefix = "[larry]"

var (
	level           = 0
	out   io.Writer = os.Stderr
)

func clamp(n int) int {
	if n < 0 {
		return 0
	}
	if n > 3 {
		return 3
	}
	return n
}

// SetLevel sets the package-level verbosity (0 results, 1 info, 2 debug,
// 3 trace). Values outside the range are clamped.
func SetLevel(n int) {
	level = clamp(n)
}

// Level returns the current package-level verbosity.
func Level() int {
	return level
}

// SetOutput redirects diagnostics. A nil writer restores os.Stderr.
func SetOutput(w io.Writer) {
	if w == nil {
		w = os.Stderr
	}
	out = w
}

func emit(marker, msg string) {
	// Diagnostics output failures are not actionable; the command's own
	// errors still reach the caller.
	_, _ = fmt.Fprint(out, prefix, " ", marker, msg, "\n")
}

// Debug prints a debug message to the configured writer at level >= 2.
func Debug(msg string) {
	if level < 2 {
		return
	}
	emit("", msg)
}

// Debugf prints a formatted debug message at level >= 2.
func Debugf(format string, args ...any) {
	if level < 2 {
		return
	}
	emit("", fmt.Sprintf(format, args...))
}

// Trace prints a trace message to the configured writer at level >= 3.
func Trace(msg string) {
	if level < 3 {
		return
	}
	emit("... ", msg)
}

// Tracef prints a formatted trace message at level >= 3.
func Tracef(format string, args ...any) {
	if level < 3 {
		return
	}
	emit("... ", fmt.Sprintf(format, args...))
}
