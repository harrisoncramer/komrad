package ui

import (
	"slices"
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	tabNextKey = "tab"
	tabPrevKey = "shift+tab"
)

type tabDirection int

const (
	tabDown tabDirection = iota
	tabUp
)

func (d tabDirection) glyph() string {
	if d == tabUp {
		return "↑"
	}

	return "↓"
}

// stops is a row of labels the tab keys walk, the focused one drawn as the cursor and the lit one
// as the choice in force.
type stops struct {
	labels []string
	// marks is the letter underlined in each label, the key that jumps to it.
	marks []string
	focus int
	lit   int
}

// noStop is a focus or a lit stop that stands on none of the row.
const noStop = -1

func (s stops) parts(palette paint.Palette) []string {
	muted := palette.Style(paint.ToneMuted)
	lit := palette.Style(paint.ToneAccent)

	parts := make([]string, len(s.labels))
	for i := range s.labels {
		style := muted
		switch i {
		case s.focus:
			style = palette.Cursor()
		case s.lit:
			style = lit
		}
		parts[i] = s.render(style, i)
	}

	return parts
}

// render draws one label in its style, its mark underlined where the label holds it.
func (s stops) render(style lipgloss.Style, i int) string {
	label := s.labels[i]
	if i >= len(s.marks) || s.marks[i] == "" {
		return style.Render(label)
	}

	at := strings.Index(label, s.marks[i])
	if at < 0 {
		return style.Render(label)
	}

	end := at + len(s.marks[i])

	return style.Render(label[:at]) + style.Underline(true).Render(label[at:end]) + style.Render(label[end:])
}

func (s stops) view(palette paint.Palette) string {
	return strings.Join(s.parts(palette), "  ")
}

// stepStop is the stop delta along from at in a row of count, wrapping at both ends.
func stepStop(at, delta, count int) int {
	return (at + delta + count) % count
}

func stopsMoveBinding(desc string) key.Binding {
	return key.NewBinding(key.WithKeys(tabNextKey, tabPrevKey), key.WithHelp("tab/S-tab", desc))
}

type tabOption struct {
	id    string
	label string
}

type tabStrip struct {
	title    string
	legend   string
	options  []tabOption
	fallback string
	// grouped is the stop's label while the rows sit under their headings.
	grouped string
}

const flatLabel = "flat"

type tabsState struct {
	chosen    string
	direction tabDirection
	focused   bool
	// focus is the column the keys act on, the chosen one while empty.
	focus string
	// onGroup says the focus is on the stop after the columns.
	onGroup bool
	// flat says the headings are off.
	flat bool
}

// leave takes the focus off the strip and back to the chosen column.
func (s *tabsState) leave() {
	s.focused, s.focus, s.onGroup = false, "", false
}

func (s tabStrip) groupLabel(state tabsState) string {
	if state.flat {
		return flatLabel
	}

	return s.grouped
}

func (s tabStrip) indexOf(state tabsState) int {
	at := slices.IndexFunc(s.options, func(option tabOption) bool { return option.id == state.chosen })
	if at < 0 {
		at = max(slices.IndexFunc(s.options, func(option tabOption) bool { return option.id == s.fallback }), 0)
	}

	return at
}

// focusIndex is the stop the focus stands on, the group stop being one past the columns.
func (s tabStrip) focusIndex(state tabsState) int {
	if state.onGroup {
		return len(s.options)
	}

	at := slices.IndexFunc(s.options, func(option tabOption) bool { return option.id == state.focus })
	if at < 0 {
		return s.indexOf(state)
	}

	return at
}

// step moves the focus along the strip, wrapping at both ends, and chooses nothing.
func (s tabStrip) step(state *tabsState, delta int) {
	stops := len(s.options)
	if s.grouped != "" {
		stops++
	}

	at := stepStop(s.focusIndex(*state), delta, stops)
	state.onGroup = at == len(s.options)
	if state.onGroup {
		state.focus = ""

		return
	}
	state.focus = s.options[at].id
}

// choose sorts by the focused column in the given direction.
func (s tabStrip) choose(state *tabsState, direction tabDirection) {
	state.chosen, state.direction = s.options[s.focusIndex(*state)].id, direction
	state.focus = ""
}

