package ui

import (
	"slices"
	"strings"

	"weave/internal/ui/paint"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
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

	at := (s.focusIndex(*state) + delta + stops) % stops
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
	chosen, focus := s.indexOf(state), s.focusIndex(state)

	parts := make([]string, 0, len(s.options))
	for i, option := range s.options {
		text := option.label
		if i == chosen {
			text = state.direction.glyph() + " " + text
		}
		if state.focused && i == focus {
			text = palette.Cursor().Render(text)
		} else {
			text = muted.Render(text)
		}
		parts = append(parts, text)
	}

	line := " " + muted.Render(s.title) + "   " + strings.Join(parts, "  ")
	if s.grouped != "" {
		group := muted.Render(s.groupLabel(state))
		if state.focused && state.onGroup {
			group = palette.Cursor().Render(s.groupLabel(state))
		}
		line += "  " + muted.Render("·") + "  " + group
	}
	if width > 0 {
		line = ansi.Truncate(line, width, "…")
	}

	return line
}

// activeTabs is the strip the screen that is up draws, and false on a screen that draws none.
func (m model) activeTabs() (tabbed, bool) {
	t, ok := m.active().(tabbed)

	return t, ok
}

func (m model) isTabsDrawn() bool {
	t, ok := m.activeTabs()

	return ok && t.tabsFit(m)
}

func (m model) isTabsFocused() bool {
	t, ok := m.activeTabs()

	return ok && t.tabsState(&m).focused && t.tabsFit(m)
}

func (m model) tabbedHeader(title, tally string) string {
	if !m.isTabsDrawn() {
		return m.talliedHeader(title, tally)
	}

	return m.headerLines(title, tally, m.tabsLine())
}

func (m model) tabsLine() string {
	t, _ := m.activeTabs()

	return t.tabs().view(*t.tabsState(&m), paint.Panel, m.paneWidth()) + "\n"
}

func (m model) doesListFitTabs(title, tally string, help func(model) keyMap) bool {
	t, ok := m.activeTabs()
	if !ok {
		return false
	}

	if m.height <= 0 {
		return true
	}

	m.details.focused, m.helpExpanded = false, false
	t.tabsState(&m).focused = false

	return m.spareLines(m.headerLines(title, tally, m.tabsLine()), m.renderHelp(help(m))) >= 1
}

func (m *model) blurTabs() {
	if t, ok := m.activeTabs(); ok {
		t.tabsState(m).leave()
	}
}

func (m model) focusTabs() model {
	t, ok := m.activeTabs()
	if !ok || !t.tabsFit(m) {
		return m
	}

	t.tabsState(&m).focused = true
	m.details.focused = false

	return m
}

func (m model) updateTabs(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	t, _ := m.activeTabs()
	strip, state := t.tabs(), t.tabsState(&m)
	k := key.String()
	hovered := m.hoveredFilterKey()

	switch {
	case k == tabNextKey:
		strip.step(state, 1)
	case k == tabPrevKey:
		strip.step(state, -1)
	case state.onGroup && isBound(k, concatKeys(m.keys.Nav.Down, m.keys.Nav.Up)):
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
	column := key.NewBinding(key.WithKeys(tabNextKey, tabPrevKey), key.WithHelp("tab/S-tab", "move"))
	direction := binding(concatKeys(m.keys.Nav.Down, m.keys.Nav.Up), "sort down/up, flip")
	back := binding([]string{"esc"}, "back to the list")

	return keyMap{
		short: []key.Binding{column, direction, back, m.helpBinding()},
		full:  slices.Concat([]key.Binding{column, direction, back}, m.globalHelp()),
	}
}
