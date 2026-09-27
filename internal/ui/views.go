package ui

import (
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const komradLogo = glyph.Logo + " komrad"

// viewsBarFloor is the shortest pane the switcher is drawn in.
const viewsBarFloor = 20

// viewEntry is one view on the switcher.
type viewEntry struct {
	screen screen
	label  string
	keys   func(config.ViewsKeys) []string
}

func viewEntries() []viewEntry {
	return []viewEntry{
		{screen: screenAgents, label: "agents", keys: func(k config.ViewsKeys) []string { return k.Agents }},
		{screen: screenSearch, label: "search", keys: func(k config.ViewsKeys) []string { return k.Query }},
		{screen: screenSchedules, label: "schedules", keys: func(k config.ViewsKeys) []string { return k.Schedules }},
		{screen: screenArchive, label: "archive", keys: func(k config.ViewsKeys) []string { return k.Archive }},
		{screen: screenWorktrees, label: "worktrees", keys: func(k config.ViewsKeys) []string { return k.Worktrees }},
	}
}

// viewsPicker is whether the switcher holds the keys and the view its cursor is on.
type viewsPicker struct {
	open  bool
	focus int
}

// picksViews reports whether the switcher answers on a screen.
func (s screen) picksViews() bool {
	return s != screenReview
}

func (m model) isViewsOpen() bool {
	return m.views.open && m.screen.picksViews()
}

// viewsBar is the logo and the switcher drawn over a list.
func (m model) viewsBar() string {
	if m.height > 0 && m.height < viewsBarFloor {
		return ""
	}

	palette := paint.Panel
	muted := palette.Style(paint.ToneMuted)
	current := palette.Style(paint.ToneAccent).Bold(true)
	focused := current.Reverse(true)

	entries := viewEntries()
	parts := make([]string, len(entries))
	for i, entry := range entries {
		switch {
		case m.isViewsOpen() && i == m.views.focus:
			parts[i] = focused.Render(entry.label)
		case entry.screen == m.screen:
			parts[i] = current.Render(entry.label)
		default:
			parts[i] = muted.Render(entry.label)
		}
	}

	lines := []string{palette.Title(komradLogo), strings.Join(parts, "  ")}
	if width := m.paneWidth(); width > 0 {
		for i, line := range lines {
			lines[i] = ansi.Truncate(line, width, "…")
		}
	}

	return strings.Join(lines, "\n") + "\n\n"
}

func (m model) openViews() model {
	m.blurTabs()
	m.views = viewsPicker{open: true}

	if at := slices.IndexFunc(viewEntries(), func(e viewEntry) bool { return e.screen == m.screen }); at >= 0 {
		m.views.focus = at
	}

	return m
}

func (m model) updateViews(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	entries := viewEntries()
	k := key.String()

	switch {
	case k == tabNextKey:
		m.views.focus = (m.views.focus + 1) % len(entries)
	case k == tabPrevKey:
		m.views.focus = (m.views.focus + len(entries) - 1) % len(entries)
	case k == "enter":
		return m.pickView(entries[m.views.focus])
	case k == "esc":
		m.views = viewsPicker{}
	case isBound(k, m.quitKeys()):
		m.views = viewsPicker{}

		return m, m.hide()
	default:
		for _, entry := range entries {
			if isBound(k, entry.keys(m.keys.Views)) {
				return m.pickView(entry)
			}
		}
	}

	return m, nil
}

func (m model) pickView(entry viewEntry) (tea.Model, tea.Cmd) {
	m.views = viewsPicker{}
	if entry.screen == m.screen {
		return m, nil
	}

	var target screenTarget
	if entry.screen == screenWorktrees {
		target.agentID = m.hoveredAgentID()
	}

	cmd := m.enter(entry.screen, target)

	return m, cmd
}

// hoveredAgentID is the agent under the cursor on a screen that lists agents.
func (m model) hoveredAgentID() string {
	switch m.screen {
	case screenAgents:
		return m.agents.hoveredAgentID(m.shared())
	case screenArchive:
		return m.archive.hoveredAgentID(m.shared())
	case screenSchedules, screenSearch, screenReview, screenWorktrees, screenDocuments:
	}

	return ""
}

func (m model) viewsBinding() key.Binding {
	return binding(m.keys.Views.Pick, "switch view")
}

func (m model) viewsHelp() keyMap {
	move := key.NewBinding(key.WithKeys(tabNextKey, tabPrevKey), key.WithHelp("tab/S-tab", "move"))
	pick := binding([]string{"enter"}, "open")
	back := binding([]string{"esc"}, "back")

	bindings := append([]key.Binding{move, pick, back}, xslice.Map(viewEntries(), func(entry viewEntry) key.Binding {
		return binding(entry.keys(m.keys.Views), entry.label)
	})...)

	return keyMap{short: bindings, full: bindings}
}
