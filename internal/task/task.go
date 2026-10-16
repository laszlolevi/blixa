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
	From        string `json:"from,omitempty"`   // checkout only: (re)create Branch at this ref, e.g. "origin/main"
}

// ID: for a script, "<project path>/<dir>:<script>". It's built from the path, not the
// name, so same-named projects under different roots don't collide. The built-in
// operations (pull/checkout/install) share one slot per project, so only one of them
// can run at a time.
func (s Spec) ID() string {
	if s.Kind == KindScript {
		return s.projectKey() + "/" + s.Dir + ":" + s.Script
	}
	return BuiltinID(s.projectKey())
}

// projectKey: the project's path; the name only for a spec persisted without one.
func (s Spec) projectKey() string {
	if s.ProjectPath != "" {
		return s.ProjectPath
	}
	return s.Project
}

// BuiltinID: the built-in operations' ID for the project at projectPath.
func BuiltinID(projectPath string) string { return projectPath + "/@builtin" }

func (s Spec) Label() string {
	switch s.Kind {
	case KindScript:
		if s.Dir == "" || s.Dir == "." {
			return s.Script
		}
		return s.Dir + " › " + s.Script
	case KindCheckout:
		if s.From != "" {
			return "checkout " + s.From
		}
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
	Spec      Spec           `json:"spec"`
	Status    Status         `json:"status"`
	PID       int            `json:"pid"` // also the process group id
	Ports     []int          `json:"ports,omitempty"`
	PortKinds map[int]string `json:"portKinds,omitempty"` // what speaks on each port: http, ws, debug, tcp, …
	LastPort  int            `json:"lastPort,omitempty"`
	ExitCode  int            `json:"exitCode"`
	StartedAt time.Time      `json:"startedAt"`
	RunningAt time.Time      `json:"runningAt,omitempty"` // when the first LISTEN port showed up
	EndedAt   time.Time      `json:"endedAt,omitempty"`
	CPU       float64        `json:"cpu,omitempty"`   // the process group's CPU, in percent of one core (while active)
	MemKB     int64          `json:"memKB,omitempty"` // the process group's resident memory (while active)
	Adopted   bool           `json:"adopted,omitempty"`
	// ProcStart: the group leader process's start time (e.g. `ps -o lstart=`), so that
	// after a restart we can detect if another process has since taken over the PID.
	ProcStart string `json:"procStart,omitempty"`
}
