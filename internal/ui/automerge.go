package ui

import (
	"errors"
	"time"

	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

const autoMergeTimeout = 30 * time.Second

var errNoAutoMerge = errors.New("the forge refused to queue it, so merge it now if nothing is left to wait for, or check that the repository allows auto-merge")

// autoMergeLabel is the merge menu entry that turns auto-merge on or off, naming which way it goes.
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
		return errors.New(task.PullRequest.Describe("is a draft, so mark it ready first"))
	case !task.PullRequest.MergeReadiness.AutoMergeable:
		return errNoAutoMerge
	}

	return nil
}

// autoMergeWrite is the write that asks the forge to merge the task's pull request once its
// checks and reviews pass, or to stop waiting to.
func (m *model) autoMergeWrite(task panel.Task, params mergeParams, on bool) screen.Write {
	clients := m.api
	ref := task.PullRequest.Ref()
	doing, said := "cancelling auto-merge on "+ref, " no longer merges on its own"
	if on {
		doing, said = "queueing "+ref+" to merge", " merges when ready"
	}

	return screen.Write{
		Doing:   doing,
		Summary: "Could not change auto-merge on " + ref + " of " + task.Slug,
		Send: func() screen.Landed {
			return screen.Landed{
				Err:    clients.setPullRequestAutoMerge(params, on),
				Notice: &screen.Notice{Summary: task.Slug + ": " + ref + said, Detail: task.PullRequest.Title},
			}
		},
	}
}
