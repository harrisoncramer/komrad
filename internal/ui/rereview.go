package ui

import (
	"errors"
	"time"

	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

const rereviewTimeout = 30 * time.Second

// rereviewRefusal is why the task's pull request has nobody to ask for another review, nil when
// it has.
func rereviewRefusal(task panel.Task) error {
	if err := openPullRequestRefusal(task); err != nil {
		return err
	}

	state := task.PullRequest.State
	if state != pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED && state != pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED {
		return errors.New("nobody has asked for changes on its " + task.PullRequest.Noun())
	}

	return nil
}

// requestRereview asks the forge to put the task's pull request back in front of everyone who
// asked for changes on it, or says why it cannot.
func (m *model) requestRereview(task panel.Task) tea.Cmd {
	if err := rereviewRefusal(task); err != nil {
		return m.apply(screen.Report{Summary: "Could not ask for another review of the " + task.PullRequest.Noun() + " of " + task.Slug, Err: err})
	}

	clients := m.api
	standing := task.PullRequest

	return m.apply(screen.Write{
		Doing:   "asking for another review of " + standing.Ref(),
		Summary: "Could not ask for another review of " + standing.Ref() + " of " + task.Slug,
		Send: func() screen.Landed {
			return screen.Landed{
				Err:    clients.requestPullRequestRereview(standing.URL),
				Notice: &screen.Notice{Summary: task.Slug + ": " + standing.Ref() + " is waiting on another review", Detail: standing.Title},
			}
		},
	})
}
