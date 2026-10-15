package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/laszlolevi/blixa/internal/task"
)

// Palette. Hex colors are degraded by lipgloss to what the terminal supports, and
// NO_COLOR turns coloring off entirely.
var (
	colorAccent  = lipgloss.Color("#CB3837") // npm red
	colorAccent2 = lipgloss.Color("#F5A623")
	colorGreen   = lipgloss.Color("#5FD787")
	colorYellow  = lipgloss.Color("#E5C07B")
	colorRed     = lipgloss.Color("#E06C75")
	colorCyan    = lipgloss.Color("#56B6C2")
	colorBlue    = lipgloss.Color("#61AFEF")
	colorPurple  = lipgloss.Color("#C678DD")
	colorOrange  = lipgloss.Color("#D19A66")
	colorGray    = lipgloss.Color("8")
	colorCursor  = lipgloss.AdaptiveColor{Light: "#E4E4E4", Dark: "#3A3A3A"}
)

var (
	styleTitle  = lipgloss.NewStyle().Bold(true)
	styleDim    = lipgloss.NewStyle().Foreground(colorGray)
	styleCursor = lipgloss.NewStyle().Background(colorCursor).Bold(true)
	styleErr    = lipgloss.NewStyle().Foreground(colorRed)
	styleOK     = lipgloss.NewStyle().Foreground(colorGreen)
	styleWarn   = lipgloss.NewStyle().Foreground(colorYellow)
	styleAccent = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleKey    = lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	stylePort   = lipgloss.NewStyle().Foreground(colorCyan)
	styleBranch = lipgloss.NewStyle().Foreground(colorBlue)
	styleAhead  = lipgloss.NewStyle().Foreground(colorGreen)
	styleBehind = lipgloss.NewStyle().Foreground(colorYellow)
	styleDirty  = lipgloss.NewStyle().Foreground(colorOrange)
	styleCheck  = lipgloss.NewStyle().Foreground(colorGreen)
	stylePlain  = lipgloss.NewStyle()

	styleActiveTab = lipgloss.NewStyle().Background(colorCursor).Foreground(colorAccent2).Bold(true)

	styleMatch        = lipgloss.NewStyle().Background(colorYellow).Foreground(lipgloss.Color("#000000"))
	styleCurrentMatch = lipgloss.NewStyle().Background(colorAccent2).Foreground(lipgloss.Color("#000000")).Bold(true)
)

// seg is a piece of a row with its own style. Rows are built from segments, so the
// cursor background can be applied to every segment (an inner ANSI reset would
// otherwise cut a single outer background short).
type seg struct {
	text  string
	style lipgloss.Style
}

// joinSegs renders the segments; base (cursor, dimmed) is layered under each segment's style.
func joinSegs(segs []seg, base *lipgloss.Style) string {
	var b strings.Builder
	for _, s := range segs {
		st := s.style
		if base != nil {
			st = base.Inherit(s.style)
		}
		b.WriteString(st.Render(s.text))
	}
	return b.String()
}

// plainText concatenates the segments without styling.
func plainText(segs []seg) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.text)
	}
	return b.String()
}

// statusStyle: the color of a task's status (and of a project's last builtin operation).
func statusStyle(info *task.Info) lipgloss.Style {
	if info == nil {
		return styleDim
	}
	switch info.Status {
	case task.StatusStarting:
		return styleWarn
	case task.StatusRunning:
		return styleOK
	case task.StatusExited:
		if info.ExitCode == 0 {
			return styleOK
		}
		return styleErr
	case task.StatusCrashed:
		return styleErr
	case task.StatusUnknown:
		return lipgloss.NewStyle().Foreground(colorPurple)
	}
	return styleDim
}

// gitSegs colors the tokens of an already width-fitted git text: the branch, ↑N, ↓N, ✎.
func gitSegs(text string) []seg {
	trimmed := strings.TrimRight(text, " ")
	tokens := strings.Split(trimmed, " ")
	var segs []seg
	for i, tok := range tokens {
		if i > 0 {
			segs = append(segs, seg{" ", stylePlain})
		}
		st := styleBranch
		switch {
		case i == 0:
		case strings.HasPrefix(tok, "↑"):
			st = styleAhead
		case strings.HasPrefix(tok, "↓"):
			st = styleBehind
		case tok == "✎":
			st = styleDirty
		}
		segs = append(segs, seg{tok, st})
	}
	if pad := len(text) - len(trimmed); pad > 0 {
		segs = append(segs, seg{strings.Repeat(" ", pad), stylePlain})
	}
	return segs
}

// renderKeys renders a "k label  k label" help line with highlighted keys.
func renderKeys(line string) string {
	var b strings.Builder
	for i, part := range strings.Split(line, "  ") {
		if i > 0 {
			b.WriteString("  ")
		}
		key, label, found := strings.Cut(part, " ")
		if !found {
			b.WriteString(styleDim.Render(part))
			continue
		}
		b.WriteString(styleKey.Render(key) + " " + styleDim.Render(label))
	}
	return b.String()
}
