package supervisor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/laszlolevi/blixa/internal/task"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("BLIXA_HELPER"); mode != "" {
		runHelper(mode)
		return
	}
	os.Exit(m.Run())
}

func runHelper(mode string) {
	switch mode {
	case "exit3":
		fmt.Println("bye")
		os.Exit(3)
	case "serve":
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			os.Exit(1)
		}
		fmt.Println("listening", ln.Addr())
		if os.Getenv("BLIXA_HELPER_CRASH") != "" {
			time.Sleep(1500 * time.Millisecond)
			os.Exit(7)
		}
		time.Sleep(time.Hour)
	case "tree":
		exe, _ := os.Executable()
		child := exec.Command(exe)
		child.Env = append(os.Environ(), "BLIXA_HELPER=serve")
		child.Stdout = os.Stdout
		child.Start()
		child.Wait()
	case "sleep":
		time.Sleep(time.Hour)
	case "orphan":
		exe, _ := os.Executable()
		child := exec.Command(exe)
		child.Env = append(os.Environ(), "BLIXA_HELPER=serve")
		pr, pw, err := os.Pipe()
		if err != nil {
			os.Exit(1)
		}
		child.Stdout = pw
		ready := make(chan struct{})
		go func() {
			sc := bufio.NewScanner(pr)
			for sc.Scan() {
				line := sc.Text()
				fmt.Println(line)
				if strings.Contains(line, "listening") {
					close(ready)
				}
			}
		}()
		if err := child.Start(); err != nil {
			os.Exit(1)
		}
		pw.Close()
		// We wait until the child is actually listening on the port (we only synchronize on
		// this, not on its shutdown), so the test can reliably read its port from the log.
		<-ready
		fmt.Println("child pid", child.Process.Pid)
		os.Exit(0)
	case "trapparent":
		// We start a child that traps SIGTERM and exits only after a short delay;
		// the leader (this process) does not trap it, so under default handling
		// it exits immediately on its own SIGTERM, well before its child. The child's
		// stdout points directly at the (real) log file — not relayed through a
		// goroutine running in the leader — so it can still write after the leader dies.
		exe, _ := os.Executable()
		child := exec.Command(exe)
		child.Env = append(os.Environ(), "BLIXA_HELPER=trapchild")
		child.Stdout = os.Stdout
		if err := child.Start(); err != nil {
			os.Exit(1)
		}
		time.Sleep(time.Hour)
	case "trapchild":
		sigc := make(chan os.Signal, 8)
		signal.Notify(sigc, syscall.SIGTERM)
		fmt.Println("ready")
		<-sigc
		fmt.Println("SIGTERM")
		deadline := time.After(300 * time.Millisecond)
		for {
			select {
			case <-sigc:
				fmt.Println("SIGTERM")
			case <-deadline:
				os.Exit(0)
			}
		}
	}
	os.Exit(0)
}

func helperCommand(env string) func(task.Spec) (string, error) {
	exe, _ := os.Executable()
	return func(task.Spec) (string, error) { return env + " exec " + shellQuote(exe), nil }
}

