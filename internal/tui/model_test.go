package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/laszlolevi/blixa/internal/config"
	"github.com/laszlolevi/blixa/internal/discovery"
	"github.com/laszlolevi/blixa/internal/git"
	"github.com/laszlolevi/blixa/internal/supervisor"
	"github.com/laszlolevi/blixa/internal/task"
)

type fakeClient struct {
	mu          sync.Mutex
	calls       []string
	specs       []task.Spec
	owner       *supervisor.PortOwner
	shutdownErr error
}

func (f *fakeClient) rec(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func (f *fakeClient) has(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == call {
			return true
		}
	}
	return false
}

func (f *fakeClient) Start(s task.Spec) (task.Info, error) {
	f.rec("start " + s.ID())
	f.mu.Lock()
	f.specs = append(f.specs, s)
	f.mu.Unlock()
	return task.Info{Spec: s}, nil
}

func (f *fakeClient) Restart(s task.Spec) (task.Info, error) {
	f.rec("restart " + s.ID())
	return task.Info{Spec: s}, nil
}

func (f *fakeClient) Stop(id string) error { f.rec("stop " + id); return nil }

func (f *fakeClient) PortOwner(port int) (*supervisor.PortOwner, error) {
	f.rec(fmt.Sprintf("portowner %d", port))
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.owner, nil
}

func (f *fakeClient) Kill(pid int) error {
	f.rec(fmt.Sprintf("kill %d", pid))
	f.mu.Lock()
	f.owner = nil
	f.mu.Unlock()
	return nil
}

func (f *fakeClient) Shutdown(stopAll bool) error {
	f.rec(fmt.Sprintf("shutdown %v", stopAll))
	return f.shutdownErr
}

func testModel(t *testing.T) (Model, *fakeClient) {
	t.Helper()
	fc := &fakeClient{}
	deps := Deps{
		Client:     fc,
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Subscribe:  func() (<-chan []task.Info, error) { return nil, errors.New("no daemon") },
		StreamLog: func(ctx context.Context, id string) (<-chan string, error) {
			ch := make(chan string, 1)
			ch <- "hello"
			close(ch)
			return ch, nil
		},
		GitStatus: func(string) (git.Status, error) { return git.Status{Branch: "main"}, nil },
		Branches: func(string) ([]git.Branch, error) {
			return []git.Branch{{Name: "main"}, {Name: "feature/x", Remote: "origin"}}, nil
		},
		Now: func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) },
	}
	m := New(deps, sampleConfig(), nil)
	m = update(t, m, projectsMsg{projects: sampleProjects()})
	return m, fc
}

// TestScanCmdUsesAbsoluteRoot: the daemon runs with $HOME as its cwd, so a relative
// `root` in config.json would be relative to the daemon's home directory, not to the
// user's expected working directory -- so scanCmd must pass an absolute path to Scan.
func TestScanCmdUsesAbsoluteRoot(t *testing.T) {
	cfg := sampleConfig()
	cfg.Root = "relative/dir"
	var gotRoot string
	deps := Deps{
		Scan: func(root string) ([]discovery.Project, error) {
			gotRoot = root
			return nil, nil
		},
	}
	m := New(deps, cfg, nil)
	cmd := m.scanCmd()
	cmd()
	if !filepath.IsAbs(gotRoot) {
		t.Fatalf("Scan did not receive an absolute root: %q", gotRoot)
	}
}

func update(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(Model)
}

func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func press(t *testing.T, m Model, keys ...string) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var next tea.Model
		next, cmd = m.Update(keyMsg(k))
		m = next.(Model)
	}
	return m, cmd
}

func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg
	}
	// a batch (e.g. reading the log + capturing the mouse): the first message of our own,
	// skipping bubbletea's terminal-control messages
	for _, c := range batch {
		if c == nil {
			continue
		}
		if msg := c(); !strings.HasPrefix(fmt.Sprintf("%T", msg), "tea.") {
			return msg
		}
	}
	t.Fatal("the batch has no message of our own")
	return nil
}

