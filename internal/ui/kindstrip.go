package ui

import (
	"slices"

	"komrad/internal/ui/legend"
	"komrad/internal/ui/tabs"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// activeKinds is the kind switcher of the screen that is up, and false on a screen listing one kind.
func (m *model) activeKinds() (kindSwitcher, bool) {
	k, ok := m.active().(kindSwitcher)

	return k, ok
}

func (m *model) isKindsRendered() bool {
	_, ok := m.activeKinds()

	return ok && m.kindsFit()
}

func (m *model) isKindsFocused() bool {
	k, ok := m.activeKinds()

	return ok && *k.kindsFocused(m) && m.kindsFit()
}

func (m *model) kindsFit() bool {
	params, fits := m.listFitsTabs()

	return fits && !params.HidesKinds
}

func (m *model) blurKinds() {
	if k, ok := m.activeKinds(); ok {
		*k.kindsFocused(m) = false
	}
}

func (m *model) focusKinds() {
	k, ok := m.activeKinds()
	if !ok || !m.kindsFit() || !k.kindsSwitch() {
		return
	}

	m.blurTabs()
	*k.kindsFocused(m) = true
	m.details.focused = false
}

func (m *model) clickKind(label string) tea.Cmd {
	k, ok := m.activeKinds()
	if !ok || !k.kindsSwitch() {
		return nil
	}

	row := k.kinds(m)
	at := slices.Index(row.Labels, label)
	if at == tabs.NoStop {
		return nil
	}

	if m.isViewsOpen() {
		m.closeViews()
	}
	cmd := m.enterStop(stopList)
	if at == row.Lit {
		return cmd
	}

	return tea.Batch(cmd, m.apply(k.stepKind(m, m.inputs(), at-row.Lit)))
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
	if k, ok := m.activeKinds(); !ok || !k.kindsSwitch() {
		return key.Binding{}
	}

	return binding(m.keys.Kinds.Focus, "kind")
}

func (m *model) kindsHelp() legend.Help {
	kind := stopsMoveBinding("switch")
	back := binding([]string{"enter", "esc"}, "back to the list")

	return legend.Help{
		Short: []key.Binding{kind, back, m.helpBinding()},
		Full:  slices.Concat([]key.Binding{kind, back}, m.fullGlobalHelp()),
	}
}
