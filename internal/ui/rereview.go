package ui

import (
	"errors"
	"fmt"
	"time"

	"komrad/internal/ui/panel"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

const rereviewTimeout = 30 * time.Second

var errNoChangesRequested = errors.New("nobody has asked for changes on its pull request")

// rereviewedMsg is a request for another review come back.
type rereviewedMsg struct {
	name   string
	number int32
	title  string
	err    error
	write  int
}

// rereviewRefusal is why the agent's pull request has nobody to ask for another review, nil when
// it has.
func rereviewRefusal(item panel.AgentRow) error {
	if err := openPullRequestRefusal(item); err != nil {
		return err
	}

	state := item.PullRequest.State
	if state != pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED && state != pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED {
		return errNoChangesRequested
	}

	return nil
}

// requestRereview asks the forge to put the agent's pull request back in front of everyone who
// asked for changes on it, or says why it cannot.
func (m *model) requestRereview(item panel.AgentRow) tea.Cmd {
	if err := rereviewRefusal(item); err != nil {
		m.report("Could not ask for another review of the pull request of "+item.Agent.Name, err)

		return nil
	}

	clients := m.api
	standing := item.PullRequest
	rereviewed := rereviewedMsg{
		name:   item.Agent.Name,
		number: standing.Number,
		title:  standing.Title,
	}
	rereviewed.write = m.beginWrite(fmt.Sprintf("asking for another review of #%d", standing.Number))

	return func() tea.Msg {
		rereviewed.err = clients.requestPullRequestRereview(standing.URL)

		return rereviewed
	}
}

// applyRereviewed reports the request for another review and takes down the menu held for it.
func (m *model) applyRereviewed(msg rereviewedMsg) tea.Cmd {
	held := m.landHeld(msg.write)

	if msg.err != nil {
		m.report(fmt.Sprintf("Could not ask for another review of #%d of %s", msg.number, msg.name), msg.err)
	} else {
		m.pushMessage(message{
			kind:    messageInfo,
			summary: fmt.Sprintf("%s: #%d is waiting on another review", msg.name, msg.number),
			detail:  msg.title,
		})
	}

	if !held {
		return nil
	}

	return m.closeForm()
}
