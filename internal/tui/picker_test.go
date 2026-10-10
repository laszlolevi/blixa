package tui

import (
	"testing"

	"blixa/internal/config"
	"blixa/internal/git"
	"blixa/internal/task"
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
	if m.mode != modeBranches || len(m.picker.items) != 2 {
		t.Fatalf("mode=%v items=%v", m.mode, m.picker.items)
	}
	m, _ = press(t, m, "f", "e", "a", "t")
	m, cmd = press(t, m, "enter")
	run(t, cmd)
	last := fc.specs[len(fc.specs)-1]
	if m.mode != modeMain || last.Kind != task.KindCheckout || last.Branch != "feature/x" || last.ProjectPath != "/w/alpha" {
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
