// Package supervisor is the task supervisor running in the daemon: starts, watches, stops.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/laszlolevi/blixa/internal/task"
)

var (
	ErrAlreadyRunning = errors.New("the task is already running")
	ErrNotFound       = errors.New("no such task")
)

type Options struct {
	StateDir     string
	Shell        string                          // default $SHELL, or /bin/zsh if unset
	Command      func(task.Spec) (string, error) // default BuildCommand
	PollInterval time.Duration                   // default 2s
	StopTimeout  time.Duration                   // default 5s
	MaxLogSize   int64                           // default 10 MB
	Ports        func(pgid int) ([]int, error)   // default GroupListenPorts
	Usage        func() (map[int]Usage, error)   // default GroupUsage
}

type entry struct {
	info          task.Info
	stopRequested bool
	done          chan struct{} // closed when Wait returns; nil for an adopted task
}

type Supervisor struct {
	opts    Options
	mu      sync.Mutex
	tasks   map[string]*entry
	subs    map[int]chan []task.Info
	nextSub int
}

func New(opts Options) (*Supervisor, error) {
	if opts.Shell == "" {
		opts.Shell = os.Getenv("SHELL")
		if opts.Shell == "" {
			opts.Shell = "/bin/zsh"
		}
	}
	if opts.Command == nil {
		opts.Command = BuildCommand
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = 2 * time.Second
	}
	if opts.StopTimeout == 0 {
		opts.StopTimeout = 5 * time.Second
	}
	if opts.MaxLogSize == 0 {
		opts.MaxLogSize = 10 << 20
	}
	if opts.Ports == nil {
		opts.Ports = GroupListenPorts
	}
	if opts.Usage == nil {
		opts.Usage = GroupUsage
	}
	s := &Supervisor{opts: opts, tasks: map[string]*entry{}, subs: map[int]chan []task.Info{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Start starts the task in its own process group; ErrAlreadyRunning for a running task.
func (s *Supervisor) Start(spec task.Spec) (task.Info, error) {
	command, err := s.opts.Command(spec)
	if err != nil {
		return task.Info{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := spec.ID()
	prev, hadPrev := s.tasks[id]
	if hadPrev && prev.info.Status.Active() {
		return prev.info, ErrAlreadyRunning
	}
	logFile, err := OpenLog(LogPath(s.opts.StateDir, spec))
	if err != nil {
		return task.Info{}, err
	}
	fmt.Fprintf(logFile, "$ %s\n", command)
	cmd := exec.Command(s.opts.Shell, "-lc", command)
	cmd.Dir = spec.WorkDir()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(logFile, "blixa: failed to start: %v\n", err)
		logFile.Close()
		return task.Info{}, err
	}
	procStart, err := processStartTime(cmd.Process.Pid)
	if err != nil {
		fmt.Fprintf(logFile, "blixa: could not read process start time: %v\n", err)
	}
	e := &entry{
		info: task.Info{Spec: spec, Status: task.StatusStarting, PID: cmd.Process.Pid, StartedAt: time.Now(), ProcStart: procStart},
		done: make(chan struct{}),
	}
	if hadPrev {
		e.info.LastPort = prev.info.LastPort
	}
	s.tasks[id] = e
	s.changedLocked()
	go s.wait(e, cmd, logFile)
	return e.info, nil
}

func (s *Supervisor) wait(e *entry, cmd *exec.Cmd, logFile *os.File) {
	_ = cmd.Wait()
	code := cmd.ProcessState.ExitCode()
	pgid := cmd.Process.Pid

	// The shell leader exited, but there may still be processes left in the group
	// (e.g. the leader started `cmd &`, or npm died but its node child still holds the port).
	// Leaving these unsupervised would mean Stop later becomes a no-op on them.
	// But if the user requested the stop (Stop), it has already sent its own
	// SIGTERM→SIGKILL sequence to the group — a second SIGTERM would often hit
	// processes mid-shutdown (e.g. npm/node) as a force-quit, so in that case we
	// don't intervene again here; Stop will wait/escalate as needed.
	s.mu.Lock()
	stopRequested := e.stopRequested
	s.mu.Unlock()
	if !stopRequested && syscall.Kill(-pgid, 0) == nil {
		terminateGroup(pgid, s.opts.StopTimeout)
		fmt.Fprintf(logFile, "blixa: stopped the remaining processes in the group\n")
	}

	fmt.Fprintf(logFile, "blixa: exited (exit %d)\n", code)
	logFile.Close()

	s.mu.Lock()
	defer s.mu.Unlock()
	ev := task.EventExited
	if e.stopRequested {
		ev = task.EventStopped
	}
	e.info.Status = task.Transition(e.info.Status, ev)
	e.info.ExitCode = code
	e.info.EndedAt = time.Now()
	e.info.Ports = nil
	e.info.CPU, e.info.MemKB = 0, 0
	close(e.done)
	s.changedLocked()
}

// List: every task's state, sorted by ID.
func (s *Supervisor) List() []task.Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked()
}

func (s *Supervisor) listLocked() []task.Info {
	out := make([]task.Info, 0, len(s.tasks))
	for _, e := range s.tasks {
		out = append(out, e.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spec.ID() < out[j].Spec.ID() })
	return out
}

// LogPathFor: a known task's log file.
func (s *Supervisor) LogPathFor(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.tasks[id]
	if !ok {
		return "", false
	}
	return ExistingLogPath(s.opts.StateDir, e.info.Spec), true
}

// Subscribe: immediately receives the current state, then every change.
// For a slow reader, an unread old snapshot is dropped.
func (s *Supervisor) Subscribe() (<-chan []task.Info, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan []task.Info, 1)
	id := s.nextSub
	s.nextSub++
	s.subs[id] = ch
	ch <- s.listLocked()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subs, id)
			s.mu.Unlock()
			// The delete from subs and the close happen under the same lock as the
			// sends (changedLocked), so no more sends can arrive on this channel by
			// now, making the close safe.
			close(ch)
		})
	}
	return ch, cancel
}

