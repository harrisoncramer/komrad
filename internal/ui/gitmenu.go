package ui

import (
	"errors"
	"slices"

	"komrad/internal/ui/panel"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

// gitChoice is one entry of the git menu.
type gitChoice int

const (
	gitChoiceRebase gitChoice = iota
	gitChoiceOpenPullRequest
)

var (
	errNoWorktree           = errors.New("this agent has no worktree of its own, so there is no base to move it onto")
	errAgentGone            = errors.New("the agent the menu was opened on has left the list")
	errNoPullRequest        = errors.New("komrad has read the repository and nothing has been opened from this branch")
	errMovedPastPullRequest = errors.New("the branch has moved on since its last pull request finished and nothing has been opened from it since")
	errPullRequestsUnread   = errors.New("komrad has not read this agent's repository yet, or its checkout is not on a forge")
)

// openGitMenu draws the git actions beside the list, for the agent the cursor is on.
func openGitMenu(h host, s panelShared, item panel.AgentRow) tea.Cmd {
	agent := item.Agent
	agentID := agent.ID

	return openSubmenu(h, submenu{
		kind:  formGit,
		title: "Git on " + s.listing.agentName(agent),
		crumb: s.listing.agentName(agent),
		entries: []submenuEntry{
			{label: "rebase with base", keys: s.keys.Git.Rebase, isOff: !s.listing.isRebaseable(agent)},
			{label: "open pr", keys: s.keys.Git.OpenPullRequest, isOff: item.PullRequest.URL == ""},
		},
		answer: func(m model, choice int) (tea.Model, tea.Cmd) {
			return confirmSubmit(func(m model) (model, tea.Cmd) { return m.runGitChoice(agentID, gitChoice(choice)) })(m)
		},
	})
}

// pullRequestRefusal is why an agent with no current pull request has none to open.
func pullRequestRefusal(item panel.AgentRow) error {
	switch {
	case item.PullRequest.State != pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE:
		return errPullRequestsUnread
	case len(item.PullRequests) > 0:
		return errMovedPastPullRequest
	default:
		return errNoPullRequest
	}
}

// runGitChoice does what the entry asked for, or says why this agent is not one to ask it of.
func (m model) runGitChoice(agentID string, choice gitChoice) (model, tea.Cmd) {
	// The list may have moved under the open menu.
	item, ok := m.listing.liveAgent(agentID)
	if !ok {
		m.report("Could not act on the agent", errAgentGone)

		return m, nil
	}

	agent := item.Agent

	switch choice {
	case gitChoiceRebase:
		if !m.listing.isRebaseable(agent) {
			m.report("Could not rebase the worktree of "+agent.Name, errNoWorktree)

			return m, nil
		}

		cmd := m.planRebaseCmd(agent.ID, agent.Name)

		return m, cmd
	case gitChoiceOpenPullRequest:
		return m.openPullRequest(item)
	}

	// The switch carries every entry and no default, so this is what an entry added without an arm
	// compiles to until the exhaustive check refuses it.
	return m, nil
}

// isRebaseable reports whether an agent sits in a worktree komrad cut for it alone.
func (l listing) isRebaseable(agent panel.Agent) bool {
	if !hasCheckout(agent) {
		return false
	}

	index := slices.IndexFunc(l.workflows, func(w panel.Workflow) bool { return w.ID == agent.WorkflowID })
	if index < 0 {
		return false
	}

	// A child sits in its parent's checkout.
	if agent.ParentAgentID != nil {
		return false
	}

	return l.workflows[index].Worktree.Enabled
}
