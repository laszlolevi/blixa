package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"blixa/internal/task"
)

func TestLogViewShowsLinesAndCloses(t *testing.T) {
	m, _ := testModel(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})
	m, _ = press(t, m, "down")
	m, cmd := press(t, m, "enter")
	if m.mode != modeLog || m.log.id != "/w/alpha/.:start" {
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
	if m.mode != modeLog || m.log.id != "/w/alpha/@builtin" {
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

func TestLogLineNumbersFollowTrimmingAndToggle(t *testing.T) {
	m, _ := testModel(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 5})
	m, _ = press(t, m, "down", "enter")
	lines := make([]string, maxLogLines+5)
	for i := range lines {
		lines[i] = "x"
	}
	m = update(t, m, logLinesMsg{gen: m.log.gen, lines: lines, closed: true})
	view := m.log.vp.View()
	if !strings.Contains(view, "10005 │ x") {
		t.Fatalf("the last line should keep its own number after trimming:\n%s", view)
	}
	m, _ = press(t, m, "#")
	if strings.Contains(m.log.vp.View(), "│") {
		t.Fatalf("# should hide the line numbers:\n%s", m.log.vp.View())
	}
}

func TestLogMouseWheelScrollsAndMouseIsReleasedOnExit(t *testing.T) {
	m, _ := testModel(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 5})
	m, _ = press(t, m, "down", "enter")
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "x"
	}
	m = update(t, m, logLinesMsg{gen: m.log.gen, lines: lines, closed: true})
	m = update(t, m, tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.log.follow || m.log.vp.AtBottom() {
		t.Fatal("scrolling up with the wheel should leave the bottom and stop following")
	}
	_, cmd := press(t, m, "esc")
	if cmd == nil || fmt.Sprintf("%T", cmd()) != "tea.disableMouseMsg" {
		t.Fatal("leaving the log view should release the mouse")
	}
}

func TestHighlightMarksEveryMatchAndKeepsTheLinesColors(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	st := lipgloss.NewStyle().Background(lipgloss.Color("3"))
	mark := func(s string) string { return st.Render(s) }

	if got, want := highlight("Error: an error", "error", st), mark("Error")+": an "+mark("error"); got != want {
		t.Fatalf("both matches, case-insensitively:\n%q\n%q", got, want)
	}
	red := "\x1b[31m"
	got := highlight(red+"fail here"+"\x1b[0m", "fail", st)
	if want := red + mark("fail") + red + " here\x1b[0m"; got != want {
		t.Fatalf("the line's own color should resume after the match:\n%q\n%q", got, want)
	}
	if got := highlight(red+"x", "31", st); got != red+"x" {
		t.Fatalf("a match inside an escape sequence must be left alone: %q", got)
	}
}

func TestSearchHighlightsWhileTyping(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	m, _ := testModel(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 10})
	m, _ = press(t, m, "down", "enter")
	m = update(t, m, logLinesMsg{gen: m.log.gen, lines: []string{"booting", "listening on 3000"}, closed: true})
	m, _ = press(t, m, "/")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("listen")})
	if !strings.Contains(m.log.vp.View(), styleMatch.Render("listen")) {
		t.Fatalf("the match should be highlighted while typing:\n%q", m.log.vp.View())
	}
	m, _ = press(t, m, "enter")
	if !strings.Contains(m.log.vp.View(), styleCurrentMatch.Render("listen")) {
		t.Fatalf("the found match should get the current-match color:\n%q", m.log.vp.View())
	}
}
