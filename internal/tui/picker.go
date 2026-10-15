package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"blixa/internal/config"
	"blixa/internal/discovery"
	"blixa/internal/task"
)

type pickerItem struct {
	label   string
	detail  string
	value   string
	checked bool
}

type picker struct {
	title   string
	project string
	multi   bool
	items   []pickerItem
	input   textinput.Model
	cursor  int // index into the visible() list
}

func newPicker(title, project string, multi bool, items []pickerItem) picker {
	in := textinput.New()
	in.Prompt = "filter: "
	in.Focus()
	return picker{title: title, project: project, multi: multi, items: items, input: in}
}

// visible: the indexes of items matching the filter.
func (p picker) visible() []int {
	q := strings.ToLower(p.input.Value())
	var idx []int
	for i, it := range p.items {
		if q == "" || strings.Contains(strings.ToLower(it.label), q) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (p picker) selected() (int, bool) {
	vis := p.visible()
	if p.cursor < 0 || p.cursor >= len(vis) {
		return 0, false
	}
	return vis[p.cursor], true
}

func (m Model) openScriptPicker(p discovery.Project) (tea.Model, tea.Cmd) {
	var items []pickerItem
	for _, s := range p.Scripts {
		ref := config.Ref{Dir: s.Dir, Script: s.Name}
		label := task.Spec{Kind: task.KindScript, Dir: s.Dir, Script: s.Name}.Label()
		items = append(items, pickerItem{label: label, detail: s.Command, value: ref.String(), checked: m.cfg.IsSelected(p.Name, ref)})
	}
	m.picker = newPicker(p.Name+" - scripts (space: select, enter/esc: done)", p.Name, true, items)
	m.mode = modeScripts
	return m, textinput.Blink
}

// beginCheckout: asks for confirmation first if there are uncommitted changes.
func (m Model) beginCheckout(p discovery.Project) (tea.Model, tea.Cmd) {
	br := m.deps.Branches
	load := func() tea.Msg {
		b, err := br(p.Path)
		return branchesMsg{project: p.Name, branches: b, err: err}
	}
	if st, ok := m.gitStatus[p.Name]; ok && st.Dirty {
		m.confirm = confirmDialog{
			text:    p.Name + ": there are uncommitted changes. Continue with the checkout?",
			options: []confirmOption{{key: "y", label: "yes", cmd: load}},
		}
		m.mode = modeConfirm
		return m, nil
	}
	m.flash = "fetching branches..."
	return m, load
}

func (m Model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeMain
		m.clampCursor()
		return m, nil
	case "up":
		if m.picker.cursor > 0 {
			m.picker.cursor--
		}
		return m, nil
	case "down":
		if m.picker.cursor < len(m.picker.visible())-1 {
			m.picker.cursor++
		}
		return m, nil
	case " ":
		if m.mode == modeScripts {
			return m.toggleScript()
		}
	case "enter":
		m.mode = modeMain
		m.clampCursor()
		return m, nil
	}
	var cmd tea.Cmd
	m.picker.input, cmd = m.picker.input.Update(msg)
	if m.picker.cursor >= len(m.picker.visible()) {
		m.picker.cursor = 0
	}
	return m, cmd
}

func (m Model) toggleScript() (tea.Model, tea.Cmd) {
	if m.cfgErr != nil {
		m.flash = m.saveConfig().Error()
		return m, nil
	}
	i, ok := m.picker.selected()
	if !ok {
		return m, nil
	}
	ref, err := config.ParseRef(m.picker.items[i].value)
	if err != nil {
		return m, nil
	}
	m.cfg.Toggle(m.picker.project, ref)
	m.picker.items[i].checked = !m.picker.items[i].checked
	if err := m.saveConfig(); err != nil {
		m.flash = "saving config: " + err.Error()
	}
	return m, nil
}

// maxPickerLabel: past this, a label is cut so the details column stays on screen.
const maxPickerLabel = 60

func (m Model) viewPicker() string {
	p := m.picker
	var b strings.Builder
	b.WriteString(styleAccent.Render(p.title) + "\n" + p.input.View() + "\n\n")
	vis := p.visible()
	from, to := window(p.cursor, len(vis), m.height-5)
	// the details (a script's command, a branch's age) line up in one column
	labelW := 0
	for _, i := range vis {
		labelW = max(labelW, runewidth.StringWidth(p.items[i].label))
	}
	labelW = min(labelW, maxPickerLabel)
	for vi := from; vi < to; vi++ {
		it := p.items[vis[vi]]
		label := it.label
		if it.detail != "" {
			label = fit(label, labelW)
		}
		mark := ""
		if p.multi {
			mark = "[ ] "
			if it.checked {
				mark = "[x] "
			}
		}
		markStyle := styleDim
		if it.checked {
			markStyle = styleCheck
		}
		line := markStyle.Render(mark) + label
		if vi == p.cursor {
			line = joinSegs([]seg{{mark, markStyle}, {label, stylePlain}}, &styleCursor)
		}
		if it.detail != "" {
			line += "  " + styleDim.Render(it.detail)
		}
		b.WriteString(line + "\n")
	}
	if m.flash != "" {
		b.WriteString("\n" + m.flash)
	}
	return b.String()
}
