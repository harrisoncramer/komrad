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
	gitChoiceCopyPullRequestURL
)

var (
	errNoWorktree           = errors.New("this agent has no worktree of its own, so there is no base to move it onto")
	errRebaseArchived       = errors.New("an archived agent's branch is not rebased")
	errAgentGone            = errors.New("the agent the menu was opened on has left the list")
	errNoPullRequest        = errors.New("komrad has read the repository and nothing has been opened from this branch")
	errMovedPastPullRequest = errors.New("the branch has moved on since its last pull request finished and nothing has been opened from it since")
	errPullRequestsUnread   = errors.New("komrad has not read this agent's repository yet, or its checkout is not on a forge")
)

// gitMenu is the git actions on an agent, opened from its menu.
func gitMenu(s panelShared, from screen, item panel.AgentRow) submenu {
	agent := item.Agent
	agentID := agent.ID

	return submenu{
		kind:  formGit,
		title: "git",
		crumb: "git",
		entries: []submenuEntry{
			{label: "rebase with base", keys: s.keys.Git.Rebase, isOff: rebaseRefusal(s.listing, from, agent) != nil},
			{label: "open pr", keys: s.keys.Git.OpenPullRequest, isOff: item.PullRequest.URL == ""},
			{label: "copy pr url", keys: s.keys.Git.CopyPullRequestURL, isOff: item.PullRequest.URL == ""},
			nestedEntry("merge pr", s.keys.Git.Merge, mergeMenu(s, from, item)),
		},
		answer: func(m model, choice int) (tea.Model, tea.Cmd) {
			return confirmSubmit(func(m model) (model, tea.Cmd) { return m.runGitChoice(from, agentID, gitChoice(choice)) })(m)
		},
	}
}

// rebaseRefusal is why an agent's worktree cannot be rebased from where the menu was opened, nil
// when it can.
func rebaseRefusal(l listing, from screen, agent panel.Agent) error {
	switch {
	case from == screenArchive:
		return errRebaseArchived
	case !l.isRebaseable(agent):
		return errNoWorktree
	}

	return nil
}

// pullRequestRefusal is why an agent has no current pull request.
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
func (m model) runGitChoice(from screen, agentID string, choice gitChoice) (model, tea.Cmd) {
	// The list may have moved under the open menu.
	item, ok := m.listing.liveAgent(agentID)
	if from == screenArchive {
		item, ok = m.listing.anyAgent(agentID)
	}

	if !ok {
		m.report("Could not act on the agent", errAgentGone)

		return m, nil
	}

	agent := item.Agent

	switch choice {
	case gitChoiceRebase:
		if err := rebaseRefusal(m.listing, from, agent); err != nil {
			m.report("Could not rebase the worktree of "+agent.Name, err)

			return m, nil
		}

		cmd := m.planRebaseCmd(agent.ID, agent.Name)

		return m, cmd
	case gitChoiceOpenPullRequest:
		return m.openPullRequest(item)
	case gitChoiceCopyPullRequestURL:
		return m.copyPullRequestURL(item)
	}

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
