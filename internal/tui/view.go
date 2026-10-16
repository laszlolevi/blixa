package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/laszlolevi/blixa/internal/paths"
	"github.com/laszlolevi/blixa/internal/supervisor"
	"github.com/laszlolevi/blixa/internal/task"
)

const footer = "s start  x stop  r restart  l log  p pull  c checkout  i install  e scripts  / filter  a all  tab folder  o open  ? help  q quit"
const shortFooter = "s/x/r task  l log  p/c/i project  e scripts  tab/o folder  ? help  q quit"

// helpSections: the ? screen, one section per place the keys work in.
var helpSections = []struct {
	title string
	keys  [][2]string
}{
	{"Projects", [][2]string{
		{"↑ ↓", "move"}, {"enter", "expand / collapse"}, {"/", "filter"}, {"a", "all projects"},
		{"e", "select scripts"}, {"p", "git pull"}, {"c", "git checkout"}, {"i", "install"},
		{"l", "last operation's log"}, {"D", "remove a missing project"},
	}},
	{"Tasks", [][2]string{
		{"s", "start"}, {"x", "stop"}, {"r", "restart"}, {"l / enter", "log"},
		{"⌘ click port", "open in the browser"},
	}},
	{"Folders", [][2]string{
		{"↑ on top", "into the folder tabs"}, {"← →", "switch (in the tabs)"}, {"tab / ⇧tab", "next / previous"},
		{"o", "open a folder"}, {"W", "close the tab"}, {"↓ / esc", "back to the projects"},
	}},
	{"Opening a folder", [][2]string{
		{"tab", "complete / next match"}, {"enter", "open"}, {"esc", "cancel"},
	}},
	{"Log", [][2]string{
		{"↑ ↓ pgup pgdn", "scroll"}, {"wheel", "scroll"}, {"f", "follow"}, {"/", "search"},
		{"n", "next match"}, {"#", "line numbers"}, {"⌥ drag", "select text"}, {"esc", "back"},
	}},
	{"Script picker", [][2]string{
		{"type", "filter"}, {"space", "select"}, {"enter", "done"}, {"esc", "back"},
	}},
	{"Checkout", [][2]string{
		{"tab / ← →", "local ⇄ remote"}, {"type", "filter both"}, {"enter", "check out"},
		{"enter (remote)", "reset local to it"}, {"esc", "cancel"},
	}},
	{"Anywhere", [][2]string{
		{"?", "this help"}, {"q", "quit (tasks keep running)"}, {"Q", "stop everything"},
	}},
}

func (m Model) viewHelp() string {
	width := m.width
	if width <= 0 {
		width = 100
	}
	const keyW, descW = 15, 26
	perLine := max((width-4)/(keyW+descW), 1)
	var lines []string
	for i, sec := range helpSections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, styleAccent.Render(sec.title))
		for j := 0; j < len(sec.keys); j += perLine {
			var b strings.Builder
			for _, k := range sec.keys[j:min(j+perLine, len(sec.keys))] {
				b.WriteString(styleKey.Render(fit(k[0], keyW)) + styleDim.Render(fit(k[1], descW)))
			}
			lines = append(lines, b.String())
		}
	}
	return m.header() + "\n" + frame("keys", lines, width, true) + styleDim.Render("any key: back")
}

