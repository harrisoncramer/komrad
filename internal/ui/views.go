package ui

import (
	"slices"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/tabs"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// viewEntry is one view on the switcher.
type viewEntry struct {
	screen screen.ID
	label  string
}

func viewEntries() []viewEntry {
	return []viewEntry{
		{
			screen: screen.Agents,
			label:  "agents",
		},
		{
			screen: screen.Schedules,
			label:  "schedules",
		},
		{
			screen: screen.Documents,
			label:  "documents",
		},
		{
			screen: screen.Worktrees,
			label:  "worktrees",
		},
	}
}

// rendersSwitcher reports whether the screen renders the row of views and answers the keys that
// switch them.
func rendersSwitcher(s screen.ID) bool {
	return slices.ContainsFunc(viewEntries(), func(entry viewEntry) bool { return entry.screen == s })
}

func (m *model) isViewsOpen() bool {
	return m.viewsOpen && rendersSwitcher(m.screen)
}

// viewIndex is where the screen that is up sits on the switcher, and tabs.NoStop when it is not there.
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

	return m.enterView(entries[tabs.StepStop(m.viewIndex(), delta, len(entries))])
}

// viewsDelta is how far a press steps through the views, and zero for a press that steps none.
func (m *model) viewsDelta(pressed string) int {
	switch {
	case keys.IsBound(pressed, m.keys.Views.Next):
		return 1
	case keys.IsBound(pressed, m.keys.Views.Previous):
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
	if at == tabs.NoStop {
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

	var target screen.Target
	if entry.screen == screen.Worktrees {
		if item, ok := m.hoveredRecordAgent(); ok {
			target.AgentID = item.Agent.ID
		} else {
			target.AgentID = m.hoveredAgentID()
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

	return screen.hoveredAgentID(m, m.inputs())
}

func (m *model) viewsBinding() key.Binding {
	return binding(m.keys.Views.Next, "switch view")
}

func (m *model) viewsHelp() legend.Help {
	bindings := []key.Binding{stopsMoveBinding("switch"), binding([]string{"esc"}, "done")}

	return legend.Help{Short: bindings, Full: bindings}
}
