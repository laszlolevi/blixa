package task

import "testing"

func TestSpecIDLabelWorkDir(t *testing.T) {
	root := Spec{Project: "p", ProjectPath: "/w/p", Kind: KindScript, Dir: ".", Script: "s:api:dev1"}
	if root.ID() != "p/.:s:api:dev1" || root.Label() != "s:api:dev1" || root.WorkDir() != "/w/p" {
		t.Errorf("root: %q %q %q", root.ID(), root.Label(), root.WorkDir())
	}
	ws := Spec{Project: "p", ProjectPath: "/w/p", Kind: KindScript, Dir: "apps/server", Script: "dev"}
	if ws.ID() != "p/apps/server:dev" || ws.Label() != "apps/server › dev" || ws.WorkDir() != "/w/p/apps/server" {
		t.Errorf("ws: %q %q %q", ws.ID(), ws.Label(), ws.WorkDir())
	}
	co := Spec{Project: "p", ProjectPath: "/w/p", Kind: KindCheckout, Branch: "main"}
	pull := Spec{Project: "p", ProjectPath: "/w/p", Kind: KindPull}
	if co.ID() != "p/@builtin" || pull.ID() != co.ID() || BuiltinID("p") != co.ID() {
		t.Errorf("builtin IDs: %q %q", co.ID(), pull.ID())
	}
	if co.Label() != "checkout main" || pull.Label() != "pull" || co.WorkDir() != "/w/p" {
		t.Errorf("builtin label: %q %q", co.Label(), pull.Label())
	}
}

func TestTransition(t *testing.T) {
	cases := []struct {
		from Status
		ev   Event
		want Status
	}{
		{StatusStarting, EventPortOpened, StatusRunning},
		{StatusRunning, EventPortOpened, StatusRunning},
		{StatusStarting, EventExited, StatusExited},
		{StatusRunning, EventExited, StatusCrashed},
		{StatusStarting, EventStopped, StatusStopped},
		{StatusRunning, EventStopped, StatusStopped},
		{StatusRunning, EventLost, StatusUnknown},
		{StatusExited, EventPortOpened, StatusExited},
		{StatusStopped, EventExited, StatusStopped},
		{StatusCrashed, EventLost, StatusCrashed},
	}
	for _, c := range cases {
		if got := Transition(c.from, c.ev); got != c.want {
			t.Errorf("Transition(%s, %d) = %s, want %s", c.from, c.ev, got, c.want)
		}
	}
	if !StatusStarting.Active() || !StatusRunning.Active() || StatusExited.Active() {
		t.Error("Active() wrong")
	}
}
