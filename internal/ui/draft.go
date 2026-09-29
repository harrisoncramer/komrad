package ui

import (
	"fmt"
	"time"

	"komrad/internal/ui/panel"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

const draftTimeout = 30 * time.Second

// draftedMsg is a draft change come back.
type draftedMsg struct {
	name   string
	number int32
	title  string
	draft  bool
	err    error
	write  int
}

// isDraft says whether the agent's pull request is a draft.
func isDraft(item panel.AgentRow) bool {
	return item.PullRequest.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT
}

// draftLabel is the git menu entry that toggles the draft, naming which way it goes.
func draftLabel(item panel.AgentRow) string {
	if isDraft(item) {
		return "toggle draft off"
	}

	return "toggle draft on"
}

// draftRefusal is why an agent's pull request cannot be toggled, nil when it can.
func draftRefusal(item panel.AgentRow) error {
	if item.PullRequest.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED {
		return errAlreadyMerged
	}
	if item.PullRequest.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED {
		return errPullRequestClosed
	}

	if item.PullRequest.URL == "" {
		return pullRequestRefusal(item)
	}

	return nil
}

// toggleDraft asks the forge to flip whether the agent's pull request is a draft, or says why it
// cannot.
func (m model) toggleDraft(item panel.AgentRow) (model, tea.Cmd) {
	if err := draftRefusal(item); err != nil {
		m.report("Could not change the pull request of "+item.Agent.Name, err)

		return m, nil
	}

	clients := m.api
	standing := item.PullRequest
	drafted := draftedMsg{
		name:   item.Agent.Name,
		number: standing.Number,
		title:  standing.Title,
		draft:  !isDraft(item),
		write:  m.beginWrite(),
	}

	return m, func() tea.Msg {
		drafted.err = clients.setPullRequestDraft(standing.URL, drafted.draft)

		return drafted
	}
}

// applyDrafted reports the draft change and takes down the menu held for it.
func (m model) applyDrafted(msg draftedMsg) (tea.Model, tea.Cmd) {
	held := m.landHeld(msg.write)

	switch {
	case msg.err != nil:
		m.report(fmt.Sprintf("Could not change #%d of %s", msg.number, msg.name), msg.err)
	case msg.draft:
		m.pushMessage(message{
			kind:    messageInfo,
			summary: fmt.Sprintf("%s: #%d is a draft", msg.name, msg.number),
			detail:  msg.title,
		})
	default:
		m.pushMessage(message{
			kind:    messageInfo,
			summary: fmt.Sprintf("%s: #%d is ready for review", msg.name, msg.number),
			detail:  msg.title,
		})
	}

	if !held {
		return m, nil
	}

	return m.closeForm()
}
