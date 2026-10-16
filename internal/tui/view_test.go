package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/laszlolevi/blixa/internal/config"
	"github.com/laszlolevi/blixa/internal/discovery"
	"github.com/laszlolevi/blixa/internal/git"
	"github.com/laszlolevi/blixa/internal/supervisor"
	"github.com/laszlolevi/blixa/internal/task"
)

// TestViewRowBudgetAccountsForCfgErrAndCrashPanel: the config-error line and the
// crash-panel lines also "consume" the row space -- if View doesn't account for this,
// the rendered text can be taller than the actual height, and the header can scroll off.
func TestViewRowBudgetAccountsForCfgErrAndCrashPanel(t *testing.T) {
	fc := &fakeClient{}
	stateDir := t.TempDir()
	deps := Deps{
		Client:     fc,
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		StateDir:   stateDir,
		Subscribe:  func() (<-chan []task.Info, error) { return nil, errors.New("no daemon") },
		StreamLog:  func(ctx context.Context, id string) (<-chan string, error) { return nil, errors.New("no log") },
		GitStatus:  func(string) (git.Status, error) { return git.Status{}, nil },
		Branches:   func(string) ([]git.Branch, error) { return nil, nil },
		Now:        func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) },
	}

	var scripts []discovery.Script
	for i := 0; i < 15; i++ {
		scripts = append(scripts, discovery.Script{Dir: ".", Name: "s" + string(rune('0'+i/10)) + string(rune('0'+i%10))})
	}
	refs := make([]string, len(scripts))
	for i, s := range scripts {
		refs[i] = config.Ref{Dir: s.Dir, Script: s.Name}.String()
	}
	cfg := config.Default()
	cfg.Projects["big"] = config.Project{Scripts: refs}
	proj := discovery.Project{Name: "big", Path: "/w/big", Scripts: scripts}

	m := New(deps, cfg, nil)
	m = update(t, m, projectsMsg{projects: []discovery.Project{proj}})
	m.cfgErr = errors.New("bad config") // only setting the flag, not replacing cfg
	m.width, m.height = 80, 20

	crashSpec := task.Spec{Project: "big", ProjectPath: "/w/big", Kind: task.KindScript, Dir: ".", Script: scripts[0].Name}
	logPath := supervisor.LogPath(stateDir, crashSpec)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("l1\nl2\nl3\nl4\nl5\nl6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: crashSpec, Status: task.StatusExited, ExitCode: 1}}})

	rows := m.rows()
	if len(rows) != 16 {
		t.Fatalf("row count: %d, got: %v", len(rows), labels(rows))
	}
	m.cursor = 1 // the first task row (crashSpec)

	out := m.View()
	lines := strings.Split(out, "\n")
	if len(lines) > m.height {
		t.Fatalf("the view (%d lines) overflows the height (%d):\n%s", len(lines), m.height, out)
	}
	if !strings.Contains(lines[0], "blixa") {
		t.Fatalf("the header is not the first line: %q", lines[0])
	}
}

func TestFooterWidthAware(t *testing.T) {
	m, _ := testModel(t)
	m.width = len(footer) - 1
	out := m.View()
	if !strings.Contains(out, shortFooter) {
		t.Fatalf("expected the short footer on a narrow window:\n%s", out)
	}
	if strings.Contains(out, footer) {
		t.Fatalf("the full footer doesn't fit, yet it appeared:\n%s", out)
	}

	m.width = len(footer) + 10
	out = m.View()
	if !strings.Contains(out, footer) {
		t.Fatalf("expected the full footer on a wide window:\n%s", out)
	}
}

func TestClockTickRearms(t *testing.T) {
	m, _ := testModel(t)
	if _, cmd := m.Update(clockTickMsg{}); cmd == nil {
		t.Fatal("clockTickMsg must reschedule itself so the elapsed time keeps updating")
	}
}

// statusCol: the display column of the status symbol in the row.
func statusCol(line string) int {
	for _, sym := range []string{"·", "●", "■", "✔", "✖", "?"} {
		if i := strings.Index(line, sym); i >= 0 {
			return runewidth.StringWidth(line[:i])
		}
	}
	return -1
}