func (m Model) View() string {
	if m.splash {
		return m.viewSplash()
	}
	switch m.mode {
	case modeHelp:
		return m.viewHelp()
	case modeScripts:
		return m.viewPicker()
	case modeBranches:
		return m.viewBranchPicker()
	case modeLog:
		return m.viewLog()
	}
	width := m.width
	if width <= 0 {
		width = 100 // no size yet (tests): a sensible default
	}
	inner := width - 4 // the frame's border and padding

	var b strings.Builder
	b.WriteString(m.header() + "\n")
	if m.cfgErr != nil {
		b.WriteString(styleErr.Render("config error: "+m.cfgErr.Error()) + "\n")
	}
	b.WriteString(frame("folders", []string{m.folderLine(inner)}, width, m.folderFocus || m.chooser.active))

	// everything but the project list has a fixed height; the list gets the rest, so the
	// details, the status line and the keys always sit at the bottom of the screen, and
	// nothing moves when a task's ports or log show up
	rows := m.rows()
	used := 1 + 3 + 2 + (detailHeight + 2) + 2 // header, folders, projects' borders, details, status, keys
	if m.cfgErr != nil {
		used++
	}
	listHeight := max(m.height-used, 1)
	if m.height <= 0 {
		listHeight = 0 // no size yet: no limit
	}
	var list []string
	if len(rows) == 0 {
		list = append(list, styleDim.Render("no projects to show -- a: all projects, e: select scripts"))
	}
	from, to := window(m.cursor, len(rows), listHeight)
	now := m.columns(rows)
	cols := m.cols.grow(now)
	cols.op = now.op // operations come and go on the rows: the branch gets the room back
	cols = cols.gitRoom(inner)
	for i := from; i < to; i++ {
		segs := m.rowSegs(rows[i], cols)
		var base *lipgloss.Style
		switch {
		case i == m.cursor && !m.folderFocus:
			cursor := styleCursor                                          // the cursor bar spans the whole line
			if pad := inner - ansi.StringWidth(plainText(segs)); pad > 0 { // ansi: the port is a link
				segs = append(segs, seg{strings.Repeat(" ", pad), stylePlain})
			}
			base = &cursor
		case !m.connected:
			dim := styleDim // without a connection: last known state, grayed out
			base = &dim
		}
		list = append(list, joinSegs(segs, base))
	}
	if m.height > 0 {
		for len(list) < listHeight {
			list = append(list, "")
		}
	}
	b.WriteString(frame("projects", list, width, !m.folderFocus && !m.chooser.active))
	title, details := m.details(rows)
	b.WriteString(frame(title, details, width, false))
	b.WriteString(m.statusLine() + "\n")
	b.WriteString(m.keysLine())
	return b.String()
}

// folderLine: the top frame's content -- the root tabs, or the folder chooser while it's
// open (on the same single line, so nothing below moves).
func (m Model) folderLine(inner int) string {
	if c := m.chooser; c.active {
		var parts []string
		for i, name := range c.matches {
			st := styleDim
			if i == c.cycle {
				st = styleKey
			}
			parts = append(parts, st.Render(name+"/"))
		}
		matches := strings.Join(parts, "  ")
		if len(c.matches) == 0 {
			matches = styleDim.Render("no subfolders here")
		}
		return c.input.View() + "   " + matches
	}
	var tabs []string
	for _, r := range m.cfg.KnownRoots() {
		st := styleDim
		switch {
		case r == m.cfg.Root && m.folderFocus:
			st = styleCursor.Foreground(colorAccent2)
		case r == m.cfg.Root:
			st = styleActiveTab
		}
		tabs = append(tabs, st.Render(" "+r+" "))
	}
	line := strings.Join(tabs, styleDim.Render("│"))
	keys := "↑ folders  tab switch  o open  W close"
	if m.folderFocus {
		keys = "←→ switch  ↓/enter projects  o open  W close"
	}
	hint := renderKeys(keys)
	if pad := inner - ansi.StringWidth(line) - ansi.StringWidth(hint); pad > 0 {
		line += strings.Repeat(" ", pad) + hint
	}
	return line
}

