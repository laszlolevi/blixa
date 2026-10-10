package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blixa/internal/task"
)

func TestLogPath(t *testing.T) {
	ws := task.Spec{Project: "p", Kind: task.KindScript, Dir: "apps/server", Script: "dev"}
	if got := LogPath("/s", ws); got != "/s/logs/p/apps%2Fserver:dev.log" {
		t.Errorf("ws: %s", got)
	}
	pull := task.Spec{Project: "p", Kind: task.KindPull}
	if got := LogPath("/s", pull); got != "/s/logs/p/@builtin.log" {
		t.Errorf("pull: %s", got)
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
