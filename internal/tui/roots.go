package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"blixa/internal/git"
	"blixa/internal/paths"
)

// rootChooser: the folder chooser in the top frame -- a path input with tab completion.
type rootChooser struct {
	active  bool
	input   textinput.Model
	matches []string // the subdirectories matching what's typed so far
	cycle   int      // >= 0 while tab cycles through matches (then they aren't recomputed)
}

func (m Model) openRootChooser() (tea.Model, tea.Cmd) {
	in := textinput.New()
	in.Prompt = "folder: "
	in.SetValue(strings.TrimSuffix(m.cfg.Root, "/") + "/")
	in.CursorEnd()
	in.Focus()
	m.chooser = rootChooser{active: true, input: in, cycle: -1}
	m.chooser.matches = dirMatches(in.Value())
	return m, textinput.Blink
}

func (m Model) updateRootChooser(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := &m.chooser
	switch msg.String() {
	case "esc":
		c.active = false
		return m, nil
	case "enter":
		dir := strings.TrimSpace(c.input.Value())
		abs, err := filepath.Abs(paths.Expand(dir))
		if fi, statErr := os.Stat(abs); err != nil || statErr != nil || !fi.IsDir() {
			m.flash = styleErr.Render("not a folder: " + dir)
			return m, nil
		}
		c.active = false
		return m.switchRoot(paths.Collapse(abs))
	case "tab":
		m.completeRoot()
		return m, nil
	}
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	c.matches, c.cycle = dirMatches(c.input.Value()), -1
	return m, cmd
}

// completeRoot: tab extends the input to the matches' common prefix; when there's nothing
// more in common, each further tab steps to the next match.
func (m *Model) completeRoot() {
	c := &m.chooser
	if len(c.matches) == 0 {
		return
	}
	dir, base := splitPath(c.input.Value())
	if c.cycle < 0 {
		if len(c.matches) == 1 {
			c.input.SetValue(dir + c.matches[0] + "/")
			c.input.CursorEnd()
			c.matches = dirMatches(c.input.Value())
			return
		}
		if p := commonPrefix(c.matches); len(p) > len(base) {
			c.input.SetValue(dir + p)
			c.input.CursorEnd()
			c.matches = dirMatches(c.input.Value())
			return
		}
	}
	c.cycle = (c.cycle + 1) % len(c.matches)
	c.input.SetValue(dir + c.matches[c.cycle])
	c.input.CursorEnd()
}

// splitPath: "~/work/te" -> "~/work/", "te".
func splitPath(p string) (string, string) {
	i := strings.LastIndex(p, "/")
	return p[:i+1], p[i+1:]
}

// dirMatches: the subdirectories of the typed path's folder whose name starts with the
// last segment (case-insensitively); hidden ones only when the segment starts with ".".
func dirMatches(typed string) []string {
	dir, base := splitPath(typed)
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(paths.Expand(dir))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(base)) {
			continue
		}
		if e.IsDir() || isDirLink(filepath.Join(paths.Expand(dir), name)) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func isDirLink(p string) bool {
	fi, err := os.Stat(p) // follows the symlink
	return err == nil && fi.IsDir()
}

func commonPrefix(ss []string) string {
	p := []rune(ss[0])
	for _, s := range ss[1:] {
		for !strings.HasPrefix(strings.ToLower(s), strings.ToLower(string(p))) {
			p = p[:len(p)-1]
		}
	}
	return string(p)
}

// updateFolders: the keys while the folder tabs have the focus -- left/right switch
// right away (the list below follows), down / enter / esc go back to the projects.
func (m Model) updateFolders(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.flash = ""
	switch msg.String() {
	case "left", "h", "shift+tab":
		return m.stepRoot(-1)
	case "right", "l", "tab":
		return m.stepRoot(1)
	case "down", "j", "enter", "esc":
		m.folderFocus = false
	case "o":
		return m.openRootChooser()
	case "W":
		m.closeRoot()
	case "?":
		m.mode = modeHelp
	case "q":
		return m, tea.Quit
	case "Q":
		m.openShutdownConfirm()
	}
	return m, nil
}

// stepRoot: tab / shift+tab moves to the next / previous root tab.
func (m Model) stepRoot(d int) (tea.Model, tea.Cmd) {
	roots := m.cfg.KnownRoots()
	if len(roots) < 2 {
		m.flash = "o: open another folder"
		return m, nil
	}
	i := sort.SearchStrings(roots, m.cfg.Root)
	return m.switchRoot(roots[(i+d+len(roots))%len(roots)])
}

// switchRoot: makes root current (adding it as a tab if it's new) and rescans. Running
// tasks are left alone: they keep running in the daemon.
func (m Model) switchRoot(root string) (tea.Model, tea.Cmd) {
	if m.cfgErr != nil {
		m.flash = m.saveConfig().Error()
		return m, nil
	}
	if root == m.cfg.Root {
		return m, nil
	}
	m.cfg.SetRoot(root)
	if err := m.saveConfig(); err != nil {
		m.flash = "saving config: " + err.Error()
	}
	m.projects, m.scanned = nil, false
	m.gitStatus = map[string]git.Status{}
	m.collapsed = map[string]bool{}
	m.cursor = 0
	m.cols = columns{}
	m.filter.SetValue("")
	return m, m.scanCmd()
}

// closeRoot: asks before removing the current tab; the neighbouring root becomes current.
func (m *Model) closeRoot() {
	roots := m.cfg.KnownRoots()
	if len(roots) < 2 {
		m.flash = "this is the only folder, it can't be closed"
		return
	}
	cur := m.cfg.Root
	i := sort.SearchStrings(roots, cur)
	next := roots[(i+1)%len(roots)]
	m.mode = modeConfirm
	m.confirm = confirmDialog{
		text: "Close the " + cur + " tab? Its script selections are forgotten, running tasks keep running.",
		options: []confirmOption{{key: "enter", label: "close", cmd: func() tea.Msg {
			return closeRootMsg{root: cur, next: next}
		}}},
	}
}

type closeRootMsg struct{ root, next string }

func (m Model) applyCloseRoot(msg closeRootMsg) (tea.Model, tea.Cmd) {
	if m.cfg.Root != msg.root {
		return m, nil
	}
	next, cmd := m.switchRoot(msg.next)
	nm := next.(Model)
	nm.cfg.RemoveRoot(msg.root)
	if err := nm.saveConfig(); err != nil {
		nm.flash = "saving config: " + err.Error()
	}
	return nm, cmd
}
