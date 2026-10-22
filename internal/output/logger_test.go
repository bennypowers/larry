package output

import (
	"bytes"
	"strings"
	"testing"
)

func newTestLogger(t *testing.T, level int, quiet bool, colorMode string) (*Logger, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	color, err := NewColorHelper(colorMode)
	if err != nil {
		t.Fatal(err)
	}
	var outBuf, errBuf bytes.Buffer
	return &Logger{
		level: level,
		quiet: quiet,
		color: color,
		out:   &outBuf,
		errw:  &errBuf,
	}, &outBuf, &errBuf
}

func TestLogger_LevelGating(t *testing.T) {
	tests := []struct {
		level                                   int
		wantMsg, wantInfo, wantDebug, wantTrace bool
		wantWarn, wantError                     bool
	}{
		{0, true, false, false, false, true, true},
		{1, true, true, false, false, true, true},
		{2, true, true, true, false, true, true},
		{3, true, true, true, true, true, true},
	}
	for _, tt := range tests {
		t.Run(levelName(tt.level), func(t *testing.T) {
			log, out, errw := newTestLogger(t, tt.level, false, "never")
			log.Msg("m")
			log.Info("i")
			log.Debug("d")
			log.Trace("t")
			log.Warn("w")
			log.Error("e")

			gotOut := out.String()
			gotErr := errw.String()
			check := func(want bool, needle, stream string, got string) {
				has := strings.Contains(got, needle)
				if has != want {
					t.Errorf("%s presence = %v, want %v; stream = %q", needle, has, want, got)
				}
			}
			check(tt.wantMsg, ">>> m", "out", gotOut)
			check(tt.wantInfo, ">>> i", "out", gotOut)
			check(tt.wantDebug, ">>> d", "err", gotErr)
			check(tt.wantTrace, "... t", "err", gotErr)
			check(tt.wantWarn, "*** w", "err", gotErr)
			check(tt.wantError, "!!! e", "err", gotErr)
		})
	}
}

func levelName(n int) string {
	switch n {
	case 0:
		return "level0"
	case 1:
		return "level1"
	case 2:
		return "level2"
	default:
		return "level3"
	}
}

func TestLogger_QuietNeverSuppressesError(t *testing.T) {
	log, out, errw := newTestLogger(t, LevelTrace, true, "never")
	log.Msg("m")
	log.Info("i")
	log.Debug("d")
	log.Trace("t")
	log.Warn("w")
	log.Error("e")

	if got := out.String(); got != "" {
		t.Errorf("quiet out = %q, want empty", got)
	}
	if got := errw.String(); got != "!!! e\n" {
		t.Errorf("quiet err = %q, want exactly %q", got, "!!! e\n")
	}
}

func TestLogger_Streams(t *testing.T) {
	log, out, errw := newTestLogger(t, LevelTrace, false, "never")
	log.Msg("m")
	log.Info("i")
	log.Debug("d")
	log.Trace("t")
	log.Warn("w")
	log.Error("e")

	if strings.Contains(out.String(), "***") || strings.Contains(out.String(), "!!!") ||
		strings.Contains(out.String(), "... ") {
		t.Errorf("out contains error/trace lines: %q", out.String())
	}
	if strings.Contains(errw.String(), ">>> m") || strings.Contains(errw.String(), ">>> i") {
		t.Errorf("err contains Msg/Info lines: %q", errw.String())
	}
}

func TestLogger_Prefixes(t *testing.T) {
	log, out, errw := newTestLogger(t, LevelTrace, false, "never")
	log.Msg("hello")
	log.Info("info")
	log.Debug("debug")
	log.Trace("trace")
	log.Warn("warn")
	log.Error("error")

	wantOut := ">>> hello\n>>> info\n"
	if got := out.String(); got != wantOut {
		t.Errorf("out = %q, want %q", got, wantOut)
	}
	wantErr := ">>> debug\n... trace\n*** warn\n!!! error\n"
	if got := errw.String(); got != wantErr {
		t.Errorf("err = %q, want %q", got, wantErr)
	}
}

func TestLogger_LevelClamp(t *testing.T) {
	log, _, errw := newTestLogger(t, 0, false, "never")
	log.SetLevel(-5)
	if got := log.Level(); got != LevelResults {
		t.Errorf("Level after SetLevel(-5) = %d, want %d", got, LevelResults)
	}
	log.Trace("hidden")
	if errw.Len() != 0 {
		t.Errorf("negative level enabled trace: %q", errw.String())
	}

	log.SetLevel(99)
	if got := log.Level(); got != LevelTrace {
		t.Errorf("Level after SetLevel(99) = %d, want %d", got, LevelTrace)
	}
	log.Trace("visible")
	if !strings.Contains(errw.String(), "... visible") {
		t.Errorf("clamped trace level did not emit: %q", errw.String())
	}
}

func TestLoggerFormatting(t *testing.T) {
	log, out, _ := newTestLogger(t, 0, false, "never")
	log.Msg("x=%d", 7)
	if got := out.String(); got != ">>> x=7\n" {
		t.Errorf("Msg formatting = %q, want %q", got, ">>> x=7\n")
	}
}

func TestColorNeverProducesNoAnsi(t *testing.T) {
	log, out, errw := newTestLogger(t, LevelTrace, false, "never")
	log.Msg("hello")
	log.Info("info")
	log.Debug("debug")
	log.Trace("trace")
	log.Warn("careful")
	log.Error("boom")

	if containsAnsi(out.String()) {
		t.Errorf("colour 'never' produced ANSI in out: %q", out.String())
	}
	if containsAnsi(errw.String()) {
		t.Errorf("colour 'never' produced ANSI in err: %q", errw.String())
	}
}

func TestColorAlwaysProducesAnsi(t *testing.T) {
	log, out, errw := newTestLogger(t, 0, false, "always")
	log.Msg("hello")
	log.Warn("careful")
	log.Error("boom")

	if !containsAnsi(out.String()) {
		t.Error("Msg with 'always' should produce ANSI in out")
	}
	if !containsAnsi(errw.String()) {
		t.Error("Warn/Error with 'always' should produce ANSI in err")
	}
}

func containsAnsi(s string) bool {
	return bytes.Contains([]byte(s), []byte{'\x1b', '['})
}
