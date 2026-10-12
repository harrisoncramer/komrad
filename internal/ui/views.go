package ui

import (
	"slices"
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const komradName = "komrad"

// viewsBarFloor is the shortest pane the switcher is rendered in.
const viewsBarFloor = 20

// viewEntry is one view on the switcher.
type viewEntry struct {
	screen screen
	label  string
}

func viewEntries() []viewEntry {
	return []viewEntry{
		{
			screen: screenAgents,
			label:  "agents",
		},
		{
			screen: screenSchedules,
			label:  "schedules",
		},
		{
			screen: screenDocuments,
			label:  "documents",
		},
		{
			screen: screenWorktrees,
			label:  "worktrees",
		},
	}
}

// rendersSwitcher reports whether the screen renders the row of views and answers the keys that
// switch them.
func rendersSwitcher(s screen) bool {
	return slices.ContainsFunc(viewEntries(), func(entry viewEntry) bool { return entry.screen == s })
}

func (m *model) isViewsOpen() bool {
	return m.viewsOpen && rendersSwitcher(m.screen)
}

// viewsBar is the logo and the switcher rendered over a list, and under them the trail down to the
// row or page the cursor is on.
func (m *model) viewsBar() string {
	return m.viewsBars()[0]
}

// viewsBars is every form the bar can take, tallest first.
func (m *model) viewsBars() []string {
	trail, _, opened := m.activeCrumbs()
	if m.term.height > 0 && m.term.height < viewsBarFloor {
		if m.isMessagesFlashing() && m.paneWidth() >= 3 {
			return []string{m.engineLine(m.paneWidth()) + "\n"}
		}

		if !opened {
			return []string{""}
		}

		return []string{trail.view(paint.Panel, m.paneWidth()) + "\n"}
	}

	lines := m.switcherLines(trail)
	logo, engine := lines[0], lines[1]

	return slices.Compact([]string{
		strings.Join(lines, "\n") + "\n",
		logo + "\n" + engine + "\n",
		engine + "\n",
	})
}

// switcherLines is the logo and, on a screen that renders the switcher, the row of views.
func (m *model) switcherLines(trail crumbs) []string {
	logo := paint.Panel.Title(glyph.Logo + " " + komradName)
	if path := trail.view(paint.Panel, 0); path != "" {
		logo += paint.Panel.Style(paint.ToneMuted).Render(" "+glyph.Crumb+" ") + path
	}

	lines := []string{logo, m.engineLine(m.paneWidth()), ""}
	if rendersSwitcher(m.screen) {
		lines = append(lines, m.viewsRow())
	}
	if width := m.paneWidth(); width > 0 {
		for i, line := range lines {
			lines[i] = ansi.Truncate(line, width, "…")
		}
	}

	return lines
}

func (m *model) viewsRow() string {
	labels := xslice.Map(viewEntries(), func(entry viewEntry) string { return entry.label })
	row := stops{
		labels: labels,
		focus:  noStop,
		lit:    noStop,
		mark:   m.spanMarker(sectionViews, labels),
	}
	if m.isViewsOpen() {
		row.focus = m.viewIndex()
	} else {
		row.lit = m.viewIndex()
	}

	return row.view()
}

// viewIndex is where the screen that is up sits on the switcher, and noStop when it is not there.
func (m *model) viewIndex() int {
	_, at, _ := m.activeCrumbs()

	return slices.IndexFunc(viewEntries(), func(entry viewEntry) bool { return entry.screen == at })
}

// openViews hands the switcher the keys and opens the view delta along from the one that is up.
func (m *model) openViews(delta int) tea.Cmd {
	m.pickView()
	return m.stepView(delta)
}

func (m *model) pickView() tea.Cmd {
	m.blurTabs()
	m.blurKinds()
	m.viewsOpen = true

	return nil
}

// closeViews hands the keys back to the view that is up, closing a shell it cannot hold.
func (m *model) closeViews() {
	m.viewsOpen = false
	m.closeTerminalLeaving(m.screen)
}

// stepView opens the view delta along from the one that is up, wrapping at both ends.
func (m *model) stepView(delta int) tea.Cmd {
	entries := viewEntries()

	return m.enterView(entries[stepStop(m.viewIndex(), delta, len(entries))])
}

// viewsDelta is how far a press steps through the views, and zero for a press that steps none.
func (m *model) viewsDelta(pressed string) int {
	switch {
	case isBound(pressed, m.keys.Views.Next):
		return 1
	case isBound(pressed, m.keys.Views.Previous):
		return -1
	}

	return 0
}

// holdsViewsKey reports whether the open switcher answers a press, any other closing it and reaching
// the view underneath.
func (m *model) holdsViewsKey(pressed string) bool {
	return pressed == tabNextKey || pressed == tabPrevKey || pressed == "esc" || m.viewsDelta(pressed) != 0
}

func (m *model) updateViews(key tea.KeyPressMsg) tea.Cmd {
	cmd := m.pressStrip(key.String(), stripKeys{
		step:  m.stepView,
		close: m.closeViews,
		other: func(pressed string) tea.Cmd {
			if delta := m.viewsDelta(pressed); delta != 0 {
				return m.stepView(delta)
			}

			return nil
		},
	})

	return cmd
}

func (m *model) clickView(label string) tea.Cmd {
	entries := viewEntries()
	at := slices.IndexFunc(entries, func(entry viewEntry) bool { return entry.label == label })
	if at == noStop {
		return nil
	}

	wasOpen := m.isViewsOpen()
	cmd := m.enterView(entries[at])
	if wasOpen {
		m.closeViews()
	}

	return cmd
}

func (m *model) enterView(entry viewEntry) tea.Cmd {
	if entry.screen == m.screen {
		return nil
	}

	var target screenTarget
	if entry.screen == screenWorktrees {
		if item, ok := m.hoveredRecordAgent(); ok {
			target.agentID = item.Agent.ID
		} else {
			target.agentID = m.hoveredAgentID()
		}
	}

	return m.enter(entry.screen, target)
}

// hoveredAgentID is the agent under the cursor on a screen that lists agents.
func (m *model) hoveredAgentID() string {
	screen, ok := m.active().(hoveredAgentReporter)
	if !ok {
		return ""
	}

	return screen.hoveredAgentID(m)
}

func (m *model) viewsBinding() key.Binding {
	return binding(m.keys.Views.Next, "switch view")
}

func (m *model) viewsHelp() keyMap {
	bindings := []key.Binding{stopsMoveBinding("switch"), binding([]string{"esc"}, "done")}

	return keyMap{short: bindings, full: bindings}
}
