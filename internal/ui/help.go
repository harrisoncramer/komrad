package ui

import (
	"slices"
	"strings"

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

func binding(keys []string, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(keyLabel(keys), desc))
}

func headLabel(keys []string) string {
	if len(keys) == 0 {
		return ""
	}

	return keyLabel(keys[:1])
}

func (m *model) moveBinding() key.Binding {
	keys := slices.Concat(m.keys.Nav.Up, m.keys.Nav.Down)

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(keyLabel(m.keys.Nav.Up)+"/"+keyLabel(m.keys.Nav.Down), "move"),
	)
}

func doubledLabel(keys []string) string {
	pairs := make([]string, len(keys))
	for i, k := range keys {
		label := keyLabel([]string{k})
		pairs[i] = label + label
	}

	return strings.Join(pairs, "/")
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

	return append(full, m.messagesBinding(), binding(slices.Concat(m.keys.Sidebar.Narrow, m.keys.Sidebar.Widen), "resize"), binding([]string{leaderKey}, "leader"))
}

var keyAbbreviations = strings.NewReplacer("ctrl+", "C-", "shift+", "S-")

func keyLabel(keys []string) string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = keyAbbreviations.Replace(k)
	}

	return strings.Join(out, "/")
}

// menuChordLabel is the presses that reach an entry through the menu, spaced apart.
func (m *model) menuChordLabel(entries ...[]string) string {
	return strings.Join(xslice.Map(slices.Concat([][]string{m.keys.Menu.Open}, entries), keyLabel), " ")
}
