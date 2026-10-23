package gitmenu

import (
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/screen"
	"komrad/lib/xsha"
	agentv1 "komrad/proto/gen/agent/v1"

	"charm.land/huh/v2"
)

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

func rebaseOp(taskID, name string, ontoBase bool) operation {
	return operation{
		planning:   "planning the rebase of " + name,
		planFailed: "Could not work out what a rebase would do",
		plan: func(clients api.Clients) (confirmForm, error) {
			response, err := clients.PlanRebase(taskID, ontoBase)
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
		run: func(clients api.Clients) report {
			result, err := clients.RebaseWorktree(taskID, ontoBase)

			return func(screen.Inputs) screen.Request { return reportRebased(name, result, err) }
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
		return screen.PostMessage{
			MessageKind: screen.Error,
			Summary:     name + ": the rebase stopped on conflicts",
			Detail: fmt.Sprintf("onto %s (%s). The rebase is still in progress in that worktree, and komrad told %s to settle it.",
				baseRef, xsha.Short(baseSHA), name),
		}
	}

	if result.GetMoved() {
		return screen.PostMessage{
			MessageKind: screen.Info,
			Summary:     name + ": moved onto " + baseRef,
			Detail: fmt.Sprintf("%s. The base already had its work, so komrad dropped %s instead of replaying them.",
				xsha.Short(baseSHA), label.Count(int(result.GetLandedCommits()), "commit", "commits")),
		}
	}

	return screen.PostMessage{
		MessageKind: screen.Info,
		Summary:     name + ": rebased onto " + baseRef,
		Detail: fmt.Sprintf("%s. Komrad replayed %s.",
			xsha.Short(baseSHA), label.Count(int(result.GetReplayCommits()), "commit", "commits")),
	}
}

// rebaseForm asks before a worktree is brought up to date, saying what will happen to the
// branch and to the files.
func rebaseForm(plan rebasePlan) confirmForm {
	return func(fixed config.FixedKeys) (*huh.Form, *bool) {
		return form.Confirm(
			"Bring the worktree of "+plan.name+" up to date?",
			rebaseDescription(plan),
			rebaseAffirmative(plan),
			fixed,
		)
	}
}

// rebaseDescription says what the operation will do, in the order somebody deciding needs it:
// what happens to the branch, then what happens to the files, then what might go wrong.
func rebaseDescription(plan rebasePlan) string {
	lines := []string{}

	// A dirty worktree never reaches here.
	switch {
	case plan.replay == 0 && plan.landed == 0:
		lines = append(lines, fmt.Sprintf("This branch has no commits of its own. Komrad moves it to %s.", plan.baseRef))
	case plan.replay == 0:
		lines = append(lines, fmt.Sprintf("The base already holds every commit on this branch. Komrad moves the branch to %s "+
			"and drops %s from it.", plan.baseRef, label.Count(plan.landed, "commit", "commits")))
	default:
		lines = append(lines, fmt.Sprintf("Komrad replays the branch's %s onto %s.",
			label.Count(plan.replay, "commit", "commits"), plan.baseRef))
		if plan.landed > 0 {
			lines = append(lines, fmt.Sprintf("It drops %d more that the base already holds.", plan.landed))
		}
	}

	lines = append(lines, "The worktree's files change, but komrad keeps every untracked file. Komrad saves a copy of "+
		"the worktree first, so you can get it back.")

	if warning := rebaseWarning(plan); warning != "" {
		lines = append(lines, warning)
	}

	return strings.Join(lines, " ")
}

// rebaseAffirmative names the button for what will actually run.
func rebaseAffirmative(plan rebasePlan) string {
	if plan.replay == 0 {
		return "Move"
	}

	return "Rebase"
}

// rebaseWarning is the trouble a rebase would run into that the description cannot say from
// the commit counts alone.
func rebaseWarning(plan rebasePlan) string {
	warnings := []string{}

	// The landed commits are not warned about here.
	if plan.merges > 0 {
		warnings = append(warnings, fmt.Sprintf("A rebase throws away %s and replays what they settled.",
			label.Count(plan.merges, "merge commit", "merge commits")))
	}
	if len(plan.conflictPaths) > 0 {
		warnings = append(warnings, "Both sides changed "+namedPaths(plan.conflictPaths, 3)+".")
	}

	return strings.Join(warnings, " ")
}

// namedPaths is a few paths read out.
func namedPaths(paths []string, most int) string {
	if len(paths) <= most {
		return strings.Join(paths, ", ")
	}

	return fmt.Sprintf("%s and %d more", strings.Join(paths[:most], ", "), len(paths)-most)
}
