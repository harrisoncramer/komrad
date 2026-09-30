package ui

import (
	"slices"
	"strings"

	"komrad/internal/panes"
	"komrad/internal/ui/paint"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// tabStop is how far apart tmux sets a pane's tab stops.
const tabStop = 8

// pickerLabel is the label drawn over the sidebar while the picker holds the window.
const pickerLabel = " PANES "

// pickLetters are the keys the picker hands out, one per target in reading order.
var pickLetters = []string{"d", "f", "j", "k", "l", "s", "a"}

// pickMargin is the blank cells cleared around a letter, across and down.
const (
	pickMarginX = 3
	pickMarginY = 1
)

// pickerState is the window as the pane picker froze it.
type pickerState struct {
	// freezing is whether a snapshot is being taken.
	freezing bool
	// frozen is the window drawn as it stood, empty while nothing is frozen.
	frozen string
	// returnTo is the pane that held the keys before the window froze.
	returnTo string
	targets  []pickTarget
}

func (p pickerState) isActive() bool {
	return p.freezing || p.frozen != ""
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

// pickerFrozenMsg is the snapshot of the window, taken off the update loop.
type pickerFrozenMsg struct {
	frozen   string
	returnTo string
	targets  []pickTarget
}

// routePicker answers every key while the picker holds the window.
func (m model) routePicker(key tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if !m.picker.isActive() {
		return m, nil, false
	}

	if m.picker.freezing {
		return m, nil, true
	}

	pressed := key.String()
	if pressed == "esc" || pressed == "q" {
		return m.thaw(), nil, true
	}

	at := slices.IndexFunc(m.picker.targets, func(t pickTarget) bool { return t.letter == pressed })
	if at < 0 {
		return m, nil, true
	}

	next, cmd := m.land(m.picker.targets[at])

	return next, cmd, true
}

func (m model) freeze() (tea.Model, tea.Cmd) {
	m.picker = pickerState{freezing: true}

	blocks := m.findSidebarBlocks()
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
		targets := findPickTargets(shown, sidebar, blocks)

		return pickerFrozenMsg{frozen: window.drawPicker(targets), returnTo: returnTo, targets: targets}
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
	column, hasColumn := zones[zoneColumn]
	if hasColumn {
		if !m.frame.IsSidebarOnRight() {
			list.Width = column.Left - dividerCols
		} else {
			list.Left = column.Left + column.Width + dividerCols
			list.Width = m.width - list.Left
		}
	}

	if marked, ok := zones[zoneList]; ok {
		list = marked
	}

	block, hasBlock := zones[zoneBlock]
	if !hasBlock {
		block, hasBlock = column, hasColumn
	}

	if hasBlock && slices.Contains(stops, stopBlock) {
		blocks = append(blocks, sidebarBlock{stop: stopBlock, rect: block})
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

func (m model) applyFreeze(msg pickerFrozenMsg) model {
	if !m.picker.freezing {
		return m
	}

	if msg.frozen == "" {
		m.picker = pickerState{}

		return m
	}

	m.picker = pickerState{frozen: msg.frozen, returnTo: msg.returnTo, targets: msg.targets}
	m.frame.Freeze()

	return m
}

func (m model) thaw() model {
	returnTo := m.picker.returnTo
	m.picker = pickerState{}

	m.frame.Thaw()

	if returnTo != "" && returnTo != m.settings.Pane {
		m.tmux.SelectPane(returnTo)
	}

	return m
}

// land thaws the window and hands the keys to a target.
func (m model) land(target pickTarget) (tea.Model, tea.Cmd) {
	m.picker = pickerState{}
	m.frame.Thaw()

	if target.pane != "" {
		m.tmux.SelectPane(target.pane)
		m.keysElsewhere = target.pane != m.settings.Pane

		return m, nil
	}

	m.keysElsewhere = false
	if target.stop != stopList {
		m.filter.typing = false
	}

	return m.enterStop(target.stop)
}

// frozenWindow is every pane on show in the panel's window and what each was showing.
type frozenWindow struct {
	shown   []panes.Rect
	screens map[string]string
	sidebar string
	palette paint.Palette
}

// drawPicker lays each pane's screen where the pane sat, with a letter drawn over each
// target on a patch of blank cells.
func (w frozenWindow) drawPicker(targets []pickTarget) string {
	shown, palette := w.shown, w.palette
	if len(shown) == 0 {
		return ""
	}

	left, top, width, height := boundsOf(shown)
	letter := lipgloss.NewStyle().Foreground(palette.Color(paint.ToneOther)).Bold(true)

	layers := make([]*lipgloss.Layer, 0, len(shown)+2*len(targets)+1)
	for _, rect := range shown {
		layers = append(layers, lipgloss.NewLayer(fitScreen(w.screens[rect.Pane], rect)).X(rect.Left-left).Y(rect.Top-top))

		if rect.Pane == w.sidebar {
			layers = append(layers, w.labelLayer(rect, left, top))
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

func (w frozenWindow) labelLayer(rect panes.Rect, left, top int) *lipgloss.Layer {
	label := w.palette.Visual().Render(pickerLabel)

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
