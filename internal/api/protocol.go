// Package api is the socket protocol between the TUI and the daemon (newline-delimited JSON).
// One request per connection; the response to "subscribe" and "log" is a continuous stream.
package api

import (
	"blixa/internal/supervisor"
	"blixa/internal/task"
)

type Request struct {
	Op      string     `json:"op"` // version, list, start, restart, stop, portowner, kill, subscribe, log, shutdown
	Spec    *task.Spec `json:"spec,omitempty"`
	ID      string     `json:"id,omitempty"`
	Port    int        `json:"port,omitempty"`
	PID     int        `json:"pid,omitempty"`
	StopAll bool       `json:"stopAll,omitempty"`
}

type Response struct {
	Error   string                `json:"error,omitempty"`
	Version string                `json:"version,omitempty"`
	Tasks   []task.Info           `json:"tasks,omitempty"`
	Task    *task.Info            `json:"task,omitempty"`
	Owner   *supervisor.PortOwner `json:"owner,omitempty"`
	Line    *string               `json:"line,omitempty"`
}

func errResponse(err error) Response {
	if err != nil {
		return Response{Error: err.Error()}
	}
	return Response{}
}
