package ui

import (
	"fmt"
	"time"

	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/screen"
	"komrad/lib/xsha"
	agentv1 "komrad/proto/gen/agent/v1"
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

func rebaseOp(taskID, name string, ontoBase bool) gitOp {
	return gitOp{
		planning:   "planning the rebase of " + name,
		planFailed: "Could not work out what a rebase would do",
		plan: func(clients api) (gitConfirmForm, error) {
			response, err := clients.planRebase(taskID, ontoBase)
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
			result, err := clients.rebaseWorktree(taskID, ontoBase)

			return func(*model) screen.Request { return reportRebased(name, result, err) }
		},
	}
}

func reportRebased(name string, result *agentv1.RebaseWorktreeResponse, err error) screen.Request {
	if err != nil {
		return screen.Report{Summary: "Could not rebase the worktree of " + name, Err: err}
	}

	baseRef, baseSHA := result.GetBaseRef(), result.GetBaseSha()

	// Neither a success nor a failure: the work is intact and the agent has something to do.
	if result.GetConflicted() {
		return screen.Note{
			Kind:    screen.Error,
			Summary: name + ": the rebase stopped on conflicts",
			Detail: fmt.Sprintf("onto %s (%s). The rebase is still in progress in that worktree, and komrad told %s to settle it.",
				baseRef, xsha.Short(baseSHA), name),
		}
	}

	if result.GetMoved() {
		return screen.Note{
			Kind:    screen.Info,
			Summary: name + ": moved onto " + baseRef,
			Detail: fmt.Sprintf("%s. The base already had its work, so komrad dropped %s instead of replaying them.",
				xsha.Short(baseSHA), label.Count(int(result.GetLandedCommits()), "commit", "commits")),
		}
	}

	return screen.Note{
		Kind:    screen.Info,
		Summary: name + ": rebased onto " + baseRef,
		Detail: fmt.Sprintf("%s. Komrad replayed %s.",
			xsha.Short(baseSHA), label.Count(int(result.GetReplayCommits()), "commit", "commits")),
	}
}