// frame draws a rounded box of the given total width around the lines, with the title
// in the top border. Lines are cut to fit and padded, so the right border stays straight.
// A focused frame (where the arrow keys act) gets a colored border.
func frame(title string, lines []string, width int, focused bool) string {
	inner := width - 4
	border := styleDim
	if focused {
		border = styleKey.UnsetBold()
	}
	title = ansi.Truncate(title, max(width-6, 0), "…")
	var b strings.Builder
	fill := max(width-5-ansi.StringWidth(title), 0)
	b.WriteString(border.Render("╭─ ") + styleTitle.Render(title) + border.Render(" "+strings.Repeat("─", fill)+"╮") + "\n")
	side := border.Render("│")
	for _, l := range lines {
		l = ansi.Truncate(l, inner, "…")
		pad := max(inner-ansi.StringWidth(l), 0)
		b.WriteString(side + " " + l + strings.Repeat(" ", pad) + " " + side + "\n")
	}
	b.WriteString(border.Render("╰"+strings.Repeat("─", max(width-2, 0))+"╯") + "\n")
	return b.String()
}

// footer: if the full help line doesn't fit the window width, returns a shorter version.
func (m Model) footer() string {
	if m.width > 0 && len(footer)+m.versionWidth() > m.width {
		return shortFooter
	}
	return footer
}

// versionWidth: the room the version takes in the bottom right corner, gap included.
func (m Model) versionWidth() int {
	if m.deps.Version == "" {
		return 0
	}
	return 2 + runewidth.StringWidth(m.deps.Version)
}

// keysLine: the keys, and the version in the bottom right corner if it fits.
func (m Model) keysLine() string {
	keys := m.footer()
	line := renderKeys(keys)
	if m.width <= 0 || m.deps.Version == "" {
		return line
	}
	pad := m.width - len(keys) - runewidth.StringWidth(m.deps.Version)
	if pad < 2 {
		return line
	}
	return line + strings.Repeat(" ", pad) + styleDim.Render(m.deps.Version)
}

func (m Model) header() string {
	conn := styleOK.Render("daemon ●")
	if !m.connected {
		conn = styleErr.Render("daemon ○ reconnecting...")
	}
	return gradient("▰ blixa", colorAccent, colorAccent2) + styleDim.Render(" ─ "+m.cfg.Root) + "   " + conn
}

// Upper bound on the name column, so a long project name doesn't stretch the row (the git
// column gets whatever room is left, see gitRoom); the status column has a fixed width.
const (
	maxNameWidth = 40
	minGitWidth  = 12
	statusWidth  = 12
	minPortWidth = 10 // ":65535 +12", reserved so the duration doesn't jump when a port shows up
	durWidth     = 7  // "123h45m"
)

const noGit = "no git"

// columns: the main view's column widths based on all rows (so they don't jump while scrolling).
// Missing projects are left out: their row has no columns to align, and removing one
// from the config must not shift the others.
type columns struct {
	name int // the name column including the indent
	git  int
	op   int // a project's last builtin operation, e.g. "checkout master ✔ 11:46"
	port int
}

func (m Model) columns(rows []row) columns {
	c := columns{name: 10, port: minPortWidth}
	for _, r := range rows {
		if r.missing {
			continue
		}
		if r.kind == rowProject {
			c.name = max(c.name, 2+runewidth.StringWidth(r.project))
			st, ok := m.gitStatus[r.project]
			c.git = max(c.git, runewidth.StringWidth(gitText(st, ok)))
			if info := m.builtin(r.project); info != nil {
				c.op = max(c.op, runewidth.StringWidth(rowOpText(info, m.deps.Now())))
			}
			if p, found := m.project(r.project); found && !p.IsGit {
				c.git = max(c.git, runewidth.StringWidth(noGit))
			}
			continue
		}
		c.name = max(c.name, 4+runewidth.StringWidth(r.spec.Label()))
		if info, ok := m.tasks[r.spec.ID()]; ok {
			c.port = max(c.port, runewidth.StringWidth(portText(&info)))
		}
	}
	c.name = min(c.name, maxNameWidth)
	return c
}

func (c columns) grow(o columns) columns {
	return columns{name: max(c.name, o.name), git: max(c.git, o.git), op: max(c.op, o.op), port: max(c.port, o.port)}
}

