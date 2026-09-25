package ui

import (
	"fmt"
	"time"

	"komrad/internal/git"
	"komrad/internal/ui/paint/label"

	tea "charm.land/bubbletea/v2"
)

const rebaseTimeout = 30 * time.Second

// rebasePlan is what a rebase would do, carried from the server to the picker.
type rebasePlan struct {
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

// rebasePlannedMsg is a plan come back, or the reason there is none.
type rebasePlannedMsg struct {
	plan  rebasePlan
	err   error
	write int
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
	write int
}

// planRebaseCmd asks what a rebase would do.
func (m *model) planRebaseCmd(agentID, name string) tea.Cmd {
	clients := m.api
	write := m.beginWrite()

	return func() tea.Msg {
		plan, err := clients.planRebase(agentID)
		if err != nil {
			return rebasePlannedMsg{err: err, write: write}
		}

		return rebasePlannedMsg{write: write, plan: rebasePlan{
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
	write := m.beginWrite()

	return func() tea.Msg {
		result, err := clients.rebaseWorktree(agentID)
		if err != nil {
			return rebasedMsg{name: name, err: err, write: write}
		}

		return rebasedMsg{
			write:      write,
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

// applyRebasePlanned opens the confirmation, or says why there is nothing to confirm.
func (m *model) applyRebasePlanned(msg rebasePlannedMsg) (tea.Model, tea.Cmd) {
	held := m.landHeld(msg.write)

	if msg.err != nil {
		m.report("Could not work out what a rebase would do", msg.err)

		if held {
			return m.closeForm()
		}

		return m, nil
	}

	return m.openRebaseConfirm(msg.plan)
}

// applyRebased reports the outcome, and takes down the confirmation held for it.
func (m *model) applyRebased(msg rebasedMsg) (tea.Model, tea.Cmd) {
	held := m.landHeld(msg.write)

	m.reportRebased(msg)

	if held {
		return m.closeForm()
	}

	return m, nil
}

func (m *model) reportRebased(msg rebasedMsg) {
	if msg.err != nil {
		m.report("Could not rebase the worktree of "+msg.name, msg.err)

		return
	}

	// Neither a success nor a failure: the work is intact and the agent has something to do.
	if msg.conflicted {
		m.pushMessage(message{
			kind:    messageError,
			summary: msg.name + ": the rebase stopped on conflicts",
			detail: fmt.Sprintf("onto %s (%s). The rebase is still in progress in that worktree. %s has been told to settle it.",
				msg.baseRef, git.ShortSHA(msg.baseSHA), msg.name),
		})

		return
	}

	if msg.moved {
		m.pushMessage(message{
			kind:    messageInfo,
			summary: msg.name + ": moved onto " + msg.baseRef,
			detail: fmt.Sprintf("%s. The base already had its work, so %s were dropped instead of replayed.",
				git.ShortSHA(msg.baseSHA), label.Count(msg.landed, "commit", "commits")),
		})

		return
	}

	m.pushMessage(message{
		kind:    messageInfo,
		summary: msg.name + ": rebased onto " + msg.baseRef,
		detail: fmt.Sprintf("%s. %s replayed.",
			git.ShortSHA(msg.baseSHA), label.Count(msg.replay, "commit", "commits")),
	})
}
