package main

import (
	"os"
	"strings"
	"testing"
)

func TestContainerfile_Exists(t *testing.T) {
	_, err := os.Stat("Containerfile.larry-test")
	if err != nil {
		t.Fatalf("Containerfile.larry-test does not exist or is not accessible: %v", err)
	}

	data, err := os.ReadFile("Containerfile.larry-test")
	if err != nil {
		t.Fatalf("Failed to read Containerfile.larry-test: %v", err)
	}

	content := string(data)

	// Must have FROM base image
	if !strings.Contains(content, "FROM") {
		t.Error("Containerfile.larry-test must contain a FROM instruction")
	}

	// Must reference portage-related packages
	if !strings.Contains(content, "portage") && !strings.Contains(content, "gentoo") && !strings.Contains(content, "emerge") {
		t.Error("Containerfile.larry-test should reference portage or emerge")
	}

	// Must create a non-root user
	if !strings.Contains(content, "useradd") || !strings.Contains(content, "larry") {
		t.Error("Containerfile.larry-test should create a 'larry' user")
	}

	// Must set EPREFIX
	if !strings.Contains(content, "EPREFIX") {
		t.Error("Containerfile.larry-test should set EPREFIX")
	}
}
