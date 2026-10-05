package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestMsgWarnErrorPrefixes(t *testing.T) {
	color, err := NewColorHelper("never")
	if err != nil {
		t.Fatal(err)
	}

	var outBuf, errBuf bytes.Buffer
	log := &Logger{
		verbose: 0,
		quiet:   false,
		color:   color,
		out:     &outBuf,
		errw:    &errBuf,
	}

	log.Msg("hello")
	if got := outBuf.String(); got != ">>> hello\n" {
		t.Errorf("Msg('hello') out = %q, want %q", got, ">>> hello\n")
	}
	if errBuf.Len() != 0 {
		t.Errorf("Msg did not write to errw; errw = %q", errBuf.String())
	}

	outBuf.Reset()
	log.Warn("careful")
	if got := errBuf.String(); got != "*** careful\n" {
		t.Errorf("Warn('careful') errw = %q, want %q", got, "*** careful\n")
	}
	if outBuf.Len() != 0 {
		t.Errorf("Warn did not write to out; out = %q", outBuf.String())
	}

	errBuf.Reset()
	log.Error("boom")
	if got := errBuf.String(); got != "!!! boom\n" {
		t.Errorf("Error('boom') errw = %q, want %q", got, "!!! boom\n")
	}
}

func TestWarnErrorGoToStderr(t *testing.T) {
	color, err := NewColorHelper("never")
	if err != nil {
		t.Fatal(err)
	}

	var outBuf, errBuf bytes.Buffer
	log := &Logger{
		verbose: 0,
		quiet:   false,
		color:   color,
		out:     &outBuf,
		errw:    &errBuf,
	}

	log.Msg("hello")
	log.Warn("careful")
	log.Error("boom")

	if strings.Contains(outBuf.String(), "***") || strings.Contains(outBuf.String(), "!!!") {
		t.Errorf("out should not contain *** or !!!; out = %q", outBuf.String())
	}
}

func TestQuietSuppressesMsgAndWarnNotError(t *testing.T) {
	color, err := NewColorHelper("never")
	if err != nil {
		t.Fatal(err)
	}

	var outBuf, errBuf bytes.Buffer
	log := &Logger{
		verbose: 0,
		quiet:   true,
		color:   color,
		out:     &outBuf,
		errw:    &errBuf,
	}

	log.Msg("a")
	log.Warn("b")
	log.Error("c")

	if got := outBuf.String(); got != "" {
		t.Errorf("quiet out = %q, want empty", got)
	}
	if got := errBuf.String(); got != "!!! c\n" {
		t.Errorf("quiet errw = %q, want '!!! c\\n'", got)
	}
}

func TestLoggerFormatting(t *testing.T) {
	color, err := NewColorHelper("never")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	log := &Logger{
		verbose: 0,
		quiet:   false,
		color:   color,
		out:     &buf,
		errw:    &buf,
	}

	log.Msg("x=%d", 7)
	if got := buf.String(); got != ">>> x=7\n" {
		t.Errorf("Msg formatting = %q, want %q", got, ">>> x=7\n")
	}
}

func TestLogger_VerboseLevels(t *testing.T) {
	color, err := NewColorHelper("never")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		verbose int
		quiet   bool
		wantLog bool
	}{
		{"no verbosity", 0, false, false},
		{"debug shown at -vv", 2, false, true},
		{"quiet debug suppressed", 2, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var errBuf bytes.Buffer
			log := &Logger{
				verbose: tt.verbose,
				quiet:   tt.quiet,
				color:   color,
				out:     &bytes.Buffer{},
				errw:    &errBuf,
			}

			log.Debug("test message")

			hasLog := errBuf.Len() > 0
			if hasLog != tt.wantLog {
				t.Errorf("verbose=%d, quiet=%v, wantLog=%v, gotLog=%v, output=%q",
					tt.verbose, tt.quiet, tt.wantLog, hasLog, errBuf.String())
			}
		})
	}
}

func TestColorNeverProducesNoAnsi(t *testing.T) {
	color, err := NewColorHelper("never")
	if err != nil {
		t.Fatal(err)
	}

	var outBuf, errBuf bytes.Buffer
	log := &Logger{
		verbose: 0,
		quiet:   false,
		color:   color,
		out:     &outBuf,
		errw:    &errBuf,
	}

	log.Msg("hello")
	log.Warn("careful")
	log.Error("boom")

	outStr := outBuf.String()
	errStr := errBuf.String()
	if containsAnsi(outStr) {
		t.Errorf("Msg/Warn/Error with 'never' produced ANSI in out: %q", outStr)
	}
	if containsAnsi(errStr) {
		t.Errorf("Msg/Warn/Error with 'never' produced ANSI in errw: %q", errStr)
	}
}

func TestColorAlwaysProducesAnsi(t *testing.T) {
	color, err := NewColorHelper("always")
	if err != nil {
		t.Fatal(err)
	}

	var outBuf, errBuf bytes.Buffer
	log := &Logger{
		verbose: 0,
		quiet:   false,
		color:   color,
		out:     &outBuf,
		errw:    &errBuf,
	}

	log.Msg("hello")
	log.Warn("careful")
	log.Error("boom")

	outStr := outBuf.String()
	errStr := errBuf.String()
	if !containsAnsi(outStr) {
		t.Error("Msg with 'always' should produce ANSI in out")
	}
	if !containsAnsi(errStr) {
		t.Error("Warn/Error with 'always' should produce ANSI in errw")
	}
}

func containsAnsi(s string) bool {
	return len(s) > 0 && s[0] == '\x1b' || len(s) > 1 && s[1] == '\x1b' ||
		bytes.Contains([]byte(s), []byte{'\x1b', '['})
}
