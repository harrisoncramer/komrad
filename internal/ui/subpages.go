package ui

import (
	"errors"

	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// subpage is one entry of the subpages menu.
type subpage int

const (
	subpageReview subpage = iota
	subpageDocuments
	subpageLogs
)

var (
	errReviewArchived   = errors.New("an archived agent's work is not reviewed")
	errReviewChild      = errors.New("this agent works in its parent's checkout, so its work is reviewed there")
	errReviewNoCheckout = errors.New("this agent has no checkout of its own to read")
	errNoDocuments      = errors.New("this agent has not written any documents yet")
)

// openAgentSubpages draws the subpages of an agent beside the list it was opened over.
func openAgentSubpages(h host, s panelShared, from screen, item panel.AgentRow) tea.Cmd {
	agent := item.Agent
	agentID := agent.ID

	return openSubmenu(h, submenu{
		kind:  formSubpages,
		title: "Subpages of " + agent.Name,
		crumb: agent.Name,
		entries: []submenuEntry{
			{label: "review", keys: s.keys.Subpages.Review, isOff: reviewRefusal(from, agent) != nil},
			{label: "documents", keys: s.keys.Subpages.Documents, isOff: item.Documents == 0},
			{label: "logs", keys: s.keys.Subpages.Logs},
		},
		answer: func(m model, choice int) (tea.Model, tea.Cmd) {
			return m.enterSubpage(from, agentID, subpage(choice))
		},
	})
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

// enterSubpage closes the menu and goes to the subpage picked, or says why the agent has nothing
// there.
func (m model) enterSubpage(from screen, agentID string, picked subpage) (tea.Model, tea.Cmd) {
	closed, back := m.closeForm()
	next, _ := closed.(model)

	item, ok := next.listing.anyAgent(agentID)
	if !ok {
		next.report("Could not open the subpage", errAgentGone)

		return next, back
	}

	name := item.Agent.Name

	switch picked {
	case subpageReview:
		if err := reviewRefusal(from, item.Agent); err != nil {
			next.report("Could not open the review of "+name, err)

			return next, back
		}

		cmd := next.enter(screenReview, screenTarget{agentID: agentID})

		return next, tea.Batch(back, cmd)
	case subpageDocuments:
		if item.Documents == 0 {
			next.report("Could not open the documents of "+name, errNoDocuments)

			return next, back
		}

		cmd := next.enter(screenDocuments, screenTarget{agentID: agentID, from: from})

		return next, tea.Batch(back, cmd)
	case subpageLogs:
		return next, back
	}

	return next, back
}
