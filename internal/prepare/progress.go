package prepare

// ProgressKind classifies a progress event.
type ProgressKind int

const (
	// ProgressStart begins a run or a package.
	ProgressStart ProgressKind = iota
	// ProgressStep advances or names a pipeline step.
	ProgressStep
	// ProgressLog carries one line of subprocess output.
	ProgressLog
	// ProgressDone completes a run.
	ProgressDone
	// ProgressError reports a failure.
	ProgressError
)

// ProgressEvent is emitted by the prepare pipeline. Done/Total are only
// meaningful on Step/Start/Done events.
type ProgressEvent struct {
	Kind    ProgressKind
	Package string
	Step    string
	Message string
	Done    int
	Total   int
	Err     error
}

// ProgressFunc receives pipeline events. It must not block indefinitely.
type ProgressFunc func(ProgressEvent)

func (c *PrepareConfig) report(ev ProgressEvent) {
	if c != nil && c.Progress != nil {
		c.Progress(ev)
	}
}

func (c *PrepareConfig) contextErr() error {
	if c != nil && c.Context != nil {
		return c.Context.Err()
	}
	return nil
}
