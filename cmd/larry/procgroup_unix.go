//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup puts the child in its own process group. Cancelling an
// action then reaches grandchildren (pkgdev, podman) instead of orphaning them.
func configureProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// terminateProcessGroup signals the child's whole process group. When force is
// false it sends SIGTERM first, giving the child a chance to clean up.
func terminateProcessGroup(cmd *exec.Cmd, force bool) {
	if cmd.Process == nil {
		return
	}
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
		// The group may already be gone; fall back to the process itself.
		_ = cmd.Process.Signal(sig)
	}
}
