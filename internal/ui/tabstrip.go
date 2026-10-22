package ui

import (
	"slices"
	"strconv"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/list"
	"komrad/internal/ui/tabs"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

const (
	tabNextKey = "tab"
	tabPrevKey = "shift+tab"
)

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
	case keys.IsBound(pressed, fixedKeys().Quit):
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

// activeTabs is the tab switcher of the screen that is up, and false on a screen that renders no strip.
func (m *model) activeTabs() (tabSwitcher, bool) {
	t, ok := m.active().(tabSwitcher)

	return t, ok
}

func (m *model) isTabsRendered() bool {
	_, ok := m.activeTabs()

	return ok && m.tabsFit()
}

func (m *model) isTabsFocused() bool {
	t, ok := m.activeTabs()

	return ok && t.tabsState().Focused && m.tabsFit()
}

func (m *model) tabsFit() bool {
	_, fits := m.listFitsTabs()

	return fits
}

// listFitsTabs is the active list's params and whether the list has the room to render its strips,
// judged with the keys on the list.
func (m *model) listFitsTabs() (list.Params, bool) {
	probe := *m
	t, tabbed := probe.activeTabs()
	k, kinded := probe.activeKinds()
	if !tabbed && !kinded {
		return list.Params{}, false
	}

	probe.details.focused, probe.helpExpanded = false, false
	if tabbed {
		t.tabsState().Focused = false
	}
	if kinded {
		*k.kindsFocused(&probe) = false
	}

	l, ok := m.active().(listRenderer)
	if !ok {
		return list.Params{}, false
	}
	params := l.listParams(&probe, probe.inputs())

	if probe.term.height <= 0 {
		return params, true
	}

	var strips string
	if tabbed {
		strips += probe.tabsLine()
	}
	if kinded {
		strips += probe.kindsLine()
	}

	return params, probe.spareLines(probe.headerLines(params.Title, params.Tally, strips), probe.Help(probe.listHelp(params.Help))) >= 1
}

func (m *model) blurTabs() {
	if t, ok := m.activeTabs(); ok {
		t.tabsState().Leave()
	}
}

func (m *model) focusTabs() {
	t, ok := m.activeTabs()
	if !ok || !m.tabsFit() {
		return
	}

	m.blurKinds()
	t.tabsState().Focused = true
	m.details.focused = false
}

func (m *model) updateTabs(key tea.KeyPressMsg) tea.Cmd {
	t, _ := m.activeTabs()
	strip, state := t.tabs(), t.tabsState()
	hovered := m.hoveredFilterKey()

	cmd := m.pressStrip(key.String(), stripKeys{
		step: func(delta int) tea.Cmd {
			strip.Step(state, delta)

			return nil
		},
		pick: func() {
			if !state.OnGroup {
				strip.Choose(state, state.Direction)
			}
		},
		close: state.Leave,
		other: func(pressed string) tea.Cmd {
			switch {
			case state.OnGroup && keys.IsBound(pressed, slices.Concat(m.keys.Nav.Down, m.keys.Nav.Up)):
				state.Flat = !state.Flat
			case keys.IsBound(pressed, m.keys.Nav.Down):
				strip.Choose(state, tabs.Down)
			case keys.IsBound(pressed, m.keys.Nav.Up):
				strip.Choose(state, tabs.Up)
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
	if err != nil || at < 0 || at >= strip.CountStops() {
		return nil
	}

	if m.isViewsOpen() {
		m.closeViews()
	}
	cmd := m.enterStop(stopList)
	m.focusTabs()
	hovered := m.hoveredFilterKey()

	state.OnGroup = at == len(strip.Options)
	if state.OnGroup {
		state.Focus = ""
		state.Flat = !state.Flat
	} else {
		direction := state.Direction
		if at == strip.IndexOf(*state) {
			direction = direction.Flip()
		}
		state.Focus = strip.Options[at].ID
		strip.Choose(state, direction)
	}

	m.placeFilteredCursor(hovered)

	return cmd
}

func (m *model) tabsBinding() key.Binding {
	t, ok := m.activeTabs()
	if !ok {
		return key.Binding{}
	}

	return binding(m.keys.Tabs.Focus, t.tabs().Legend)
}

func (m *model) tabsHelp() legend.Help {
	column := stopsMoveBinding("move")
	direction := binding(slices.Concat(m.keys.Nav.Down, m.keys.Nav.Up), "sort down/up, flip")
	back := binding([]string{"enter", "esc"}, "back to the list")

	return legend.Help{
		Short: []key.Binding{column, direction, back, m.helpBinding()},
		Full:  slices.Concat([]key.Binding{column, direction, back}, m.fullGlobalHelp()),
	}
}
