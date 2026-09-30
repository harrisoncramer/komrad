package ui

import (
	"strings"

	"komrad/internal/panes"
	"komrad/internal/ui/paint"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// tabStop is how far apart tmux sets a pane's tab stops.
const tabStop = 8

// visualLabel is what the header says while visual mode holds the window.
const visualLabel = " VISUAL "

// visualState is the window as visual mode froze it.
type visualState struct {
	// freezing is whether a snapshot is being taken.
	freezing bool
	// frozen is the window drawn as it stood, empty while visual mode is off.
	frozen string
	// returnTo is the pane that held the keys before the window froze.
	returnTo string
}

func (v visualState) isActive() bool {
	return v.freezing || v.frozen != ""
}

// visualFrozenMsg is the snapshot of the window, taken off the update loop.
type visualFrozenMsg struct {
	frozen   string
	returnTo string
}

// routeVisual answers every key while visual mode holds the window.
func (m model) routeVisual(key tea.KeyPressMsg) (model, bool) {
	if !m.visual.isActive() {
		return m, false
	}

	if m.visual.freezing {
		return m, true
	}

	if pressed := key.String(); pressed == "esc" || pressed == "q" {
		return m.thaw(), true
	}

	return m, true
}

func (m model) freezeCmd() tea.Cmd {
	sidebar := m.settings.Pane
	tmux := m.tmux
	palette := paint.Panel

	return func() tea.Msg {
		returnTo := tmux.GetActivePane(sidebar)
		shown := tmux.GetShownPanes(sidebar)

		screens := make(map[string]string, len(shown))
		for _, rect := range shown {
			screens[rect.Pane] = tmux.CaptureScreen(rect.Pane)
		}

		return visualFrozenMsg{
			frozen:   drawFrozen(shown, screens, sidebar, palette),
			returnTo: returnTo,
		}
	}
}

func (m model) applyFreeze(msg visualFrozenMsg) model {
	if !m.visual.freezing {
		return m
	}

	m.visual = visualState{frozen: msg.frozen, returnTo: msg.returnTo}
	if msg.frozen == "" {
		m.visual = visualState{}

		return m
	}

	m.frame.Freeze()

	return m
}

func (m model) thaw() model {
	returnTo := m.visual.returnTo
	m.visual = visualState{}

	m.frame.Thaw()

	if returnTo != "" && returnTo != m.settings.Pane {
		m.tmux.SelectPane(returnTo)
	}

	return m
}

// drawFrozen lays each pane's screen where the pane sat, with the visual label over the right of
// the sidebar's first row.
func drawFrozen(shown []panes.Rect, screens map[string]string, sidebar string, palette paint.Palette) string {
	if len(shown) == 0 {
		return ""
	}

	left, top := shown[0].Left, shown[0].Top
	right, bottom := 0, 0

	for _, rect := range shown {
		left, top = min(left, rect.Left), min(top, rect.Top)
		right, bottom = max(right, rect.Left+rect.Width), max(bottom, rect.Top+rect.Height)
	}

	layers := make([]*lipgloss.Layer, 0, len(shown)+1)

	for _, rect := range shown {
		layers = append(layers, lipgloss.NewLayer(fitScreen(screens[rect.Pane], rect)).
			X(rect.Left-left).
			Y(rect.Top-top))

		if rect.Pane != sidebar {
			continue
		}

		label := palette.Visual().Render(visualLabel)
		layers = append(layers, lipgloss.NewLayer(label).
			X(rect.Left-left+rect.Width-lipgloss.Width(label)-gutter-1).
			Y(rect.Top-top).
			Z(1))
	}

	canvas := lipgloss.NewCanvas(right-left, bottom-top)
	canvas.Compose(lipgloss.NewCompositor(layers...))

	return canvas.Render()
}

// fitScreen is a captured screen cut to its pane's rows, with its tabs spent as the spaces tmux
// moved the cursor over.
func fitScreen(screen string, rect panes.Rect) string {
	lines := strings.Split(strings.TrimSuffix(screen, "\n"), "\n")
	if len(lines) > rect.Height {
		lines = lines[:rect.Height]
	}

	for i, line := range lines {
		lines[i] = expandTabs(line)
	}

	return strings.Join(lines, "\n")
}

func expandTabs(line string) string {
	if !strings.Contains(line, "\t") {
		return line
	}

	var b strings.Builder

	col := 0
	for i, piece := range strings.Split(line, "\t") {
		if i > 0 {
			pad := tabStop - col%tabStop
			b.WriteString(strings.Repeat(" ", pad))
			col += pad
		}

		b.WriteString(piece)
		col += lipgloss.Width(piece)
	}

	return b.String()
}
