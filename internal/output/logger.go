package output

import (
	"fmt"
	"io"
	"os"
)

// Logger wraps simple formatted output with verbosity control, quiet mode,
// colour, injectable writers, and the script's >>> / *** / !!! prefixes.
type Logger struct {
	verbose int
	quiet   bool
	color   *ColorHelper
	out     io.Writer // default os.Stdout
	errw    io.Writer // default os.Stderr
}

// NewLogger creates a new Logger with the given verbosity, quiet mode, and color helper.
func NewLogger(verbose int, quiet bool, color *ColorHelper) (*Logger, error) {
	return &Logger{
		verbose: verbose,
		quiet:   quiet,
		color:   color,
		out:     os.Stdout,
		errw:    os.Stderr,
	}, nil
}

// SetOut sets the writer used for Msg/Info/Debug (when verbose >= 2).
func (l *Logger) SetOut(w io.Writer) {
	l.out = w
}

// SetErr sets the writer used for Warn/Error/Debug.
func (l *Logger) SetErr(w io.Writer) {
	l.errw = w
}

// Msg writes a message with the ">>> " prefix to the output writer.
// Suppressed by --quiet.
func (l *Logger) Msg(format string, args ...any) {
	if l.quiet {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if l.color != nil && l.color.Enabled() {
		msg = l.color.Blue(msg)
	}
	// Log output failures are unrecoverable and not meaningful in this context.
	_, _ = fmt.Fprintf(l.out, ">>> %s\n", msg)
}

// Debug writes a message with the ">>> " prefix to the error writer.
// Suppressed by --quiet; additionally gated on verbose >= 2.
func (l *Logger) Debug(format string, args ...any) {
	if l.quiet || l.verbose < 2 {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if l.color != nil && l.color.Enabled() {
		msg = l.color.Blue(msg)
	}
	// Log output failures are unrecoverable and not meaningful in this context.
	_, _ = fmt.Fprintf(l.errw, ">>> %s\n", msg)
}

// Warn writes a warning with the "*** " prefix to the error writer.
// Suppressed by --quiet.
func (l *Logger) Warn(format string, args ...any) {
	if l.quiet {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if l.color != nil && l.color.Enabled() {
		msg = l.color.Yellow(msg)
	}
	// Log output failures are unrecoverable and not meaningful in this context.
	_, _ = fmt.Fprintf(l.errw, "*** %s\n", msg)
}

// Error writes an error with the "!!! " prefix to the error writer.
// NEVER suppressed — even --quiet does not silence errors.
func (l *Logger) Error(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if l.color != nil && l.color.Enabled() {
		msg = l.color.Red(msg)
	}
	// Log output failures are unrecoverable and not meaningful in this context.
	_, _ = fmt.Fprintf(l.errw, "!!! %s\n", msg)
}

// Info is a thin alias of Msg.
func (l *Logger) Info(format string, args ...any) {
	l.Msg(format, args...)
}

// Infof is a thin alias of Msg using fmt.Sprintf internally.
func (l *Logger) Infof(format string, args ...any) {
	l.Msg(format, args...)
}

// Debugf is a thin alias of Debug using fmt.Sprintf internally.
func (l *Logger) Debugf(format string, args ...any) {
	l.Debug(format, args...)
}
