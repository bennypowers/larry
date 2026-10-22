package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"bennypowers.dev/larry/internal/upstream"
)

func decodeJSONOutput(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	return decoded
}

func TestUpstreamJSON_EmitsAllResults(t *testing.T) {
	ov := verbosityOverlay(t)
	stubUpstreamResults(t, []upstream.UpstreamResult{
		{Package: "dev-util/foo", Current: "1.0.0", Latest: "2.0.0", Status: upstream.StatusOutdated},
		{Package: "dev-util/bar", Current: "1.0.0", Latest: "1.0.0", Status: upstream.StatusUpToDate},
	}, nil)

	stdout, _, err := execCmd(t, "upstream", "--overlay", ov, "--output", "json")
	if err != nil {
		t.Fatalf("execCmd: %v", err)
	}
	decoded := decodeJSONOutput(t, stdout)
	rows, ok := decoded["results"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("results = %#v, want 2 rows", decoded["results"])
	}
	if strings.Contains(stdout, ">>>") || strings.Contains(stdout, "Checking packages") {
		t.Errorf("JSON stdout contains prose: %q", stdout)
	}
}

func TestUpstreamJSON_IgnoresVerbosityFilter(t *testing.T) {
	ov := verbosityOverlay(t)
	stubUpstreamResults(t, []upstream.UpstreamResult{
		{Package: "dev-util/foo", Current: "1.0.0", Status: upstream.StatusUpToDate},
	}, nil)

	plain, _, err := execCmd(t, "upstream", "--overlay", ov, "--output", "json")
	if err != nil {
		t.Fatalf("plain: %v", err)
	}
	verbose, _, err := execCmd(t, "upstream", "--overlay", ov, "--output", "json", "-v")
	if err != nil {
		t.Fatalf("-v: %v", err)
	}
	if plain != verbose {
		t.Errorf("-v changed JSON output:\nplain: %s\n-v:    %s", plain, verbose)
	}
	rows := decodeJSONOutput(t, plain)["results"].([]any)
	if len(rows) != 1 {
		t.Errorf("insignificant row was filtered from JSON: %#v", rows)
	}
}

func TestUpstreamJSON_QuietStillEmitsJSON(t *testing.T) {
	ov := verbosityOverlay(t)
	stubUpstreamResults(t, []upstream.UpstreamResult{
		{Package: "dev-util/foo", Status: upstream.StatusOutdated},
	}, nil)

	stdout, stderr, err := execCmd(t, "upstream", "--overlay", ov, "--output", "json", "--quiet")
	if err != nil {
		t.Fatalf("execCmd: %v (stderr %q)", err, stderr)
	}
	decodeJSONOutput(t, stdout)
}

func TestUpstreamJSON_ColorAlwaysNoANSI(t *testing.T) {
	ov := verbosityOverlay(t)
	stubUpstreamResults(t, []upstream.UpstreamResult{
		{Package: "dev-util/foo", Status: upstream.StatusOutdated},
	}, nil)

	stdout, _, err := execCmd(t, "upstream", "--overlay", ov, "--output", "json", "--color", "always")
	if err != nil {
		t.Fatalf("execCmd: %v", err)
	}
	if strings.Contains(stdout, "\x1b") {
		t.Errorf("JSON contains ANSI: %q", stdout)
	}
	decodeJSONOutput(t, stdout)
}

func TestUpstreamJSON_InvalidOutput(t *testing.T) {
	ov := verbosityOverlay(t)
	_, _, err := execCmd(t, "upstream", "--overlay", ov, "--output", "yaml")
	if err == nil {
		t.Fatal("expected an error for --output yaml")
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Errorf("error = %v, want it to name the invalid value", err)
	}
}

func TestUpstreamJSON_ErrorWritesNoPartialJSON(t *testing.T) {
	ov := verbosityOverlay(t)
	stubUpstreamResults(t, nil, errors.New("walk exploded"))

	stdout, _, err := execCmd(t, "upstream", "--overlay", ov, "--output", "json")
	if err == nil {
		t.Fatal("expected an error")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on failure", stdout)
	}
}
