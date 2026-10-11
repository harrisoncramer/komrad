package ui

import (
	"errors"
	"slices"

	"komrad/internal/ui/panel"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

// subpage is one entry of the subpages menu.
type subpage int

const (
	subpageReview subpage = iota
	subpageLogs
	subpageChecks
	subpageTerminal
)

var (
	errReviewArchived   = errors.New("an archived agent's work is not reviewed")
	errReviewChild      = errors.New("this agent works in its parent's checkout, so its work is reviewed there")
	errReviewNoCheckout = errors.New("this agent has no checkout of its own to read")
	errTerminalHere     = errors.New("a terminal opens from the agents list")
)

// agentMenu is the band of the menu that acts on an agent, with the entries a screen adds of its own before git.
func agentMenu(m *model, item panel.AgentRow, own ...submenuEntry) submenu {
	agent := item.Agent
	task := m.listing.tasks.findForAgent(agent)

	entries := slices.Concat(subpageEntries(m, item, agentSubpages(agent)), own)
	if agent.ArchivedAt == nil || task.ArchivedAt != nil {
		entries = append(entries, nestedEntry("git", m.keys.Menu.Git, gitMenu(m, task)))
	}

	return submenu{
		kind:    formMenu,
		title:   m.listing.agentName(agent),
		entries: append(entries, nestedEntry("jump", m.keys.Menu.Jump, jumpMenu(m, screenAgents, agent.ID))),
	}
}

// subpageEntries are the entries that open the agent's pages from the agents list.
func subpageEntries(m *model, item panel.AgentRow, pages []subpage) []submenuEntry {
	return xslice.Map(pages, func(page subpage) submenuEntry {
		entry := subpageEntry(m, screenAgents, item, page)
		if entry.opens == nil {
			entry.open = func(m *model) tea.Cmd { return m.enterSubpage(screenAgents, item.Agent.ID, page) }
		}

		return entry
	})
}

// agentSubpages is the subpages an agent has, less an orchestrator's work.
func agentSubpages(agent panel.Agent) []subpage {
	if agent.Role == panel.RoleOrchestrator {
		return []subpage{subpageLogs, subpageTerminal}
	}

	return []subpage{subpageReview, subpageLogs, subpageChecks, subpageTerminal}
}

// subpageEntry is how a subpage is rendered in an agent's menu.
func subpageEntry(m *model, openedFrom screen, item panel.AgentRow, page subpage) submenuEntry {
	switch page {
	case subpageReview:
		return submenuEntry{
			label: "review",
			keys:  m.keys.Menu.Review,
			isOff: m.listing.reviewRefusal(item.Agent) != nil,
		}
	case subpageLogs:
		return submenuEntry{label: "logs", keys: m.keys.Menu.Logs}
	case subpageChecks:
		return submenuEntry{label: "status checks", keys: m.keys.Menu.StatusChecks}
	case subpageTerminal:
		return submenuEntry{
			label: "terminal",
			keys:  m.keys.Menu.Terminal,
			isOff: terminalRefusal(openedFrom, item.Agent) != nil,
		}
	}

	return submenuEntry{}
}

// reviewRefusal is why an agent's work cannot be reviewed, nil when it can.
func (l listing) reviewRefusal(agent panel.Agent) error {
	switch {
	case agent.ArchivedAt != nil:
		return errReviewArchived
	case agent.ParentAgentID != nil:
		return errReviewChild
	case !l.isReviewable(agent):
		return errReviewNoCheckout
	}

	return nil
}

// terminalRefusal is why a shell cannot open on an agent from where the menu was opened, nil when
// it can.
func terminalRefusal(openedFrom screen, agent panel.Agent) error {
	switch {
	case !openedFrom.opensTerminal(), agent.ArchivedAt != nil:
		return errTerminalHere
	case agent.Cwd == "":
		return errNoAgentDir
	}

	return nil
}

// enterSubpage goes from the menu to the subpage picked, or closes it and says why the agent has
// nothing there.
func (m *model) enterSubpage(openedFrom screen, agentID string, picked subpage) tea.Cmd {
	item, ok := m.listing.anyAgent(agentID)
	if !ok {
		return m.refuseSubpage("Could not open the subpage", errAgentGone)
	}

	name := item.Agent.Name

	switch picked {
	case subpageReview:
		if err := m.listing.reviewRefusal(item.Agent); err != nil {
			return m.refuseSubpage("Could not open the review of "+name, err)
		}

		m.leaveForm()

		return m.enter(screenReview, screenTarget{agentID: agentID})
	case subpageLogs:
		m.leaveForm()

		return m.enter(screenLogs, screenTarget{agentID: agentID, openedFrom: openedFrom})
	case subpageChecks:
		m.leaveForm()

		return m.enter(screenStatusChecks, screenTarget{agentID: agentID, openedFrom: openedFrom})
	case subpageTerminal:
		if err := terminalRefusal(openedFrom, item.Agent); err != nil {
			return m.refuseSubpage("Could not open a terminal", err)
		}

		return tea.Batch(m.closeForm(), m.openTerminal(item.Agent.Cwd))
	}

	return m.closeForm()
}

// refuseSubpage closes the menu and says why the subpage picked did not open.
func (m *model) refuseSubpage(title string, err error) tea.Cmd {
	back := m.closeForm()
	m.report(title, err)

	return back
}
