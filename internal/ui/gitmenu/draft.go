package ui

import (
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// isDraft says whether the task's pull request is a draft.
func isDraft(task panel.Task) bool {
	return task.PullRequest.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT
}

// draftLabel is the git menu entry that toggles the draft, naming which way it goes.
func draftLabel(task panel.Task) string {
	if isDraft(task) {
		return "toggle draft off"
	}

	return "toggle draft on"
}

// toggleDraft asks the forge to flip whether the task's pull request is a draft, or says why it
// cannot.
func (m *model) toggleDraft(task panel.Task) screen.Request {
	if err := openPullRequestRefusal(task); err != nil {
		return screen.Report{Summary: "Could not change the " + task.PullRequest.Noun() + " of " + task.Slug, Err: err}
	}

	clients := m.api
	standing, draft := task.PullRequest, !isDraft(task)
	doing, said := "marking "+standing.Ref()+" ready", " is ready for review"
	if draft {
		doing, said = "marking "+standing.Ref()+" a draft", " is a draft"
	}

	return screen.Write{
		Doing:   doing,
		Summary: "Could not change " + standing.Ref() + " of " + task.Slug,
		Send: func() screen.Landed {
			return screen.Landed{
				Err:    clients.SetPullRequestDraft(standing.URL, draft),
				Notice: &screen.Notice{Summary: task.Slug + ": " + standing.Ref() + said, Detail: standing.Title},
			}
		},
	}
}