func TestNavigationShowAllAndCollapse(t *testing.T) {
	m, _ := testModel(t)
	if got := labels(m.rows()); !eq(got, []string{"P:alpha", "T:start", "T:apps/web › dev"}) {
		t.Fatalf("rows: %v", got)
	}
	m, _ = press(t, m, "a")
	if len(m.rows()) != 4 {
		t.Fatalf("expected 4 rows after a, got: %v", labels(m.rows()))
	}
	m, _ = press(t, m, "a", "enter")
	if len(m.rows()) != 1 {
		t.Fatalf("expected 1 row after collapsing, got: %v", labels(m.rows()))
	}
	m, _ = press(t, m, "/", "w", "e", "b", "enter")
	if got := labels(m.rows()); !eq(got, []string{"P:alpha", "T:apps/web › dev"}) {
		t.Fatalf("filter: %v", got)
	}
}

func TestStartStopRestart(t *testing.T) {
	m, fc := testModel(t)
	m, _ = press(t, m, "down")
	_, cmd := press(t, m, "s")
	run(t, cmd)
	_, cmd = press(t, m, "x")
	run(t, cmd)
	_, cmd = press(t, m, "r")
	run(t, cmd)
	for _, c := range []string{"start /w/alpha/.:start", "stop /w/alpha/.:start", "restart /w/alpha/.:start"} {
		if !fc.has(c) {
			t.Errorf("missing call: %s (calls: %v)", c, fc.calls)
		}
	}
}

func TestPullAndInstallFromProjectRow(t *testing.T) {
	m, fc := testModel(t)
	_, cmd := press(t, m, "p")
	run(t, cmd)
	_, cmd = press(t, m, "i")
	run(t, cmd)
	if len(fc.specs) != 2 || fc.specs[0].Kind != task.KindPull || fc.specs[1].Kind != task.KindInstall || fc.specs[0].ProjectPath != "/w/alpha" {
		t.Fatalf("specs: %+v", fc.specs)
	}
}

func TestBuiltinBlockedWhileRunning(t *testing.T) {
	m, fc := testModel(t)
	pull := task.Spec{Project: "alpha", ProjectPath: "/w/alpha", Kind: task.KindPull}
	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: pull, Status: task.StatusStarting}}})
	_, cmd := press(t, m, "i")
	msg := run(t, cmd).(actionMsg)
	if msg.err == nil || !strings.Contains(msg.err.Error(), "already running") || len(fc.calls) != 0 {
		t.Fatalf("msg=%+v calls=%v", msg, fc.calls)
	}
}

func TestPortConflictKillsThenStarts(t *testing.T) {
	m, fc := testModel(t)
	spec := m.rows()[1].spec
	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: spec, Status: task.StatusStopped, LastPort: 3001}}})
	fc.owner = &supervisor.PortOwner{PID: 42, Command: "node"}
	m, _ = press(t, m, "down")
	_, cmd := press(t, m, "s")
	m = update(t, m, run(t, cmd))
	if m.mode != modeConfirm || !strings.Contains(m.confirm.text, ":3001") || !strings.Contains(m.confirm.text, "PID 42") {
		t.Fatalf("mode=%v text=%q", m.mode, m.confirm.text)
	}
	m, cmd = press(t, m, "k")
	run(t, cmd)
	if m.mode != modeMain || !fc.has("kill 42") || !fc.has("start /w/alpha/.:start") {
		t.Fatalf("mode=%v calls=%v", m.mode, fc.calls)
	}
}

func TestShutdownConfirm(t *testing.T) {
	m, fc := testModel(t)
	m, _ = press(t, m, "Q")
	if m.mode != modeConfirm {
		t.Fatal("expected confirmation")
	}
	_, cmd := press(t, m, "y")
	if _, ok := run(t, cmd).(quitMsg); !ok || !fc.has("shutdown true") {
		t.Fatalf("calls: %v", fc.calls)
	}
}

func TestMissingProjectCanBeRemoved(t *testing.T) {
	m, _ := testModel(t)
	m.cfg.Projects["gone"] = config.Project{Scripts: []string{".:x"}}
	rows := m.rows()
	m.cursor = len(rows) - 1
	m, _ = press(t, m, "D")
	saved, err := config.Load(m.deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := saved.Projects["gone"]; ok {
		t.Fatal("the missing project remained")
	}
	if _, ok := saved.Projects["alpha"]; !ok {
		t.Fatal("the alpha project was lost")
	}
}

func TestViewRendersRows(t *testing.T) {
	m, _ := testModel(t)
	spec := m.rows()[1].spec
	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: spec, Status: task.StatusRunning, Ports: []int{3001}, StartedAt: time.Date(2026, 10, 9, 11, 0, 0, 0, time.UTC)}}})
	m = update(t, m, gitMsg{project: "alpha", status: git.Status{Branch: "main", Behind: 2}})
	out := m.View()
	for _, want := range []string{"blixa", "alpha", "main ↓2", "● running", ":3001", "1h00m", "daemon ●"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing from the view: %q\n%s", want, out)
		}
	}
}

