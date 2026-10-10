package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"blixa/internal/discovery"
	"blixa/internal/task"
)

const maxLogLines = 10000

const logStreamClosedLine = "── the log stream closed ──"

type logView struct {
	id        string
	gen       int // increases on every open; discards messages from the old stream
	title     string
	lines     []string
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
		follow:    true,
		cancel:    cancel,
		search:    s,
		lastMatch: -1,
	}
	m.mode = modeLog
	return m, readLog(gen, ch)
}

func (m Model) openBuiltinLog(p discovery.Project) (tea.Model, tea.Cmd) {
	info, ok := m.tasks[task.BuiltinID(p.Name)]
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
	}
	m.log.vp.SetContent(strings.Join(m.log.lines, "\n"))
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
			return m, nil
		}
		var cmd tea.Cmd
		m.log.search, cmd = m.log.search.Update(msg)
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
		return m, textinput.Blink
	case "n":
		m.log.findNext()
		return m, nil
	}
	var cmd tea.Cmd
	m.log.vp, cmd = m.log.vp.Update(msg)
	m.log.follow = m.log.vp.AtBottom()
	return m, cmd
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
	bottom := styleDim.Render(status + "   up/down/pgup/pgdn scroll   / search   n next   esc back")
	if m.log.searching {
		bottom = m.log.search.View()
	}
	return styleAccent.Render(m.log.title) + "\n" + m.log.vp.View() + "\n" + bottom
}
