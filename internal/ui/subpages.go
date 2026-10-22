package ui

import (
	"errors"
	"slices"

	"komrad/config"
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
func agentMenu(m *model, bindings config.Keybindings, item panel.AgentRow, own ...menu.Entry) menu.Menu {
	agent := item.Agent
	task := m.listing.Tasks.FindForAgent(agent)

	entries := slices.Concat(subpageEntries(m, bindings, item, agentSubpages(agent)), own)
	if agent.ArchivedAt == nil || task.ArchivedAt != nil {
		entries = append(entries, menu.Nested("git", bindings.Menu.Git, gitMenu(m, task)))
	}

	return menu.Menu{
		Title:   m.listing.AgentName(agent),
		Entries: append(entries, menu.Nested("jump", bindings.Menu.Jump, jumpMenu(m, bindings, screen.Agents, screen.Target{AgentID: agent.ID}))),
	}
}

// subpageEntries are the entries that open the agent's pages from the agents list.
func subpageEntries(m *model, bindings config.Keybindings, item panel.AgentRow, pages []subpage) []menu.Entry {
	return xslice.Map(pages, func(page subpage) menu.Entry {
		entry := page.entry(m, bindings, screen.Agents, item)
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

// entry is how the subpage is rendered in an agent's menu.
func (p subpage) entry(m *model, bindings config.Keybindings, openedFrom screen.ID, item panel.AgentRow) menu.Entry {
	switch p {
	case subpageReview:
		return menu.Entry{
			Label: "review",
			Keys:  bindings.Menu.Review,
			Off:   reviewRefusal(m.listing, item.Agent) != nil,
		}
	case subpageLogs:
		return menu.Entry{Label: "logs", Keys: bindings.Menu.Logs}
	case subpageChecks:
		return menu.Entry{Label: "status checks", Keys: bindings.Menu.StatusChecks}
	case subpageTerminal:
		return menu.Entry{
			Label: "terminal",
			Keys:  bindings.Menu.Terminal,
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
