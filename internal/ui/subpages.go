package ui

import (
	"errors"

	"komrad/internal/ui/panel"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

// subpage is one entry of the subpages menu.
type subpage int

const (
	subpageReview subpage = iota
	subpageDocuments
	subpageLogs
	subpageTerminal
)

var (
	errReviewArchived   = errors.New("an archived agent's work is not reviewed")
	errReviewChild      = errors.New("this agent works in its parent's checkout, so its work is reviewed there")
	errReviewNoCheckout = errors.New("this agent has no checkout of its own to read")
	errNoDocuments      = errors.New("this agent has not written any documents yet")
	errTerminalHere     = errors.New("a terminal opens from the agents list")
)

// agentMenu is the band of the menu that acts on an agent.
func agentMenu(s panelShared, from screen, item panel.AgentRow) submenu {
	agent := item.Agent
	agentID := agent.ID
	pages := agentSubpages(agent)

	return submenu{
		kind:  formMenu,
		title: s.listing.agentName(agent),
		crumb: s.listing.agentName(agent),
		entries: append(
			xslice.Map(pages, func(page subpage) submenuEntry {
				entry := subpageEntry(s, from, item, page)
				if entry.opens == nil {
					entry.open = func(m model) (tea.Model, tea.Cmd) { return m.enterSubpage(from, agentID, page) }
				}

				return entry
			}),
			nestedEntry("git", s.keys.Menu.Git, gitMenu(s, from, item)),
		),
	}
}

// agentSubpages is the subpages an agent has, less an orchestrator's work and writing.
func agentSubpages(agent panel.Agent) []subpage {
	if agent.Role == panel.RoleOrchestrator {
		return []subpage{subpageLogs, subpageTerminal}
	}

	return []subpage{subpageReview, subpageDocuments, subpageLogs, subpageTerminal}
}

// subpageEntry is how a subpage is drawn in an agent's menu.
func subpageEntry(s panelShared, from screen, item panel.AgentRow, page subpage) submenuEntry {
	switch page {
	case subpageReview:
		return submenuEntry{
			label: "review",
			keys:  s.keys.Menu.Review,
			isOff: reviewRefusal(from, item.Agent) != nil,
		}
	case subpageDocuments:
		return nestedEntry("documents", s.keys.Menu.Documents, documentsMenu(s, from, item))
	case subpageLogs:
		return submenuEntry{label: "logs", keys: s.keys.Menu.Logs}
	case subpageTerminal:
		return submenuEntry{
			label: "terminal",
			keys:  s.keys.Menu.Terminal,
			isOff: terminalRefusal(from, item.Agent) != nil,
		}
	}

	return submenuEntry{}
}

// reviewRefusal is why an agent's work cannot be reviewed from where the menu was opened, nil
// when it can.
func reviewRefusal(from screen, agent panel.Agent) error {
	switch {
	case from == screenArchive:
		return errReviewArchived
	case agent.ParentAgentID != nil:
		return errReviewChild
	case !isReviewable(agent):
		return errReviewNoCheckout
	}

	return nil
}

// terminalRefusal is why a shell cannot open on an agent from where the menu was opened, nil when
// it can.
func terminalRefusal(from screen, agent panel.Agent) error {
	switch {
	case !from.opensTerminal():
		return errTerminalHere
	case agent.Cwd == "":
		return errNoAgentDir
	}

	return nil
}

// enterSubpage goes from the menu to the subpage picked, or closes it and says why the agent has
// nothing there.
func (m model) enterSubpage(from screen, agentID string, picked subpage) (tea.Model, tea.Cmd) {
	item, ok := m.listing.anyAgent(agentID)
	if !ok {
		return m.refuseSubpage("Could not open the subpage", errAgentGone)
	}

	name := item.Agent.Name

	switch picked {
	case subpageReview:
		if err := reviewRefusal(from, item.Agent); err != nil {
			return m.refuseSubpage("Could not open the review of "+name, err)
		}

		m.leaveForm()

		return m, m.enter(screenReview, screenTarget{agentID: agentID})
	case subpageDocuments:
		if item.Documents == 0 {
			return m.refuseSubpage("Could not open the documents of "+name, errNoDocuments)
		}

		m.leaveForm()

		return m, m.enter(screenDocuments, screenTarget{agentID: agentID, from: from})
	case subpageLogs:
		m.leaveForm()

		return m, m.enter(screenLogs, screenTarget{agentID: agentID, from: from})
	case subpageTerminal:
		if err := terminalRefusal(from, item.Agent); err != nil {
			return m.refuseSubpage("Could not open a terminal", err)
		}

		closed, back := m.closeForm()
		next, _ := closed.(model)
		opened, cmd := next.openTerminal(item.Agent)

		return opened, tea.Batch(back, cmd)
	}

	return m.closeForm()
}

// refuseSubpage closes the menu and says why the subpage picked did not open.
func (m model) refuseSubpage(title string, err error) (tea.Model, tea.Cmd) {
	closed, back := m.closeForm()
	next, _ := closed.(model)
	next.report(title, err)

	return next, back
}
