package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"blixa/internal/git"
	"blixa/internal/task"
)

// The checkout picker has two lists, each in its own frame and with its own cursor, so
// the remote branches are reachable without scrolling through every local one.
const (
	listLocal = iota
	listRemote
)

type branchItem struct {
	b      git.Branch
	detail string
	local  *git.Branch // a remote branch's local counterpart, if there is one
}

type branchPicker struct {
	project string
	current string // the branch we're on
	lists   [2][]branchItem
	cursor  [2]int // index into visible(list)
	focus   int
	input   textinput.Model
}

type branchesMsg struct {
	project  string
	branches []git.Branch
	err      error
}

// localOnlyMsg: the answer to "would resetting the local branch to the remote one drop
// commits?", asked before doing it.
type localOnlyMsg struct {
	project string
	remote  git.Branch
	count   int
	err     error
}

func (m Model) openBranchPicker(msg branchesMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.flash = "branches: " + msg.err.Error()
		return m, nil
	}
	m.flash = ""
	now := m.deps.Now()
	st, known := m.gitStatus[msg.project]
	bp := branchPicker{project: msg.project}
	if known {
		bp.current = st.Branch
	}
	locals := map[string]*git.Branch{}
	for _, b := range msg.branches {
		if b.Remote == "" {
			locals[b.Name] = &b
		}
	}
	for _, b := range msg.branches {
		it := branchItem{b: b, detail: ago(now, b.Date)}
		if b.Remote == "" {
			if known && b.Name == st.Branch {
				cur := "current"
				if st.Ahead > 0 {
					cur += fmt.Sprintf(" ↑%d", st.Ahead)
				}
				if st.Behind > 0 {
					cur += fmt.Sprintf(" ↓%d", st.Behind)
				}
				it.detail = cur + " · " + it.detail
			}
			bp.lists[listLocal] = append(bp.lists[listLocal], it)
			continue
		}
		if l, ok := locals[b.Name]; ok {
			it.local = l
			if l.Hash == b.Hash {
				it.detail = "= local · " + it.detail
			} else {
				it.detail = "≠ local · " + it.detail
			}
		}
		bp.lists[listRemote] = append(bp.lists[listRemote], it)
	}
	bp.input = textinput.New()
	bp.input.Prompt = "filter: "
	bp.input.Focus()
	if len(bp.lists[listLocal]) == 0 {
		bp.focus = listRemote
	}
	m.branches = bp
	m.mode = modeBranches
	return m, textinput.Blink
}

