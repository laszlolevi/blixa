package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"blixa/internal/task"
)

func splashModel(t *testing.T) (Model, *fakeClient) {
	t.Helper()
	m, fc := testModel(t)
	m.splash, m.splashMinDone = true, false
	return m, fc
}

func TestSplashShowsLogoUntilMinTimeAndScan(t *testing.T) {
	m, _ := splashModel(t)
	m.scanned = false
	m = update(t, m, windowSize(80, 24))
	if out := ansi.Strip(m.View()); !strings.Contains(out, "███") || !strings.Contains(out, "terminal UI for npm, pnpm, yarn & bun projects") {
		t.Fatalf("splash with the big logo expected:\n%s", out)
	}
	m = update(t, m, splashDoneMsg{})
	if !m.splash {
		t.Fatal("the splash must stay until the projects are loaded")
	}
	m = update(t, m, projectsMsg{projects: sampleProjects()})
	if m.splash {
		t.Fatal("the splash must close once the min time passed and projects are loaded")
	}
}

func TestSplashKeyDismissesWithoutAction(t *testing.T) {
	m, fc := splashModel(t)
	m, cmd := press(t, m, "p")
	if m.splash || len(fc.calls) != 0 || cmd != nil {
		t.Fatalf("a key must only dismiss the splash: splash=%v calls=%v", m.splash, fc.calls)
	}
}

func TestSplashSmallTerminalUsesSmallLogo(t *testing.T) {
	m, _ := splashModel(t)
	m = update(t, m, windowSize(30, 10))
	out := ansi.Strip(m.View())
	if strings.Contains(out, "███") || !strings.Contains(out, "┌┐ ┬") {
		t.Fatalf("small logo expected:\n%s", out)
	}
}

func TestRowsAreColoredAndCursorKeepsText(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	m, _ := testModel(t)
	spec := m.rows()[1].spec
	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: spec, Status: task.StatusRunning, Ports: []int{3000}}}})
	cols := m.columns(m.rows())
	plain := ansi.Strip(m.renderRow(m.rows()[1], cols))
	colored := m.renderRow(m.rows()[1], cols)
	if colored == plain || !strings.Contains(plain, "● running") || !strings.Contains(plain, ":3000") {
		t.Fatalf("a colored row with the same text expected:\nplain=%q\ncolored=%q", plain, colored)
	}
	m.cursor = 1
	out := m.View()
	if !strings.Contains(ansi.Strip(out), plain) {
		t.Fatalf("the cursor row must keep the same text:\n%s", ansi.Strip(out))
	}
}

func windowSize(w, h int) tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: w, Height: h} }