func newTestSupervisor(t *testing.T, stateDir, env string) *Supervisor {
	t.Helper()
	s, err := New(Options{
		StateDir:     stateDir,
		Shell:        "/bin/sh",
		Command:      helperCommand(env),
		PollInterval: 100 * time.Millisecond,
		StopTimeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)
	t.Cleanup(func() {
		s.StopAll()
		cancel()
	})
	return s
}

func testSpec(t *testing.T) task.Spec {
	return task.Spec{Project: "p", ProjectPath: t.TempDir(), Kind: task.KindScript, Dir: ".", Script: "x"}
}

func waitFor(t *testing.T, s *Supervisor, id string, cond func(task.Info) bool) task.Info {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, info := range s.List() {
			if info.Spec.ID() == id && cond(info) {
				return info
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout: %s, last state: %+v", id, s.List())
	return task.Info{}
}

func TestExitCodeAndLog(t *testing.T) {
	s := newTestSupervisor(t, t.TempDir(), "BLIXA_HELPER=exit3")
	spec := testSpec(t)
	info, err := s.Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != task.StatusStarting || info.PID <= 0 {
		t.Fatalf("start: %+v", info)
	}
	done := waitFor(t, s, spec.ID(), func(i task.Info) bool { return !i.Status.Active() })
	if done.Status != task.StatusExited || done.ExitCode != 3 || done.EndedAt.IsZero() {
		t.Fatalf("end: %+v", done)
	}
	path, ok := s.LogPathFor(spec.ID())
	if !ok {
		t.Fatal("no log path")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "bye") || !strings.Contains(string(data), "exit 3") {
		t.Fatalf("log: %q", data)
	}
}

func TestSubscribeReceivesChanges(t *testing.T) {
	s := newTestSupervisor(t, t.TempDir(), "BLIXA_HELPER=exit3")
	ch, cancel := s.Subscribe()
	defer cancel()
	if first := <-ch; len(first) != 0 {
		t.Fatalf("first snapshot: %+v", first)
	}
	spec := testSpec(t)
	if _, err := s.Start(spec); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case snap := <-ch:
			if len(snap) == 1 && snap[0].Status == task.StatusExited {
				return
			}
		case <-deadline:
			t.Fatal("no exited snapshot arrived")
		}
	}
}

// TestSubscribeCancelClosesChannel: after cancel(), the channel must close so that
// the reader goroutine can exit (not get stuck waiting on a value that never arrives).
func TestSubscribeCancelClosesChannel(t *testing.T) {
	s := newTestSupervisor(t, t.TempDir(), "BLIXA_HELPER=exit3")
	ch, cancel := s.Subscribe()
	<-ch // initial snapshot
	cancel()
	cancel() // calling cancel twice must not panic (double close)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case v, ok := <-ch:
			if !ok {
				return
			}
			_ = v
		case <-deadline:
			t.Fatal("the channel did not close")
		}
	}
}

func portClosed(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return true
	}
	conn.Close()
	return false
}

func TestPortDetectionAndStop(t *testing.T) {
	s := newTestSupervisor(t, t.TempDir(), "BLIXA_HELPER=serve")
	spec := testSpec(t)
	if _, err := s.Start(spec); err != nil {
		t.Fatal(err)
	}
	running := waitFor(t, s, spec.ID(), func(i task.Info) bool { return i.Status == task.StatusRunning })
	if len(running.Ports) == 0 || running.LastPort != running.Ports[0] {
		t.Fatalf("ports: %+v", running)
	}
	if err := s.Stop(spec.ID()); err != nil {
		t.Fatal(err)
	}
	stopped := waitFor(t, s, spec.ID(), func(i task.Info) bool { return !i.Status.Active() })
	if stopped.Status != task.StatusStopped || stopped.LastPort != running.LastPort {
		t.Fatalf("after stop: %+v", stopped)
	}
	if !portClosed(running.Ports[0]) {
		t.Fatal("the port is still open")
	}
}

func TestStopKillsWholeTree(t *testing.T) {
	s := newTestSupervisor(t, t.TempDir(), "BLIXA_HELPER=tree")
	spec := testSpec(t)
	if _, err := s.Start(spec); err != nil {
		t.Fatal(err)
	}
	running := waitFor(t, s, spec.ID(), func(i task.Info) bool { return i.Status == task.StatusRunning })
	if err := s.Stop(spec.ID()); err != nil {
		t.Fatal(err)
	}
	if !portClosed(running.Ports[0]) {
		t.Fatal("the grandchild process's port is still open")
	}
}

