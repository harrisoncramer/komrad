package gitmenu

import (
	"errors"

	"komrad/internal/ui/api"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

var errNoAutoMerge = errors.New("the forge refused to queue it, so merge it now if nothing is left to wait for, or check that the repository allows auto-merge")

// autoMergeLabel is the forge menu entry that turns auto-merge on or off, naming which way it goes.
func autoMergeLabel(task panel.Task) string {
	if task.PullRequest.MergeReadiness.AutoMerge {
		return "cancel auto-merge"
	}

	return "merge when ready"
}

// autoMergeRefusal says why the user cannot change auto-merge on the task's pull request, nil
// when they can, which they always can while it is on.
func autoMergeRefusal(task panel.Task) error {
	if err := openPullRequestRefusal(task); err != nil {
		return err
	}

	switch {
	case task.PullRequest.MergeReadiness.AutoMerge:
		return nil
	case isDraft(task):
		return draftRefusal(task)
	case !task.PullRequest.MergeReadiness.AutoMergeable:
		return errNoAutoMerge
	}

	return nil
}

// autoMergeWrite is the write that asks the forge to merge the task's pull request once its
// checks and reviews pass, or to stop waiting to.
func (g Menus) autoMergeWrite(task panel.Task, params api.MergeParams, on bool) screen.Write {
	clients := g.deps.Clients
	ref := task.PullRequest.Ref()
	doing, said := "cancelling auto-merge on "+ref, " no longer merges on its own"
	if on {
		doing, said = "queueing "+ref+" to merge", " merges when ready"
	}

	return screen.Write{
		InFlightText:   doing,
		FailureSummary: "Could not change auto-merge on " + ref + " of " + task.Slug,
		Send: func() screen.WriteResult {
			return screen.WriteResult{
				Err:           clients.SetPullRequestAutoMerge(params, on),
				WaitFor:       screen.WaitFor{Shows: showsTask(task.ID, func(task panel.Task) bool { return task.PullRequest.MergeReadiness.AutoMerge == on })},
				SuccessNotice: &screen.Notice{Summary: task.Slug + ": " + ref + said, Detail: task.PullRequest.Title},
			}
		},
	}
}
