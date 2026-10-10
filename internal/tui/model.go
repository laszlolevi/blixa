package tui

import (
	"context"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"blixa/internal/config"
	"blixa/internal/discovery"
	"blixa/internal/git"
	"blixa/internal/paths"
	"blixa/internal/supervisor"
	"blixa/internal/task"
)

// Client: the part of the daemon API the TUI uses (implemented by *api.Client).
type Client interface {
	Start(task.Spec) (task.Info, error)
	Restart(task.Spec) (task.Info, error)
	Stop(id string) error
	PortOwner(port int) (*supervisor.PortOwner, error)
	Kill(pid int) error
	Shutdown(stopAll bool) error
}

// Deps: the model's external dependencies (replaceable with fakes in tests).
type Deps struct {
	Client     Client
	ConfigPath string
	StateDir   string
	Subscribe  func() (<-chan []task.Info, error)
	StreamLog  func(ctx context.Context, id string) (<-chan string, error)
	Scan       func(root string) ([]discovery.Project, error)
	GitStatus  func(dir string) (git.Status, error)
	Branches   func(dir string) ([]string, error)
	Now        func() time.Time
	Splash     bool   // show the logo splash on startup
	Version    string // shown on the splash
}

type mode int

const (
	modeMain mode = iota
	modeConfirm
	modeHelp
	modeScripts
	modeBranches
	modeLog
)

type Model struct {
	deps      Deps
	cfg       config.Config
	cfgErr    error
	projects  []discovery.Project
	scanned   bool
	gitStatus map[string]git.Status
	tasks     map[string]task.Info
	collapsed map[string]bool
	showAll   bool
	cursor    int
	filter    textinput.Model
	filtering bool
	connected bool
	flash     string
	mode      mode
	width     int
	height    int
	confirm   confirmDialog
	picker    picker
	log       logView

	splash        bool
	splashMinDone bool
}

type (
	tasksMsg struct {
		tasks []task.Info
		ch    <-chan []task.Info
	}
	disconnectedMsg struct{}
	reconnectMsg    struct{}
	projectsMsg     struct {
		projects []discovery.Project
		err      error
	}
	gitMsg struct {
		project string
		status  git.Status
		err     error
	}
	gitTickMsg   struct{}
	clockTickMsg struct{}
	actionMsg    struct {
		err  error
		text string
	}
	portConflictMsg struct {
		spec    task.Spec
		port    int
		owner   supervisor.PortOwner
		restart bool
	}
	quitMsg struct{}
)

// New: with an invalid config (cfgErr), every project is shown, and nothing is saved.
func New(deps Deps, cfg config.Config, cfgErr error) Model {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if cfgErr != nil {
		cfg = config.Default()
	}
	f := textinput.New()
	f.Prompt = "/"
	return Model{
		deps:      deps,
		cfg:       cfg,
		cfgErr:    cfgErr,
		gitStatus: map[string]git.Status{},
		tasks:     map[string]task.Info{},
		collapsed: map[string]bool{},
		showAll:   cfgErr != nil,
		filter:    f,
		splash:    deps.Splash,
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.subscribeCmd(), m.scanCmd(), gitTick(), clockTick()}
	if m.splash {
		cmds = append(cmds, splashTimer())
	}
	return tea.Batch(cmds...)
}

func (m Model) subscribeCmd() tea.Cmd {
	sub := m.deps.Subscribe
	return func() tea.Msg {
		ch, err := sub()
		if err != nil {
			return disconnectedMsg{}
		}
		return waitTasks(ch)()
	}
}

func waitTasks(ch <-chan []task.Info) tea.Cmd {
	return func() tea.Msg {
		tasks, ok := <-ch
		if !ok {
			return disconnectedMsg{}
		}
		return tasksMsg{tasks: tasks, ch: ch}
	}
}

func gitTick() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg { return gitTickMsg{} })
}

// clockTick: a redraw every second, so running tasks' elapsed time updates live.
func clockTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return clockTickMsg{} })
}

