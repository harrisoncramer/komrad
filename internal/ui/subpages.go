package ui

import (
	"errors"
	"slices"

	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
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
	errReviewReviewer   = errors.New("this agent reviews its task's checkout, so open the review from the task's writer")
	errReviewNoCheckout = errors.New("this agent has no checkout of its own to read")
	errTerminalHere     = errors.New("open a terminal from the agents list")
)

// agentMenu is the band of the menu that acts on an agent, with the entries a screen adds of its own before git.
func agentMenu(m *model, item panel.AgentRow, own ...submenuEntry) submenu {
	agent := item.Agent
	task := m.listing.Tasks.FindForAgent(agent)

	entries := slices.Concat(subpageEntries(m, item, agentSubpages(agent)), own)
	if agent.ArchivedAt == nil || task.ArchivedAt != nil {
		entries = append(entries, nestedEntry("git", m.keys.Menu.Git, gitMenu(m, task)))
	}

	return submenu{
		kind:    formMenu,
		title:   m.listing.AgentName(agent),
		entries: append(entries, nestedEntry("jump", m.keys.Menu.Jump, jumpMenu(m, screen.Agents, screen.Target{AgentID: agent.ID}))),
	}
}

// subpageEntries are the entries that open the agent's pages from the agents list.
func subpageEntries(m *model, item panel.AgentRow, pages []subpage) []submenuEntry {
	return xslice.Map(pages, func(page subpage) submenuEntry {
		entry := subpageEntry(m, screen.Agents, item, page)
		if entry.opens == nil {
			entry.open = func(m *model) tea.Cmd { return m.enterSubpage(screen.Agents, item.Agent.ID, page) }
		}

		return entry
	})
}

// agentSubpages is the subpages an agent has, less an orchestrator's work and an archived agent's review.
func agentSubpages(agent panel.Agent) []subpage {
	switch {
	case agent.Role == panel.RoleOrchestrator:
		return []subpage{subpageLogs, subpageTerminal}
	case agent.ArchivedAt != nil:
		return []subpage{subpageLogs, subpageChecks, subpageTerminal}
	}

	return []subpage{subpageReview, subpageLogs, subpageChecks, subpageTerminal}
}

// subpageEntry is how a subpage is rendered in an agent's menu.
func subpageEntry(m *model, openedFrom screen.ID, item panel.AgentRow, page subpage) submenuEntry {
	switch page {
	case subpageReview:
		return submenuEntry{
			label: "review",
			keys:  m.keys.Menu.Review,
			isOff: reviewRefusal(m.listing, item.Agent) != nil,
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
func reviewRefusal(l panel.Listing, agent panel.Agent) error {
	switch {
	case agent.Role == panel.RoleReviewer:
		return errReviewReviewer
	case !l.IsReviewable(agent):
		return errReviewNoCheckout
	}

	return nil
}

// terminalRefusal is why a shell cannot open on an agent from where the menu was opened, nil when
// it can.
func terminalRefusal(openedFrom screen.ID, agent panel.Agent) error {
	switch {
	case !opensTerminal(openedFrom), agent.ArchivedAt != nil:
		return errTerminalHere
	case agent.Cwd == "":
		return errNoAgentDir
	}

	return nil
}

// enterSubpage goes from the menu to the subpage picked, or closes it and says why the agent has
// nothing there.
func (m *model) enterSubpage(openedFrom screen.ID, agentID string, picked subpage) tea.Cmd {
	item, ok := m.listing.AnyAgent(agentID)
	if !ok {
		return m.refuseSubpage("Could not open the subpage", errAgentGone)
	}

	name := item.Agent.Name

	switch picked {
	case subpageReview:
		if err := reviewRefusal(m.listing, item.Agent); err != nil {
			return m.refuseSubpage("Could not open the review of "+name, err)
		}

		m.leaveForm()

		return m.apply(screen.Open{Screen: screen.Review, Target: screen.Target{AgentID: agentID}})
	case subpageLogs:
		m.leaveForm()

		return m.apply(screen.Open{Screen: screen.Logs, Target: screen.Target{AgentID: agentID, OpenedFrom: openedFrom}})
	case subpageChecks:
		m.leaveForm()

		return m.apply(screen.Open{Screen: screen.StatusChecks, Target: screen.Target{AgentID: agentID, OpenedFrom: openedFrom}})
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
