package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"

	"blixa/internal/config"
	"blixa/internal/discovery"
	"blixa/internal/git"
	"blixa/internal/supervisor"
	"blixa/internal/task"
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
		Branches:   func(string) ([]string, error) { return nil, nil },
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
	proj := m.renderRow(rows[0], cols)
	if strings.Contains(proj, long) || !strings.Contains(proj, "…") || !strings.Contains(proj, "↓2") {
		t.Fatalf("the long branch should be truncated, the ↓2 must remain: %q", proj)
	}
}
