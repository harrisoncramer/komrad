package gitmenu

import (
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// isDraft says whether the task's pull request is a draft.
func isDraft(task panel.Task) bool {
	return task.PullRequest.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT
}

// draftLabel is the forge menu entry that toggles the draft, naming which way it goes.
func draftLabel(task panel.Task) string {
	if isDraft(task) {
		return "toggle draft off"
	}

	return "toggle draft on"
}

// toggleDraft asks the forge to flip whether the task's pull request is a draft, or says why it
// cannot.
func (g Menus) toggleDraft(task panel.Task) screen.Request {
	if err := openPullRequestRefusal(task); err != nil {
		return screen.Report{Summary: "Could not change the " + task.PullRequest.Noun() + " of " + task.Slug, Err: err}
	}

	clients := g.deps.Clients
	standing, draft := task.PullRequest, !isDraft(task)
	doing, said := "marking "+standing.Ref()+" ready", " is ready for review"
	if draft {
		doing, said = "marking "+standing.Ref()+" a draft", " is a draft"
	}

	return screen.Write{
		InFlightText:   doing,
		FailureSummary: "Could not change " + standing.Ref() + " of " + task.Slug,
		Send: func() screen.WriteResult {
			return screen.WriteResult{
				Err:           clients.SetPullRequestDraft(standing.URL, draft),
				SuccessNotice: &screen.Notice{Summary: task.Slug + ": " + standing.Ref() + said, Detail: standing.Title},
			}
		},
	}
}