func (s tabStrip) view(state tabsState, palette paint.Palette, width int) string {
	muted := palette.Style(paint.ToneMuted)
	chosen := s.indexOf(state)

	labels := make([]string, len(s.options), len(s.options)+1)
	for i, option := range s.options {
		labels[i] = option.label
		if i == chosen {
			labels[i] = state.direction.glyph() + " " + option.label
		}
	}
	if s.grouped != "" {
		labels = append(labels, s.groupLabel(state))
	}

	row := stops{labels: labels, focus: noStop, lit: noStop}
	if state.focused {
		row.focus = s.focusIndex(state)
	}
	parts := row.parts(palette)

	line := muted.Render(s.title) + "   " + strings.Join(parts[:len(s.options)], "  ")
	if s.grouped != "" {
		line += "  " + muted.Render(glyph.Separator) + "  " + parts[len(s.options)]
	}
	if width > 0 {
		line = ansi.Truncate(line, width, "…")
	}

	return line
}

// activeTabs is the tabbed facet of the screen that is up, and false on a screen that draws no strip.
func (m *model) activeTabs() (tabbed, bool) {
	t, ok := m.active().(tabbed)

	return t, ok
}

func (m model) isTabsDrawn() bool {
	t, ok := m.activeTabs()

	return ok && t.tabsFit(m.shared())
}

func (m model) isTabsFocused() bool {
	t, ok := m.activeTabs()

	return ok && t.tabsState().focused && t.tabsFit(m.shared())
}

func (m model) tabbedHeader(title, tally string) string {
	var strips string
	if m.isTabsDrawn() {
		strips += m.tabsLine()
	}
	if m.isKindsDrawn() {
		strips += m.kindsLine()
	}
	if strips == "" {
		return m.talliedHeader(title, tally)
	}

	return m.headerLines(title, tally, strips)
}

func (m model) tabsLine() string {
	t, _ := m.activeTabs()

	return t.tabs().view(*t.tabsState(), paint.Panel, m.paneWidth()) + "\n"
}

// listFitsTabs is whether a list has the room to draw its strips, judged with the keys on the
// list.
func (m model) listFitsTabs(params screenListParams) bool {
	t, tabbed := m.activeTabs()
	k, kinded := m.activeKinds()
	if !tabbed && !kinded {
		return false
	}

	if m.height <= 0 {
		return true
	}

	m.details.focused, m.helpExpanded = false, false
	var strips string
	if tabbed {
		t.tabsState().focused = false
		strips += m.tabsLine()
	}
	if kinded {
		*k.kindsFocused() = false
		strips += m.kindsLine()
	}

	return m.spareLines(m.headerLines(params.title, params.tally, strips), m.renderHelp(m.listHelpOf(params))) >= 1
}

func (m *model) blurTabs() {
	if t, ok := m.activeTabs(); ok {
		t.tabsState().leave()
	}
}

func (m model) focusTabs() model {
	t, ok := m.activeTabs()
	if !ok || !t.tabsFit(m.shared()) {
		return m
	}

	m.blurKinds()
	t.tabsState().focused = true
	m.details.focused = false

	return m
}

func (m model) updateTabs(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	t, _ := m.activeTabs()
	strip, state := t.tabs(), t.tabsState()
	k := key.String()
	hovered := m.hoveredFilterKey()

	switch {
	case k == tabNextKey:
		strip.step(state, 1)
	case k == tabPrevKey:
		strip.step(state, -1)
	case state.onGroup && isBound(k, slices.Concat(m.keys.Nav.Down, m.keys.Nav.Up)):
		state.flat = !state.flat
	case isBound(k, m.keys.Nav.Down):
		strip.choose(state, tabDown)
	case isBound(k, m.keys.Nav.Up):
		strip.choose(state, tabUp)
	case k == "esc":
		state.leave()
	case isBound(k, m.quitKeys()):
		return m, m.hide()
	}

	m.placeFilteredCursor(hovered)

	return m, nil
}

func (m model) tabsBinding() key.Binding {
	t, ok := m.activeTabs()
	if !ok {
		return key.Binding{}
	}

	return binding(m.keys.Tabs.Focus, t.tabs().legend)
}

func (m model) tabsHelp() keyMap {
	column := stopsMoveBinding("move")
	direction := binding(slices.Concat(m.keys.Nav.Down, m.keys.Nav.Up), "sort down/up, flip")
	back := binding([]string{"esc"}, "back to the list")

	return keyMap{
		short: []key.Binding{column, direction, back, m.helpBinding()},
		full:  slices.Concat([]key.Binding{column, direction, back}, m.fullGlobalHelp()),
	}
}
