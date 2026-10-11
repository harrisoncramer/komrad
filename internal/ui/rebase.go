package ui

import (
	"fmt"
	"time"

	"komrad/internal/git"
	"komrad/internal/ui/paint/label"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

const rebaseTimeout = 30 * time.Second

// rebasePlan is what a rebase would do, carried from the server to the picker.
type rebasePlan struct {
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

func rebaseOp(taskID, name string) gitOp {
	return gitOp{
		kind:       formAgentRebase,
		planning:   "planning the rebase of " + name,
		planFailed: "Could not work out what a rebase would do",
		plan: func(clients api) (gitConfirmForm, error) {
			response, err := clients.planRebase(taskID)
			if err != nil {
				return nil, err
			}

			plan := rebasePlan{
				name:          name,
				baseRef:       response.GetBaseRef(),
				baseSHA:       response.GetBaseSha(),
				commits:       int(response.GetCommits()),
				landed:        int(response.GetLandedCommits()),
				replay:        int(response.GetReplayCommits()),
				merges:        int(response.GetMerges()),
				conflictPaths: response.GetConflictPaths(),
			}

			return rebaseForm(plan), nil
		},
		running: "rebasing " + name,
		run: func(clients api) gitReport {
			result, err := clients.rebaseWorktree(taskID)

			return func(m *model) tea.Cmd {
				m.reportRebased(name, result, err)

				return nil
			}
		},
	}
}

func (m *model) reportRebased(name string, result *agentv1.RebaseWorktreeResponse, err error) {
	if err != nil {
		m.report("Could not rebase the worktree of "+name, err)

		return
	}

	baseRef, baseSHA := result.GetBaseRef(), result.GetBaseSha()

	// Neither a success nor a failure: the work is intact and the agent has something to do.
	if result.GetConflicted() {
		m.pushMessage(message{
			kind:    messageError,
			summary: name + ": the rebase stopped on conflicts",
			detail: fmt.Sprintf("onto %s (%s). The rebase is still in progress in that worktree. %s has been told to settle it.",
				baseRef, git.ShortSHA(baseSHA), name),
		})

		return
	}

	if result.GetMoved() {
		m.pushMessage(message{
			kind:    messageInfo,
			summary: name + ": moved onto " + baseRef,
			detail: fmt.Sprintf("%s. The base already had its work, so %s were dropped instead of replayed.",
				git.ShortSHA(baseSHA), label.Count(int(result.GetLandedCommits()), "commit", "commits")),
		})

		return
	}

	m.pushMessage(message{
		kind:    messageInfo,
		summary: name + ": rebased onto " + baseRef,
		detail: fmt.Sprintf("%s. %s replayed.",
			git.ShortSHA(baseSHA), label.Count(int(result.GetReplayCommits()), "commit", "commits")),
	})
}
