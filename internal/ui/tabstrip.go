package ui

import (
	"slices"
	"strconv"

	"komrad/internal/glyph"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
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

func (d tabDirection) flip() tabDirection {
	if d == tabUp {
		return tabDown
	}

	return tabUp
}

func (d tabDirection) glyph() string {
	if d == tabUp {
		return glyph.Up
	}

	return glyph.Down
}

// stops is a row of labels the tab keys walk, the focused one rendered as the cursor and the lit one
// as the choice in force.
type stops struct {
	labels []string
	focus  int
	lit    int
	mark   func(int, string) string
}

// noStop is a focus or a lit stop that stands on none of the row.
const noStop = -1

// stepStop is the stop delta along from at in a row of count, wrapping at both ends.
func stepStop(at, delta, count int) int {
	return (at + delta + count) % count
}

// stripKeys is how one row of stops answers the keys every row shares.
type stripKeys struct {
	step  func(delta int) tea.Cmd
	close func()
	// pick applies the focused stop on enter, and is nil on a row where stepping already applies it.
	pick func()
	// other takes a key the row has a use of its own for, and is nil on a row with none.
	other func(pressed string) tea.Cmd
}

// pressStrip applies a key pressed on a row of stops: tab steps along it, enter or esc lets it go, and
// the quit keys let it go and hide the panel.
func (m *model) pressStrip(pressed string, strip stripKeys) tea.Cmd {
	switch {
	case pressed == tabNextKey:
		return strip.step(1)
	case pressed == tabPrevKey:
		return strip.step(-1)
	case pressed == "enter":
		if strip.pick != nil {
			strip.pick()
		}
		strip.close()
	case pressed == "esc":
		strip.close()
	case isBound(pressed, fixedKeys().Quit):
		strip.close()

		return m.hide()
	case strip.other != nil:
		return strip.other(pressed)
	}

	return nil
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

func (s tabStrip) countStops() int {
	if s.grouped != "" {
		return len(s.options) + 1
	}

	return len(s.options)
}

// step moves the focus along the strip, wrapping at both ends, and chooses nothing.
func (s tabStrip) step(state *tabsState, delta int) {
	at := stepStop(s.focusIndex(*state), delta, s.countStops())
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

// activeTabs is the tab switcher of the screen that is up, and false on a screen that renders no strip.
func (m *model) activeTabs() (tabSwitcher, bool) {
	t, ok := m.active().(tabSwitcher)

	return t, ok
}

func (m *model) isTabsRendered() bool {
	t, ok := m.activeTabs()

	return ok && t.tabsFit(m)
}

func (m *model) isTabsFocused() bool {
	t, ok := m.activeTabs()

	return ok && t.tabsState().focused && t.tabsFit(m)
}

// listFitsTabs is whether a list has the room to render its strips, judged with the keys on the
// list.
func (m *model) listFitsTabs(params screenListParams) bool {
	probe := *m
	t, tabbed := probe.activeTabs()
	k, kinded := probe.activeKinds()
	if !tabbed && !kinded {
		return false
	}

	if probe.term.height <= 0 {
		return true
	}

	probe.details.focused, probe.helpExpanded = false, false
	var strips string
	if tabbed {
		t.tabsState().focused = false
		strips += probe.tabsLine()
	}
	if kinded {
		*k.kindsFocused(&probe) = false
		strips += probe.kindsLine()
	}

	return probe.spareLines(probe.headerLines(params.title, params.tally, strips), probe.renderHelp(probe.listHelpOf(params))) >= 1
}

func (m *model) blurTabs() {
	if t, ok := m.activeTabs(); ok {
		t.tabsState().leave()
	}
}

func (m *model) focusTabs() {
	t, ok := m.activeTabs()
	if !ok || !t.tabsFit(m) {
		return
	}

	m.blurKinds()
	t.tabsState().focused = true
	m.details.focused = false
}

func (m *model) updateTabs(key tea.KeyPressMsg) tea.Cmd {
	t, _ := m.activeTabs()
	strip, state := t.tabs(), t.tabsState()
	hovered := m.hoveredFilterKey()

	cmd := m.pressStrip(key.String(), stripKeys{
		step: func(delta int) tea.Cmd {
			strip.step(state, delta)

			return nil
		},
		pick: func() {
			if !state.onGroup {
				strip.choose(state, state.direction)
			}
		},
		close: state.leave,
		other: func(pressed string) tea.Cmd {
			switch {
			case state.onGroup && isBound(pressed, slices.Concat(m.keys.Nav.Down, m.keys.Nav.Up)):
				state.flat = !state.flat
			case isBound(pressed, m.keys.Nav.Down):
				strip.choose(state, tabDown)
			case isBound(pressed, m.keys.Nav.Up):
				strip.choose(state, tabUp)
			}

			return nil
		},
	})

	m.placeFilteredCursor(hovered)

	return cmd
}

func (m *model) clickTab(key string) tea.Cmd {
	t, ok := m.activeTabs()
	if !ok {
		return nil
	}

	strip, state := t.tabs(), t.tabsState()
	at, err := strconv.Atoi(key)
	if err != nil || at < 0 || at >= strip.countStops() {
		return nil
	}

	if m.isViewsOpen() {
		m.closeViews()
	}
	cmd := m.enterStop(stopList)
	m.focusTabs()
	hovered := m.hoveredFilterKey()

	state.onGroup = at == len(strip.options)
	if state.onGroup {
		state.focus = ""
		state.flat = !state.flat
	} else {
		direction := state.direction
		if at == strip.indexOf(*state) {
			direction = direction.flip()
		}
		state.focus = strip.options[at].id
		strip.choose(state, direction)
	}

	m.placeFilteredCursor(hovered)

	return cmd
}

func (m *model) tabsBinding() key.Binding {
	t, ok := m.activeTabs()
	if !ok {
		return key.Binding{}
	}

	return binding(m.keys.Tabs.Focus, t.tabs().legend)
}

func (m *model) tabsHelp() keyMap {
	column := stopsMoveBinding("move")
	direction := binding(slices.Concat(m.keys.Nav.Down, m.keys.Nav.Up), "sort down/up, flip")
	back := binding([]string{"enter", "esc"}, "back to the list")

	return keyMap{
		short: []key.Binding{column, direction, back, m.helpBinding()},
		full:  slices.Concat([]key.Binding{column, direction, back}, m.fullGlobalHelp()),
	}
}
