//go:build unix

package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRunLarryChild_CancelKillsProcessGroup proves that cancelling an action
// takes down grandchildren (pkgdev, podman, sleep) and not just the direct
// child, which would otherwise keep working and holding the pipes open.
func TestRunLarryChild_CancelKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")
	script := filepath.Join(dir, "fake-larry")
	body := "#!/bin/sh\n" +
		"sleep 30 &\n" +
		"echo $! > \"$LARRY_TEST_PIDFILE\"\n" +
		"wait\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LARRY_TEST_PIDFILE", pidFile)

	orig := larryExecutable
	larryExecutable = func() (string, error) { return script, nil }
	t.Cleanup(func() { larryExecutable = orig })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- runLarryChildFunc(ctx, nil, func(string) {}) }()

	pid := waitForPIDFile(t, pidFile)
	cancel()

	select {
	case err := <-done:
		// The child died from our signal; the exact error is platform-specific.
		_ = err
	case <-time.After(5 * time.Second):
		t.Fatal("runLarryChildFunc did not return after cancel")
	}

	// SIGTERM may take a moment; poll briefly for the grandchild to vanish.
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if err == syscall.ESRCH {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d still alive after cancel: %v", pid, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("pid file %s never appeared", path)
	return 0
}