func (s *Supervisor) changedLocked() {
	s.notifyLocked()
	if err := s.saveLocked(); err != nil {
		fmt.Fprintf(os.Stderr, "blixa: saving state.json: %v\n", err)
	}
}

// notifyLocked sends the current state to the subscribers, without saving it -- for
// changes not worth a write to disk (CPU and memory change on every poll).
func (s *Supervisor) notifyLocked() {
	snapshot := s.listLocked()
	for _, ch := range s.subs {
		select {
		case <-ch:
		default:
		}
		ch <- snapshot
	}
}

// Stop: SIGTERM to the whole process group, SIGKILL after StopTimeout. Waits for it to stop.
func (s *Supervisor) Stop(id string) error {
	s.mu.Lock()
	e, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	if !e.info.Status.Active() {
		s.mu.Unlock()
		return nil
	}
	e.stopRequested = true
	pgid, done := e.info.PID, e.done
	s.mu.Unlock()

	terminateGroup(pgid, s.opts.StopTimeout)
	if done != nil {
		<-done
		return nil
	}
	s.mu.Lock()
	s.finishAdoptedLocked(e, task.EventStopped)
	s.mu.Unlock()
	return nil
}

// terminateGroup: SIGTERM to the whole process group, SIGKILL after timeout. Waits for it to stop.
func terminateGroup(pgid int, timeout time.Duration) {
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	if !waitGroupGone(pgid, timeout) {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		waitGroupGone(pgid, 2*time.Second)
	}
}

func waitGroupGone(pgid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if syscall.Kill(-pgid, 0) != nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return syscall.Kill(-pgid, 0) != nil
}

// finishAdoptedLocked: closes out an adopted task (no Wait, so no exit code either).
func (s *Supervisor) finishAdoptedLocked(e *entry, ev task.Event) {
	if !e.info.Status.Active() {
		return
	}
	e.info.Status = task.Transition(e.info.Status, ev)
	e.info.Ports = nil
	e.info.CPU, e.info.MemKB = 0, 0
	e.info.EndedAt = time.Now()
	s.changedLocked()
}

func (s *Supervisor) Restart(spec task.Spec) (task.Info, error) {
	if err := s.Stop(spec.ID()); err != nil && !errors.Is(err, ErrNotFound) {
		return task.Info{}, err
	}
	return s.Start(spec)
}

// StopAll stops every running task in parallel.
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	var ids []string
	for id, e := range s.tasks {
		if e.info.Status.Active() {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = s.Stop(id)
		}(id)
	}
	wg.Wait()
}

// Run: every PollInterval, watches ports, checks adopted tasks' liveness, truncates logs.
func (s *Supervisor) Run(ctx context.Context) {
	t := time.NewTicker(s.opts.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.poll()
		}
	}
}

func (s *Supervisor) poll() {
	s.mu.Lock()
	var active []*entry
	for _, e := range s.tasks {
		if e.info.Status.Active() {
			active = append(active, e)
		}
	}
	s.mu.Unlock()

	for _, e := range active {
		s.mu.Lock()
		pid, spec, adopted := e.info.PID, e.info.Spec, e.done == nil
		s.mu.Unlock()

		if adopted && syscall.Kill(-pid, 0) != nil {
			s.mu.Lock()
			ev := task.EventLost
			if e.stopRequested {
				ev = task.EventStopped
			}
			s.finishAdoptedLocked(e, ev)
			s.mu.Unlock()
			continue
		}
		_ = TruncateHead(ExistingLogPath(s.opts.StateDir, spec), s.opts.MaxLogSize)
		found, err := s.opts.Ports(pid)
		if err != nil {
			continue
		}
		s.mu.Lock()
		if ports := mergePorts(e.info.Ports, found); e.info.Status.Active() && !equalInts(ports, e.info.Ports) {
			e.info.Ports = ports
			if len(ports) > 0 {
				e.info.LastPort = ports[0]
				e.info.Status = task.Transition(e.info.Status, task.EventPortOpened)
				if e.info.RunningAt.IsZero() {
					e.info.RunningAt = time.Now()
				}
			}
			s.changedLocked()
		}
		s.mu.Unlock()
	}
	s.sampleUsage(active)
}

// sampleUsage: the active tasks' CPU and memory; sent to the TUI, but not saved.
func (s *Supervisor) sampleUsage(active []*entry) {
	if len(active) == 0 {
		return
	}
	usage, err := s.opts.Usage()
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, e := range active {
		if !e.info.Status.Active() {
			continue
		}
		u := usage[e.info.PID] // PID is also the process group id
		if u.CPU != e.info.CPU || u.MemKB != e.info.MemKB {
			e.info.CPU, e.info.MemKB = u.CPU, u.MemKB
			changed = true
		}
	}
	if changed {
		s.notifyLocked()
	}
}

// mergePorts: the currently open ports in the order they were first seen -- the ones
// still open from prev keep their place, new ones go to the end. So Ports[0] is the
// first port the task opened (its primary port, usually the dev server).
func mergePorts(prev, found []int) []int {
	open := map[int]bool{}
	for _, p := range found {
		open[p] = true
	}
	var ports []int
	seen := map[int]bool{}
	for _, p := range prev {
		if open[p] {
			ports = append(ports, p)
			seen[p] = true
		}
	}
	for _, p := range found {
		if !seen[p] {
			ports = append(ports, p)
		}
	}
	return ports
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
