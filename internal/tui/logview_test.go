package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"blixa/internal/task"
)

func TestLogViewShowsLinesAndCloses(t *testing.T) {
	m, _ := testModel(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})
	m, _ = press(t, m, "down")
	m, cmd := press(t, m, "enter")
	if m.mode != modeLog || m.log.id != "alpha/.:start" {
		t.Fatalf("mode=%v id=%q", m.mode, m.log.id)
	}
	m = update(t, m, run(t, cmd))
	if len(m.log.lines) != 2 || m.log.lines[0] != "hello" || m.log.lines[1] != logStreamClosedLine {
		t.Fatalf("lines: %v", m.log.lines)
	}
	m, _ = press(t, m, "esc")
	if m.mode != modeMain {
		t.Fatal("expected the main view after esc")
	}
}

func TestStaleLogLinesIgnored(t *testing.T) {
	m, _ := testModel(t)
	m, _ = press(t, m, "down", "enter")
	gen := m.log.gen
	m = update(t, m, logLinesMsg{gen: gen - 1, lines: []string{"old"}, closed: true})
	if len(m.log.lines) != 0 {
		t.Fatalf("lines from the old stream were included: %v", m.log.lines)
	}
}

func TestProjectRowLogOpensBuiltin(t *testing.T) {
	m, _ := testModel(t)
	m, _ = press(t, m, "l")
	if m.mode != modeMain || m.flash == "" {
		t.Fatal("expected a message when there's no operation log")
	}
	pull := task.Spec{Project: "alpha", ProjectPath: "/w/alpha", Kind: task.KindPull}
	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: pull, Status: task.StatusExited}}})
	m, _ = press(t, m, "l")
	if m.mode != modeLog || m.log.id != "alpha/@builtin" {
		t.Fatalf("mode=%v id=%q", m.mode, m.log.id)
	}
}

func TestFindNext(t *testing.T) {
	m, _ := testModel(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 5})
	m, _ = press(t, m, "down", "enter")
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "x"
	}
	lines[30] = "ERROR here"
	m = update(t, m, logLinesMsg{gen: m.log.gen, lines: lines, closed: true})
	m, _ = press(t, m, "/", "e", "r", "r", "o", "r", "enter")
	if m.log.vp.YOffset != 30 || m.log.follow {
		t.Fatalf("YOffset=%d follow=%v", m.log.vp.YOffset, m.log.follow)
	}
}

// TestFindNextReachesAllMatchesNearEnd: two matches at the very end of the list; repeated
// "n" presses must reach both and wrap around to the first one -- because the viewport
// clamps vp.YOffset (on the last screen), the search must continue from the actually
// found line, not from the displayed offset.
func TestFindNextReachesAllMatchesNearEnd(t *testing.T) {
	m, _ := testModel(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 5})
	m, _ = press(t, m, "down", "enter")
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "x"
	}
	lines[47] = "ERROR a"
	lines[49] = "ERROR b"
	m = update(t, m, logLinesMsg{gen: m.log.gen, lines: lines, closed: true})
	m, _ = press(t, m, "/", "e", "r", "r", "o", "r", "enter")
	if m.log.lastMatch != 47 {
		t.Fatalf("first match: lastMatch=%d", m.log.lastMatch)
	}
	m, _ = press(t, m, "n")
	if m.log.lastMatch != 49 {
		t.Fatalf("second match: lastMatch=%d", m.log.lastMatch)
	}
	m, _ = press(t, m, "n")
	if m.log.lastMatch != 47 {
		t.Fatalf("third match (wrapped to the first): lastMatch=%d", m.log.lastMatch)
	}
}
