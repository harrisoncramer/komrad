package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// globalArm is one arm the panel offers a press to ahead of the screen's own table.
type globalArm struct {
	describe string
	keys     func(config.Keybindings) []string
	// guarded says the arm answers only some of the presses of its keys that reach it.
	guarded bool
	// on is whether a screen renders what the arm answers for, nil on an arm every screen has.
	on   func(screen.ID) bool
	when func(*model) bool
	// every says the arm takes every press while it answers, not only those of its keys.
	every bool
	run   func(*model, tea.KeyPressMsg) tea.Cmd
}

// globalTable is the arms routeGlobalKey offers a press to, in the order it offers them.
func globalTable() []globalArm {
	return []globalArm{
		{
			describe: "the view switcher",
			keys:     func(k config.Keybindings) []string { return slices.Concat(k.Views.Next, k.Views.Previous) },
			on:       rendersSwitcher,
			when:     func(m *model) bool { return !m.isTabsFocused() && !m.isKindsFocused() },
			run:      func(m *model, key tea.KeyPressMsg) tea.Cmd { return m.openViews(m.viewsDelta(key.String())) },
		},
		{
			describe: "the help toggle",
			keys:     func(k config.Keybindings) []string { return k.Help.Toggle },
			run: func(m *model, _ tea.KeyPressMsg) tea.Cmd {
				m.helpExpanded = !m.helpExpanded

				return nil
			},
		},
		{
			describe: "the tabs focus",
			keys:     func(k config.Keybindings) []string { return k.Tabs.Focus },
			on:       screenIs[tabSwitcher],
			when:     func(m *model) bool { return !m.isTabsFocused() },
			run: func(m *model, _ tea.KeyPressMsg) tea.Cmd {
				m.focusTabs()

				return nil
			},
		},
		{
			describe: "the kinds focus",
			keys:     func(k config.Keybindings) []string { return k.Kinds.Focus },
			on:       screenIs[kindSwitcher],
			when: func(m *model) bool {
				return !m.isTabsFocused() && m.isKindsRendered() && !m.isKindsFocused()
			},
			run: func(m *model, _ tea.KeyPressMsg) tea.Cmd {
				m.focusKinds()

				return nil
			},
		},
		{
			describe: "the menu",
			keys:     func(k config.Keybindings) []string { return k.Menu.Open },
			when: func(m *model) bool {
				if m.isTabsFocused() || m.isKindsFocused() {
					return false
				}

				_, ok := m.buildMenu()

				return ok
			},
			run: func(m *model, _ tea.KeyPressMsg) tea.Cmd {
				band, _ := m.buildMenu()

				return m.apply(menu.Open{Menu: band})
			},
		},
		{
			describe: "the tabs",
			every:    true,
			when:     func(m *model) bool { return m.isTabsFocused() },
			run:      (*model).updateTabs,
		},
		{
			describe: "the kinds",
			every:    true,
			when:     func(m *model) bool { return m.isKindsFocused() },
			run:      (*model).updateKinds,
		},
		{
			describe: "the details copy",
			keys:     func(k config.Keybindings) []string { return k.Details.Copy },
			guarded:  true,
			on:       screenIs[recordRenderer],
			every:    true,
			when:     func(m *model) bool { return m.isDetailsFocused(m.inputs()) },
			run:      (*model).updateDetails,
		},
		{
			describe: "the document",
			every:    true,
			when:     func(m *model) bool { return m.inputs().Room.DocumentReader.HoldsKeys },
			run:      (*model).updateDoc,
		},
	}
}

// routeGlobalKey answers the keys the panel owns whatever screen is up, reporting whether it
// took the press so the screen underneath gets whatever it left alone.
func (m *model) routeGlobalKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	for _, arm := range globalTable() {
		if !arm.every && !keys.IsBound(key.String(), arm.keys(m.keys)) {
			continue
		}

		if arm.on != nil && !arm.on(m.screen) {
			continue
		}

		if arm.when != nil && !arm.when(m) {
			continue
		}

		return arm.run(m, key), true
	}

	return nil, false
}

// globalArms are the keys the panel answers on one screen ahead of that screen's own table, in
// the order it reads them.
func globalArms(s screen.ID, bindings config.Keybindings) []keys.Arm {
	arms := []keys.Arm{
		{Describe: "the leader", Keys: []string{leaderKey}},
		{Describe: "the key that reads the messages", Keys: bindings.Messages.Open},
	}

	for _, arm := range globalTable() {
		if arm.keys == nil || (arm.on != nil && !arm.on(s)) {
			continue
		}

		arms = append(arms, keys.Arm{Describe: arm.describe, Keys: arm.keys(bindings), IsGuarded: arm.guarded})
	}

	return arms
}

// screenIs reports whether the screen behind the enum is a T.
func screenIs[T any](s screen.ID) bool {
	var zero model
	_, ok := zero.screenOf(s).(T)

	return ok
}
