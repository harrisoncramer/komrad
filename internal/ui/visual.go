package ui

import (
	"slices"
	"strings"

	"komrad/internal/panes"
	"komrad/internal/ui/paint"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// tabStop is how far apart tmux sets a pane's tab stops.
const tabStop = 8

// frozenMode is what the window was frozen for.
type frozenMode int

const (
	frozenVisual frozenMode = iota
	frozenPicker
)

func (f frozenMode) label() string {
	if f == frozenPicker {
		return " PANES "
	}

	return " VISUAL "
}

// pickLetters are the keys the picker hands out, one per target in reading order.
var pickLetters = []string{"d", "f", "j", "k", "l", "s", "a"}

// pickMargin is the blank cells cleared around a letter, across and down.
const (
	pickMarginX = 3
	pickMarginY = 1
)

// visualState is the window as it was frozen.
type visualState struct {
	mode frozenMode
	// freezing is whether a snapshot is being taken.
	freezing bool
	// frozen is the window drawn as it stood, empty while nothing is frozen.
	frozen string
	// returnTo is the pane that held the keys before the window froze.
	returnTo string
	targets  []pickTarget
}

func (v visualState) isActive() bool {
	return v.freezing || v.frozen != ""
}

// pickTarget is one place the picker can land the keys, in the window's cells.
type pickTarget struct {
	letter string
	rect   panes.Rect
	// pane is the tmux pane to select, empty for a block inside the sidebar.
	pane string
	// stop is the sidebar block landed on when pane is empty.
	stop stop
}

// sidebarBlock is a block the panel drew inside its own pane that the picker can land on.
type sidebarBlock struct {
	stop stop
	rect panes.Rect
}

// visualFrozenMsg is the snapshot of the window, taken off the update loop.
type visualFrozenMsg struct {
	frozen   string
	returnTo string
	targets  []pickTarget
}

// routeVisual answers every key while the window is frozen.
func (m model) routeVisual(key tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if !m.visual.isActive() {
		return m, nil, false
	}

	if m.visual.freezing {
		return m, nil, true
	}

	pressed := key.String()
	if pressed == "esc" || pressed == "q" {
		return m.thaw(), nil, true
	}

	if m.visual.mode != frozenPicker {
		return m, nil, true
	}

	at := slices.IndexFunc(m.visual.targets, func(t pickTarget) bool { return t.letter == pressed })
	if at < 0 {
		return m, nil, true
	}

	next, cmd := m.land(m.visual.targets[at])

	return next, cmd, true
}

func (m model) freeze(mode frozenMode) (tea.Model, tea.Cmd) {
	m.visual = visualState{mode: mode, freezing: true}

	var blocks []sidebarBlock
	if mode == frozenPicker {
		blocks = m.findSidebarBlocks()
	}

	sidebar := m.settings.Pane
	tmux := m.tmux
	palette := paint.Panel

	return m, func() tea.Msg {
		returnTo := tmux.GetActivePane(sidebar)
		shown := tmux.GetShownPanes(sidebar)

		screens := make(map[string]string, len(shown))
		for _, rect := range shown {
			screens[rect.Pane] = tmux.CaptureScreen(rect.Pane)
		}

		window := frozenWindow{shown: shown, screens: screens, sidebar: sidebar, palette: palette}
		if mode == frozenVisual {
			return visualFrozenMsg{frozen: window.drawVisual(), returnTo: returnTo}
		}

		targets := findPickTargets(shown, sidebar, blocks)

		return visualFrozenMsg{frozen: window.drawPicker(targets), returnTo: returnTo, targets: targets}
	}
}

// findSidebarBlocks draws the panel once with its blocks marked and reads where each one landed.
func (m model) findSidebarBlocks() []sidebarBlock {
	marked := m
	marked.markingZones = true

	zones := scanZones(marked.drawInset(marked.insideView()))
	stops := m.listStops()
	height := m.height

	var blocks []sidebarBlock

	details, hasDetails := zones[zoneDetails]
	if hasDetails && slices.Contains(stops, stopRecord) {
		blocks = append(blocks, sidebarBlock{stop: stopRecord, rect: details})
		height = details.Top
	}

	list := panes.Rect{Left: 0, Top: 0, Width: m.width, Height: height}
	if column, ok := zones[zoneColumn]; ok {
		if column.Left > m.width/2 {
			list.Width = column.Left - dividerCols
		} else {
			list.Left = column.Left + column.Width + dividerCols
			list.Width = m.width - list.Left
		}

		if slices.Contains(stops, stopBlock) {
			blocks = append(blocks, sidebarBlock{stop: stopBlock, rect: column})
		}
	}

	return append([]sidebarBlock{{stop: stopList, rect: list}}, blocks...)
}

// findPickTargets letters the sidebar's blocks and then every other pane on show, in reading order.
func findPickTargets(shown []panes.Rect, sidebar string, blocks []sidebarBlock) []pickTarget {
	var targets []pickTarget

	others := slices.Clone(shown)
	slices.SortFunc(others, func(a, b panes.Rect) int {
		if a.Left != b.Left {
			return a.Left - b.Left
		}

		return a.Top - b.Top
	})

	for _, rect := range others {
		if rect.Pane != sidebar {
			continue
		}

		for _, block := range blocks {
			clipped, ok := clipRect(block.rect, panes.Rect{Width: rect.Width, Height: rect.Height})
			if !ok {
				continue
			}

			clipped.Left += rect.Left
			clipped.Top += rect.Top
			targets = append(targets, pickTarget{rect: clipped, stop: block.stop})
		}
	}

	for _, rect := range others {
		if rect.Pane != sidebar {
			targets = append(targets, pickTarget{rect: rect, pane: rect.Pane})
		}
	}

	if len(targets) > len(pickLetters) {
		targets = targets[:len(pickLetters)]
	}

	for i := range targets {
		targets[i].letter = pickLetters[i]
	}

	return targets
}

func (m model) applyFreeze(msg visualFrozenMsg) model {
	if !m.visual.freezing {
		return m
	}

	if msg.frozen == "" {
		m.visual = visualState{}

		return m
	}

	m.visual = visualState{mode: m.visual.mode, frozen: msg.frozen, returnTo: msg.returnTo, targets: msg.targets}
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

// land thaws the window and hands the keys to a target.
func (m model) land(target pickTarget) (tea.Model, tea.Cmd) {
	m.visual = visualState{}
	m.frame.Thaw()

	if target.pane != "" {
		m.tmux.SelectPane(target.pane)
		m.keysElsewhere = target.pane != m.settings.Pane

		return m, nil
	}

	m.keysElsewhere = false

	return m.enterStop(target.stop)
}

// frozenWindow is every pane on show in the panel's window and what each was showing.
type frozenWindow struct {
	shown   []panes.Rect
	screens map[string]string
	sidebar string
	palette paint.Palette
}

// drawVisual lays each pane's screen where the pane sat, with the visual label over the right of
// the sidebar's first row.
func (w frozenWindow) drawVisual() string {
	if len(w.shown) == 0 {
		return ""
	}

	left, top, width, height := boundsOf(w.shown)

	layers := make([]*lipgloss.Layer, 0, len(w.shown)+1)
	for _, rect := range w.shown {
		layers = append(layers, lipgloss.NewLayer(fitScreen(w.screens[rect.Pane], rect)).X(rect.Left-left).Y(rect.Top-top))

		if rect.Pane == w.sidebar {
			layers = append(layers, w.labelLayer(rect, left, top, frozenVisual))
		}
	}

	return compose(width, height, layers)
}

// drawPicker lays each pane's screen dimmed where the pane sat, with a letter drawn over each
// target on a patch of blank cells.
func (w frozenWindow) drawPicker(targets []pickTarget) string {
	shown, palette := w.shown, w.palette
	if len(shown) == 0 {
		return ""
	}

	left, top, width, height := boundsOf(shown)
	dim := palette.Style(paint.ToneMuted).Faint(true)
	letter := lipgloss.NewStyle().Foreground(palette.Color(paint.ToneOther)).Bold(true)

	layers := make([]*lipgloss.Layer, 0, len(shown)+2*len(targets)+1)
	for _, rect := range shown {
		layers = append(layers, lipgloss.NewLayer(dimScreen(w.screens[rect.Pane], rect, dim)).X(rect.Left-left).Y(rect.Top-top))

		if rect.Pane == w.sidebar {
			layers = append(layers, w.labelLayer(rect, left, top, frozenPicker))
		}
	}

	for _, target := range targets {
		glyph := drawLetter(target.letter, target.rect)
		glyphWidth, glyphHeight := lipgloss.Width(glyph), strings.Count(glyph, "\n")+1
		clearWidth, clearHeight := glyphWidth+2*pickMarginX, glyphHeight+2*pickMarginY
		if clearWidth > target.rect.Width || clearHeight > target.rect.Height {
			clearWidth, clearHeight = glyphWidth, glyphHeight
		}

		x := target.rect.Left - left + (target.rect.Width-clearWidth)/2
		y := target.rect.Top - top + (target.rect.Height-clearHeight)/2
		blank := strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", clearWidth)+"\n", clearHeight), "\n")

		layers = append(layers,
			lipgloss.NewLayer(blank).X(x).Y(y).Z(1),
			lipgloss.NewLayer(letter.Render(glyph)).X(x+(clearWidth-glyphWidth)/2).Y(y+(clearHeight-glyphHeight)/2).Z(2),
		)
	}

	return compose(width, height, layers)
}

func boundsOf(shown []panes.Rect) (left, top, width, height int) {
	left, top = shown[0].Left, shown[0].Top
	right, bottom := 0, 0

	for _, rect := range shown {
		left, top = min(left, rect.Left), min(top, rect.Top)
		right, bottom = max(right, rect.Left+rect.Width), max(bottom, rect.Top+rect.Height)
	}

	return left, top, right - left, bottom - top
}

func (w frozenWindow) labelLayer(rect panes.Rect, left, top int, mode frozenMode) *lipgloss.Layer {
	label := w.palette.Visual().Render(mode.label())

	return lipgloss.NewLayer(label).
		X(rect.Left - left + rect.Width - lipgloss.Width(label) - gutter - 1).
		Y(rect.Top - top).
		Z(3)
}

func compose(width, height int, layers []*lipgloss.Layer) string {
	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewCompositor(layers...))

	return canvas.Render()
}

// dimScreen is a captured screen cut to its pane with its colours dropped for one faint style.
func dimScreen(screen string, rect panes.Rect, dim lipgloss.Style) string {
	lines := strings.Split(fitScreen(screen, rect), "\n")
	for i, line := range lines {
		lines[i] = dim.Render(ansi.Strip(line))
	}

	return strings.Join(lines, "\n")
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

// clipRect is the part of rect inside bounds, and whether any of it is.
func clipRect(rect, bounds panes.Rect) (panes.Rect, bool) {
	left, top := max(rect.Left, bounds.Left), max(rect.Top, bounds.Top)
	right := min(rect.Left+rect.Width, bounds.Left+bounds.Width)
	bottom := min(rect.Top+rect.Height, bounds.Top+bounds.Height)

	if right <= left || bottom <= top {
		return panes.Rect{}, false
	}

	return panes.Rect{Pane: rect.Pane, Left: left, Top: top, Width: right - left, Height: bottom - top}, true
}
