package output

import (
	"fmt"
	"io"
	"os"
)

// Verbosity levels. The CLI count flag maps directly onto these.
const (
	LevelResults = 0
	LevelInfo    = 1
	LevelDebug   = 2
	LevelTrace   = 3
)

// Logger wraps simple formatted output with verbosity control, quiet mode,
// colour, injectable writers, and the script's >>> / ... / *** / !!! prefixes.
type Logger struct {
	level int
	quiet bool
	color *ColorHelper
	out   io.Writer // default os.Stdout
	errw  io.Writer // default os.Stderr
}

// NewLogger creates a new Logger with the given verbosity, quiet mode, and color helper.
func NewLogger(verbose int, quiet bool, color *ColorHelper) (*Logger, error) {
	return &Logger{
		level: clampLevel(verbose),
		quiet: quiet,
		color: color,
		out:   os.Stdout,
		errw:  os.Stderr,
	}, nil
}

func clampLevel(n int) int {
	if n < LevelResults {
		return LevelResults
	}
	if n > LevelTrace {
		return LevelTrace
	}
	return n
}

// SetOut sets the writer used for Msg/Info.
func (l *Logger) SetOut(w io.Writer) {
	l.out = w
}

// SetErr sets the writer used for Warn/Error/Debug/Trace.
func (l *Logger) SetErr(w io.Writer) {
	l.errw = w
}

// Level returns the current verbosity level.
func (l *Logger) Level() int {
	return l.level
}

// SetLevel sets the verbosity level, clamping it to the supported range.
func (l *Logger) SetLevel(n int) {
	l.level = clampLevel(n)
}

// Msg writes a result message with the ">>> " prefix to the output writer.
// Suppressed by --quiet, but shown at every verbosity level.
func (l *Logger) Msg(format string, args ...any) {
	if l.quiet {
		return
	}
	l.emit(l.out, ">>> ", format, args, l.color.Blue)
}

// Info writes an informational message with the ">>> " prefix to the output
// writer. Shown at -v and above; suppressed by --quiet.
func (l *Logger) Info(format string, args ...any) {
	if l.quiet || l.level < LevelInfo {
		return
	}
	l.emit(l.out, ">>> ", format, args, l.color.Blue)
}

// Debug writes a debug message with the ">>> " prefix to the error writer.
// Suppressed by --quiet; additionally gated on verbose >= 2.
func (l *Logger) Debug(format string, args ...any) {
	if l.quiet || l.level < LevelDebug {
		return
	}
	l.emit(l.errw, ">>> ", format, args, l.color.Blue)
}

// Trace writes a trace message with the "... " prefix to the error writer.
// Suppressed by --quiet; additionally gated on verbose >= 3.
func (l *Logger) Trace(format string, args ...any) {
	if l.quiet || l.level < LevelTrace {
		return
	}
	l.emit(l.errw, "... ", format, args, l.color.Dim)
}

// Warn writes a warning with the "*** " prefix to the error writer.
// Suppressed by --quiet.
func (l *Logger) Warn(format string, args ...any) {
	if l.quiet {
		return
	}
	l.emit(l.errw, "*** ", format, args, l.color.Yellow)
}

// Error writes an error with the "!!! " prefix to the error writer.
// NEVER suppressed — even --quiet does not silence errors.
func (l *Logger) Error(format string, args ...any) {
	l.emit(l.errw, "!!! ", format, args, l.color.Red)
}

func (l *Logger) emit(w io.Writer, prefix, format string, args []any, colorize func(string) string) {
	msg := fmt.Sprintf(format, args...)
	if l.color != nil && l.color.Enabled() {
		msg = colorize(msg)
	}
	// Log output failures are unrecoverable and not meaningful in this context.
	_, _ = fmt.Fprintf(w, "%s%s\n", prefix, msg)
}

// Info alias helpers kept for call-site compatibility.

// Infof writes an informational message.
func (l *Logger) Infof(format string, args ...any) {
	l.Info(format, args...)
}

// Debugf writes a formatted debug message.
func (l *Logger) Debugf(format string, args ...any) {
	l.Debug(format, args...)
}

// Tracef writes a formatted trace message.
func (l *Logger) Tracef(format string, args ...any) {
	l.Trace(format, args...)
}
