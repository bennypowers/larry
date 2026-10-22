package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

// spinnerFrames are the frames for the loading spinner animation.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// IsTTY reports whether w is a terminal.
func IsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// AnimateSpinner runs fn() while drawing a spinner to out. The result and
// error are returned to the caller and never printed here, so the caller can
// honour --quiet and output formats. Animation is skipped unless enabled.
func AnimateSpinner(out io.Writer, msg string, enabled bool, fn func() (string, error)) (string, error) {
	if !enabled {
		return fn()
	}

	type result struct {
		out string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		o, err := fn()
		ch <- result{o, err}
	}()

	s := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			frame := spinnerFrames[s%len(spinnerFrames)]
			s++
			// Best-effort animation; a closed writer just stops drawing.
			_, _ = fmt.Fprintf(out, "\r%s %s  ", frame, msg)
		case res := <-ch:
			_, _ = fmt.Fprint(out, "\r\033[K")
			return res.out, res.err
		}
	}
}
