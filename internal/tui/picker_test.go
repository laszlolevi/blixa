package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/laszlolevi/blixa/internal/config"
	"github.com/laszlolevi/blixa/internal/git"
	"github.com/laszlolevi/blixa/internal/task"
)

func TestScriptPickerTogglesAndSaves(t *testing.T) {
	m, _ := testModel(t)
	m, _ = press(t, m, "e")
	if m.mode != modeScripts || len(m.picker.items) != 3 {
		t.Fatalf("mode=%v items=%d", m.mode, len(m.picker.items))
	}
	if !m.picker.items[0].checked || m.picker.items[1].checked || !m.picker.items[2].checked {
		t.Fatalf("initial checks: %+v", m.picker.items)
	}
	m, _ = press(t, m, "down", " ")
	if !m.picker.items[1].checked {
		t.Fatal("build was not checked")
	}
	saved, err := config.Load(m.deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.IsSelected("alpha", config.Ref{Dir: ".", Script: "build"}) {
		t.Fatalf("saved config: %+v", saved)
	}
	m, _ = press(t, m, "esc")
	if m.mode != modeMain || len(m.rows()) != 4 {
		t.Fatalf("mode=%v rows=%v", m.mode, labels(m.rows()))
	}
}

func TestScriptPickerFilter(t *testing.T) {
	m, _ := testModel(t)
	m, _ = press(t, m, "e", "d", "e", "v")
	if vis := m.picker.visible(); len(vis) != 1 || m.picker.items[vis[0]].label != "apps/web › dev" {
		t.Fatalf("filtered items: %v", vis)
	}
}

func TestCheckoutDirtyAsksThenStarts(t *testing.T) {
	m, fc := testModel(t)
	m = update(t, m, gitMsg{project: "alpha", status: git.Status{Branch: "main", Dirty: true}})
	m, _ = press(t, m, "c")
	if m.mode != modeConfirm {
		t.Fatal("expected confirmation for a dirty state")
	}
	m, cmd := press(t, m, "y")
	m = update(t, m, run(t, cmd))
	if m.mode != modeBranches || len(m.branches.lists[listLocal]) != 1 || len(m.branches.lists[listRemote]) != 1 {
		t.Fatalf("mode=%v lists=%+v", m.mode, m.branches.lists)
	}
	m, _ = press(t, m, "f", "e", "a", "t") // matches only the remote branch: the focus follows it
	if m.branches.focus != listRemote {
		t.Fatal("the focus should move to the list with the matches")
	}
	m, cmd = press(t, m, "enter")
	run(t, cmd)
	last := fc.specs[len(fc.specs)-1]
	if m.mode != modeMain || last.Kind != task.KindCheckout || last.Branch != "feature/x" || last.From != "origin/feature/x" || last.ProjectPath != "/w/alpha" {
		t.Fatalf("mode=%v spec=%+v", m.mode, last)
	}
}

func TestCheckoutCleanGoesStraightToPicker(t *testing.T) {
	m, _ := testModel(t)
	m = update(t, m, gitMsg{project: "alpha", status: git.Status{Branch: "main"}})
	m, cmd := press(t, m, "c")
	m = update(t, m, run(t, cmd))
	if m.mode != modeBranches {
		t.Fatalf("mode=%v", m.mode)
	}
}

// branchModel: the checkout picker opened on alpha (on "release", 7 ahead, 18 behind).
func branchModel(t *testing.T, branches []git.Branch) (Model, *fakeClient) {
	t.Helper()
	m, fc := testModel(t)
	m = update(t, m, gitMsg{project: "alpha", path: "/w/alpha", status: git.Status{Branch: "release", Ahead: 7, Behind: 18}})
	next, _ := m.openBranchPicker(branchesMsg{project: "alpha", branches: branches})
	return next.(Model), fc
}

func TestBranchPickerSplitsLocalAndRemote(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	m, _ := branchModel(t, []git.Branch{
		{Name: "release", Hash: "a", Date: now.Add(-time.Hour)},
		{Name: "release", Remote: "origin", Hash: "b", Date: now.Add(-2 * time.Hour)},
		{Name: "main", Hash: "c", Date: now.Add(-3 * 24 * time.Hour)},
		{Name: "main", Remote: "origin", Hash: "c", Date: now.Add(-3 * 24 * time.Hour)},
		{Name: "feature/fresh", Remote: "origin", Hash: "d", Date: now.Add(-5 * time.Minute)},
	})
	local, remote := m.branches.lists[listLocal], m.branches.lists[listRemote]
	if len(local) != 2 || len(remote) != 3 {
		t.Fatalf("local=%d remote=%d", len(local), len(remote))
	}
	if local[0].detail != "current ↑7 ↓18 · 1h ago" || local[1].detail != "3d ago" {
		t.Fatalf("local details: %q %q", local[0].detail, local[1].detail)
	}
	want := map[string]string{"origin/release": "≠ local · 2h ago", "origin/main": "= local · 3d ago", "origin/feature/fresh": "5m ago"}
	for _, it := range remote {
		if it.detail != want[it.b.FullName()] {
			t.Errorf("%s: %q, want %q", it.b.FullName(), it.detail, want[it.b.FullName()])
		}
	}
	m, _ = press(t, m, "tab")
	if m.branches.focus != listRemote {
		t.Fatal("tab should move to the remote list")
	}
	view := m.View()
	if !strings.Contains(view, "local (2)") || !strings.Contains(view, "remote (3)") || !strings.Contains(view, "origin/release") {
		t.Fatalf("two frames expected:\n%s", view)
	}
}

func TestCheckoutCurrentBranchDoesNothing(t *testing.T) {
	m, fc := branchModel(t, []git.Branch{{Name: "release", Hash: "a"}})
	m, cmd := press(t, m, "enter")
	if cmd != nil || len(fc.specs) != 0 || m.flash == "" {
		t.Fatal("checking out the current branch should only say so")
	}
}

func TestResetToRemoteAsksOnlyWhenCommitsWouldBeLost(t *testing.T) {
	branches := []git.Branch{{Name: "release", Hash: "a"}, {Name: "release", Remote: "origin", Hash: "b"}}
	for _, lost := range []int{0, 2} {
		m, fc := branchModel(t, branches)
		m.deps.LocalOnly = func(dir, local, upstream string) (int, error) {
			if dir != "/w/alpha" || local != "release" || upstream != "origin/release" {
				t.Fatalf("LocalOnly(%s, %s, %s)", dir, local, upstream)
			}
			return lost, nil
		}
		m, _ = press(t, m, "tab")
		m, cmd := press(t, m, "enter")
		next, cmd := m.Update(run(t, cmd))
		m = next.(Model)
		if lost > 0 {
			if m.mode != modeConfirm || !strings.Contains(m.confirm.text, "2 commit(s)") {
				t.Fatalf("lost=%d: expected a warning, mode=%v %q", lost, m.mode, m.confirm.text)
			}
			m, cmd = press(t, m, "enter")
		} else if m.mode != modeMain {
			t.Fatalf("nothing lost: no question expected, mode=%v", m.mode)
		}
		run(t, cmd)
		last := fc.specs[len(fc.specs)-1]
		if last.Branch != "release" || last.From != "origin/release" {
			t.Fatalf("lost=%d: spec=%+v", lost, last)
		}
	}
}
