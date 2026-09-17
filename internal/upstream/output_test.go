package upstream

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderTable_Outdated(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "dev-python/requests", Current: "1.0.0", Latest: "2.0.0", Status: StatusOutdated, Source: "https://pypi.org"},
	}
	if err := RenderTable(&buf, results); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "dev-python/requests") {
		t.Fatalf("output should contain package name, got: %s", output)
	}
	if !strings.Contains(output, "1.0.0") {
		t.Fatalf("output should contain current version, got: %s", output)
	}
	if !strings.Contains(output, "2.0.0") {
		t.Fatalf("output should contain latest version, got: %s", output)
	}
}

func TestRenderTable_UpToDate(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "dev-python/requests", Current: "2.0.0", Latest: "2.0.0", Status: StatusUpToDate, Source: ""},
	}
	if err := RenderTable(&buf, results); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "dev-python/requests") {
		t.Fatalf("output should contain package name, got: %s", output)
	}
}

func TestRenderTable_Ahead(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "dev-python/requests", Current: "3.0.0", Latest: "2.0.0", Status: StatusAhead, Source: ""},
	}
	if err := RenderTable(&buf, results); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "dev-python/requests") {
		t.Fatalf("output should contain package name, got: %s", output)
	}
}

func TestRenderTable_Live(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "dev-python/requests", Current: "9999", Latest: "", Status: StatusLive, Source: ""},
	}
	if err := RenderTable(&buf, results); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "dev-python/requests") {
		t.Fatalf("output should contain package name, got: %s", output)
	}
}

func TestRenderTable_Unknown(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "dev-python/requests", Current: "1.0.0", Latest: "", Status: StatusUnknown, Source: ""},
	}
	if err := RenderTable(&buf, results); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "dev-python/requests") {
		t.Fatalf("output should contain package name, got: %s", output)
	}
}

func TestRenderTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{}
	if err := RenderTable(&buf, results); err != nil {
		t.Fatal(err)
	}
	// Should produce some output (at least a header)
	output := buf.String()
	if output == "" {
		t.Fatal("output should not be empty")
	}
}

func TestRenderTable_SourceHyperlink(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "dev-python/requests", Current: "1.0.0", Latest: "2.0.0", Status: StatusOutdated, Source: "https://pypi.org/project/requests/"},
	}
	if err := RenderTable(&buf, results); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "https://pypi.org/project/requests/") {
		t.Fatalf("output should contain source URL, got: %s", output)
	}
}

func TestRenderTable_MultipleResults(t *testing.T) {
	var buf bytes.Buffer
	results := []UpstreamResult{
		{Package: "dev-python/requests", Current: "1.0.0", Latest: "2.0.0", Status: StatusOutdated},
		{Package: "dev-python/flask", Current: "2.0.0", Latest: "2.0.0", Status: StatusUpToDate},
		{Package: "dev-python/django", Current: "5.0.0", Latest: "4.0.0", Status: StatusAhead},
	}
	if err := RenderTable(&buf, results); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "dev-python/requests") {
		t.Fatal("output should contain dev-python/requests")
	}
	if !strings.Contains(output, "dev-python/flask") {
		t.Fatal("output should contain dev-python/flask")
	}
	if !strings.Contains(output, "dev-python/django") {
		t.Fatal("output should contain dev-python/django")
	}
}

func TestStatusString(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusOutdated, "OUTDATED"},
		{StatusUpToDate, "UP-TO-DATE"},
		{StatusAhead, "AHEAD"},
		{StatusLive, "LIVE"},
		{StatusUnknown, "UNKNOWN"},
	}
	for _, tt := range tests {
		got := tt.status.String()
		if got != tt.want {
			t.Fatalf("status %d = %q, want %q", tt.status, got, tt.want)
		}
	}
}
