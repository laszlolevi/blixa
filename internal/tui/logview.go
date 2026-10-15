package tui

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/laszlolevi/blixa/internal/discovery"
	"github.com/laszlolevi/blixa/internal/task"
)

const maxLogLines = 10000

const logStreamClosedLine = "── the log stream closed ──"

type logView struct {
	id        string
	gen       int // increases on every open; discards messages from the old stream
	title     string
	lines     []string
	dropped   int // lines trimmed off the front (maxLogLines), so the numbers stay the log's own
	numbers   bool
	vp        viewport.Model
	follow    bool
	cancel    context.CancelFunc
	search    textinput.Model
	searching bool
	lastMatch int // the line index of the last match; -1 if we haven't searched yet
}

type logLinesMsg struct {
	gen    int
	lines  []string
	ch     <-chan string
	closed bool
}

func (l *logView) resize(w, h int) {
	l.vp.Width = w
	l.vp.Height = max(h-3, 1)
}

func (l *logView) close() {
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
}

// leaveLog: exits the log view -- must be called everywhere the log closes.
func (m *Model) leaveLog() {
	m.log.close()
	m.mode = modeMain
}

func (m Model) openLog(spec task.Spec) (tea.Model, tea.Cmd) {
	m.log.close()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := m.deps.StreamLog(ctx, spec.ID())
	if err != nil {
		cancel()
		m.flash = "log: " + err.Error()
		return m, nil
	}
	s := textinput.New()
	s.Prompt = "search: "
	gen := m.log.gen + 1
	m.log = logView{
		id:        spec.ID(),
		gen:       gen,
		title:     spec.Project + " › " + spec.Label(),
		vp:        viewport.New(m.width, max(m.height-3, 1)),
		numbers:   true,
		follow:    true,
		cancel:    cancel,
		search:    s,
		lastMatch: -1,
	}
	m.mode = modeLog
	return m, readLog(gen, ch)
}

func (m Model) openBuiltinLog(p discovery.Project) (tea.Model, tea.Cmd) {
	info, ok := m.tasks[task.BuiltinID(p.Path)]
	if !ok {
		m.flash = "pull / checkout / install hasn't run on this project yet"
		return m, nil
	}
	return m.openLog(info.Spec)
}

// readLog: one blocking read, then the already-buffered lines (max 500) in one message.
func readLog(gen int, ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return logLinesMsg{gen: gen, closed: true}
		}
		lines := []string{line}
		for len(lines) < 500 {
			select {
			case l, ok := <-ch:
				if !ok {
					return logLinesMsg{gen: gen, lines: lines, closed: true}
				}
				lines = append(lines, l)
			default:
				return logLinesMsg{gen: gen, lines: lines, ch: ch}
			}
		}
		return logLinesMsg{gen: gen, lines: lines, ch: ch}
	}
}

func (m Model) updateLogLines(msg logLinesMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeLog || msg.gen != m.log.gen {
		return m, nil
	}
	m.log.lines = append(m.log.lines, msg.lines...)
	if msg.closed {
		m.log.lines = append(m.log.lines, logStreamClosedLine)
	}
	if over := len(m.log.lines) - maxLogLines; over > 0 {
		m.log.lines = m.log.lines[over:]
		m.log.dropped += over
		m.log.lastMatch = max(m.log.lastMatch-over, -1)
	}
	m.log.render()
	if m.log.follow {
		m.log.vp.GotoBottom()
	}
	if msg.closed {
		return m, nil
	}
	return m, readLog(msg.gen, msg.ch)
}

func (m Model) updateLog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.log.searching {
		switch msg.String() {
		case "esc":
			m.log.searching = false
			m.log.search.Blur()
			return m, nil
		case "enter":
			m.log.searching = false
			m.log.search.Blur()
			m.log.findNext()
			m.log.render()
			return m, nil
		}
		var cmd tea.Cmd
		m.log.search, cmd = m.log.search.Update(msg)
		m.log.render() // the matches are highlighted while typing
		return m, cmd
	}
	switch msg.String() {
	case "esc":
		m.leaveLog()
		return m, nil
	case "f":
		m.log.follow = true
		m.log.vp.GotoBottom()
		return m, nil
	case "/":
		m.log.searching = true
		m.log.search.SetValue("")
		m.log.search.Focus()
		m.log.lastMatch = -1
		m.log.render()
		return m, textinput.Blink
	case "n":
		m.log.findNext()
		m.log.render()
		return m, nil
	case "#":
		m.log.numbers = !m.log.numbers
		m.log.render()
		return m, nil
	}
	return m, m.log.scroll(msg)
}

