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
	Branches   func(dir string) ([]git.Branch, error)
	Fetch      func(dir string) error // periodic background fetch; nil: none
	LocalOnly  func(dir, local, upstream string) (int, error)
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
	cols      columns // the widest columns so far: they only grow, so nothing jumps when rows go away
	confirm   confirmDialog
	picker    picker
	branches  branchPicker
	log       logView
	chooser   rootChooser
	// the arrow keys act on the folder tabs instead of the project list
	folderFocus bool

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
		root     string // the root it was scanned under: a result for a previous root is dropped
		projects []discovery.Project
		err      error
	}
	gitMsg struct {
		project string
		path    string // a result for a same-named project of a previous root is dropped
		status  git.Status
		err     error
	}
	gitTickMsg   struct{}
	fetchTickMsg struct{}
	scanTickMsg  struct{}
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
	cmds := []tea.Cmd{m.subscribeCmd(), m.scanCmd(), gitTick(), scanTick(), clockTick(), fetchTick(firstFetchDelay)}
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

// The background fetch keeps the ↑/↓ counts honest: without it they'd only change when
// a pull or checkout happens to fetch.
const (
	firstFetchDelay = 10 * time.Second
	fetchInterval   = 5 * time.Minute
)

func fetchTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return fetchTickMsg{} })
}

// fetchAll: fetches every git project of the current root, then refreshes its status;
// a project with a pull / checkout / install in progress is skipped.
func (m Model) fetchAll() tea.Cmd {
	if m.deps.Fetch == nil {
		return nil
	}
	fetch, gs := m.deps.Fetch, m.deps.GitStatus
	var cmds []tea.Cmd
	for _, p := range m.projects {
		if info := m.builtin(p.Name); !p.IsGit || (info != nil && info.Status.Active()) {
			continue
		}
		cmds = append(cmds, func() tea.Msg {
			_ = fetch(p.Path) // offline / no access: the status is still refreshed from what's known
			st, err := gs(p.Path)
			return gitMsg{project: p.Name, path: p.Path, status: st, err: err}
		})
	}
	return tea.Batch(cmds...)
}

func gitTick() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg { return gitTickMsg{} })
}

// scanTick: a periodic rescan, so projects added to or removed from the root show up
// without restarting.
func scanTick() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return scanTickMsg{} })
}

// clockTick: a redraw every second, so running tasks' elapsed time updates live.
func clockTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return clockTickMsg{} })
}

func (m Model) scanCmd() tea.Cmd {
	cfgRoot := m.cfg.Root
	root, scan := paths.Expand(cfgRoot), m.deps.Scan
	// the daemon runs with $HOME as its cwd, so a relative root would be relative to that,
	// not to the user's expected working directory -- make it absolute before passing it to Scan.
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return func() tea.Msg {
		ps, err := scan(root)
		return projectsMsg{root: cfgRoot, projects: ps, err: err}
	}
}

func (m Model) gitCmd(p discovery.Project) tea.Cmd {
	gs := m.deps.GitStatus
	return func() tea.Msg {
		st, err := gs(p.Path)
		return gitMsg{project: p.Name, path: p.Path, status: st, err: err}
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
	next, cmd := m.step(msg)
	if nm, ok := next.(Model); ok {
		nm.cols = nm.cols.grow(nm.columns(nm.rows()))
		// the mouse is captured only in the log view (for wheel / touchpad scrolling): elsewhere
		// the terminal keeps it, so text can still be selected and copied as usual
		switch {
		case m.mode != modeLog && nm.mode == modeLog:
			cmd = tea.Batch(cmd, tea.EnableMouseCellMotion)
		case m.mode == modeLog && nm.mode != modeLog:
			cmd = tea.Batch(cmd, tea.DisableMouse)
		}
		next = nm
	}
	return next, cmd
}

func (m Model) step(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		if msg.root != "" && msg.root != m.cfg.Root {
			return m, nil
		}
		if msg.err != nil {
			m.flash = "reading projects: " + msg.err.Error()
			return m, nil
		}
		return m, m.applyProjects(msg.projects)
	case gitMsg:
		if p, ok := m.project(msg.project); msg.path != "" && (!ok || p.Path != msg.path) {
			return m, nil
		}
		if msg.err == nil {
			m.gitStatus[msg.project] = msg.status
		}
		return m, nil
	case fetchTickMsg:
		return m, tea.Batch(m.fetchAll(), fetchTick(fetchInterval))
	case gitTickMsg:
		return m, tea.Batch(m.refreshAllGit(), gitTick())
	case scanTickMsg:
		return m, tea.Batch(m.scanCmd(), scanTick())
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
	case localOnlyMsg:
		return m.applyLocalOnly(msg)
	case closeRootMsg:
		return m.applyCloseRoot(msg)
	case tea.MouseMsg:
		if m.mode == modeLog && !m.log.searching {
			return m, m.log.scroll(msg)
		}
		return m, nil
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
			if m.chooser.active {
				return m.updateRootChooser(msg)
			}
			if m.folderFocus {
				return m.updateFolders(msg)
			}
			return m.updateMain(msg)
		case modeConfirm:
			return m.updateConfirm(msg)
		case modeLog:
			return m.updateLog(msg)
		case modeScripts:
			return m.updatePicker(msg)
		case modeBranches:
			return m.updateBranchPicker(msg)
		case modeHelp:
			m.mode = modeMain
			return m, nil
		}
	}
	return m, nil
}

// applyProjects adopts a (re)scan's result, keeping the cursor on the same row, and
// fetches the git status of the projects that were not known yet.
func (m *Model) applyProjects(list []discovery.Project) tea.Cmd {
	rows := m.rows()
	var cur *row
	if m.cursor < len(rows) {
		cur = &rows[m.cursor]
	}
	prev := map[string]bool{}
	for _, p := range m.projects {
		prev[p.Name] = true
	}
	var cmds []tea.Cmd
	present := map[string]bool{}
	for _, p := range list {
		present[p.Name] = true
		if !prev[p.Name] && p.IsGit {
			cmds = append(cmds, m.gitCmd(p))
		}
	}
	for name := range m.gitStatus {
		if !present[name] {
			delete(m.gitStatus, name)
		}
	}
	m.projects, m.scanned = list, true
	if cur != nil {
		m.cursor = findRow(m.rows(), *cur, m.cursor)
	}
	m.clampCursor()
	m.maybeEndSplash()
	return tea.Batch(cmds...)
}

// findRow: the index of r in rows (the same task, or the same project's header), else fallback.
func findRow(rows []row, r row, fallback int) int {
	for i, x := range rows {
		if x.kind == r.kind && x.project == r.project && (r.kind != rowTask || x.spec.ID() == r.spec.ID()) {
			return i
		}
	}
	return fallback
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

// builtin: the project's last built-in operation (pull / checkout / install), nil if none.
func (m Model) builtin(name string) *task.Info {
	p, ok := m.project(name)
	if !ok {
		return nil
	}
	if info, ok := m.tasks[task.BuiltinID(p.Path)]; ok {
		return &info
	}
	return nil
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
