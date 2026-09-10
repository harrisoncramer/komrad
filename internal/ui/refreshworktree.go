package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// refreshPlan is what a rebase would do, carried from the server to the picker.
type refreshPlan struct {
	agentID string
	name    string
	baseRef string
	baseSHA string
	commits int
	// landed is how many of the branch's own commits the base already holds, and replay how
	// many are left to move onto it.
	landed int
	replay int
	// merges is how many merge commits a rebase would discard, and conflictPaths what a merge
	// of the base into the branch could not settle.
	merges        int
	conflictPaths []string
}

// refreshPlannedMsg is a plan come back, or the reason there is none.
type refreshPlannedMsg struct {
	plan refreshPlan
	err  error
}

// rebasedMsg is a finished rebase, conflicted when it stopped and is waiting for the agent.
type rebasedMsg struct {
	name    string
	baseRef string
	baseSHA string
	// landed is how many of its commits the base already held, and replay how many moved.
	landed     int
	replay     int
	conflicted bool
	// moved says the branch reached the base without replaying anything.
	moved bool
	err   error
}

// planRefreshCmd asks what a rebase would do.
func (m *model) planRefreshCmd(agentID, name string) tea.Cmd {
	clients := m.api

	return func() tea.Msg {
		plan, err := clients.planRefresh(agentID)
		if err != nil {
			return refreshPlannedMsg{err: err}
		}

		return refreshPlannedMsg{plan: refreshPlan{
			agentID:       agentID,
			name:          name,
			baseRef:       plan.GetBaseRef(),
			baseSHA:       plan.GetBaseSha(),
			commits:       int(plan.GetCommits()),
			landed:        int(plan.GetLandedCommits()),
			replay:        int(plan.GetReplayCommits()),
			merges:        int(plan.GetMerges()),
			conflictPaths: plan.GetConflictPaths(),
		}}
	}
}

// rebaseCmd runs the rebase and reports what it did.
func (m *model) rebaseCmd(agentID, name string) tea.Cmd {
	clients := m.api

	return func() tea.Msg {
		result, err := clients.rebaseWorktree(agentID)
		if err != nil {
			return rebasedMsg{name: name, err: err}
		}

		return rebasedMsg{
			name:       name,
			baseRef:    result.GetBaseRef(),
			baseSHA:    result.GetBaseSha(),
			landed:     int(result.GetLandedCommits()),
			replay:     int(result.GetReplayCommits()),
			conflicted: result.GetConflicted(),
			moved:      result.GetMoved(),
		}
	}
}

// applyRefreshPlanned opens the picker, or says why there is nothing to pick between.
func (m *model) applyRefreshPlanned(msg refreshPlannedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.report("Could not work out what a rebase would do", msg.err)

		return m, nil
	}

	return m.openRefreshPicker(msg.plan)
}

// applyRebased reports the outcome.
func (m *model) applyRebased(msg rebasedMsg) tea.Model {
	if msg.err != nil {
		m.report("Could not rebase the worktree of "+msg.name, msg.err)

		return m
	}

	// Neither a success nor a failure: the work is intact and the agent has something to do.
	if msg.conflicted {
		m.pushMessage(message{
			kind:    messageError,
			summary: msg.name + ": the rebase stopped on conflicts",
			detail: fmt.Sprintf("onto %s (%s). The rebase is still in progress in that worktree. %s has been told to settle it.",
				msg.baseRef, shortSHA(msg.baseSHA), msg.name),
		})

		return m
	}

	if msg.moved {
		m.pushMessage(message{
			kind:    messageInfo,
			summary: msg.name + ": moved onto " + msg.baseRef,
			detail: fmt.Sprintf("%s. The base already had its work, so %s were dropped instead of replayed.",
				shortSHA(msg.baseSHA), countWord(msg.landed, "commit", "commits")),
		})

		return m
	}

	m.pushMessage(message{
		kind:    messageInfo,
		summary: msg.name + ": rebased onto " + msg.baseRef,
		detail: fmt.Sprintf("%s. %s replayed.",
			shortSHA(msg.baseSHA), countWord(msg.replay, "commit", "commits")),
	})

	return m
}
