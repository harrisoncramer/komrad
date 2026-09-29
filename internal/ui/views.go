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

// viewsBar is the logo and the switcher drawn over a list, and under them the trail down to the
// row or page the cursor is on.
func (m model) viewsBar() string {
	short := m.height > 0 && m.height < viewsBarFloor

	trail, _, opened := m.activeCrumbs()
	if short {
		if flash, ok := m.messagesFlash(m.paneWidth()); ok {
			return flash + "\n"
		}

		if !opened {
			return ""
		}

		return trail.view(paint.Panel, m.paneWidth()) + "\n"
	}

	return strings.Join(m.switcherLines(trail), "\n") + "\n"
}

// switcherLines is the logo and the row of views.
func (m model) switcherLines(trail crumbs) []string {
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

	logo := paint.Panel.Title(glyph.Logo + " " + komradName)
	if path := trail.view(paint.Panel, 0); path != "" {
		logo += paint.Panel.Style(paint.ToneMuted).Render(" "+glyph.Crumb+" ") + path
	}

	lines := []string{logo, m.engineLine(m.paneWidth()), "", row.view(m.focusPalette())}
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
	m.pickView()

	return m
}

func (m *model) pickView() tea.Cmd {
	m.blurTabs()
	m.blurKinds()
	m.viewsOpen = true

	return nil
}

func (m model) updateViews(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	entries := viewEntries()

	cmd := m.pressStrip(key.String(), stripKeys{
		step: func(delta int) tea.Cmd {
			return m.enterView(entries[stepStop(m.viewIndex(), delta, len(entries))])
		},
		close: func() { m.viewsOpen = false },
		other: func(pressed string) tea.Cmd {
			for _, entry := range entries {
				if isBound(pressed, entry.keys(m.keys.Views)) {
					m.viewsOpen = false

					return m.enterView(entry)
				}
			}

			return nil
		},
	})

	return m, cmd
}

func (m *model) enterView(entry viewEntry) tea.Cmd {
	if entry.screen == m.screen {
		return nil
	}

	var target screenTarget
	if entry.screen == screenWorktrees {
		target.agentID = m.hoveredAgentID()
	}

	return m.enter(entry.screen, target)
}

// hoveredAgentID is the agent under the cursor on a screen that lists agents.
func (m model) hoveredAgentID() string {
	switch m.screen {
	case screenAgents:
		return m.agents.hoveredAgentID(m.shared())
	case screenArchive:
		return m.archive.hoveredAgentID(m.shared())
	case screenSchedules, screenSearch, screenReview, screenWorktrees, screenDocuments, screenLogs:
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
