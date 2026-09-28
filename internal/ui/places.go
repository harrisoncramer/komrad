package ui

import (
	"errors"

	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// place is one entry of the places menu.
type place int

const (
	placeReview place = iota
	placeDocuments
	placeLogs
)

var (
	errReviewArchived   = errors.New("an archived agent's work is not reviewed")
	errReviewChild      = errors.New("this agent works in its parent's checkout, so its work is reviewed there")
	errReviewNoCheckout = errors.New("this agent has no checkout of its own to read")
	errNoDocuments      = errors.New("this agent has not written any documents yet")
)

// openPlaces draws the places that belong to an agent beside the list it was opened over.
func openPlaces(h host, s panelShared, from screen, item panel.AgentRow) tea.Cmd {
	agent := item.Agent
	agentID := agent.ID

	return openSubmenu(h, submenu{
		kind:  formPlaces,
		title: "Places in " + agent.Name,
		crumb: agent.Name,
		entries: []submenuEntry{
			{label: "review", keys: s.keys.Places.Review, isOff: reviewRefusal(from, agent) != nil},
			{label: "documents", keys: s.keys.Places.Documents, isOff: item.Documents == 0},
			{label: "logs", keys: s.keys.Places.Logs},
		},
		answer: func(m model, choice int) (tea.Model, tea.Cmd) {
			return m.enterPlace(from, agentID, place(choice))
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

// enterPlace closes the menu and goes to the place picked, or says why the agent has nothing
// there.
func (m model) enterPlace(from screen, agentID string, picked place) (tea.Model, tea.Cmd) {
	closed, back := m.closeForm()
	next, _ := closed.(model)

	item, ok := next.listing.anyAgent(agentID)
	if !ok {
		next.report("Could not open the place", errAgentGone)

		return next, back
	}

	name := item.Agent.Name

	switch picked {
	case placeReview:
		if err := reviewRefusal(from, item.Agent); err != nil {
			next.report("Could not open the review of "+name, err)

			return next, back
		}

		cmd := next.enter(screenReview, screenTarget{agentID: agentID})

		return next, tea.Batch(back, cmd)
	case placeDocuments:
		if item.Documents == 0 {
			next.report("Could not open the documents of "+name, errNoDocuments)

			return next, back
		}

		cmd := next.enter(screenDocuments, screenTarget{agentID: agentID, from: from})

		return next, tea.Batch(back, cmd)
	case placeLogs:
		return next, back
	}

	return next, back
}
