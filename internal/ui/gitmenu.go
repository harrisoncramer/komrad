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
	gitChoicePush
	gitChoiceOpenPullRequest
	gitChoiceCopyPullRequestURL
	gitChoiceToggleDraft
)

var (
	errNoWorktree           = errors.New("this agent has no worktree of its own, so there is no base to move it onto")
	errRebaseArchived       = errors.New("an archived agent's branch is not rebased")
	errPushArchived         = errors.New("an archived agent's branch is not pushed")
	errPushNoWorktree       = errors.New("this agent has no worktree of its own, so there is no branch of its own to push")
	errPushInProgress       = errors.New("the worktree is part-way through a rebase, merge or pick, so finish or abort that first")
	errAbortArchived        = errors.New("an archived agent's worktree is not touched")
	errAbortNoWorktree      = errors.New("this agent has no worktree of its own for komrad to act on")
	errAbortNothing         = errors.New("nothing is part-way through in this worktree, so there is nothing to abort")
	errAgentGone            = errors.New("the agent the menu was opened on has left the list")
	errNoPullRequest        = errors.New("komrad has read the repository and nothing has been opened from this branch")
	errMovedPastPullRequest = errors.New("the branch has moved on since its last pull request finished and nothing has been opened from it since")
	errPullRequestsUnread   = errors.New("komrad has not read this agent's repository yet, or its checkout is not on a forge")
)

// gitMenu is the git actions on an agent, opened from its menu.
func gitMenu(s panelShared, from screen, item panel.AgentRow) submenu {
	agent := item.Agent
	agentID := agent.ID
	run := func(choice gitChoice) func(model) (model, tea.Cmd) {
		return func(m model) (model, tea.Cmd) { return m.runGitChoice(from, agentID, choice) }
	}

	return submenu{
		kind:  formGit,
		title: "git",
		crumb: "git",
		entries: []submenuEntry{
			{
				label: "rebase with base",
				keys:  s.keys.Git.Rebase,
				isOff: rebaseRefusal(s.listing, from, agent) != nil,
				write: run(gitChoiceRebase),
			},
			{
				label: "push",
				keys:  s.keys.Git.Push,
				isOff: pushRefusal(s.listing, from, agent) != nil,
				write: run(gitChoicePush),
			},
			{
				label: abortLabel(agent),
				keys:  s.keys.Git.Abort,
				isOff: abortRefusal(s.listing, from, agent) != nil,
				open:  func(m model) (tea.Model, tea.Cmd) { return m.openGitAbort(from, agentID) },
			},
			{
				label: "open pr",
				keys:  s.keys.Git.OpenPullRequest,
				isOff: item.PullRequest.URL == "",
				write: run(gitChoiceOpenPullRequest),
			},
			{
				label: "copy pr url",
				keys:  s.keys.Git.CopyPullRequestURL,
				isOff: item.PullRequest.URL == "",
				write: run(gitChoiceCopyPullRequestURL),
			},
			{
				label: draftLabel(item),
				keys:  s.keys.Git.ToggleDraft,
				isOff: openPullRequestRefusal(item) != nil,
				write: run(gitChoiceToggleDraft),
			},
			nestedEntry("merge pr", s.keys.Git.Merge, mergeMenu(s, from, item)),
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

// pushRefusal is why an agent's branch cannot be pushed from where the menu was opened, nil when
// it can.
func pushRefusal(l listing, from screen, agent panel.Agent) error {
	switch {
	case from == screenArchive:
		return errPushArchived
	case !l.isRebaseable(agent):
		return errPushNoWorktree
	case agent.Sequence.IsInProgress():
		return errPushInProgress
	}

	return nil
}

// abortRefusal is why what an agent's worktree has part-way through cannot be aborted from where
// the menu was opened, nil when it can.
func abortRefusal(l listing, from screen, agent panel.Agent) error {
	switch {
	case from == screenArchive:
		return errAbortArchived
	case !l.isRebaseable(agent):
		return errAbortNoWorktree
	case !agent.Sequence.IsInProgress():
		return errAbortNothing
	}

	return nil
}

// abortLabel names the abort entry for what it would undo.
func abortLabel(agent panel.Agent) string {
	if !agent.Sequence.IsInProgress() {
		return "abort"
	}

	return "abort " + string(agent.Sequence)
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

// openPullRequestRefusal is why an agent has no open pull request to act on, nil when it has one.
func openPullRequestRefusal(item panel.AgentRow) error {
	if item.PullRequest.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED {
		return errAlreadyMerged
	}
	if item.PullRequest.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED {
		return errPullRequestClosed
	}

	if item.PullRequest.URL == "" {
		return pullRequestRefusal(item)
	}

	return nil
}

// runGitChoice does what the entry asked for, or says why this agent is not one to ask it of.
func (m model) runGitChoice(from screen, agentID string, choice gitChoice) (model, tea.Cmd) {
	item, ok := m.findGitAgent(from, agentID)
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
	case gitChoicePush:
		if err := pushRefusal(m.listing, from, agent); err != nil {
			m.report("Could not push the branch of "+agent.Name, err)

			return m, nil
		}

		cmd := m.planPushCmd(agent.ID, agent.Name)

		return m, cmd
	case gitChoiceOpenPullRequest:
		return m.openPullRequest(item)
	case gitChoiceCopyPullRequestURL:
		return m.copyPullRequestURL(item)
	case gitChoiceToggleDraft:
		return m.toggleDraft(item)
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

// findGitAgent is the agent the menu was opened on, as the list holds it now.
func (m model) findGitAgent(from screen, agentID string) (panel.AgentRow, bool) {
	// The list may have moved under the open menu.
	if from == screenArchive {
		return m.listing.anyAgent(agentID)
	}

	return m.listing.liveAgent(agentID)
}

// openGitAbort leaves the menu for the abort's confirmation, or says why there is nothing to abort.
func (m model) openGitAbort(from screen, agentID string) (tea.Model, tea.Cmd) {
	item, ok := m.findGitAgent(from, agentID)
	if !ok {
		m.report("Could not act on the agent", errAgentGone)

		return m.closeForm()
	}

	agent := item.Agent
	if err := abortRefusal(m.listing, from, agent); err != nil {
		m.report("Could not abort what the worktree of "+agent.Name+" has in progress", err)

		return m.closeForm()
	}

	m.leaveForm()

	return m.openAbortConfirm(agent.ID, agent.Name, agent.Sequence)
}
