package cli

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

// spinnerFrames are the frames for the loading spinner animation.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// AnimateSpinner runs fn() while printing a spinner animation.
// Returns fn's string result and error. The result is NOT printed here:
// callers decide whether to write it (honouring --quiet and output format).
// Errors are never printed; they belong to the caller.
// If quiet is true, the animation is suppressed.
func AnimateSpinner(msg string, quiet bool, fn func() (string, error)) (string, error) {
	if quiet {
		return fn()
	}

	// Check if stdout is a TTY
	if !isTTYStdout() {
		return fn()
	}

	// Run fn in a goroutine so spinner can run concurrently
	type result struct {
		out string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := fn()
		ch <- result{out, err}
	}()

	s := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			frame := spinnerFrames[s%len(spinnerFrames)]
			s++
			fmt.Printf("\r%s %s  ", frame, msg)
		case res := <-ch:
			// Move to a new line; the caller writes the result.
			fmt.Println()
			return res.out, res.err
		}
	}
}

// isTTYStdout checks if stdout is a TTY using golang.org/x/term.
func isTTYStdout() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}
