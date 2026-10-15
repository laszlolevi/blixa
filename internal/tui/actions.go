package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"blixa/internal/config"
	"blixa/internal/discovery"
	"blixa/internal/task"
)

type confirmOption struct {
	key   string
	label string
	cmd   tea.Cmd
}

type confirmDialog struct {
	text    string
	options []confirmOption
}

func (m Model) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		return m.updateFilter(msg)
	}
	m.flash = ""
	key := msg.String()
	switch key {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		} else {
			m.folderFocus = true // up from the first row: into the folder tabs
		}
		return m, nil
	case "down", "j":
		if m.cursor < len(m.rows())-1 {
			m.cursor++
		}
		return m, nil
	case "/":
		m.filtering = true
		m.filter.Focus()
		return m, textinput.Blink
	case "a":
		m.showAll = !m.showAll
		m.clampCursor()
		return m, nil
	case "?":
		m.mode = modeHelp
		return m, nil
	case "tab":
		return m.stepRoot(1)
	case "shift+tab":
		return m.stepRoot(-1)
	case "o":
		return m.openRootChooser()
	case "W":
		m.closeRoot()
		return m, nil
	case "q":
		return m, tea.Quit
	case "Q":
		m.openShutdownConfirm()
		return m, nil
	}

	rows := m.rows()
	if m.cursor >= len(rows) {
		return m, nil
	}
	cur := rows[m.cursor]
	if key == "D" && !cur.missing {
		m.flash = "D: only works on a missing project -- removes it from the config"
		return m, nil
	}
	if cur.missing {
		if key == "D" {
			return m.removeProject(cur.project)
		}
		return m, nil
	}
	proj, _ := m.project(cur.project)

	// project-level operations: work on both project and task rows
	if (key == "p" || key == "c") && !proj.IsGit {
		m.flash = proj.Name + ": not a git repository"
		return m, nil
	}
	switch key {
	case "p":
		return m, m.builtinCmd(proj, task.KindPull, "")
	case "i":
		return m, m.builtinCmd(proj, task.KindInstall, "")
	case "c":
		return m.beginCheckout(proj)
	case "e":
		return m.openScriptPicker(proj)
	}

	if cur.kind == rowProject {
		switch key {
		case "enter":
			m.collapsed[cur.project] = !m.collapsed[cur.project]
			m.clampCursor()
		case "l":
			return m.openBuiltinLog(proj)
		}
		return m, nil
	}

	switch key {
	case "s":
		return m, m.startCmd(cur.spec, false)
	case "r":
		return m, m.startCmd(cur.spec, true)
	case "x":
		c, id := m.deps.Client, cur.spec.ID()
		return m, func() tea.Msg { return actionMsg{err: c.Stop(id)} }
	case "enter", "l":
		return m.openLog(cur.spec)
	}
	return m, nil
}

func (m Model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filter.SetValue("")
		fallthrough
	case "enter":
		m.filtering = false
		m.filter.Blur()
		m.clampCursor()
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.cursor = 0
	return m, cmd
}

// builtinCmd: pull / checkout / install; only one can run per project at a time.
func (m Model) builtinCmd(p discovery.Project, kind task.Kind, branch string) tea.Cmd {
	return m.builtinSpecCmd(task.Spec{Project: p.Name, ProjectPath: p.Path, Kind: kind, Branch: branch})
}

// builtinSpecCmd: starts a built-in operation, unless one is already running on the project.
func (m Model) builtinSpecCmd(spec task.Spec) tea.Cmd {
	if info, ok := m.tasks[task.BuiltinID(spec.ProjectPath)]; ok && info.Status.Active() {
		label := info.Spec.Label()
		return func() tea.Msg {
			return actionMsg{err: fmt.Errorf("a task is already running on this project: %s", label)}
		}
	}
	c := m.deps.Client
	return func() tea.Msg {
		_, err := c.Start(spec)
		return actionMsg{err: err, text: spec.Label() + " started"}
	}
}

// startCmd: before starting, checks whether the task's last known port is in use.
func (m Model) startCmd(spec task.Spec, restart bool) tea.Cmd {
	c := m.deps.Client
	info, known := m.tasks[spec.ID()]
	checkPort := known && info.LastPort > 0 && !info.Status.Active()
	return func() tea.Msg {
		if checkPort {
			if owner, err := c.PortOwner(info.LastPort); err == nil && owner != nil {
				return portConflictMsg{spec: spec, port: info.LastPort, owner: *owner, restart: restart}
			}
		}
		return launch(c, spec, restart)
	}
}

func launch(c Client, spec task.Spec, restart bool) tea.Msg {
	var err error
	if restart {
		_, err = c.Restart(spec)
	} else {
		_, err = c.Start(spec)
	}
	return actionMsg{err: err, text: spec.Label() + " started"}
}

func (m *Model) openPortConflict(msg portConflictMsg) {
	who := fmt.Sprintf("an external process (PID %d, %s)", msg.owner.PID, msg.owner.Command)
	if msg.owner.TaskID != "" {
		who = "the blixa task: " + msg.owner.TaskID
	}
	c, spec, restart, owner, port := m.deps.Client, msg.spec, msg.restart, msg.owner, msg.port
	m.confirm = confirmDialog{
		text: fmt.Sprintf("Port :%d is in use by %s.", port, who),
		options: []confirmOption{
			{key: "k", label: "stop it, then start", cmd: func() tea.Msg {
				var err error
				if owner.TaskID != "" {
					err = c.Stop(owner.TaskID)
				} else {
					err = c.Kill(owner.PID)
				}
				if err != nil {
					return actionMsg{err: err}
				}
				for i := 0; i < 30; i++ {
					if o, err := c.PortOwner(port); err == nil && o == nil {
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
				return launch(c, spec, restart)
			}},
			{key: "s", label: "start anyway", cmd: func() tea.Msg { return launch(c, spec, restart) }},
		},
	}
	m.mode = modeConfirm
}

func (m *Model) openShutdownConfirm() {
	c := m.deps.Client
	m.confirm = confirmDialog{
		text: "Stop all tasks and the daemon?",
		options: []confirmOption{{key: "y", label: "yes", cmd: func() tea.Msg {
			if err := c.Shutdown(true); err != nil {
				return actionMsg{err: err}
			}
			return quitMsg{}
		}}},
	}
	m.mode = modeConfirm
}

func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "esc" || key == "n" {
		m.mode = modeMain
		return m, nil
	}
	for _, o := range m.confirm.options {
		if key == o.key {
			m.mode = modeMain
			return m, o.cmd
		}
	}
	return m, nil
}

func (m Model) saveConfig() error {
	if m.cfgErr != nil {
		return fmt.Errorf("the config is invalid, not saving: %v", m.cfgErr)
	}
	return config.Save(m.deps.ConfigPath, m.cfg)
}

func (m Model) removeProject(name string) (tea.Model, tea.Cmd) {
	if m.cfgErr != nil {
		m.flash = m.saveConfig().Error()
		return m, nil
	}
	m.cfg.RemoveProject(name)
	if err := m.saveConfig(); err != nil {
		m.flash = "saving config: " + err.Error()
	}
	m.clampCursor()
	return m, nil
}