func TestColumnsAlignAndLongBranchIsTruncated(t *testing.T) {
	m, _ := testModel(t)
	long := "feature/TICKET-1234-a-very-long-branch-name-that-never-seems-to-end"
	m = update(t, m, gitMsg{project: "alpha", status: git.Status{Branch: long, Behind: 2}})
	rows := m.rows()
	cols := m.columns(rows)
	start := m.renderRow(rows[1], cols)
	dev := m.renderRow(rows[2], cols)
	if c1, c2 := statusCol(start), statusCol(dev); c1 < 0 || c1 != c2 {
		t.Fatalf("the status columns are not aligned: %d vs %d\n%s\n%s", c1, c2, start, dev)
	}
	if proj := m.renderRow(rows[0], cols.gitRoom(60)); strings.Contains(proj, long) || !strings.Contains(proj, "…") || !strings.Contains(proj, "↓2") {
		t.Fatalf("in a narrow frame the long branch should be truncated, the ↓2 must remain: %q", proj)
	}
	if proj := m.renderRow(rows[0], cols.gitRoom(140)); !strings.Contains(proj, long+" ↓2") {
		t.Fatalf("in a wide frame the whole branch should fit: %q", proj)
	}
	_, details := m.details(rows)
	if !strings.Contains(strings.Join(details, "\n"), long+" ↓2") {
		t.Fatalf("the details should show the whole branch:\n%s", strings.Join(details, "\n"))
	}
}

// TestPackageManagerIsInTheDetailsOnly: the package manager isn't a column of the project
// rows (that's room for the branch), the details frame shows it.
func TestPackageManagerIsInTheDetailsOnly(t *testing.T) {
	m, _ := testModel(t)
	m.projects[0].PackageManager = "pnpm"
	rows := m.rows()
	if row := m.renderRow(rows[0], m.columns(rows)); strings.Contains(row, "pnpm") {
		t.Fatalf("no package manager in the row: %q", row)
	}
	title, details := m.details(rows)
	if !strings.HasPrefix(title, "alpha · ") || !strings.Contains(strings.Join(details, "\n"), "pnpm") {
		t.Fatalf("the details should show it: %q\n%s", title, strings.Join(details, "\n"))
	}
}

func TestPortsShowWhatSpeaksOnThem(t *testing.T) {
	m, _ := testModel(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 20})
	spec := m.rows()[1].spec
	kinds := map[int]string{5173: "http", 1883: "mqtt", 8443: "https", 9229: "debug"}
	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: spec, Status: task.StatusRunning, Ports: []int{5173, 1883, 8443, 9229}, PortKinds: kinds}}})
	m.cursor = 1
	_, details := m.details(m.rows())
	raw := strings.Join(details, "\n")
	panel := ansi.Strip(raw)
	if !strings.Contains(panel, "primary port  :5173 http  →  http://localhost:5173") ||
		!strings.Contains(panel, "other ports   :1883 mqtt  :8443 https  :9229 debug") {
		t.Fatalf("the panel should show each port's protocol:\n%s", panel)
	}
	if !strings.Contains(raw, hyperlink("https://localhost:8443", ":8443")) {
		t.Fatalf("an https port should link to https://: %q", raw)
	}
	if strings.Contains(raw, "localhost:1883") || strings.Contains(raw, "localhost:9229") {
		t.Fatalf("a port the browser can't open should not be a link: %q", raw)
	}

	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: spec, Status: task.StatusRunning, Ports: []int{1883}, PortKinds: map[int]string{1883: "mqtt"}}}})
	_, details = m.details(m.rows())
	raw = strings.Join(details, "\n")
	if panel := ansi.Strip(raw); !strings.Contains(panel, "primary port  :1883 mqtt\n") || strings.Contains(raw, "localhost:1883") {
		t.Fatalf("an mqtt primary port: no URL, no link:\n%s", panel)
	}
	if row := m.renderRow(m.rows()[1], m.cols); strings.Contains(row, "localhost:1883") {
		t.Fatalf("an mqtt primary port should not be a link in the row: %q", row)
	}
}