// gitRoom: the git column fitted to the frame's inner width -- as wide as the longest
// branch, if it fits next to the name and the operation columns.
func (c columns) gitRoom(inner int) columns {
	if inner > 0 {
		c.git = min(c.git, max(inner-c.name-4-c.op, minGitWidth)) // 4: the gaps around the git column
	}
	return c
}

// fit: pads the text to exactly w wide, truncating anything too long with "...".
func fit(s string, w int) string {
	return runewidth.FillRight(runewidth.Truncate(s, w, "…"), w)
}

func (m Model) renderRow(r row, c columns) string { return joinSegs(m.rowSegs(r, c), nil) }

// rowSegs: the styled pieces of a main-view row, already fitted to the column widths.
func (m Model) rowSegs(r row, c columns) []seg {
	if r.kind == rowProject {
		if r.missing {
			return []seg{{"  " + runewidth.FillRight(r.project, c.name-2) + "  missing (D: remove from config)", styleDim}}
		}
		arrow := "▾"
		if m.collapsed[r.project] {
			arrow = "▸"
		}
		st, ok := m.gitStatus[r.project]
		builtin := m.builtin(r.project)
		segs := []seg{{arrow + " ", styleDim}, {fit(r.project, c.name-2), styleTitle}, {"  ", stylePlain}}
		if p, found := m.project(r.project); found && !p.IsGit {
			segs = append(segs, seg{fit(noGit, c.git), styleDim})
		} else {
			segs = append(segs, gitSegs(fit(gitTextWidth(st, ok, c.git), c.git))...)
		}
		return append(segs, seg{"  ", stylePlain}, seg{rowOpText(builtin, m.deps.Now()), statusStyle(builtin)})
	}
	var info *task.Info
	if i, ok := m.tasks[r.spec.ID()]; ok {
		info = &i
	}
	segs := []seg{
		{"    " + fit(r.spec.Label(), c.name-4) + "  ", stylePlain},
		{fit(statusText(info), statusWidth), statusStyle(info)},
		{"  ", stylePlain},
	}
	segs = append(segs, portSegs(info, c.port)...)
	return append(segs, seg{"  ", stylePlain}, seg{durationText(info, m.deps.Now()), styleDim})
}

// portSegs: the primary port (⌘-clickable if it speaks http(s)) and the count of the
// rest, fitted to width w.
func portSegs(info *task.Info, w int) []seg {
	text := fit(portText(info), w)
	if info == nil || len(info.Ports) == 0 {
		return []seg{{text, stylePort}}
	}
	primary, rest, found := strings.Cut(text, " +")
	link := func(s string) string {
		url := portURL(info, info.Ports[0])
		if url == "" {
			return s
		}
		trimmed := strings.TrimRight(s, " ")
		return hyperlink(url, trimmed) + s[len(trimmed):]
	}
	if !found {
		return []seg{{link(text), stylePort}}
	}
	return []seg{{link(primary), stylePort}, {" +" + rest, styleDim}}
}

// detailHeight: the details frame's fixed number of lines.
const detailHeight = 3

