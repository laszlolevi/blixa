package supervisor

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/laszlolevi/blixa/internal/task"
)

func copyState(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(from, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(to, "state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAdoptRunningTaskAndStopIt(t *testing.T) {
	dir1 := t.TempDir()
	s1 := newTestSupervisor(t, dir1, "BLIXA_HELPER=serve")
	spec := testSpec(t)
	if _, err := s1.Start(spec); err != nil {
		t.Fatal(err)
	}
	running := waitFor(t, s1, spec.ID(), func(i task.Info) bool { return i.Status == task.StatusRunning })

	dir2 := t.TempDir()
	copyState(t, dir1, dir2)
	s2 := newTestSupervisor(t, dir2, "BLIXA_HELPER=serve")
	adopted := waitFor(t, s2, spec.ID(), func(i task.Info) bool { return i.Adopted })
	if adopted.Status != task.StatusRunning || adopted.PID != running.PID {
		t.Fatalf("adoption: %+v", adopted)
	}
	if err := s2.Stop(spec.ID()); err != nil {
		t.Fatal(err)
	}
	stopped := waitFor(t, s2, spec.ID(), func(i task.Info) bool { return !i.Status.Active() })
	if stopped.Status != task.StatusStopped {
		t.Fatalf("after stop: %+v", stopped)
	}
	if !portClosed(running.Ports[0]) {
		t.Fatal("the port is still open")
	}
}

func TestProcessStartTimeStable(t *testing.T) {
	pid := os.Getpid()
	first, err := processStartTime(pid)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" {
		t.Fatal("empty process start time")
	}
	second, err := processStartTime(pid)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("not stable: %q != %q", first, second)
	}

	// The output must not depend on the daemon's own locale/timezone setting -- otherwise
	// a daemon restarted with a different LC_ALL/TZ would never adopt its old tasks.
	t.Setenv("LC_ALL", "hu_HU.UTF-8")
	t.Setenv("TZ", "Asia/Tokyo")
	withLocale, err := processStartTime(pid)
	if err != nil {
		t.Fatal(err)
	}
	if withLocale != first {
		t.Fatalf("locale/TZ-dependent: %q != %q", withLocale, first)
	}
}

// TestAdoptionRejectsReusedPID: if the persisted PID has since been taken over by another
// process (e.g. the user's terminal), it must not be adopted, because Stop/StopAll
// would then kill a foreign process.
func TestAdoptionRejectsReusedPID(t *testing.T) {
	c := exec.Command("sleep", "30")
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		c.Wait()
	})

	dir := t.TempDir()
	spec := testSpec(t)
	state := []task.Info{{
		Spec: spec, Status: task.StatusRunning, PID: c.Process.Pid,
		StartedAt: time.Now(), ProcStart: "bogus",
	}}
	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644)

	s := newTestSupervisor(t, dir, "BLIXA_HELPER=sleep")
	list := s.List()
	if len(list) != 1 || list[0].Status != task.StatusUnknown {
		t.Fatalf("list: %+v", list)
	}
	if syscall.Kill(c.Process.Pid, 0) != nil {
		t.Fatal("the foreign process should have stayed alive")
	}
}

// TestAdoptionResetsStaleAdoptedFlag: if Adopted=true was left in state.json from a
// previous run (e.g. after a reboot), this must not be taken at face value on restart --
// the PID-reuse check (isGroupLeader + matching ProcStart) must still run regardless,
// otherwise the daemon could later kill a foreign process group.
func TestAdoptionResetsStaleAdoptedFlag(t *testing.T) {
	c := exec.Command("sleep", "30")
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		c.Wait()
	})

	dir := t.TempDir()
	spec := testSpec(t)
	state := []task.Info{{
		Spec: spec, Status: task.StatusRunning, PID: c.Process.Pid,
		StartedAt: time.Now(), ProcStart: "bogus", Adopted: true,
	}}
	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644)

	s := newTestSupervisor(t, dir, "BLIXA_HELPER=sleep")
	list := s.List()
	if len(list) != 1 || list[0].Status != task.StatusUnknown || list[0].Adopted {
		t.Fatalf("list: %+v", list)
	}
	if syscall.Kill(c.Process.Pid, 0) != nil {
		t.Fatal("the foreign process should have stayed alive")
	}
}

func TestDeadTaskBecomesUnknown(t *testing.T) {
	c := exec.Command("true")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	spec := testSpec(t)
	state := []task.Info{{Spec: spec, Status: task.StatusRunning, PID: c.Process.Pid, StartedAt: time.Now()}}
	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644)

	s := newTestSupervisor(t, dir, "BLIXA_HELPER=sleep")
	list := s.List()
	if len(list) != 1 || list[0].Status != task.StatusUnknown || list[0].EndedAt.IsZero() {
		t.Fatalf("list: %+v", list)
	}
}

// TestBrokenStateFileIsSetAside: a corrupt state.json must not prevent the daemon from
// starting -- we move it aside as state.json.corrupt and start with an empty task list.
func TestBrokenStateFileIsSetAside(t *testing.T) {
	dir := t.TempDir()
	broken := []byte("{nope")
	if err := os.WriteFile(filepath.Join(dir, "state.json"), broken, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{StateDir: dir})
	if err != nil {
		t.Fatalf("a corrupt state.json should not cause an error: %v", err)
	}
	if list := s.List(); len(list) != 0 {
		t.Fatalf("expected an empty list, got: %+v", list)
	}
	data, err := os.ReadFile(filepath.Join(dir, "state.json.corrupt"))
	if err != nil {
		t.Fatalf("state.json.corrupt: %v", err)
	}
	if string(data) != string(broken) {
		t.Fatalf("state.json.corrupt content differs: %q", data)
	}
}

func TestPortOwner(t *testing.T) {
	s := newTestSupervisor(t, t.TempDir(), "BLIXA_HELPER=serve")
	spec := testSpec(t)
	if _, err := s.Start(spec); err != nil {
		t.Fatal(err)
	}
	running := waitFor(t, s, spec.ID(), func(i task.Info) bool { return i.Status == task.StatusRunning })
	owner, err := s.PortOwner(running.Ports[0])
	if err != nil || owner == nil || owner.TaskID != spec.ID() {
		t.Fatalf("own task: %+v, %v", owner, err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ext, err := s.PortOwner(ln.Addr().(*net.TCPAddr).Port)
	if err != nil || ext == nil || ext.TaskID != "" || ext.PID != os.Getpid() {
		t.Fatalf("external: %+v, %v", ext, err)
	}

	free, err := s.PortOwner(1) // nobody is listening on port 1
	if err != nil || free != nil {
		t.Fatalf("free port: %+v, %v", free, err)
	}
}
