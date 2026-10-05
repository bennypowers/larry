package main

import (
	"strings"
	"testing"
)

func TestPrepareTypeValidation_Rejects(t *testing.T) {
	// --type with an unknown value should fail before any pipeline is invoked.
	overlay := fixtureOverlay(t, nil, nil)
	_, _, err := execCmd(t, "prepare", "--overlay", overlay, "--type", "npmm", "foo-1.0.ebuild")
	if err == nil {
		t.Fatal("expected error for --type npmm, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "unknown type \"npmm\"") {
		t.Errorf("err = %q, want substring %q", msg, "unknown type \"npmm\"")
	}
	if !strings.Contains(msg, "cargo") {
		t.Errorf("err = %q, want substring %q (valid types list)", msg, "cargo")
	}
}

func TestPrepareTypeValidation_Accepts(t *testing.T) {
	// --type generic --dry-run should not error on type validation.
	overlay := fixtureOverlay(t, nil, nil)
	_, _, err := execCmd(t, "prepare", "--overlay", overlay, "--type", "generic", "--dry-run", "foo-1.0.ebuild")
	// The path won't exist so it may error on the path resolution, but it
	// should NOT error on the type validation. The error (if any) should not
	// be about an unknown type.
	if err != nil {
		// Allow path-not-found errors; reject type-validation errors.
		if strings.Contains(err.Error(), "unknown type") {
			t.Errorf("unexpected type-validation error: %v", err)
		}
	}
}
