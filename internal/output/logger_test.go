package output

import (
	"bytes"
	"log/slog"
	"testing"
)

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
		level   string
	}{
		{"no verbosity", 0, false, true, "info"},
		{"debug not shown", 0, false, false, "debug"},
		{"debug shown at -vv", 2, false, true, "debug"},
		{"quiet mode", 0, true, false, "info"},
		{"quiet debug suppressed", 2, true, false, "debug"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			infoHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
			debugHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
			errorHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})
			warnHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})

			log := &Logger{
				verbose: tt.verbose,
				quiet:   tt.quiet,
				color:   color,
				output:  &buf,
				info:    slog.New(infoHandler),
				debug:   slog.New(debugHandler),
				error:   slog.New(errorHandler),
				warn:    slog.New(warnHandler),
			}

			if tt.level == "info" {
				log.Info("test message")
			} else {
				log.Debug("test message")
			}

			hasLog := buf.Len() > 0
			if hasLog != tt.wantLog {
				t.Errorf("verbose=%d, quiet=%v, wantLog=%v, gotLog=%v, output=%q",
					tt.verbose, tt.quiet, tt.wantLog, hasLog, buf.String())
			}
		})
	}
}

func TestLogger_QuietMode(t *testing.T) {
	color, err := NewColorHelper("never")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	baseHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})

	log := &Logger{
		verbose: 0,
		quiet:   true,
		color:   color,
		output:  &buf,
		info:    slog.New(baseHandler),
		debug:   slog.New(baseHandler),
		error:   slog.New(baseHandler),
		warn:    slog.New(baseHandler),
	}

	log.Info("should be suppressed")
	log.Warn("should be suppressed")
	log.Error("should be suppressed")

	if buf.Len() > 0 {
		t.Errorf("quiet mode: output=%q, expected empty", buf.String())
	}
}