// TestBranchesMsgDoesNotHijackLogMode: the branch fetch started by "c" (checkout) is
// asynchronous; if we've since entered the log view, the result must not pull us back
// out of the log into the main view/branch picker.
func TestBranchesMsgDoesNotHijackLogMode(t *testing.T) {
	m, _ := testModel(t)
	m = update(t, m, gitMsg{project: "alpha", status: git.Status{Branch: "main"}})
	m, cmd := press(t, m, "c")
	if m.mode != modeMain {
		t.Fatalf("a clean git state should not require confirmation: mode=%v", m.mode)
	}
	m, _ = press(t, m, "down", "enter")
	if m.mode != modeLog {
		t.Fatalf("opening the log failed: mode=%v", m.mode)
	}
	m = update(t, m, run(t, cmd))
	if m.mode != modeLog {
		t.Fatalf("branchesMsg overwrote the mode: %v", m.mode)
	}
}

// TestPortConflictMsgDoesNotHijackLogMode: the same problem with the port-conflict
// check's response -- if we've since entered the log view, stay there, and signal it via flash.
func TestPortConflictMsgDoesNotHijackLogMode(t *testing.T) {
	m, _ := testModel(t)
	spec := m.rows()[1].spec
	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: spec, Status: task.StatusStopped, LastPort: 3001}}})
	m, _ = press(t, m, "down", "enter")
	if m.mode != modeLog {
		t.Fatalf("opening the log failed: mode=%v", m.mode)
	}
	m = update(t, m, portConflictMsg{spec: spec, port: 3001, owner: supervisor.PortOwner{PID: 42, Command: "node"}})
	if m.mode != modeLog {
		t.Fatalf("portConflictMsg overwrote the mode: %v", m.mode)
	}
	if m.flash == "" {
		t.Fatal("expected a flash message about the lost result")
	}
}

func TestShutdownConfirmErrorDoesNotQuit(t *testing.T) {
	m, fc := testModel(t)
	fc.shutdownErr = errors.New("boom")
	m, _ = press(t, m, "Q")
	_, cmd := press(t, m, "y")
	msg := run(t, cmd)
	am, ok := msg.(actionMsg)
	if !ok || am.err == nil || am.err.Error() != "boom" {
		t.Fatalf("msg=%+v", msg)
	}
}

func TestDOnRegularRowExplains(t *testing.T) {
	m, _ := testModel(t)
	m, _ = press(t, m, "D")
	if !strings.Contains(m.flash, "missing") {
		t.Fatalf("expected an explanatory message, got: %q", m.flash)
	}
}

func TestNonGitProject(t *testing.T) {
	m, fc := testModel(t)
	m.projects[0].IsGit = false
	for _, k := range []string{"p", "c"} {
		next, cmd := press(t, m, k)
		if cmd != nil {
			run(t, cmd)
		}
		if !strings.Contains(next.flash, "not a git repository") {
			t.Errorf("%s: flash = %q", k, next.flash)
		}
	}
	if len(fc.calls) != 0 {
		t.Fatalf("a git operation must not start on a non-git project: %v", fc.calls)
	}
	if out := m.View(); !strings.Contains(out, "no git") {
		t.Fatalf("expected the 'no git' indicator in the view:\n%s", out)
	}
	if cmd := m.refreshAllGit(); cmd != nil {
		if msg, ok := cmd().(gitMsg); ok && msg.project == "alpha" {
			t.Fatal("must not request git status for a non-git project")
		}
	}
}

