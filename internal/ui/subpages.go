package ui

import (
	"errors"
	"slices"

	"komrad/internal/ui/form"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
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
func agentMenu(m *model, item panel.AgentRow, own ...menu.Entry) menu.Menu {
	agent := item.Agent
	task := m.listing.Tasks.FindForAgent(agent)

	entries := slices.Concat(subpageEntries(m, item, agentSubpages(agent)), own)
	if agent.ArchivedAt == nil || task.ArchivedAt != nil {
		entries = append(entries, menu.Nested("git", m.keys.Menu.Git, gitMenu(m, task)))
	}

	return menu.Menu{
		Title:   m.listing.AgentName(agent),
		Entries: append(entries, menu.Nested("jump", m.keys.Menu.Jump, jumpMenu(m, screen.Agents, screen.Target{AgentID: agent.ID}))),
	}
}

// subpageEntries are the entries that open the agent's pages from the agents list.
func subpageEntries(m *model, item panel.AgentRow, pages []subpage) []menu.Entry {
	return xslice.Map(pages, func(page subpage) menu.Entry {
		entry := subpageEntry(m, screen.Agents, item, page)
		if entry.Opens == nil {
			entry.Pick = func() screen.Request { return m.enterSubpage(screen.Agents, item.Agent.ID, page) }
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
func subpageEntry(m *model, openedFrom screen.ID, item panel.AgentRow, page subpage) menu.Entry {
	switch page {
	case subpageReview:
		return menu.Entry{
			Label: "review",
			Keys:  m.keys.Menu.Review,
			Off:   reviewRefusal(m.listing, item.Agent) != nil,
		}
	case subpageLogs:
		return menu.Entry{Label: "logs", Keys: m.keys.Menu.Logs}
	case subpageChecks:
		return menu.Entry{Label: "status checks", Keys: m.keys.Menu.StatusChecks}
	case subpageTerminal:
		return menu.Entry{
			Label: "terminal",
			Keys:  m.keys.Menu.Terminal,
			Off:   terminalRefusal(openedFrom, item.Agent) != nil,
		}
	}

	return menu.Entry{}
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
func (m *model) enterSubpage(openedFrom screen.ID, agentID string, picked subpage) screen.Request {
	item, ok := m.listing.AnyAgent(agentID)
	if !ok {
		return refuseSubpage("Could not open the subpage", errAgentGone)
	}

	name := item.Agent.Name

	switch picked {
	case subpageReview:
		if err := reviewRefusal(m.listing, item.Agent); err != nil {
			return refuseSubpage("Could not open the review of "+name, err)
		}

		return screen.Open{Screen: screen.Review, Target: screen.Target{AgentID: agentID}}
	case subpageLogs:
		return screen.Open{Screen: screen.Logs, Target: screen.Target{AgentID: agentID, OpenedFrom: openedFrom}}
	case subpageChecks:
		return screen.Open{Screen: screen.StatusChecks, Target: screen.Target{AgentID: agentID, OpenedFrom: openedFrom}}
	case subpageTerminal:
		if err := terminalRefusal(openedFrom, item.Agent); err != nil {
			return refuseSubpage("Could not open a terminal", err)
		}

		return screen.Batch{form.Close{}, screen.OpenTerminal{Dir: item.Agent.Cwd}}
	}

	return form.Close{}
}

// refuseSubpage closes the menu and says why the subpage picked did not open.
func refuseSubpage(title string, err error) screen.Request {
	return screen.Batch{form.Close{}, screen.Report{Summary: title, Err: err}}
}
