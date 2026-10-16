package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/laszlolevi/blixa/internal/task"
)

// PortOwner: who is listening on a port -- an blixa task (TaskID) or an external process.
type PortOwner struct {
	TaskID  string `json:"taskId,omitempty"`
	PID     int    `json:"pid"`
	Command string `json:"command,omitempty"`
}

func (s *Supervisor) statePath() string { return filepath.Join(s.opts.StateDir, "state.json") }

func (s *Supervisor) saveLocked() error {
	if err := os.MkdirAll(s.opts.StateDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.listLocked(), "", "  ")
	if err != nil {
		return err
	}
	tmp := s.statePath() + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.statePath())
}

// load reads state.json; adopts live processes, sets dead ones to unknown.
func (s *Supervisor) load() error {
	data, err := os.ReadFile(s.statePath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var infos []task.Info
	if err := json.Unmarshal(data, &infos); err != nil {
		fmt.Fprintf(os.Stderr, "blixa: state.json is corrupt (%v), moved aside as state.json.corrupt\n", err)
		corrupt := s.statePath() + ".corrupt"
		if renameErr := os.Rename(s.statePath(), corrupt); renameErr != nil {
			return fmt.Errorf("moving state.json aside: %w", renameErr)
		}
		return nil
	}
	for _, info := range infos {
		if info.Status.Active() {
			// The Adopted field from state.json must not be taken at face value — a true
			// value left over from a previous run would skip the PID-reuse check below,
			// which is needed to detect if another process has since taken over the PID.
			info.Adopted = false
			// We only adopt it if the PID is still the group leader of the same process
			// we persisted — after a long shutdown the PID may be taken over by an entirely
			// different process (e.g. the user's terminal), and Stop/StopAll would then
			// kill it as a foreign process.
			if info.ProcStart != "" && isGroupLeader(info.PID) {
				if start, err := processStartTime(info.PID); err == nil && start == info.ProcStart {
					info.Adopted = true
				}
			}
			if !info.Adopted {
				info.Status = task.Transition(info.Status, task.EventLost)
				info.Ports = nil
				info.PortKinds = nil
				info.EndedAt = time.Now()
			}
		}
		s.tasks[info.Spec.ID()] = &entry{info: info}
	}
	return nil
}

func isGroupLeader(pid int) bool {
	if pid <= 0 || syscall.Kill(pid, 0) != nil {
		return false
	}
	pgid, err := syscall.Getpgid(pid)
	return err == nil && pgid == pid
}

// processStartTime: the process's start time (ps -o lstart=), used after a restart
// to detect if another process has since taken over the PID. We run ps with a fixed
// LC_ALL/TZ so the output doesn't depend on the daemon's current locale/timezone
// setting -- otherwise a daemon restarted in a different environment would never
// recognize the previously saved value.
func processStartTime(pid int) (string, error) {
	cmd := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid))
	cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// PortOwner: nil if no one is listening on the port.
func (s *Supervisor) PortOwner(port int) (*PortOwner, error) {
	s.mu.Lock()
	for id, e := range s.tasks {
		if !e.info.Status.Active() {
			continue
		}
		for _, p := range e.info.Ports {
			if p == port {
				s.mu.Unlock()
				return &PortOwner{TaskID: id, PID: e.info.PID}, nil
			}
		}
	}
	s.mu.Unlock()
	o, ok, err := lookupPortOwner(port)
	if err != nil || !ok {
		return nil, err
	}
	return &PortOwner{PID: o.PID, Command: o.Command}, nil
}
