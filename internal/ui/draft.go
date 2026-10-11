package ui

import (
	"time"

	"komrad/internal/ui/panel"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

const draftTimeout = 30 * time.Second

// draftedMsg is a draft change come back.
type draftedMsg struct {
	name  string
	ref   string
	title string
	draft bool
	err   error
	write int
}

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
func (m *model) toggleDraft(task panel.Task) tea.Cmd {
	if err := openPullRequestRefusal(task); err != nil {
		m.report("Could not change the "+task.PullRequest.Noun()+" of "+task.Slug, err)

		return nil
	}

	clients := m.api
	standing := task.PullRequest
	drafted := draftedMsg{
		name:  task.Slug,
		ref:   standing.Ref(),
		title: standing.Title,
		draft: !isDraft(task),
	}
	drafted.write = m.beginWrite(draftingLabel(drafted))

	return func() tea.Msg {
		drafted.err = clients.setPullRequestDraft(standing.URL, drafted.draft)

		return drafted
	}
}

// applyDrafted reports the draft change and takes down the menu held for it.
func (m *model) applyDrafted(msg draftedMsg) tea.Cmd {
	held := m.landHeld(msg.write)

	switch {
	case msg.err != nil:
		m.report("Could not change "+msg.ref+" of "+msg.name, msg.err)
	case msg.draft:
		m.pushMessage(message{
			kind:    messageInfo,
			summary: msg.name + ": " + msg.ref + " is a draft",
			detail:  msg.title,
		})
	default:
		m.pushMessage(message{
			kind:    messageInfo,
			summary: msg.name + ": " + msg.ref + " is ready for review",
			detail:  msg.title,
		})
	}

	if !held {
		return nil
	}

	return m.closeForm()
}

func draftingLabel(drafted draftedMsg) string {
	if drafted.draft {
		return "marking " + drafted.ref + " a draft"
	}

	return "marking " + drafted.ref + " ready"
}
