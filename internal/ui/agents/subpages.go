package agents

import (
	"errors"
	"slices"

	"komrad/config"
	"komrad/internal/ui/form"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
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
	errReviewReviewer = errors.New("this agent reviews its task's checkout, so open the review from the task's writer")
	errTerminalHere   = errors.New("open a terminal from the agents list")
	errAgentGone      = errors.New("the agent the menu was opened on has left the list")
)

// agentMenuParams is the agent a band acts on, the menus it nests, and the entries a screen adds
// of its own before git.
type agentMenuParams struct {
	in       screen.Inputs
	nest     menu.NestedMenuBuilder
	bindings config.Keybindings
	item     panel.AgentRow
	own      []menu.Entry
}

// agentMenu is the band of the menu that acts on an agent, with the entries a screen adds of its own before git.
func agentMenu(params agentMenuParams) menu.Menu {
	agent := params.item.Agent
	task := params.in.Listing.Tasks.FindForAgent(agent)

	entries := slices.Concat(subpageEntries(params.in.Listing, params.bindings, params.item, agentSubpages(agent)), params.own)
	if agent.ArchivedAt == nil || task.ArchivedAt != nil {
		entries = append(entries, menu.Nested("git", params.bindings.Menu.Git, params.nest.Git(task)))
	}

	return menu.Menu{
		Title:   params.in.Listing.AgentName(agent),
		Entries: append(entries, menu.Nested("jump", params.bindings.Menu.Jump, params.nest.Jump(screen.Agents, screen.Target{AgentID: agent.ID}))),
	}
}

// subpageEntries are the entries that open the agent's pages from the agents list.
func subpageEntries(l panel.Listing, bindings config.Keybindings, item panel.AgentRow, pages []subpage) []menu.Entry {
	return xslice.Map(pages, func(page subpage) menu.Entry {
		entry := page.entry(l, bindings, item)
		if entry.Submenu == nil {
			entry.Pick = func(in screen.Inputs) screen.Request {
				return enterSubpage(in.Listing, item.Agent.ID, page)
			}
		}

		return entry
	})
}

// agentSubpages is the subpages an agent has, less an orchestrator's work and an archived agent's review.
func agentSubpages(agent panel.Agent) []subpage {
	switch {
	case agent.Role == agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR:
		return []subpage{subpageLogs, subpageTerminal}
	case agent.ArchivedAt != nil:
		return []subpage{subpageLogs, subpageChecks, subpageTerminal}
	}

	return []subpage{subpageReview, subpageLogs, subpageChecks, subpageTerminal}
}

// entry is how the subpage is rendered in an agent's menu.
func (p subpage) entry(l panel.Listing, bindings config.Keybindings, item panel.AgentRow) menu.Entry {
	switch p {
	case subpageReview:
		return menu.Entry{
			Label: "review",
			Keys:  bindings.Menu.Review,
			Off:   reviewRefusal(l, item.Agent) != nil,
		}
	case subpageLogs:
		return menu.Entry{Label: "logs", Keys: bindings.Menu.Logs}
	case subpageChecks:
		return menu.Entry{Label: "status checks", Keys: bindings.Menu.StatusChecks}
	case subpageTerminal:
		return menu.Entry{
			Label: "terminal",
			Keys:  bindings.Menu.Terminal,
			Off:   terminalRefusal(item.Agent) != nil,
		}
	}

	return menu.Entry{}
}

// reviewRefusal is why an agent's work cannot be reviewed, nil when it can.
func reviewRefusal(l panel.Listing, agent panel.Agent) error {
	switch {
	case agent.Role == agentv1.AgentRole_AGENT_ROLE_REVIEWER:
		return errReviewReviewer
	case !l.IsReviewable(agent):
		return panel.ErrNoCheckout
	}

	return nil
}

// terminalRefusal is why a shell cannot open on an agent, nil when it can.
func terminalRefusal(agent panel.Agent) error {
	switch {
	case agent.ArchivedAt != nil:
		return errTerminalHere
	case agent.Cwd == "":
		return panel.ErrNoAgentDir
	}

	return nil
}

// enterSubpage goes from the menu to the subpage picked, or closes it and says why the agent has
// nothing there.
func enterSubpage(l panel.Listing, agentID string, picked subpage) screen.Request {
	item, ok := l.AnyAgent(agentID)
	if !ok {
		return refuseSubpage("Could not open the subpage", errAgentGone)
	}

	name := item.Agent.Name

	switch picked {
	case subpageReview:
		if err := reviewRefusal(l, item.Agent); err != nil {
			return refuseSubpage("Could not open the review of "+name, err)
		}

		return screen.Open{Screen: screen.Review, Target: screen.Target{AgentID: agentID}}
	case subpageLogs:
		return screen.Open{Screen: screen.Logs, Target: screen.Target{AgentID: agentID, OpenedFromScreen: screen.Agents}}
	case subpageChecks:
		return screen.Open{Screen: screen.StatusChecks, Target: screen.Target{AgentID: agentID, OpenedFromScreen: screen.Agents}}
	case subpageTerminal:
		if err := terminalRefusal(item.Agent); err != nil {
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
