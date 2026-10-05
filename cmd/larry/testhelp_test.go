package main

import (
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
)

// execCmd builds a fresh root command tree, runs it with args,
// and returns stdout, stderr and the error.
func execCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	var outBuf, errBuf []byte
	cmd := newRootCmd()

	// Capture output.
	if outBuf == nil {
		outBuf = make([]byte, 0)
	}
	if errBuf == nil {
		errBuf = make([]byte, 0)
	}

	// Use a custom writer that captures to our buffers.
	cmd.SetOut(&captureWriter{buf: &outBuf})
	cmd.SetErr(&captureWriter{buf: &errBuf})
	cmd.SetArgs(args)

	err = cmd.Execute()
	return string(outBuf), string(errBuf), err
}

// captureWriter implements io.Writer, appending to a byte slice.
type captureWriter struct {
	buf *[]byte
}

func (w *captureWriter) Write(p []byte) (n int, err error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}

// fixtureOverlay creates a temp overlay containing profiles/repo_name, an
// initialised git repo with the named remotes, and the supplied ebuilds
// (key = "category/pkg/pkg-1.0.ebuild", value = file content).
// remotes maps remote name -> URL. Returns the overlay path.
func fixtureOverlay(t *testing.T, remotes map[string]string, ebuilds map[string]string) string {
	t.Helper()

	dir := t.TempDir()

	// Create profiles/repo_name (required for Gentoo overlay).
	profilesDir := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profilesDir, 0755); err != nil {
		t.Fatalf("creating profiles: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "repo_name"), []byte("test-overlay"), 0644); err != nil {
		t.Fatalf("writing repo_name: %v", err)
	}

	// Initialise git repo.
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}

	// Add remotes.
	for name, url := range remotes {
		if _, err := repo.CreateRemote(&config.RemoteConfig{
			Name: name,
			URLs: []string{url},
		}); err != nil {
			t.Fatalf("creating remote %s: %v", name, err)
		}
	}

	// Write ebuilds.
	for relPath, content := range ebuilds {
		fullPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("creating ebuild dir %s: %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("writing ebuild %s: %v", relPath, err)
		}
	}

	return dir
}
