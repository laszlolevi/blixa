package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"blixa/internal/git"
	"blixa/internal/task"
)

func TestStatusText(t *testing.T) {
	cases := []struct {
		info *task.Info
		want string
	}{
		{nil, "· –"},
		{&task.Info{Status: task.StatusStarting}, "● starting"},
		{&task.Info{Status: task.StatusRunning}, "● running"},
		{&task.Info{Status: task.StatusExited}, "✔ exit 0"},
		{&task.Info{Status: task.StatusExited, ExitCode: 2}, "✖ exit 2"},
		{&task.Info{Status: task.StatusCrashed}, "✖ crashed"},
		{&task.Info{Status: task.StatusStopped}, "■ stopped"},
		{&task.Info{Status: task.StatusUnknown}, "? unknown"},
	}
	for _, c := range cases {
		if got := statusText(c.info); got != c.want {
			t.Errorf("statusText(%+v) = %q, want %q", c.info, got, c.want)
		}
	}
}

func TestPortAndDuration(t *testing.T) {
	if got := portText(&task.Info{Ports: []int{3001, 9229}}); got != ":3001 +1" {
		t.Errorf("portText = %q", got)
	}
	if portText(nil) != "" || portText(&task.Info{}) != "" {
		t.Error("expected empty port text")
	}
	start := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	now := start.Add(2*time.Hour + 13*time.Minute)
	running := &task.Info{Status: task.StatusRunning, StartedAt: start}
	if got := durationText(running, now); got != "2h13m" {
		t.Errorf("running: %q", got)
	}
	fromRunning := &task.Info{Status: task.StatusRunning, StartedAt: start, RunningAt: start.Add(time.Hour)}
	if got := durationText(fromRunning, now); got != "1h13m" {
		t.Errorf("from running: %q", got)
	}
	if got := durationText(&task.Info{Status: task.StatusStarting, StartedAt: start}, now); got != "" {
		t.Errorf("starting: %q", got)
	}
	ended := &task.Info{Status: task.StatusExited, StartedAt: start, EndedAt: start.Add(102 * time.Second)}
	if got := durationText(ended, now); got != "1m42s" {
		t.Errorf("ended: %q", got)
	}
}

func TestGitAndBuiltinText(t *testing.T) {
	if got := gitText(git.Status{Branch: "main", Ahead: 1, Behind: 2, Dirty: true}, true); got != "main ↑1 ↓2 ✎" {
		t.Errorf("gitText = %q", got)
	}
	if gitText(git.Status{}, false) != "" {
		t.Error("expected empty text for an unknown git status")
	}
	end := time.Date(2026, 10, 9, 10, 31, 0, 0, time.Local)
	info := &task.Info{Spec: task.Spec{Kind: task.KindPull}, Status: task.StatusExited, EndedAt: end}
	if got := builtinText(info); got != "pull ✔ 10:31" {
		t.Errorf("builtinText = %q", got)
	}
	if builtinText(nil) != "" {
		t.Error("nil builtin -> empty")
	}
}

func TestWindow(t *testing.T) {
	if f, to := window(0, 5, 10); f != 0 || to != 5 {
		t.Errorf("small list: %d-%d", f, to)
	}
	if f, to := window(9, 20, 5); f != 5 || to != 10 {
		t.Errorf("scrolled: %d-%d", f, to)
	}
	if f, to := window(3, 20, 0); f != 0 || to != 20 {
		t.Errorf("unknown height: %d-%d", f, to)
	}
}

func TestTailLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.log")
	os.WriteFile(path, []byte(strings.Repeat("x\n", 10)+"a\nb\n"), 0o644)
	got := tailLines(path, 2)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("tailLines = %v", got)
	}
	if tailLines(filepath.Join(t.TempDir(), "nope"), 3) != nil {
		t.Fatal("expected nil for a missing file")
	}
}
