package supervisor

import (
	"testing"

	"blixa/internal/task"
)

func TestParseGroupUsageSumsPerGroup(t *testing.T) {
	out := "  100   1.5  2048\n  100  10,0  1024\n  200   0.0   512\n garbage\n"
	u := ParseGroupUsage(out)
	if g := u[100]; g.CPU != 11.5 || g.MemKB != 3072 {
		t.Fatalf("group 100: %+v", g)
	}
	if g := u[200]; g.CPU != 0 || g.MemKB != 512 {
		t.Fatalf("group 200: %+v", g)
	}
	if len(u) != 2 {
		t.Fatalf("groups: %v", u)
	}
}

func TestPollReportsAndClearsUsage(t *testing.T) {
	s := newTestSupervisor(t, t.TempDir(), "BLIXA_HELPER=sleep")
	spec := testSpec(t)
	if _, err := s.Start(spec); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, spec.ID(), func(i task.Info) bool { return i.MemKB > 0 })
	if err := s.Stop(spec.ID()); err != nil {
		t.Fatal(err)
	}
	if info := waitFor(t, s, spec.ID(), func(i task.Info) bool { return !i.Status.Active() }); info.MemKB != 0 || info.CPU != 0 {
		t.Fatalf("a stopped task should have no usage: %+v", info)
	}
}
