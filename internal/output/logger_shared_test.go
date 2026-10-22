package output

import (
	"bytes"
	"strings"
	"testing"
)

func resetShared(t *testing.T) {
	t.Helper()
	SetLevel(0)
	SetQuiet(false)
	SetOutput(nil)
	t.Cleanup(func() {
		SetLevel(0)
		SetQuiet(false)
		SetOutput(nil)
	})
}

func TestSharedLogger_SetLevelGating(t *testing.T) {
	resetShared(t)
	var buf bytes.Buffer
	SetOutput(&buf)

	for level, want := range map[int]struct{ debug, trace bool }{
		0: {false, false},
		1: {false, false},
		2: {true, false},
		3: {true, true},
	} {
		SetLevel(level)
		buf.Reset()
		Debug("debug-line")
		Trace("trace-line")
		got := buf.String()
		if strings.Contains(got, "debug-line") != want.debug {
			t.Errorf("level %d debug presence = %v, want %v (%q)", level,
				strings.Contains(got, "debug-line"), want.debug, got)
		}
		if strings.Contains(got, "trace-line") != want.trace {
			t.Errorf("level %d trace presence = %v, want %v (%q)", level,
				strings.Contains(got, "trace-line"), want.trace, got)
		}
	}
}

func TestSharedLogger_SetOutput(t *testing.T) {
	resetShared(t)
	var buf bytes.Buffer
	SetOutput(&buf)
	SetLevel(2)
	Debugf("n=%d", 7)
	if got := buf.String(); !strings.Contains(got, "n=7") {
		t.Errorf("captured output = %q, want it to contain n=7", got)
	}
}

func TestSharedLogger_TracePrefix(t *testing.T) {
	resetShared(t)
	var buf bytes.Buffer
	SetOutput(&buf)
	SetLevel(3)
	Trace("details")
	got := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(got, "... ") {
		t.Errorf("trace line = %q, want prefix %q", got, "... ")
	}
}

func TestSharedLogger_SetLevelClamp(t *testing.T) {
	resetShared(t)
	SetLevel(-1)
	if got := Level(); got != 0 {
		t.Errorf("Level(-1) = %d, want 0", got)
	}
	SetLevel(42)
	if got := Level(); got != 3 {
		t.Errorf("Level(42) = %d, want 3", got)
	}
}

func TestSharedLogger_QuietSuppressesDiagnostics(t *testing.T) {
	resetShared(t)
	var buf bytes.Buffer
	SetOutput(&buf)
	SetLevel(3)
	SetQuiet(true)
	Debug("debug-line")
	Trace("trace-line")
	if got := buf.String(); got != "" {
		t.Errorf("quiet diagnostics = %q, want empty", got)
	}
}