// visible: the indexes of the list's items matching the filter.
func (p branchPicker) visible(list int) []int {
	q := strings.ToLower(p.input.Value())
	var idx []int
	for i, it := range p.lists[list] {
		if q == "" || strings.Contains(strings.ToLower(it.b.FullName()), q) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (p branchPicker) selected() (branchItem, bool) {
	vis := p.visible(p.focus)
	if c := p.cursor[p.focus]; c >= 0 && c < len(vis) {
		return p.lists[p.focus][vis[c]], true
	}
	return branchItem{}, false
}

func (m Model) updateBranchPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.branches
	switch msg.String() {
	case "esc":
		m.mode = modeMain
		return m, nil
	case "up":
		if p.cursor[p.focus] > 0 {
			p.cursor[p.focus]--
		}
		return m, nil
	case "down":
		if p.cursor[p.focus] < len(p.visible(p.focus))-1 {
			p.cursor[p.focus]++
		}
		return m, nil
	case "tab", "shift+tab", "left", "right":
		p.focus = 1 - p.focus
		return m, nil
	case "enter":
		it, ok := p.selected()
		if !ok {
			return m, nil
		}
		return m.checkoutBranch(it)
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.cursor = [2]int{}
	// the focused list filtered empty, the other has matches: follow them
	if len(p.visible(p.focus)) == 0 && len(p.visible(1-p.focus)) > 0 {
		p.focus = 1 - p.focus
	}
	return m, cmd
}

// checkoutBranch: a local branch is checked out as is. A remote one becomes the local
// branch of the same name -- created if there's none, reset to the remote if there is
// one that differs, after checking that this drops no commit found only locally.
func (m Model) checkoutBranch(it branchItem) (tea.Model, tea.Cmd) {
	proj, _ := m.project(m.branches.project)
	if it.b.Remote == "" {
		if it.b.Name == m.branches.current {
			m.flash = "already on " + it.b.Name
			return m, nil
		}
		m.mode = modeMain
		return m, m.builtinCmd(proj, task.KindCheckout, it.b.Name)
	}
	if it.local == nil || it.local.Hash == it.b.Hash {
		m.mode = modeMain
		return m, m.checkoutFromCmd(proj.Name, it.b)
	}
	check, dir := m.deps.LocalOnly, proj.Path
	project, remote := proj.Name, it.b
	m.flash = "comparing with " + it.b.FullName() + "..."
	return m, func() tea.Msg {
		if check == nil {
			return localOnlyMsg{project: project, remote: remote, count: -1}
		}
		n, err := check(dir, remote.Name, remote.FullName())
		return localOnlyMsg{project: project, remote: remote, count: n, err: err}
	}
}

func (m Model) checkoutFromCmd(project string, remote git.Branch) tea.Cmd {
	p, _ := m.project(project)
	return m.builtinSpecCmd(task.Spec{Project: p.Name, ProjectPath: p.Path, Kind: task.KindCheckout, Branch: remote.Name, From: remote.FullName()})
}

// applyLocalOnly: resets right away if no commit would be lost, asks first otherwise.
func (m Model) applyLocalOnly(msg localOnlyMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeBranches || msg.project != m.branches.project {
		return m, nil // the picker was closed meanwhile
	}
	m.flash = ""
	if msg.err == nil && msg.count == 0 {
		m.mode = modeMain
		return m, m.checkoutFromCmd(msg.project, msg.remote)
	}
	text := fmt.Sprintf("%d commit(s) of %s are only local and would be dropped (git reflog still has them).", msg.count, msg.remote.Name)
	if msg.err != nil || msg.count < 0 {
		text = "Couldn't check whether " + msg.remote.Name + " has commits that are only local; they would be dropped."
	}
	m.mode = modeConfirm
	m.confirm = confirmDialog{
		text:    text + " Reset it to " + msg.remote.FullName() + "?",
		options: []confirmOption{{key: "enter", label: "reset", cmd: m.checkoutFromCmd(msg.project, msg.remote)}},
	}
	return m, nil
}

func (m Model) viewBranchPicker() string {
	p := m.branches
	width := m.width
	if width <= 0 {
		width = 100
	}
	var b strings.Builder
	b.WriteString(styleAccent.Render(p.project+" - checkout") + "\n" + p.input.View() + "\n")
	// the two frames split the height evenly, so filtering doesn't make them jump
	avail := m.height - 2 - 4 - 2 // title + filter, the frames' borders, status + keys
	heights := [2]int{avail / 2, avail - avail/2}
	for list, name := range []string{"local", "remote"} {
		vis := p.visible(list)
		h := heights[list]
		if m.height <= 0 {
			h = len(vis)
		}
		h = max(h, 1)
		labelW := 0
		for _, i := range vis {
			labelW = max(labelW, runewidth.StringWidth(p.lists[list][i].b.FullName()))
		}
		labelW = min(labelW, maxPickerLabel)
		var lines []string
		from, to := window(p.cursor[list], len(vis), h)
		for vi := from; vi < to; vi++ {
			it := p.lists[list][vis[vi]]
			label, detail := fit(it.b.FullName(), labelW)+"  ", it.detail
			labelStyle, detailStyle := stylePlain, styleDim
			if it.b.Remote == "" && it.b.Name == p.current {
				labelStyle, detailStyle = styleTitle, styleKey.UnsetBold()
			}
			segs := []seg{{label, labelStyle}, {detail, detailStyle}}
			if list == p.focus && vi == p.cursor[list] {
				if pad := width - 4 - runewidth.StringWidth(plainText(segs)); pad > 0 {
					segs = append(segs, seg{strings.Repeat(" ", pad), stylePlain})
				}
				lines = append(lines, joinSegs(segs, &styleCursor))
			} else {
				lines = append(lines, joinSegs(segs, nil))
			}
		}
		if len(vis) == 0 {
			lines = append(lines, styleDim.Render("no matching branch"))
		}
		for m.height > 0 && len(lines) < h {
			lines = append(lines, "")
		}
		title := fmt.Sprintf("%s (%d)", name, len(vis))
		b.WriteString(frame(title, lines, width, list == p.focus))
	}
	b.WriteString(m.flash + "\n")
	b.WriteString(renderKeys("↑↓ move  tab/←→ local ⇄ remote  enter checkout  esc cancel  type filter"))
	return b.String()
}
