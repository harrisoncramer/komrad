package ui

import (
	"slices"
	"strings"

	"komrad/internal/ui/keys"
	"komrad/lib/xslice"

	"charm.land/bubbles/v2/key"
)

type keyMap struct {
	short []key.Binding
	full  []key.Binding
}

func byLabel(bindings []key.Binding) []key.Binding {
	sorted := slices.Clone(bindings)
	slices.SortFunc(sorted, func(a, b key.Binding) int {
		return strings.Compare(a.Help().Desc, b.Help().Desc)
	})

	return sorted
}

// refusedExpansion re-labels the legend's own key for the short form rendered in place of an
// expansion there was no room for.
func (m *model) refusedExpansion(bindings []key.Binding) []key.Binding {
	out := slices.Clone(bindings)
	for i, b := range out {
		if slices.Equal(b.Keys(), m.keys.Help.Toggle) {
			out[i] = binding(m.keys.Help.Toggle, "show help")
		}
	}

	return out
}

func binding(bound []string, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(bound...), key.WithHelp(keys.Label(bound), desc))
}

func (m *model) moveBinding() key.Binding {
	return key.NewBinding(
		key.WithKeys(slices.Concat(m.keys.Nav.Up, m.keys.Nav.Down)...),
		key.WithHelp(keys.MoveLabel(m.keys), "move"),
	)
}

func filterHelp(mode filterMode) keyMap {
	done := binding([]string{"enter"}, "keep filter")
	flip := binding([]string{"tab"}, mode.flipped().String()+" instead")
	drop := binding([]string{"esc"}, "cancel")

	return keyMap{short: []key.Binding{done, flip, drop}, full: []key.Binding{done, flip, drop}}
}

func (m *model) helpBinding() key.Binding {
	desc := "show help"
	if m.helpExpanded {
		desc = "hide help"
	}

	return binding(m.keys.Help.Toggle, desc)
}

func (m *model) menuBinding() key.Binding {
	return binding(m.keys.Menu.Open, "menu")
}

func (m *model) globalHelp() []key.Binding {
	var keys []key.Binding
	if rendersSwitcher(m.screen) {
		keys = append(keys, m.viewsBinding())
	}

	if _, ok := m.buildMenu(); ok {
		keys = append(keys, m.menuBinding())
	}

	return append(keys, m.helpBinding())
}

// fullGlobalHelp is the expanded legend's share of the keys every screen answers.
func (m *model) fullGlobalHelp() []key.Binding {
	full := m.globalHelp()
	if rendersSwitcher(m.screen) {
		full = append(full, binding(m.keys.Views.Previous, "previous view"))
	}

	return append(full, m.messagesBinding(), binding([]string{leaderKey + " " + edgeLeftKey, leaderKey + " " + edgeRightKey}, "resize"), binding([]string{leaderKey}, "leader"))
}

// menuChordLabel is the presses that reach an entry through the menu, spaced apart.
func (m *model) menuChordLabel(entries ...[]string) string {
	return strings.Join(xslice.Map(slices.Concat([][]string{m.keys.Menu.Open}, entries), keys.Label), " ")
}

// helpOf is a screen's legend built from its table.
func (m *model) helpOf(spec keys.Spec[*model]) keyMap {
	short, full := spec.Help(m, m.keys)

	return keyMap{short: short, full: full}
}