func (m Model) scanCmd() tea.Cmd {
	root, scan := paths.Expand(m.cfg.Root), m.deps.Scan
	// the daemon runs with $HOME as its cwd, so a relative root would be relative to that,
	// not to the user's expected working directory -- make it absolute before passing it to Scan.
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return func() tea.Msg {
		ps, err := scan(root)
		return projectsMsg{projects: ps, err: err}
	}
}

func (m Model) gitCmd(p discovery.Project) tea.Cmd {
	gs := m.deps.GitStatus
	return func() tea.Msg {
		st, err := gs(p.Path)
		return gitMsg{project: p.Name, status: st, err: err}
	}
}

func (m Model) refreshAllGit() tea.Cmd {
	var cmds []tea.Cmd
	for _, p := range m.projects {
		if p.IsGit {
			cmds = append(cmds, m.gitCmd(p))
		}
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.log.resize(msg.Width, msg.Height)
		return m, nil
	case tasksMsg:
		m.connected = true
		cmd := m.applyTasks(msg.tasks)
		return m, tea.Batch(cmd, waitTasks(msg.ch))
	case disconnectedMsg:
		m.connected = false
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return reconnectMsg{} })
	case reconnectMsg:
		return m, m.subscribeCmd()
	case projectsMsg:
		if msg.err != nil {
			m.flash = "reading projects: " + msg.err.Error()
			return m, nil
		}
		m.projects, m.scanned = msg.projects, true
		m.clampCursor()
		m.maybeEndSplash()
		return m, m.refreshAllGit()
	case gitMsg:
		if msg.err == nil {
			m.gitStatus[msg.project] = msg.status
		}
		return m, nil
	case gitTickMsg:
		return m, tea.Batch(m.refreshAllGit(), gitTick())
	case clockTickMsg:
		return m, clockTick()
	case splashDoneMsg:
		m.splashMinDone = true
		m.maybeEndSplash()
		return m, nil
	case actionMsg:
		if msg.err != nil {
			m.flash = msg.err.Error()
		} else {
			m.flash = msg.text
		}
		return m, nil
	case portConflictMsg:
		if m.mode != modeMain {
			m.flash = "the port-conflict check result was lost, try again"
			return m, nil
		}
		m.openPortConflict(msg)
		return m, nil
	case branchesMsg:
		if m.mode != modeMain {
			return m, nil // a lost result: silently discarded
		}
		return m.openBranchPicker(msg)
	case logLinesMsg:
		return m.updateLogLines(msg)
	case quitMsg:
		return m, tea.Quit
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.splash { // any key skips the splash, without acting on the key
			m.splash = false
			return m, nil
		}
		switch m.mode {
		case modeMain:
			return m.updateMain(msg)
		case modeConfirm:
			return m.updateConfirm(msg)
		case modeLog:
			return m.updateLog(msg)
		case modeScripts, modeBranches:
			return m.updatePicker(msg)
		case modeHelp:
			m.mode = modeMain
			return m, nil
		}
	}
	return m, nil
}

// applyTasks adopts the new state; if a built-in operation just finished,
// it refreshes that project's git status.
func (m *Model) applyTasks(list []task.Info) tea.Cmd {
	var cmds []tea.Cmd
	next := make(map[string]task.Info, len(list))
	for _, info := range list {
		id := info.Spec.ID()
		next[id] = info
		prev, ok := m.tasks[id]
		if info.Spec.Kind != task.KindScript && ok && prev.Status.Active() && !info.Status.Active() {
			if p, found := m.project(info.Spec.Project); found && p.IsGit {
				cmds = append(cmds, m.gitCmd(p))
			}
		}
	}
	m.tasks = next
	return tea.Batch(cmds...)
}

func (m Model) project(name string) (discovery.Project, bool) {
	for _, p := range m.projects {
		if p.Name == name {
			return p, true
		}
	}
	return discovery.Project{}, false
}

func (m Model) rows() []row {
	return buildRows(m.projects, m.scanned, m.cfg, m.collapsed, m.showAll, m.filter.Value())
}

func (m *Model) clampCursor() {
	n := len(m.rows())
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}
