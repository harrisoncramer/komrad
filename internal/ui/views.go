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

const komradName = "komrad"

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
		{screen: screenSearch, label: "query", keys: func(k config.ViewsKeys) []string { return k.Query }},
		{screen: screenSchedules, label: "schedules", keys: func(k config.ViewsKeys) []string { return k.Schedules }},
		{screen: screenArchive, label: "archive", keys: func(k config.ViewsKeys) []string { return k.Archive }},
		{screen: screenWorktrees, label: "worktrees", keys: func(k config.ViewsKeys) []string { return k.Worktrees }},
	}
}

// picksViews reports whether the switcher answers on a screen.
func (s screen) picksViews() bool {
	return s != screenReview
}

func (m model) isViewsOpen() bool {
	return m.viewsOpen && m.screen.picksViews()
}

// viewsBar is the logo and the switcher drawn over a list, and under them the trail down to a
// page opened from a view, which is drawn however short the pane.
func (m model) viewsBar() string {
	var lines []string
	if m.height <= 0 || m.height >= viewsBarFloor {
		lines = m.switcherLines()
	}
	if trail, _, ok := m.activeCrumbs(); ok {
		lines = append(lines, trail.view(paint.Panel, m.paneWidth()))
	}
	if len(lines) == 0 {
		return ""
	}

	return strings.Join(lines, "\n") + "\n\n"
}

// switcherLines is the logo and the row of views.
func (m model) switcherLines() []string {
	entries := viewEntries()
	row := stops{
		labels: xslice.Map(entries, func(entry viewEntry) string { return entry.label }),
		marks:  xslice.Map(entries, func(entry viewEntry) string { return headLabel(entry.keys(m.keys.Views)) }),
		focus:  noStop,
		lit:    noStop,
	}
	if m.isViewsOpen() {
		row.focus = m.viewIndex()
	} else {
		row.lit = m.viewIndex()
	}

	lines := []string{paint.Panel.Title(m.glyphs.Spell(glyph.Logo) + " " + komradName), row.view(paint.Panel)}
	if width := m.paneWidth(); width > 0 {
		for i, line := range lines {
			lines[i] = ansi.Truncate(line, width, "…")
		}
	}

	return lines
}

// viewIndex is where the screen that is up sits on the switcher, and noStop when it is not there.
func (m model) viewIndex() int {
	_, at, _ := m.activeCrumbs()

	return slices.IndexFunc(viewEntries(), func(entry viewEntry) bool { return entry.screen == at })
}

func (m model) openViews() model {
	m.blurTabs()
	m.blurKinds()
	m.viewsOpen = true

	return m
}

func (m model) updateViews(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	entries := viewEntries()
	k := key.String()

	switch {
	case k == tabNextKey:
		return m.enterView(entries[stepStop(m.viewIndex(), 1, len(entries))])
	case k == tabPrevKey:
		return m.enterView(entries[stepStop(m.viewIndex(), -1, len(entries))])
	case k == "esc":
		m.viewsOpen = false
	case isBound(k, m.quitKeys()):
		m.viewsOpen = false

		return m, m.hide()
	default:
		for _, entry := range entries {
			if isBound(k, entry.keys(m.keys.Views)) {
				m.viewsOpen = false

				return m.enterView(entry)
			}
		}
	}

	return m, nil
}

func (m model) enterView(entry viewEntry) (tea.Model, tea.Cmd) {
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
	move := stopsMoveBinding("switch")
	done := binding([]string{"esc"}, "done")

	bindings := append([]key.Binding{move, done}, xslice.Map(viewEntries(), func(entry viewEntry) key.Binding {
		return binding(entry.keys(m.keys.Views), entry.label)
	})...)

	return keyMap{short: bindings, full: bindings}
}