// details: the title and the (always detailHeight) lines of the details frame, for the
// row under the cursor -- or for the current folder while the folder tabs have the focus. A task shows its ports, or, if it crashed or exited with an
// error, the log's last lines; a project its path, package manager and
// last operation.
func (m Model) details(rows []row) (string, []string) {
	lines := make([]string, 0, detailHeight)
	field := func(name, value string, st lipgloss.Style) {
		if value == "" {
			value, st = "–", styleDim
		}
		lines = append(lines, styleDim.Render(fmt.Sprintf("%-13s ", name))+st.Render(value))
	}
	title := "details"
	switch {
	case m.folderFocus:
		title = m.cfg.Root
		root := paths.Expand(m.cfg.Root)
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
		field("path", root, stylePlain)
		if !m.scanned {
			field("projects", "scanning…", styleDim)
		} else {
			selected := 0
			for _, p := range m.projects {
				if len(m.cfg.Selected(p.Name)) > 0 {
					selected++
				}
			}
			field("projects", fmt.Sprintf("%d  (%d with selected scripts)", len(m.projects), selected), stylePlain)
		}
		running, ports := 0, []int{}
		for _, info := range m.tasks {
			if info.Status.Active() && strings.HasPrefix(info.Spec.ProjectPath, root+string(filepath.Separator)) {
				running++
				if len(info.Ports) > 0 {
					ports = append(ports, info.Ports[0])
				}
			}
		}
		text := ""
		if running > 0 {
			sort.Ints(ports)
			text = fmt.Sprintf("%d %s", running, plural(running, "task", "tasks"))
			if len(ports) > 0 {
				text += "  · "
				for _, port := range ports {
					text += fmt.Sprintf(" :%d", port)
				}
			}
		}
		field("running", text, styleOK)
	case m.cursor < len(rows) && !rows[m.cursor].missing:
		r := rows[m.cursor]
		p, _ := m.project(r.project)
		title = r.project
		if r.kind == rowProject {
			title += " · " + paths.Collapse(p.Path)
			// the full branch: the row may have had to cut it
			st, ok := m.gitStatus[r.project]
			if text := gitText(st, ok); text != "" {
				lines = append(lines, styleDim.Render(fmt.Sprintf("%-13s ", "branch"))+joinSegs(gitSegs(text), nil))
			} else if !p.IsGit {
				field("branch", noGit, styleDim)
			} else {
				field("branch", "", stylePlain)
			}
			field("pm", p.PackageManager, stylePlain)
			op := m.builtin(r.project)
			text := ""
			if op != nil {
				text = builtinText(op)
			}
			field("last op", text, statusStyle(op))
		} else {
			title += " › " + r.spec.Label()
			info, ok := m.tasks[r.spec.ID()]
			var log []string
			if ok {
				log = tailLines(supervisor.ExistingLogPath(m.deps.StateDir, info.Spec), detailHeight)
			}
			if ok && (info.Status == task.StatusCrashed || (info.Status == task.StatusExited && info.ExitCode != 0)) {
				for _, l := range log {
					lines = append(lines, styleDim.Render(cleanLine(l)))
				}
			} else {
				// the http(s) ports and the URL are links: ⌘-click opens them in the browser
				primary, others := "", ""
				if len(info.Ports) > 0 {
					primary = portLabel(&info, info.Ports[0])
					if url := portURL(&info, info.Ports[0]); url != "" {
						primary += "  →  " + hyperlink(url, url)
					}
					var o []string
					for _, port := range info.Ports[1:] {
						o = append(o, portLabel(&info, port))
					}
					others = strings.Join(o, "  ")
				}
				field("primary port", primary, stylePort)
				field("other ports", others, stylePort)
				usage := ""
				if info.Status.Active() && info.MemKB > 0 {
					usage = fmt.Sprintf("cpu %.0f%%  ·  mem %s", info.CPU, memText(info.MemKB))
				}
				field("usage", usage, stylePlain)
			}
		}
	}
	for len(lines) < detailHeight {
		lines = append(lines, "")
	}
	return title, lines
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// cleanLine: a log line made safe for one line of a frame (tabs expanded, no carriage returns).
func cleanLine(l string) string {
	if i := strings.LastIndex(l, "\r"); i >= 0 {
		l = l[i+1:] // a progress bar redraws the line: keep its last state
	}
	return strings.ReplaceAll(l, "\t", "    ")
}

func (m Model) statusLine() string {
	switch {
	case m.mode == modeConfirm:
		return m.confirm.view()
	case m.filtering:
		return m.filter.View()
	case m.flash != "":
		return m.flash
	case m.filter.Value() != "":
		return styleDim.Render("filter: " + m.filter.Value())
	}
	return ""
}

func (d confirmDialog) view() string {
	parts := []string{styleWarn.Render(d.text)}
	for _, o := range d.options {
		parts = append(parts, o.key+": "+o.label)
	}
	parts = append(parts, "esc: cancel")
	return strings.Join(parts, "   ")
}
