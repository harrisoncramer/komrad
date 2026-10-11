package ui

import (
	"errors"
	"time"

	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

const autoMergeTimeout = 30 * time.Second

var errNoAutoMerge = errors.New("the forge will not queue it now: merge it if nothing is left to wait for, or check the repository allows auto-merge")

// autoMergedMsg is an auto-merge change come back.
type autoMergedMsg struct {
	name  string
	ref   string
	title string
	on    bool
	err   error
	write int
}

// autoMergeLabel is the merge menu entry that turns auto-merge on or off, naming which way it goes.
func autoMergeLabel(task panel.Task) string {
	if task.PullRequest.MergeReadiness.AutoMerge {
		return "cancel auto-merge"
	}

	return "merge when ready"
}

// autoMergeRefusal says why auto-merge cannot be changed on the task's pull request, nil when it
// can, which it always can be while it is on.
func autoMergeRefusal(task panel.Task) error {
	if err := openPullRequestRefusal(task); err != nil {
		return err
	}

	switch {
	case task.PullRequest.MergeReadiness.AutoMerge:
		return nil
	case isDraft(task):
		return errors.New(describePullRequest(task.PullRequest, "is a draft, so mark it ready first"))
	case !task.PullRequest.MergeReadiness.AutoMergeable:
		return errNoAutoMerge
	}

	return nil
}

// toggleAutoMerge asks the forge to merge the task's pull request once its checks and reviews
// pass, or to stop waiting to, or says why it cannot.
func (m *model) toggleAutoMerge(task panel.Task) tea.Cmd {
	if err := autoMergeRefusal(task); err != nil {
		m.report("Could not change auto-merge on the "+task.PullRequest.Noun()+" of "+task.Slug, err)

		return nil
	}

	clients := m.api
	standing := task.PullRequest
	autoMerged := autoMergedMsg{
		name:  task.Slug,
		ref:   standing.Ref(),
		title: standing.Title,
		on:    !standing.MergeReadiness.AutoMerge,
	}
	autoMerged.write = m.beginWrite(autoMergingLabel(autoMerged))

	return func() tea.Msg {
		autoMerged.err = clients.setPullRequestAutoMerge(standing.URL, standing.HeadSHA, autoMerged.on)

		return autoMerged
	}
}

// applyAutoMerged reports the auto-merge change and takes down the menu held for it.
func (m *model) applyAutoMerged(msg autoMergedMsg) tea.Cmd {
	held := m.landHeld(msg.write)

	switch {
	case msg.err != nil:
		m.report("Could not change auto-merge on "+msg.ref+" of "+msg.name, msg.err)
	case msg.on:
		m.pushMessage(message{
			kind:    messageInfo,
			summary: msg.name + ": " + msg.ref + " merges when ready",
			detail:  msg.title,
		})
	default:
		m.pushMessage(message{
			kind:    messageInfo,
			summary: msg.name + ": " + msg.ref + " no longer merges on its own",
			detail:  msg.title,
		})
	}

	if !held {
		return nil
	}

	return m.closeForm()
}

func autoMergingLabel(autoMerged autoMergedMsg) string {
	if autoMerged.on {
		return "queueing " + autoMerged.ref + " to merge"
	}

	return "cancelling auto-merge on " + autoMerged.ref
}
