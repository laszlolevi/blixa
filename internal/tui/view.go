package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"blixa/internal/supervisor"
	"blixa/internal/task"
)

const footer = "s start  x stop  r restart  l log  p pull  c checkout  i install  e scripts  / filter  a all  ? help  q quit"
const shortFooter = "s/x/r task  l log  p/c/i project  e scripts  ? help  q quit"

const helpText = `blixa - keys

Project row:    p pull   c checkout   i npm install   e select scripts   l last operation's log   enter expand/collapse
Task row:       s start   x stop   r restart   l / enter log
Missing proj.:  D remove from config
Everywhere:     up/down move   / filter   a all projects   q quit (tasks keep running)   Q stop everything
Log view:       up/down/pgup/pgdn scroll   f follow   / search   n next match   esc back

(any key: back)`

func (m Model) View() string {
	if m.splash {
		return m.viewSplash()
	}
	switch m.mode {
	case modeHelp:
		return helpText
	case modeScripts, modeBranches:
		return m.viewPicker()
	case modeLog:
		return m.viewLog()
	}
	var b strings.Builder
	b.WriteString(m.header() + "\n")
	extra := 0
	if m.cfgErr != nil {
		b.WriteString(styleErr.Render("config error: "+m.cfgErr.Error()) + "\n")
		extra++
	}
	rows := m.rows()
	if len(rows) == 0 {
		b.WriteString(styleDim.Render("  no projects to show -- a: all projects, e: select scripts") + "\n")
	}
	// we render the crash panel before computing the row budget, so its actual line
	// count can be subtracted from the space given to the list (the header must not scroll off).
	panel := m.crashPanel(rows)
	panelLines := strings.Count(panel, "\n")
	from, to := window(m.cursor, len(rows), m.height-8-extra-panelLines)
	cols := m.columns(rows)
	for i := from; i < to; i++ {
		segs := m.rowSegs(rows[i], cols)
		var base *lipgloss.Style
		switch {
		case i == m.cursor:
			cursor := styleCursor
			if m.width > 0 { // the cursor bar spans the whole line
				if pad := m.width - runewidth.StringWidth(plainText(segs)); pad > 0 {
					segs = append(segs, seg{strings.Repeat(" ", pad), stylePlain})
				}
			}
			base = &cursor
		case !m.connected:
			dim := styleDim // without a connection: last known state, grayed out
			base = &dim
		}
		b.WriteString(joinSegs(segs, base) + "\n")
	}
	b.WriteString(panel)
	b.WriteString(m.statusLine() + "\n")
	b.WriteString(renderKeys(m.footer()))
	return b.String()
}

// footer: if the full help line doesn't fit the window width, returns a shorter version.
func (m Model) footer() string {
	if m.width > 0 && len(footer) > m.width {
		return shortFooter
	}
	return footer
}

func (m Model) header() string {
	conn := styleOK.Render("daemon ●")
	if !m.connected {
		conn = styleErr.Render("daemon ○ reconnecting...")
	}
	return gradient("▰ blixa", colorAccent, colorAccent2) + styleDim.Render(" ─ "+m.cfg.Root) + "   " + conn
}

// Upper bound on the name and git columns, so a long project or branch name
// doesn't stretch the row; the status column has a fixed width.
const (
	maxNameWidth = 40
	maxGitWidth  = 36
	statusWidth  = 12
	minPortWidth = 10 // ":65535 +12", reserved so the duration doesn't jump when a port shows up
)

const noGit = "no git"

// columns: the main view's column widths based on all rows (so they don't jump while scrolling).
type columns struct {
	name int // the name column including the indent
	git  int
	port int
}

func (m Model) columns(rows []row) columns {
	c := columns{name: 10, port: minPortWidth}
	for _, r := range rows {
		if r.kind == rowProject {
			c.name = max(c.name, 2+runewidth.StringWidth(r.project))
			st, ok := m.gitStatus[r.project]
			c.git = max(c.git, runewidth.StringWidth(gitText(st, ok)))
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
	c.git = min(c.git, maxGitWidth)
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
			return []seg{{"  " + fit(r.project, c.name-2) + "  missing (D: remove from config)", styleDim}}
		}
		arrow := "▾"
		if m.collapsed[r.project] {
			arrow = "▸"
		}
		st, ok := m.gitStatus[r.project]
		var builtin *task.Info
		if info, found := m.tasks[task.BuiltinID(r.project)]; found {
			builtin = &info
		}
		segs := []seg{{arrow + " ", styleDim}, {fit(r.project, c.name-2), styleTitle}, {"  ", stylePlain}}
		if p, found := m.project(r.project); found && !p.IsGit {
			segs = append(segs, seg{fit(noGit, c.git), styleDim})
		} else {
			segs = append(segs, gitSegs(fit(gitTextWidth(st, ok, c.git), c.git))...)
		}
		segs = append(segs, seg{"  ", stylePlain}, seg{builtinText(builtin), statusStyle(builtin)})
		if p, found := m.project(r.project); found && p.PackageManager != "" && p.PackageManager != "npm" {
			segs = append(segs, seg{"  " + p.PackageManager, styleDim}) // npm is the default, so it isn't shown
		}
		return segs
	}
	var info *task.Info
	if i, ok := m.tasks[r.spec.ID()]; ok {
		info = &i
	}
	return []seg{
		{"    " + fit(r.spec.Label(), c.name-4) + "  ", stylePlain},
		{fit(statusText(info), statusWidth), statusStyle(info)},
		{"  ", stylePlain},
		{fit(portText(info), c.port), stylePort},
		{"  ", stylePlain},
		{durationText(info, m.deps.Now()), styleDim},
	}
}

// crashPanel: the log's last lines for a task that crashed or exited with an error.
func (m Model) crashPanel(rows []row) string {
	if m.cursor >= len(rows) || rows[m.cursor].kind != rowTask {
		return ""
	}
	info, ok := m.tasks[rows[m.cursor].spec.ID()]
	failed := info.Status == task.StatusCrashed || (info.Status == task.StatusExited && info.ExitCode != 0)
	if !ok || !failed {
		return ""
	}
	var b strings.Builder
	for _, l := range tailLines(supervisor.LogPath(m.deps.StateDir, info.Spec), 5) {
		b.WriteString(styleDim.Render("  │ "+l) + "\n")
	}
	return b.String()
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
