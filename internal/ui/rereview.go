package ui

import (
	"errors"
	"time"

	"komrad/internal/ui/panel"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

const rereviewTimeout = 30 * time.Second

// rereviewedMsg is a request for another review come back.
type rereviewedMsg struct {
	name  string
	ref   string
	title string
	err   error
	write int
}

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
		m.report("Could not ask for another review of the "+task.PullRequest.Noun()+" of "+task.Slug, err)

		return nil
	}

	clients := m.api
	standing := task.PullRequest
	rereviewed := rereviewedMsg{
		name:  task.Slug,
		ref:   standing.Ref(),
		title: standing.Title,
	}
	rereviewed.write = m.beginWrite("asking for another review of " + standing.Ref())

	return func() tea.Msg {
		rereviewed.err = clients.requestPullRequestRereview(standing.URL)

		return rereviewed
	}
}

// applyRereviewed reports the request for another review and takes down the menu held for it.
func (m *model) applyRereviewed(msg rereviewedMsg) tea.Cmd {
	held := m.landHeld(msg.write)

	if msg.err != nil {
		m.report("Could not ask for another review of "+msg.ref+" of "+msg.name, msg.err)
	} else {
		m.pushMessage(message{
			kind:    messageInfo,
			summary: msg.name + ": " + msg.ref + " is waiting on another review",
			detail:  msg.title,
		})
	}

	if !held {
		return nil
	}

	return m.closeForm()
}
