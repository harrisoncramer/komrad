package ui

import (
	"slices"

	"komrad/internal/ui/paint"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// activeKinds is the kinded facet of the screen that is up, and false on a screen listing one kind.
func (m *model) activeKinds() (kinded, bool) {
	k, ok := m.active().(kinded)

	return k, ok
}

func (m model) isKindsDrawn() bool {
	k, ok := m.activeKinds()

	return ok && k.kindsFit(m.shared())
}

func (m model) isKindsFocused() bool {
	k, ok := m.activeKinds()

	return ok && *k.kindsFocused() && k.kindsFit(m.shared())
}

func (m model) kindsLine() string {
	k, _ := m.activeKinds()
	row := k.kinds()
	if *k.kindsFocused() {
		row.focus, row.lit = row.lit, noStop
	}

	line := paint.Panel.Style(paint.ToneMuted).Render(headLabel(m.keys.Kinds.Focus)) + "   " + row.view(m.focusPalette())
	if width := m.paneWidth(); width > 0 {
		line = ansi.Truncate(line, width, "…")
	}

	return line + "\n"
}

func (m *model) blurKinds() {
	if k, ok := m.activeKinds(); ok {
		*k.kindsFocused() = false
	}
}

func (m model) focusKinds() model {
	k, ok := m.activeKinds()
	if !ok || !k.kindsFit(m.shared()) || !k.kindsSwitch(m.shared()) {
		return m
	}

	m.blurTabs()
	*k.kindsFocused() = true
	m.details.focused = false

	return m
}

func (m model) updateKinds(press tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k, _ := m.activeKinds()

	cmd := m.pressStrip(press.String(), stripKeys{
		step:  func(delta int) tea.Cmd { return k.stepKind(&m, m.shared(), delta) },
		close: func() { *k.kindsFocused() = false },
	})

	return m, cmd
}

func (m model) kindsBinding() key.Binding {
	if k, ok := m.activeKinds(); !ok || !k.kindsSwitch(m.shared()) {
		return key.Binding{}
	}

	return binding(m.keys.Kinds.Focus, "kind")
}

func (m model) kindsHelp() keyMap {
	kind := stopsMoveBinding("switch")
	back := binding([]string{"esc"}, "back to the list")

	return keyMap{
		short: []key.Binding{kind, back, m.helpBinding()},
		full:  slices.Concat([]key.Binding{kind, back}, m.fullGlobalHelp()),
	}
}
