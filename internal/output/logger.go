package output

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

// Logger wraps slog with structured output and verbosity control.
type Logger struct {
	verbose int
	quiet   bool
	color   *ColorHelper
	info    *slog.Logger
	debug   *slog.Logger
	error   *slog.Logger
	warn    *slog.Logger
	output  io.Writer
}

// NewLogger creates a new Logger with the given verbosity, quiet mode, and color helper.
func NewLogger(verbose int, quiet bool, color *ColorHelper) (*Logger, error) {
	writer := os.Stderr
	l := &Logger{
		verbose: verbose,
		quiet:   quiet,
		color:   color,
		output:  writer,
	}

	infoHandler := slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelInfo, AddSource: false})
	debugHandler := slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: false})
	errorHandler := slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelError, AddSource: false})
	warnHandler := slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelWarn, AddSource: false})

	l.info = slog.New(infoHandler)
	l.debug = slog.New(debugHandler)
	l.error = slog.New(errorHandler)
	l.warn = slog.New(warnHandler)

	return l, nil
}

// Info logs at info level.
func (l *Logger) Info(msg string, args ...any) {
	if !l.quiet {
		if l.color != nil && l.color.Enabled() {
			msg = l.color.Blue(msg)
		}
		l.info.InfoContext(nil, msg, args...)
	}
}

// Debug logs at debug level (only when verbose >= 2).
func (l *Logger) Debug(msg string, args ...any) {
	if !l.quiet && l.verbose >= 2 {
		l.debug.DebugContext(nil, msg, args...)
	}
}

// Warn logs a warning.
func (l *Logger) Warn(msg string, args ...any) {
	if !l.quiet {
		if l.color != nil && l.color.Enabled() {
			msg = l.color.Yellow(msg)
		}
		l.warn.WarnContext(nil, msg, args...)
	}
}

// Error logs an error.
func (l *Logger) Error(msg string, args ...any) {
	if !l.quiet {
		if l.color != nil && l.color.Enabled() {
			msg = l.color.Red(msg)
		}
		l.error.ErrorContext(nil, msg, args...)
	}
}

// Infof is like Info but uses format specifiers.
func (l *Logger) Infof(format string, args ...any) {
	l.Info(fmt.Sprintf(format, args...))
}

// Debugf is like Debug but uses format specifiers.
func (l *Logger) Debugf(format string, args ...any) {
	if !l.quiet && l.verbose >= 2 {
		l.debug.DebugContext(nil, fmt.Sprintf(format, args...))
	}
}