// TestStopDoesNotDoubleSignalChild: Stop already sends SIGTERM to the whole
// process group. If meanwhile the shell leader exits sooner than the rest of the
// group's members (e.g. because it doesn't trap the signal, while its child does
// and keeps cleaning up after itself), wait() must NOT send SIGTERM again — many dev
// servers interpret a second signal as a force-quit, which can interrupt a
// graceful shutdown.
func TestStopDoesNotDoubleSignalChild(t *testing.T) {
	s := newTestSupervisor(t, t.TempDir(), "BLIXA_HELPER=trapparent")
	spec := testSpec(t)
	if _, err := s.Start(spec); err != nil {
		t.Fatal(err)
	}
	path, ok := s.LogPathFor(spec.ID())
	if !ok {
		t.Fatal("no log path")
	}
	// We wait until the child signals that it has installed its SIGTERM handler.
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), "ready") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the child did not signal ready: %q", data)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := s.Stop(spec.ID()); err != nil {
		t.Fatal(err)
	}
	stopped := waitFor(t, s, spec.ID(), func(i task.Info) bool { return !i.Status.Active() })
	if stopped.Status != task.StatusStopped {
		t.Fatalf("after stop: %+v", stopped)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(data), "SIGTERM"); count != 1 {
		t.Fatalf("expected exactly one SIGTERM line, got %d: %q", count, data)
	}
}

// TestOrphanedChildrenAreTerminated: the shell leader (the "orphan" helper) exits without
// waiting for its child — wait() must still stop the remaining members of the group,
// otherwise they are left unsupervised (e.g. the port stays open).
//
// We deliberately use a very infrequent poll() (our own Options instead of
// newTestSupervisor): poll() watches the process group's ports independently of wait(),
// and if it happens to run while the child is already listening on a port but the shell
// leader hasn't exited yet, it sets the task to Running (correctly, per the existing
// semantics) — which would make the end state after the leader exits Crashed instead of
// Exited. That is existing behavior, independent of the orphan-termination implemented
// in wait(), which we don't want to test here.
func TestOrphanedChildrenAreTerminated(t *testing.T) {
	stateDir := t.TempDir()
	s, err := New(Options{
		StateDir:     stateDir,
		Shell:        "/bin/sh",
		Command:      helperCommand("BLIXA_HELPER=orphan"),
		PollInterval: time.Hour,
		StopTimeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		s.StopAll()
		cancel()
	})
	go s.Run(ctx)
	spec := testSpec(t)
	if _, err := s.Start(spec); err != nil {
		t.Fatal(err)
	}
	done := waitFor(t, s, spec.ID(), func(i task.Info) bool { return !i.Status.Active() })
	if done.Status != task.StatusExited || done.ExitCode != 0 {
		t.Fatalf("end: %+v", done)
	}
	path, ok := s.LogPathFor(spec.ID())
	if !ok {
		t.Fatal("no log path")
	}
	var data []byte
	deadlineLog := time.Now().Add(5 * time.Second)
	for {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "listening 127.0.0.1:") || time.Now().After(deadlineLog) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	idx := strings.Index(string(data), "listening 127.0.0.1:")
	if idx < 0 {
		t.Fatalf("cannot find the child's port in the log: %q", data)
	}
	rest := string(data)[idx+len("listening 127.0.0.1:"):]
	end := strings.IndexAny(rest, "\r\n")
	if end < 0 {
		end = len(rest)
	}
	portStr := rest[:end]
	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil {
		t.Fatalf("port parse: %q: %v", portStr, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !portClosed(port) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !portClosed(port) {
		t.Fatal("the child's port is still open")
	}
}

func TestCrashAfterRunning(t *testing.T) {
	s := newTestSupervisor(t, t.TempDir(), "BLIXA_HELPER=serve BLIXA_HELPER_CRASH=1")
	spec := testSpec(t)
	if _, err := s.Start(spec); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, spec.ID(), func(i task.Info) bool { return i.Status == task.StatusRunning })
	crashed := waitFor(t, s, spec.ID(), func(i task.Info) bool { return !i.Status.Active() })
	if crashed.Status != task.StatusCrashed || crashed.ExitCode != 7 {
		t.Fatalf("crash: %+v", crashed)
	}
}

func TestAlreadyRunningAndRestart(t *testing.T) {
	s := newTestSupervisor(t, t.TempDir(), "BLIXA_HELPER=sleep")
	spec := testSpec(t)
	first, err := s.Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(spec); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning, got: %v", err)
	}
	second, err := s.Restart(spec)
	if err != nil {
		t.Fatal(err)
	}
	if second.PID == first.PID || second.Status != task.StatusStarting {
		t.Fatalf("restart: first=%d second=%+v", first.PID, second)
	}
	if err := s.Stop("p/nincs:ilyen"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}
