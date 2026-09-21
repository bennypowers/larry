package logger

import (
	"fmt"
	"os"
)

const prefix = "[larry]"

// Verbose controls whether verbose debug logs are printed.
var Verbose bool

// Debug prints a verbose debug message to stderr.
func Debug(msg string) {
	if !Verbose {
		return
	}
	fmt.Fprint(os.Stderr, prefix, " ")
	fmt.Fprintln(os.Stderr, msg)
}

// Debugf prints a formatted verbose debug message to stderr.
func Debugf(format string, args ...any) {
	if !Verbose {
		return
	}
	fmt.Fprint(os.Stderr, prefix, " ")
	fmt.Fprintf(os.Stderr, format, args...)
	fmt.Fprintln(os.Stderr)
}
