//go:build !unix

package main

import "os/exec"

// configureProcessGroup is a no-op on platforms without process groups.
func configureProcessGroup(_ *exec.Cmd) {}

// terminateProcessGroup kills the direct child only.
func terminateProcessGroup(cmd *exec.Cmd, _ bool) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
