// Package task holds the description of runs (tasks) and their state machine.
package task

import (
	"path/filepath"
	"time"
)

type Kind string

const (
	KindScript   Kind = "script"
	KindPull     Kind = "pull"
	KindCheckout Kind = "checkout"
	KindInstall  Kind = "install"
)

// Spec: what to run and where.
type Spec struct {
	Project     string `json:"project"`
	ProjectPath string `json:"projectPath"`
	Kind        Kind   `json:"kind"`
	Dir         string `json:"dir,omitempty"`    // script only: package dir, "." for the root
	Script      string `json:"script,omitempty"` // script only
	Branch      string `json:"branch,omitempty"` // checkout only
}

// ID: for a script, "<project>/<dir>:<script>". The built-in operations (pull/checkout/install)
// share one slot per project, so only one of them can run at a time.
func (s Spec) ID() string {
	if s.Kind == KindScript {
		return s.Project + "/" + s.Dir + ":" + s.Script
	}
	return BuiltinID(s.Project)
}

func BuiltinID(project string) string { return project + "/@builtin" }

func (s Spec) Label() string {
	switch s.Kind {
	case KindScript:
		if s.Dir == "" || s.Dir == "." {
			return s.Script
		}
		return s.Dir + " › " + s.Script
	case KindCheckout:
		return "checkout " + s.Branch
	default:
		return string(s.Kind)
	}
}

// WorkDir: the absolute working directory for the run.
func (s Spec) WorkDir() string {
	if s.Kind == KindScript && s.Dir != "" && s.Dir != "." {
		return filepath.Join(s.ProjectPath, s.Dir)
	}
	return s.ProjectPath
}

type Status string

const (
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusExited   Status = "exited"
	StatusCrashed  Status = "crashed"
	StatusStopped  Status = "stopped"
	StatusUnknown  Status = "unknown"
)

func (s Status) Active() bool { return s == StatusStarting || s == StatusRunning }

type Event int

const (
	EventPortOpened Event = iota // we saw a LISTEN port
	EventExited                  // the process exited on its own
	EventStopped                 // the process was stopped at the user's request
	EventLost                    // an adopted process disappeared, exit code unknown
)

// Transition: a pure state transition; no further transition out of a terminal state.
func Transition(cur Status, ev Event) Status {
	if !cur.Active() {
		return cur
	}
	switch ev {
	case EventPortOpened:
		return StatusRunning
	case EventExited:
		if cur == StatusRunning {
			return StatusCrashed
		}
		return StatusExited
	case EventStopped:
		return StatusStopped
	case EventLost:
		return StatusUnknown
	}
	return cur
}

// Info: a task's current state (the daemon persists this and sends it to the TUI).
type Info struct {
	Spec      Spec      `json:"spec"`
	Status    Status    `json:"status"`
	PID       int       `json:"pid"` // also the process group id
	Ports     []int     `json:"ports,omitempty"`
	LastPort  int       `json:"lastPort,omitempty"`
	ExitCode  int       `json:"exitCode"`
	StartedAt time.Time `json:"startedAt"`
	RunningAt time.Time `json:"runningAt,omitempty"` // when the first LISTEN port showed up
	EndedAt   time.Time `json:"endedAt,omitempty"`
	Adopted   bool      `json:"adopted,omitempty"`
	// ProcStart: the group leader process's start time (e.g. `ps -o lstart=`), so that
	// after a restart we can detect if another process has since taken over the PID.
	ProcStart string `json:"procStart,omitempty"`
}