func TestKeysStayAtTheBottomAndPortsShowPrimary(t *testing.T) {
	m, _ := testModel(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 20})
	spec := m.rows()[1].spec
	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: spec, Status: task.StatusRunning, Ports: []int{5173, 3000, 9229}}}})
	m.cursor = 1
	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		t.Fatalf("the view should fill the screen (%d lines), got %d", m.height, len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "quit") {
		t.Fatalf("the keys should be on the last line: %q", lines[len(lines)-1])
	}
	row := m.renderRow(m.rows()[1], m.cols)
	if !strings.Contains(ansi.Strip(row), ":5173 +2") {
		t.Fatalf("the row should show the first-opened port and the count of the rest: %q", row)
	}
	if !strings.Contains(row, hyperlink("http://localhost:5173", ":5173")) {
		t.Fatalf("the primary port should be a link: %q", row)
	}
	_, details := m.details(m.rows())
	panel := ansi.Strip(strings.Join(details, "\n"))
	if !strings.Contains(panel, "primary port  :5173  →  http://localhost:5173") || !strings.Contains(panel, "other ports   :3000  :9229") {
		t.Fatalf("the panel should list the primary and the other ports:\n%s", panel)
	}
	if !strings.Contains(panel, "usage         –") {
		t.Fatalf("no usage measured yet: a dash\n%s", panel)
	}
	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: m.rows()[1].spec, Status: task.StatusRunning, CPU: 12.4, MemKB: 340 << 10}}})
	if _, details := m.details(m.rows()); !strings.Contains(ansi.Strip(strings.Join(details, "\n")), "usage         cpu 12%  ·  mem 340 MB") {
		t.Fatalf("usage:\n%s", strings.Join(details, "\n"))
	}
}

// TestNothingMovesWhenPortsShowUp: the details frame has a fixed height, so a task's ports
// appearing doesn't shift the frames or the keys.
func TestNothingMovesWhenPortsShowUp(t *testing.T) {
	m, _ := testModel(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 20})
	m.cursor = 1
	spec := m.rows()[1].spec
	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: spec, Status: task.StatusStarting}}})
	before := strings.Split(m.View(), "\n")
	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: spec, Status: task.StatusRunning, Ports: []int{3000, 3001}}}})
	after := strings.Split(m.View(), "\n")
	if len(before) != m.height || len(after) != m.height {
		t.Fatalf("the view should fill the screen: %d / %d lines", len(before), len(after))
	}
	for i := range before {
		if strings.HasPrefix(before[i], "╭") != strings.HasPrefix(after[i], "╭") {
			t.Fatalf("a frame moved at line %d:\n%s\n---\n%s", i, strings.Join(before, "\n"), strings.Join(after, "\n"))
		}
	}
}

func TestNarrowFrameKeepsTheOperationWhole(t *testing.T) {
	m, _ := testModel(t)
	m.showAll = true
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 18})
	m = update(t, m, gitMsg{project: "beta", status: git.Status{Branch: "feature/a-rather-long-branch-name-for-a-narrow-frame"}})
	co := task.Spec{Project: "beta", ProjectPath: "/w/beta", Kind: task.KindCheckout, Branch: "master"}
	m = update(t, m, tasksMsg{tasks: []task.Info{{Spec: co, Status: task.StatusExited, ExitCode: 1}}})
	if !strings.Contains(m.View(), "checkout master ✖ 00:00") {
		t.Fatalf("the branch should give way, not the operation:\n%s", m.View())
	}
}

// TestHelpKeysFit: every key of the ? screen fits its column, nothing is cut with "…".
func TestHelpKeysFit(t *testing.T) {
	for _, sec := range helpSections {
		for _, k := range sec.keys {
			if runewidth.StringWidth(k[0]) >= 15 || runewidth.StringWidth(k[1]) >= 26 {
				t.Errorf("%s: %q / %q doesn't fit", sec.title, k[0], k[1])
			}
		}
	}
}

func TestVersionInTheBottomRightCorner(t *testing.T) {
	m, _ := testModel(t)
	m.deps.Version = "v1.2.3"
	m = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 20})
	lines := strings.Split(m.View(), "\n")
	last := lines[len(lines)-1]
	if !strings.HasSuffix(last, "v1.2.3") || runewidth.StringWidth(last) != m.width {
		t.Fatalf("the version should end the last line, at the right edge: %q", last)
	}
	m = update(t, m, tea.WindowSizeMsg{Width: 90, Height: 20}) // only the short keys fit, with the version
	if last := strings.Split(m.View(), "\n")[19]; !strings.HasPrefix(last, "s/x/r") || !strings.HasSuffix(last, "v1.2.3") {
		t.Fatalf("narrow: short keys and the version: %q", last)
	}
	m = update(t, m, tea.WindowSizeMsg{Width: 76, Height: 20}) // not even that: the keys win
	if last := strings.Split(m.View(), "\n")[19]; strings.Contains(last, "v1.2.3") {
		t.Fatalf("too narrow for the version: %q", last)
	}
}
