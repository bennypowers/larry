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
// Returns fn's string result and error. fn's result is printed only on
// success — error text is never printed here; errors belong to the caller,
// which reports them (stderr, non-zero exit).
// If quiet is true, the spinner is suppressed (the result string is still
// printed, matching the previous behaviour).
func AnimateSpinner(msg string, quiet bool, fn func() (string, error)) (string, error) {
	if quiet {
		out, err := fn()
		if err == nil {
			fmt.Print(out)
		}
		return out, err
	}

	// Check if stdout is a TTY
	if !isTTYStdout() {
		out, err := fn()
		if err == nil {
			fmt.Print(out)
		}
		return out, err
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
			// Move to a new line, then print the result — success text only;
			// error text is the caller's to report.
			fmt.Println()
			if res.err == nil {
				fmt.Print(res.out)
			}
			return res.out, res.err
		}
	}
}

// isTTYStdout checks if stdout is a TTY using golang.org/x/term.
func isTTYStdout() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}
