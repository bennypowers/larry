package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestIsTTY_NonFileWriterFalse(t *testing.T) {
	if IsTTY(&bytes.Buffer{}) {
		t.Error("IsTTY(buffer) = true, want false")
	}
}

func TestIsTTY_RegularFileFalse(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if IsTTY(f) {
		t.Error("IsTTY(regular file) = true, want false")
	}
}

func TestAnimateSpinner_DisabledDoesNotWrite(t *testing.T) {
	var buf bytes.Buffer
	out, err := AnimateSpinner(&buf, "working", false, func() (string, error) {
		return "RESULT", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "RESULT" {
		t.Errorf("out = %q, want RESULT", out)
	}
	if buf.Len() != 0 {
		t.Errorf("disabled spinner wrote %q", buf.String())
	}
}

func TestAnimateSpinner_DoesNotPrintResult(t *testing.T) {
	var buf bytes.Buffer
	out, err := AnimateSpinner(&buf, "working", true, func() (string, error) {
		return "RESULT", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "RESULT" {
		t.Errorf("out = %q, want RESULT", out)
	}
	if strings.Contains(buf.String(), "RESULT") {
		t.Errorf("spinner printed the result itself: %q", buf.String())
	}
}

func TestAnimateSpinner_ErrorNotPrinted(t *testing.T) {
	var buf bytes.Buffer
	_, err := AnimateSpinner(&buf, "working", true, func() (string, error) {
		return "", errTest
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(buf.String(), errTest.Error()) {
		t.Errorf("spinner printed the error: %q", buf.String())
	}
}

var errTest = &testError{"boom"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
