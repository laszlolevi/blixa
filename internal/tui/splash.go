package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// splashMinDuration: the splash stays at least this long, so it doesn't just flash by.
const splashMinDuration = 800 * time.Millisecond

const tagline = "terminal UI for npm, pnpm, yarn & bun projects"

var bigLogo = []string{
	"██████╗ ██╗     ██╗██╗  ██╗ █████╗ ",
	"██╔══██╗██║     ██║╚██╗██╔╝██╔══██╗",
	"██████╔╝██║     ██║ ╚███╔╝ ███████║",
	"██╔══██╗██║     ██║ ██╔██╗ ██╔══██║",
	"██████╔╝███████╗██║██╔╝ ██╗██║  ██║",
	"╚═════╝ ╚══════╝╚═╝╚═╝  ╚═╝╚═╝  ╚═╝",
}

var smallLogo = []string{
	"┌┐ ┬  ┬─┐ ┬┌─┐",
	"├┴┐│  │┌┴┬┘├─┤",
	"└─┘┴─┘┴┴ └─┴ ┴",
}

type splashDoneMsg struct{}

func splashTimer() tea.Cmd {
	return tea.Tick(splashMinDuration, func(time.Time) tea.Msg { return splashDoneMsg{} })
}

// maybeEndSplash closes the splash once the minimum time has passed and the projects
// are loaded (the daemon connection is shown in the header afterwards anyway).
func (m *Model) maybeEndSplash() {
	if m.splash && m.splashMinDone && m.scanned {
		m.splash = false
	}
}

func (m Model) viewSplash() string {
	logo := bigLogo
	if m.width > 0 && (m.width < runewidth.StringWidth(bigLogo[0])+4 || m.height < len(bigLogo)+6) {
		logo = smallLogo
	}
	var b strings.Builder
	for _, line := range logo {
		b.WriteString(gradient(line, colorAccent, colorAccent2) + "\n")
	}
	b.WriteString("\n" + styleDim.Render(tagline))
	if m.deps.Version != "" {
		b.WriteString(styleDim.Render(" · " + m.deps.Version))
	}
	status := "loading projects…"
	if m.scanned && !m.connected {
		status = "connecting to daemon…"
	}
	b.WriteString("\n\n" + styleWarn.Render(status))
	if m.width == 0 || m.height == 0 {
		return b.String()
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, b.String())
}

// gradient colors the line character by character from one hex color to another.
func gradient(line string, from, to lipgloss.Color) string {
	r1, g1, b1 := hexRGB(string(from))
	r2, g2, b2 := hexRGB(string(to))
	runes := []rune(line)
	var b strings.Builder
	for i, r := range runes {
		t := 0.0
		if len(runes) > 1 {
			t = float64(i) / float64(len(runes)-1)
		}
		c := fmt.Sprintf("#%02X%02X%02X", lerp(r1, r2, t), lerp(g1, g2, t), lerp(b1, b2, t))
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(string(r)))
	}
	return b.String()
}

func hexRGB(hex string) (int, int, int) {
	var r, g, b int
	fmt.Sscanf(strings.TrimPrefix(hex, "#"), "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

func lerp(a, b int, t float64) int { return a + int(float64(b-a)*t+0.5) }
