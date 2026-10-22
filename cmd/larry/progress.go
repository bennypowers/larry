package main

import (
	"encoding/json"
	"io"
	"os"

	"bennypowers.dev/larry/internal/prepare"
)

// progressPrefix marks structured progress lines on stderr, shared by the
// hidden --progress=json channel of commands that `larry manage` runs.
const progressPrefix = "larry-progress:"

// actionProgressWire is the NDJSON shape consumed by `larry manage`.
type actionProgressWire struct {
	Kind    int      `json:"kind"`
	Steps   []string `json:"steps,omitempty"`
	Step    string   `json:"step,omitempty"`
	State   int      `json:"state,omitempty"`
	Message string   `json:"message,omitempty"`
	Error   string   `json:"error,omitempty"`
}

const (
	progressStart = iota
	progressStep
	progressDone
	progressError
)

const (
	_ = iota // pending
	stepRunning
	stepDone
	stepFailed
)

// progressEmitter writes structured events when the hidden channel is active
// and does nothing otherwise.
type progressEmitter struct {
	enabled bool
	w       io.Writer // nil means os.Stderr
}

func newProgressEmitter(mode string) progressEmitter {
	return progressEmitter{enabled: mode == "json"}
}

func (e progressEmitter) emit(wire actionProgressWire) {
	if !e.enabled {
		return
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return
	}
	// Deliberately raw stderr: this is a machine channel, not user output.
	line := append([]byte(progressPrefix), data...)
	line = append(line, '\n')
	w := e.w
	if w == nil {
		w = os.Stderr
	}
	_, _ = w.Write(line)
}

func (e progressEmitter) start(steps ...string) {
	e.emit(actionProgressWire{Kind: progressStart, Steps: steps})
}

func (e progressEmitter) step(name string, state int) {
	e.emit(actionProgressWire{Kind: progressStep, Step: name, State: state})
}

// detail reports the current sub-step without adding a checklist row, for
// callers whose child pipeline names work dynamically.
func (e progressEmitter) detail(name string) {
	e.emit(actionProgressWire{Kind: progressStep, State: stepRunning, Message: name})
}

func (e progressEmitter) done() {
	e.emit(actionProgressWire{Kind: progressDone})
}

func (e progressEmitter) fail(err error) {
	wire := actionProgressWire{Kind: progressError}
	if err != nil {
		wire.Error = err.Error()
	}
	e.emit(wire)
}

// prepareProgressReporter maps pipeline events onto the action checklist.
// Prepare names steps as it reaches them ("prepare", "manifest", "custom
// script"), so the consumer appends unknown names on first sight.
func prepareProgressReporter(mode string) prepare.ProgressFunc {
	emitter := newProgressEmitter(mode)
	if !emitter.enabled {
		return nil
	}
	return func(ev prepare.ProgressEvent) {
		mapPrepareProgress(emitter, ev)
	}
}

// mapPrepareProgress is the testable core of prepareProgressReporter.
func mapPrepareProgress(emitter progressEmitter, ev prepare.ProgressEvent) {
	switch ev.Kind {
	case prepare.ProgressStep:
		if ev.Step != "" {
			emitter.step(ev.Step, stepRunning)
		}
	case prepare.ProgressDone:
		emitter.done()
	case prepare.ProgressError:
		emitter.fail(ev.Err)
	case prepare.ProgressStart, prepare.ProgressLog:
		// Start carries no step list; logs already stream to the pane.
	}
}

// mapPrepareSubstep forwards prepare's named steps as detail text, for callers
// (bump) that own the checklist and only need to show what prepare is doing.
func mapPrepareSubstep(emitter progressEmitter, ev prepare.ProgressEvent) {
	switch ev.Kind {
	case prepare.ProgressStart:
		emitter.detail("prepare: start")
	case prepare.ProgressStep:
		if ev.Step != "" {
			emitter.detail(ev.Step)
		}
	}
}
