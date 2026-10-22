package main

import (
	"encoding/json"
	"io"
	"os"
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
