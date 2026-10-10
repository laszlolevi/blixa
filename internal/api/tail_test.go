package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func expectLines(t *testing.T, ch <-chan string, want ...string) {
	t.Helper()
	for _, w := range want {
		select {
		case got := <-ch:
			if got != w {
				t.Fatalf("line = %q, want: %q", got, w)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("did not arrive: %q", w)
		}
	}
}

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(s)
	f.Close()
}

func TestTailFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.log")
	os.WriteFile(path, []byte("a\nb\nc\n"), 0o644)
	lines := make(chan string, 100)
	stop := make(chan struct{})
	defer close(stop)
	go tailFile(path, 2, func(l string) error { lines <- l; return nil }, stop)

	expectLines(t, lines, "b", "c") // backlog: last 2 lines
	appendTo(t, path, "d\npart")
	expectLines(t, lines, "d")
	appendTo(t, path, "ial\n")
	expectLines(t, lines, "partial")

	os.Rename(path, path+".1") // new run: rotation
	os.WriteFile(path, []byte("new\n"), 0o644)
	expectLines(t, lines, restartMarker, "new")
}

// TestTailFileTrim: TruncateHead shortens the same file (the same inode) in place --
// this is not a rotation, there's no duplication: a single trimMarker signals the
// truncation, and only the lines appended after it arrive, exactly once.
func TestTailFileTrim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.log")
	os.WriteFile(path, []byte("a\nb\nc\nd\ne\n"), 0o644)
	lines := make(chan string, 100)
	stop := make(chan struct{})
	defer close(stop)
	go tailFile(path, 2, func(l string) error { lines <- l; return nil }, stop)

	expectLines(t, lines, "d", "e") // backlog: last 2 lines

	// truncating the same file in place (TruncateHead) -- NOT a rotation, signaled by a single trimMarker
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	expectLines(t, lines, trimMarker)

	// lines appended AFTER the truncation should arrive exactly once, without backlog trimming
	appendTo(t, path, "x\ny\nz\n")
	expectLines(t, lines, "x", "y", "z")

	select {
	case extra := <-lines:
		t.Fatalf("unexpected extra line arrived: %q", extra)
	case <-time.After(300 * time.Millisecond):
	}
}
