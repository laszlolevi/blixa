// Package tui is blixa's Bubble Tea interface.
package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/laszlolevi/blixa/internal/git"
	"github.com/laszlolevi/blixa/internal/task"
)

func statusText(info *task.Info) string {
	if info == nil {
		return "· –"
	}
	switch info.Status {
	case task.StatusStarting:
		return "● starting"
	case task.StatusRunning:
		return "● running"
	case task.StatusExited:
		if info.ExitCode == 0 {
			return "✔ exit 0"
		}
		return fmt.Sprintf("✖ exit %d", info.ExitCode)
	case task.StatusCrashed:
		return "✖ crashed"
	case task.StatusStopped:
		return "■ stopped"
	case task.StatusUnknown:
		return "? unknown"
	}
	return string(info.Status)
}

// hyperlink wraps text in an OSC 8 link: the terminal opens url on ⌘-click, whatever the
// text says (terminals without OSC 8 support just show the text).
func hyperlink(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

func localURL(port int) string { return fmt.Sprintf("http://localhost:%d", port) }

// memText: resident memory in MB, or GB from 1 GB up.
func memText(kb int64) string {
	if kb >= 1<<20 {
		return fmt.Sprintf("%.1f GB", float64(kb)/(1<<20))
	}
	return fmt.Sprintf("%d MB", kb>>10)
}

func portText(info *task.Info) string {
	if info == nil || len(info.Ports) == 0 {
		return ""
	}
	s := fmt.Sprintf(":%d", info.Ports[0])
	if n := len(info.Ports) - 1; n > 0 {
		s += fmt.Sprintf(" +%d", n)
	}
	return s
}

// durationText: counts from when the task reached "running"; nothing while it's still starting.
// A task that never got there (e.g. a one-off script) shows its total run time once it ended.
func durationText(info *task.Info, now time.Time) string {
	if info == nil || info.Status == task.StatusStarting {
		return ""
	}
	start := info.RunningAt
	if start.IsZero() {
		start = info.StartedAt // never reached running, or persisted before RunningAt existed
	}
	if start.IsZero() {
		return ""
	}
	end := now
	if !info.Status.Active() && !info.EndedAt.IsZero() {
		end = info.EndedAt
	}
	return formatDuration(end.Sub(start))
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm%02ds", m, s)
}

func gitText(st git.Status, ok bool) string {
	if !ok {
		return ""
	}
	parts := []string{st.Branch}
	if st.Ahead > 0 {
		parts = append(parts, fmt.Sprintf("↑%d", st.Ahead))
	}
	if st.Behind > 0 {
		parts = append(parts, fmt.Sprintf("↓%d", st.Behind))
	}
	if st.Dirty {
		parts = append(parts, "✎")
	}
	return strings.Join(parts, " ")
}

// gitTextWidth: like gitText, but truncates the branch name so the ↑/↓/✎ symbols
// stay within width w.
func gitTextWidth(st git.Status, ok bool, w int) string {
	full := gitText(st, ok)
	if runewidth.StringWidth(full) <= w {
		return full
	}
	suffix := strings.TrimPrefix(full, st.Branch)
	bw := w - runewidth.StringWidth(suffix)
	if bw < 1 {
		return runewidth.Truncate(full, w, "…")
	}
	return runewidth.Truncate(st.Branch, bw, "…") + suffix
}

// ago: a short relative age, e.g. "5m ago", "3h ago", "2d ago", "4mo ago".
func ago(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/24/30))
	}
	return fmt.Sprintf("%dy ago", int(d.Hours()/24/365))
}

// opShownFor: how long a successful operation stays on the project row.
const opShownFor = 10 * time.Second

// rowOpText: the operation as the project row shows it -- while it runs, if it failed,
// and for a few seconds after it succeeded; otherwise nothing (the details frame has it).
func rowOpText(info *task.Info, now time.Time) string {
	if info == nil {
		return ""
	}
	ok := info.Status == task.StatusExited && info.ExitCode == 0
	if ok && now.Sub(info.EndedAt) > opShownFor {
		return ""
	}
	return builtinText(info)
}

// builtinText: a summary of the project's last built-in operation, e.g. "pull ✔ 10:31".
func builtinText(info *task.Info) string {
	if info == nil {
		return ""
	}
	sym := "✖"
	switch {
	case info.Status.Active():
		sym = "●"
	case info.Status == task.StatusExited && info.ExitCode == 0:
		sym = "✔"
	}
	at := info.StartedAt
	if !info.EndedAt.IsZero() {
		at = info.EndedAt
	}
	return fmt.Sprintf("%s %s %s", info.Spec.Label(), sym, at.Format("15:04"))
}

// window: the visible [from, to) range such that the cursor stays visible.
func window(cursor, total, height int) (int, int) {
	if height <= 0 || total <= height {
		return 0, total
	}
	from := cursor - height + 1
	if from < 0 {
		from = 0
	}
	return from, from + height
}

// tailLines: the file's last n lines (from at most the last 16 KB).
func tailLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	if off := fi.Size() - 16*1024; off > 0 {
		f.Seek(off, io.SeekStart)
	}
	data, _ := io.ReadAll(f)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