// TestRescanAddsAndRemovesProjects: a rescan while running picks up new and deleted
// projects, keeps the cursor on the same row and fetches git status only for new projects.
func TestRescanAddsAndRemovesProjects(t *testing.T) {
	m, _ := testModel(t)
	m, _ = press(t, m, "a", "down", "down", "down") // P:alpha T:start T:apps/web › dev [P:beta]
	if r := m.rows()[m.cursor]; r.project != "beta" {
		t.Fatalf("cursor on %v", r)
	}

	added := append([]discovery.Project{{Name: "aaa", Path: "/w/aaa", IsGit: true}}, sampleProjects()...)
	next, cmd := m.Update(projectsMsg{projects: added})
	m = next.(Model)
	if got := labels(m.rows()); !eq(got, []string{"P:aaa", "P:alpha", "T:start", "T:apps/web › dev", "P:beta"}) {
		t.Fatalf("rows after add: %v", got)
	}
	if r := m.rows()[m.cursor]; r.project != "beta" {
		t.Fatalf("cursor moved to %v", r)
	}
	msg, ok := run(t, cmd).(gitMsg)
	if !ok || msg.project != "aaa" {
		t.Fatalf("expected a git refresh of aaa only, got %#v", msg)
	}
	m = update(t, m, msg)

	m = update(t, m, projectsMsg{projects: sampleProjects()[:1]})
	if got := labels(m.rows()); !eq(got, []string{"P:alpha", "T:start", "T:apps/web › dev"}) {
		t.Fatalf("rows after removal: %v", got)
	}
	if m.cursor != 2 {
		t.Fatalf("cursor not clamped: %d", m.cursor)
	}
	if _, ok := m.gitStatus["aaa"]; ok {
		t.Fatal("git status of a removed project kept")
	}
}

// TestMissingProjectDoesNotWidenColumns: a missing project with a long name must not
// widen the columns, or removing it (D) would shift the status, port & duration columns.
func TestMissingProjectDoesNotWidenColumns(t *testing.T) {
	m, _ := testModel(t)
	before := m.columns(m.rows())
	m.cfg.Projects["a-really-long-project-name-that-is-gone"] = config.Project{Scripts: []string{".:x"}}
	if after := m.columns(m.rows()); after != before {
		t.Fatalf("columns changed: %+v -> %+v", before, after)
	}
	if out := m.View(); !strings.Contains(out, "a-really-long-project-name-that-is-gone  missing") {
		t.Fatalf("missing project not rendered in full:\n%s", out)
	}
}

// TestColumnsDoNotShrinkWhenProjectGoesMissing: when a project's folder is deleted, its row
// turns into "missing" -- the remaining rows' columns must stay where they were.
func TestColumnsDoNotShrinkWhenProjectGoesMissing(t *testing.T) {
	m, _ := testModel(t)
	m.cfg.Projects["a-long-project-name"] = config.Project{Scripts: []string{".:dev"}}
	long := discovery.Project{Name: "a-long-project-name", Path: "/w/long", Scripts: []discovery.Script{{Dir: ".", Name: "dev"}}}
	m = update(t, m, projectsMsg{projects: append(sampleProjects(), long)})
	before := strings.Split(m.View(), "\n")[1]
	m = update(t, m, projectsMsg{projects: sampleProjects()})
	if after := strings.Split(m.View(), "\n")[1]; after != before {
		t.Fatalf("the alpha row moved:\n%q\n%q", before, after)
	}
}

func TestBackgroundFetchSkipsBusyAndNonGitProjects(t *testing.T) {
	m, _ := testModel(t)
	m.showAll = true
	var mu sync.Mutex
	var fetched []string
	m.deps.Fetch = func(dir string) error {
		mu.Lock()
		fetched = append(fetched, dir)
		mu.Unlock()
		return nil
	}
	m.projects[0].IsGit, m.projects[1].IsGit = true, true
	pull := task.Spec{Project: "beta", ProjectPath: m.projects[1].Path, Kind: task.KindPull}
	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: pull, Status: task.StatusRunning}}})
	msg := m.fetchAll()()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		batch = tea.BatchMsg{func() tea.Msg { return msg }}
	}
	for _, c := range batch {
		if c != nil {
			m = update(t, m, c())
		}
	}
	if len(fetched) != 1 || fetched[0] != m.projects[0].Path {
		t.Fatalf("only the idle git project should be fetched: %v", fetched)
	}
}

func TestGitResultOfAPreviousRootIsDropped(t *testing.T) {
	m, _ := testModel(t)
	m = update(t, m, gitMsg{project: "alpha", path: "/elsewhere/alpha", status: git.Status{Branch: "stale"}})
	if m.gitStatus["alpha"].Branch == "stale" {
		t.Fatal("a same-named project's status from another root must not be applied")
	}
}
