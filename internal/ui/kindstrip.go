package ui

import (
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// activeKinds is the kind switcher of the screen that is up, and false on a screen listing one kind.
func (m *model) activeKinds() (kindSwitcher, bool) {
	k, ok := m.active().(kindSwitcher)

	return k, ok
}

func (m *model) isKindsRendered() bool {
	k, ok := m.activeKinds()

	return ok && k.kindsFit(m, m.inputs())
}

func (m *model) isKindsFocused() bool {
	k, ok := m.activeKinds()

	return ok && *k.kindsFocused(m) && k.kindsFit(m, m.inputs())
}

func (m *model) blurKinds() {
	if k, ok := m.activeKinds(); ok {
		*k.kindsFocused(m) = false
	}
}

func (m *model) focusKinds() {
	k, ok := m.activeKinds()
	if !ok || !k.kindsFit(m, m.inputs()) || !k.kindsSwitch(m) {
		return
	}

	m.blurTabs()
	*k.kindsFocused(m) = true
	m.details.focused = false
}

func (m *model) clickKind(label string) tea.Cmd {
	k, ok := m.activeKinds()
	if !ok || !k.kindsSwitch(m) {
		return nil
	}

	row := k.kinds(m)
	at := slices.Index(row.labels, label)
	if at == noStop {
		return nil
	}

	if m.isViewsOpen() {
		m.closeViews()
	}
	cmd := m.enterStop(stopList)
	if at == row.lit {
		return cmd
	}

	return tea.Batch(cmd, m.apply(k.stepKind(m, m.inputs(), at-row.lit)))
}

func (m *model) updateKinds(press tea.KeyPressMsg) tea.Cmd {
	k, _ := m.activeKinds()

	cmd := m.pressStrip(press.String(), stripKeys{
		step:  func(delta int) tea.Cmd { return m.apply(k.stepKind(m, m.inputs(), delta)) },
		close: func() { *k.kindsFocused(m) = false },
	})

	return cmd
}

func (m *model) kindsBinding() key.Binding {
	if k, ok := m.activeKinds(); !ok || !k.kindsSwitch(m) {
		return key.Binding{}
	}

	return binding(m.keys.Kinds.Focus, "kind")
}

func (m *model) kindsHelp() keyMap {
	kind := stopsMoveBinding("switch")
	back := binding([]string{"enter", "esc"}, "back to the list")

	return keyMap{
		short: []key.Binding{kind, back, m.helpBinding()},
		full:  slices.Concat([]key.Binding{kind, back}, m.fullGlobalHelp()),
	}
}
