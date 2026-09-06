package github

import (
	"os/exec"
)

// StubRunner stubs exec.Command for testing from other packages.
// It implements CmdRunner interface.
type StubRunner struct {
	// Outputs maps full command strings to output bytes
	// The key is the full command like "gh release view --json tagName,url"
	Outputs map[string][]byte
	// Errors maps command strings to errors
	Errors map[string]error
}

// NewStubRunner creates a new stub runner for use by external test packages.
func NewStubRunner() *StubRunner {
	return &StubRunner{
		Outputs: make(map[string][]byte),
		Errors:  make(map[string]error),
	}
}

// cmdArgs returns "gh arg1 arg2 ..." from a cmd
func cmdArgs(cmd *exec.Cmd) string {
	// Use cmd.Args[0] which is always the program name as passed to exec.Command
	result := cmd.Args[0]
	for _, a := range cmd.Args[1:] {
		result += " " + a
	}
	return result
}

// Run implements CmdRunner.
func (s *StubRunner) Run(cmd *exec.Cmd) error {
	key := cmdArgs(cmd)
	if err, ok := s.Errors[key]; ok {
		return err
	}
	return nil
}

// Output implements CmdRunner.
func (s *StubRunner) Output(cmd *exec.Cmd) ([]byte, error) {
	key := cmdArgs(cmd)
	if err, ok := s.Errors[key]; ok {
		return nil, err
	}
	if out, ok := s.Outputs[key]; ok {
		return out, nil
	}
	return nil, nil
}