// scroll: hands a key or a mouse wheel event to the viewport; following stops when it
// leaves the bottom, and starts again on reaching it.
func (l *logView) scroll(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	l.vp, cmd = l.vp.Update(msg)
	l.follow = l.vp.AtBottom()
	return cmd
}

// render: puts the lines into the viewport, with a line-number gutter if it's turned on,
// and the search matches highlighted.
func (l *logView) render() {
	q := l.search.Value()
	// the styles are rendered once and reused: styling every line separately is slow at 10k lines
	pre, post, _ := strings.Cut(styleDim.Render("#"), "#")
	w := len(strconv.Itoa(l.dropped + len(l.lines)))
	var b strings.Builder
	for i, line := range l.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		if q != "" {
			st := styleMatch
			if i == l.lastMatch {
				st = styleCurrentMatch
			}
			line = highlight(line, q, st)
		}
		switch {
		case !l.numbers:
		case l.lines[i] == logStreamClosedLine:
			b.WriteString(strings.Repeat(" ", w+3))
		default:
			fmt.Fprintf(&b, "%s%*d │%s ", pre, w, l.dropped+i+1, post)
		}
		b.WriteString(line)
	}
	l.vp.SetContent(b.String())
}

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// highlight: puts st's background under every case-insensitive occurrence of q in line.
// The line may carry the program's own colors: a match overlapping an escape sequence is
// left alone, and the line's last color before a match is restored after it.
func highlight(line, q string, st lipgloss.Style) string {
	lower, lq := strings.ToLower(line), strings.ToLower(q)
	if len(lower) != len(line) { // a case change altered byte offsets: matches wouldn't line up
		lower, lq = line, q
	}
	if !strings.Contains(lower, lq) {
		return line
	}
	seqs := ansiSeq.FindAllStringIndex(line, -1)
	var b strings.Builder
	last, color := 0, ""
	for from := 0; ; {
		i := strings.Index(lower[from:], lq)
		if i < 0 {
			break
		}
		start, end := from+i, from+i+len(lq)
		from = start + 1
		if start < last || overlapsSeq(seqs, start, end) {
			continue
		}
		for _, s := range seqs {
			if s[1] <= start {
				color = line[s[0]:s[1]]
			}
		}
		b.WriteString(line[last:start] + st.Render(line[start:end]) + color)
		last, from = end, end
	}
	return b.String() + line[last:]
}

func overlapsSeq(seqs [][]int, start, end int) bool {
	for _, s := range seqs {
		if start < s[1] && s[0] < end {
			return true
		}
	}
	return false
}

// findNext: scrolls to the next match after the last one (wrapping around). We use
// lastMatch (not vp.YOffset) as the starting point, because the viewport can clamp the
// offset on the last screen, and starting from that would get the search stuck.
func (l *logView) findNext() {
	q := strings.ToLower(l.search.Value())
	if q == "" || len(l.lines) == 0 {
		return
	}
	start := l.lastMatch + 1
	for i := 0; i < len(l.lines); i++ {
		j := (start + i) % len(l.lines)
		if strings.Contains(strings.ToLower(l.lines[j]), q) {
			l.vp.SetYOffset(j)
			l.follow = false
			l.lastMatch = j
			return
		}
	}
}

func (m Model) viewLog() string {
	status := "follow: on"
	if !m.log.follow {
		status = "follow: off (f: on)"
	}
	bottom := renderKeys(status + "  ↑↓/wheel scroll  / search  n next  # line numbers  esc back")
	if m.log.searching {
		bottom = m.log.search.View()
	}
	return styleAccent.Render(m.log.title) + "\n" + m.log.vp.View() + "\n" + bottom
}
