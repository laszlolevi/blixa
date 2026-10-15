package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/laszlolevi/blixa/internal/config"
	"github.com/laszlolevi/blixa/internal/discovery"
	"github.com/laszlolevi/blixa/internal/task"
)

// rootsModel: a model with two root tabs, the second one being a real temp folder.
func rootsModel(t *testing.T) (Model, string) {
	t.Helper()
	m, _ := testModel(t)
	other := t.TempDir()
	m.deps.Scan = func(string) ([]discovery.Project, error) { return nil, nil }
	m.cfg.Roots = map[string]config.RootConfig{other: {}}
	return m, other
}

func TestTabSwitchesRootAndDropsTheOldScan(t *testing.T) {
	m, other := rootsModel(t)
	before := m.cfg.Root
	m, cmd := press(t, m, "tab")
	if m.cfg.Root != other || m.scanned || len(m.projects) != 0 || cmd == nil {
		t.Fatalf("tab should switch to %s and rescan: root=%s scanned=%v", other, m.cfg.Root, m.scanned)
	}
	m = update(t, m, projectsMsg{root: before, projects: sampleProjects()})
	if m.scanned {
		t.Fatal("a scan of the previous root must be dropped")
	}
	saved, err := config.Load(m.deps.ConfigPath)
	if err != nil || saved.Root != other {
		t.Fatalf("the new root should be saved: %v %v", saved.Root, err)
	}
	if _, ok := saved.Roots[before]; !ok {
		t.Fatal("the previous root should stay as a tab")
	}
	m, _ = press(t, m, "shift+tab")
	if m.cfg.Root != before || len(m.cfg.Selected("alpha")) == 0 {
		t.Fatalf("shift+tab should come back with the selections intact: %s", m.cfg.Root)
	}
}

func TestRootChooserCompletesAndOpens(t *testing.T) {
	m, _ := rootsModel(t)
	base := t.TempDir()
	for _, d := range []string{"website", "webapp", "oss", ".hidden"} {
		if err := os.Mkdir(filepath.Join(base, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m, _ = press(t, m, "o")
	if !m.chooser.active {
		t.Fatal("o should open the folder chooser")
	}
	m.chooser.input.SetValue(base + "/")
	m.chooser.input.CursorEnd()
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if got := strings.Join(m.chooser.matches, ","); got != "webapp,website" {
		t.Fatalf("matches: %s", got)
	}
	m, _ = press(t, m, "tab") // the common prefix
	if got := m.chooser.input.Value(); got != base+"/web" {
		t.Fatalf("after tab: %s", got)
	}
	m, _ = press(t, m, "tab", "tab") // cycles: webapp, website
	if got := m.chooser.input.Value(); got != base+"/website" {
		t.Fatalf("after cycling: %s", got)
	}
	m, _ = press(t, m, "enter")
	if m.chooser.active || m.cfg.Root != filepath.Join(base, "website") {
		t.Fatalf("enter should open the folder: root=%s", m.cfg.Root)
	}
	if !strings.Contains(m.View(), "website") {
		t.Fatal("the new folder should show up as a tab")
	}
}

func TestRootChooserRejectsAMissingFolder(t *testing.T) {
	m, _ := rootsModel(t)
	root := m.cfg.Root
	m, _ = press(t, m, "o")
	m.chooser.input.SetValue("/no/such/folder")
	m, _ = press(t, m, "enter")
	if !m.chooser.active || m.cfg.Root != root || m.flash == "" {
		t.Fatal("a missing folder should be refused, the chooser staying open")
	}
	m, _ = press(t, m, "esc")
	if m.chooser.active {
		t.Fatal("esc should close the chooser")
	}
}

func TestCloseRootTab(t *testing.T) {
	m, other := rootsModel(t)
	before := m.cfg.Root
	m, _ = press(t, m, "W")
	if m.mode != modeConfirm {
		t.Fatal("W should ask first")
	}
	if m, _ := press(t, m, "enter"); m.mode != modeConfirm {
		t.Fatal("enter should not close the tab")
	}
	m, cmd := press(t, m, "y")
	m = update(t, m, run(t, cmd))
	if m.cfg.Root != other {
		t.Fatalf("the neighbouring tab should become current: %s", m.cfg.Root)
	}
	if roots := m.cfg.KnownRoots(); len(roots) != 1 || roots[0] != other {
		t.Fatalf("the closed tab should be gone: %v (was %s)", roots, before)
	}
	m, _ = press(t, m, "W")
	if m.mode == modeConfirm || m.flash == "" {
		t.Fatal("the last tab can't be closed")
	}
}

func TestArrowsReachTheFolderTabs(t *testing.T) {
	m, other := rootsModel(t)
	before := m.cfg.Root
	m, _ = press(t, m, "up")
	if !m.folderFocus {
		t.Fatal("up on the first row should move into the folder tabs")
	}
	m, _ = press(t, m, "right")
	if m.cfg.Root != other || !m.folderFocus {
		t.Fatalf("right should switch the folder and keep the focus: %s", m.cfg.Root)
	}
	m, _ = press(t, m, "left")
	if m.cfg.Root != before {
		t.Fatalf("left should switch back: %s", m.cfg.Root)
	}
	m, _ = press(t, m, "down")
	if m.folderFocus || m.cursor != 0 {
		t.Fatal("down should go back to the project list")
	}
}

func TestFolderDetailsWhileTheTabsHaveTheFocus(t *testing.T) {
	m, _ := rootsModel(t)
	m.cfg.Root = "/w"
	start := m.rows()[1].spec // under /w/alpha
	elsewhere := start
	elsewhere.ProjectPath = "/other/alpha"
	m = update(t, m, tasksMsg{tasks: []task.Info{
		{Spec: start, Status: task.StatusRunning, Ports: []int{5173, 9229}},
		{Spec: elsewhere, Status: task.StatusRunning, Ports: []int{3000}},
	}})
	m, _ = press(t, m, "up")
	title, lines := m.details(m.rows())
	text := strings.Join(lines, "\n")
	if title != "/w" || !strings.Contains(text, "2  (1 with selected scripts)") || !strings.Contains(text, "1 task  ·  :5173") {
		t.Fatalf("folder details: %q\n%s", title, text)
	}
	if strings.Contains(text, ":3000") {
		t.Fatalf("a task under another folder must not count:\n%s", text)
	}
	m, _ = press(t, m, "down")
	if title, _ := m.details(m.rows()); !strings.HasPrefix(title, "alpha · ") {
		t.Fatalf("back on the list, the details follow the cursor again: %q", title)
	}
}
