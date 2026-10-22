package test

// ProgressKind classifies a test-run progress event.
type ProgressKind int

const (
	ProgressStart ProgressKind = iota
	ProgressStep
	ProgressDone
	ProgressError
)

// StepState is the state of one checklist entry.
type StepState int

const (
	StepPending StepState = iota
	StepRunning
	StepDone
	StepFailed
)

// ProgressEvent reports coarse test progress. Local runs invoke ebuild once,
// so their phases are reported as a planned list and marked done together;
// container runs report podman check, image build, and emerge separately.
type ProgressEvent struct {
	Kind    ProgressKind
	Steps   []string  // ProgressStart: ordered checklist entries
	Step    string    // ProgressStep: which entry changed
	State   StepState // ProgressStep: its new state
	Message string    // optional human-readable detail
	Err     error     // ProgressError
}

// ProgressFunc receives progress events. Implementations must tolerate being
// called from the goroutine running the test.
type ProgressFunc func(ProgressEvent)

func reportProgress(fn ProgressFunc, ev ProgressEvent) {
	if fn != nil {
		fn(ev)
	}
}
