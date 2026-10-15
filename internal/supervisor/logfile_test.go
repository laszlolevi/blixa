package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laszlolevi/blixa/internal/task"
)

func TestLogPath(t *testing.T) {
	ws := task.Spec{Project: "p", ProjectPath: "/w/p", Kind: task.KindScript, Dir: "apps/server", Script: "dev"}
	got := LogPath("/s", ws)
	if dir, file := filepath.Split(got); !strings.HasPrefix(dir, "/s/logs/p-") || file != "apps%2Fserver:dev.log" {
		t.Errorf("ws: %s", got)
	}
	pull := task.Spec{Project: "p", ProjectPath: "/w/p", Kind: task.KindPull}
	if got := LogPath("/s", pull); filepath.Base(got) != "@builtin.log" || filepath.Dir(got) != filepath.Dir(LogPath("/s", ws)) {
		t.Errorf("pull: %s", got)
	}
	other := ws
	other.ProjectPath = "/other/p" // the same name under another root
	if LogPath("/s", other) == LogPath("/s", ws) {
		t.Error("same-named projects under different roots must not share a log")
	}
}

func TestOpenLogRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "p", "x.log")
	f, err := OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("first\n")
	f.Close()

	f, err = OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("second\n")
	f.Close()

	cur, _ := os.ReadFile(path)
	old, _ := os.ReadFile(path + ".1")
	if string(cur) != "second\n" || string(old) != "first\n" {
		t.Fatalf("cur=%q old=%q", cur, old)
	}
}

func TestTruncateHead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.log")
	var b strings.Builder
	for i := 0; i < 300; i++ {
		b.WriteString("0123456789\n") // 11 bytes/line, 3300 bytes
	}
	b.WriteString("last\n")
	os.WriteFile(path, []byte(b.String()), 0o644)

	if err := TruncateHead(path, 1000); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if len(data) > 500 || !strings.HasSuffix(string(data), "last\n") || !strings.HasPrefix(string(data), "0123456789\n") {
		t.Fatalf("after truncation %d bytes: %q...", len(data), string(data[:20]))
	}
	if err := TruncateHead(path, 1000); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(path)
	if string(again) != string(data) {
		t.Fatal("a small file must not be touched")
	}
	if err := TruncateHead(filepath.Join(t.TempDir(), "missing.log"), 10); err != nil {
		t.Fatalf("a missing file must not be an error: %v", err)
	}
}

func TestExistingLogPathFallsBackToTheLegacyPlace(t *testing.T) {
	dir := t.TempDir()
	spec := task.Spec{Project: "p", ProjectPath: "/w/p", Kind: task.KindScript, Dir: ".", Script: "dev"}
	if got := ExistingLogPath(dir, spec); got != LogPath(dir, spec) {
		t.Fatalf("with no log at all: %s", got)
	}
	legacy := filepath.Join(dir, "logs", "p", ".:dev.log")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ExistingLogPath(dir, spec); got != legacy {
		t.Fatalf("a task started by an older blixa: %s", got)
	}
}
