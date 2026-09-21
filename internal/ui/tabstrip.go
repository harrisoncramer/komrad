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
	// direction is the way the arrow points when the tab is stepped onto.
	direction tabDirection
}

type tabStrip struct {
	title    string
	legend   string
	options  []tabOption
	fallback string
	// grouped is the label of the stop after the columns while the rows sit under their headings.
	grouped string
}

// flatLabel is what the stop after the columns reads while the headings are off.
const flatLabel = "flat"

type tabsState struct {
	chosen    string
	direction tabDirection
	focused   bool
	// onGroup says the focus is on the stop after the columns rather than on a column.
	onGroup bool
	// flat says the rows are one list rather than grouped under their headings.
	flat bool
}

func (s tabStrip) groupLabel(state tabsState) string {
	if state.flat {
		return flatLabel
	}

	return s.grouped
}

type tabsSpec struct {
	strip tabStrip
	state func(*model) *tabsState
	fits  func(model) bool
}

func (s tabStrip) indexOf(state tabsState) int {
	at := slices.IndexFunc(s.options, func(option tabOption) bool { return option.id == state.chosen })
	if at < 0 {
		at = max(slices.IndexFunc(s.options, func(option tabOption) bool { return option.id == s.fallback }), 0)
	}

	return at
}

func (s tabStrip) step(state *tabsState, delta int) {
	stops := len(s.options)
	if s.grouped != "" {
		stops++
	}

	at := s.indexOf(*state)
	if state.onGroup {
		at = len(s.options)
	}
	at = (at + delta + stops) % stops

	state.onGroup = at == len(s.options)
	if state.onGroup {
		return
	}
	state.chosen, state.direction = s.options[at].id, s.options[at].direction
}

func (s tabStrip) view(state tabsState, palette paint.Palette, width int) string {
	muted := palette.Style(paint.ToneMuted)
	chosen := s.indexOf(state)

	parts := make([]string, 0, len(s.options))
	for i, option := range s.options {
		if i != chosen {
			parts = append(parts, muted.Render(option.label))

			continue
		}

		text := state.direction.glyph() + " " + option.label
		if state.focused && !state.onGroup {
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

func (m model) tabsSpec() *tabsSpec {
	return m.screen.spec().tabs
}

func (m model) isTabsDrawn() bool {
	spec := m.tabsSpec()

	return spec != nil && spec.fits(m)
}

func (m model) isTabsFocused() bool {
	spec := m.tabsSpec()

	return spec != nil && spec.state(&m).focused && spec.fits(m)
}

func (m model) tabbedHeader(title, tally string) string {
	if !m.isTabsDrawn() {
		return m.talliedHeader(title, tally)
	}

	return m.headerLines(title, tally, m.tabsLine())
}

func (m model) tabsLine() string {
	spec := m.tabsSpec()

	return spec.strip.view(*spec.state(&m), paint.Panel, m.paneWidth()) + "\n"
}

func (m model) doesListFitTabs(title, tally string, help func(model) keyMap) bool {
	spec := m.tabsSpec()
	if spec == nil {
		return false
	}

	if m.height <= 0 {
		return true
	}

	m.details.focused, m.helpExpanded = false, false
	spec.state(&m).focused = false

	return m.spareLines(m.headerLines(title, tally, m.tabsLine()), m.renderHelp(help(m))) >= 1
}

func (m *model) blurTabs() {
	if spec := m.tabsSpec(); spec != nil {
		state := spec.state(m)
		state.focused, state.onGroup = false, false
	}
}

func (m model) focusTabs() model {
	spec := m.tabsSpec()
	if spec == nil || !spec.fits(m) {
		return m
	}

	spec.state(&m).focused = true
	m.details.focused = false

	return m
}

func (m model) updateTabs(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	spec := m.tabsSpec()
	state := spec.state(&m)
	k := key.String()
	hovered := m.hoveredFilterKey()

	switch {
	case k == tabNextKey:
		spec.strip.step(state, 1)
	case k == tabPrevKey:
		spec.strip.step(state, -1)
	case state.onGroup && isBound(k, concatKeys(m.keys.Nav.Down, m.keys.Nav.Up)):
		state.flat = !state.flat
	case isBound(k, m.keys.Nav.Down):
		state.direction = tabDown
	case isBound(k, m.keys.Nav.Up):
		state.direction = tabUp
	case k == "esc":
		state.focused, state.onGroup = false, false
	case isBound(k, m.quitKeys()):
		return m, m.hide()
	}

	m.placeFilteredCursor(hovered)

	return m, nil
}

func (m model) tabsBinding() key.Binding {
	spec := m.tabsSpec()
	if spec == nil {
		return key.Binding{}
	}

	return binding(m.keys.Tabs.Focus, spec.strip.legend)
}

func (m model) tabsHelp() keyMap {
	column := key.NewBinding(key.WithKeys(tabNextKey, tabPrevKey), key.WithHelp("tab/S-tab", "column"))
	direction := binding(concatKeys(m.keys.Nav.Down, m.keys.Nav.Up), "direction, grouping")
	back := binding([]string{"esc"}, "back to the list")

	return keyMap{
		short: []key.Binding{column, direction, back, m.helpBinding()},
		full:  slices.Concat([]key.Binding{column, direction, back}, m.globalHelp()),
	}
}
