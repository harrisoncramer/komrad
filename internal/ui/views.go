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

// viewsBarFloor is the shortest pane the switcher is drawn in.
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
			screen: screenSearch,
			label:  "query",
		},
		{
			screen: screenSchedules,
			label:  "schedules",
		},
		{
			screen: screenArchive,
			label:  "archive",
		},
		{
			screen: screenWorktrees,
			label:  "worktrees",
		},
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
		if flash, ok := m.drawMessagesFlash(m.paneWidth()); ok {
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

// openViews hands the switcher the keys and opens the view delta along from the one that is up.
func (m model) openViews(delta int) (model, tea.Cmd) {
	m.pickView()
	cmd := m.stepView(delta)

	return m, cmd
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
	if !m.screen.opensTerminal() && m.terminalDir != "" {
		*m = m.closeInfo()
	}
}

// stepView opens the view delta along from the one that is up, wrapping at both ends.
func (m *model) stepView(delta int) tea.Cmd {
	entries := viewEntries()

	return m.enterView(entries[stepStop(m.viewIndex(), delta, len(entries))])
}

// viewsDelta is how far a press steps through the views, and zero for a press that steps none.
func (m model) viewsDelta(pressed string) int {
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
func (m model) holdsViewsKey(pressed string) bool {
	return pressed == tabNextKey || pressed == tabPrevKey || pressed == "esc" || m.viewsDelta(pressed) != 0
}

func (m model) updateViews(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
	return binding(m.keys.Views.Next, "switch view")
}

func (m model) viewsHelp() keyMap {
	bindings := []key.Binding{stopsMoveBinding("switch"), binding([]string{"esc"}, "done")}

	return keyMap{short: bindings, full: bindings}
}
