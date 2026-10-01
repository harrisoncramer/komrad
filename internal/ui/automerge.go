package ui

import (
	"errors"
	"fmt"
	"time"

	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

const autoMergeTimeout = 30 * time.Second

var (
	errAutoMergeDraft = errors.New("its pull request is a draft, so mark it ready first")
	errNoAutoMerge    = errors.New("the forge will not queue it now: merge it if nothing is left to wait for, or check the repository allows auto-merge")
)

// autoMergedMsg is an auto-merge change come back.
type autoMergedMsg struct {
	name   string
	number int32
	title  string
	on     bool
	err    error
	write  int
}

// autoMergeLabel is the merge menu entry that turns auto-merge on or off, naming which way it goes.
func autoMergeLabel(item panel.AgentRow) string {
	if item.PullRequest.AutoMerge {
		return "cancel auto-merge"
	}

	return "merge when ready"
}

// autoMergeRefusal says why auto-merge cannot be changed on the agent's pull request, nil when it
// can, which it always can be while it is on.
func autoMergeRefusal(item panel.AgentRow) error {
	if err := openPullRequestRefusal(item); err != nil {
		return err
	}

	switch {
	case item.PullRequest.AutoMerge:
		return nil
	case isDraft(item):
		return errAutoMergeDraft
	case !item.PullRequest.AutoMergeable:
		return errNoAutoMerge
	}

	return nil
}

// toggleAutoMerge asks the forge to merge the agent's pull request once its checks and reviews
// pass, or to stop waiting to, or says why it cannot.
func (m model) toggleAutoMerge(item panel.AgentRow) (model, tea.Cmd) {
	if err := autoMergeRefusal(item); err != nil {
		m.report("Could not change auto-merge on the pull request of "+item.Agent.Name, err)

		return m, nil
	}

	clients := m.api
	standing := item.PullRequest
	autoMerged := autoMergedMsg{
		name:   item.Agent.Name,
		number: standing.Number,
		title:  standing.Title,
		on:     !standing.AutoMerge,
	}
	autoMerged.write = m.beginWrite(autoMergingLabel(autoMerged))

	return m, func() tea.Msg {
		autoMerged.err = clients.setPullRequestAutoMerge(standing.URL, standing.HeadSHA, autoMerged.on)

		return autoMerged
	}
}

// applyAutoMerged reports the auto-merge change and takes down the menu held for it.
func (m model) applyAutoMerged(msg autoMergedMsg) (tea.Model, tea.Cmd) {
	held := m.landHeld(msg.write)

	switch {
	case msg.err != nil:
		m.report(fmt.Sprintf("Could not change auto-merge on #%d of %s", msg.number, msg.name), msg.err)
	case msg.on:
		m.pushMessage(message{
			kind:    messageInfo,
			summary: fmt.Sprintf("%s: #%d merges when ready", msg.name, msg.number),
			detail:  msg.title,
		})
	default:
		m.pushMessage(message{
			kind:    messageInfo,
			summary: fmt.Sprintf("%s: #%d no longer merges on its own", msg.name, msg.number),
			detail:  msg.title,
		})
	}

	if !held {
		return m, nil
	}

	return m.closeForm()
}

func autoMergingLabel(autoMerged autoMergedMsg) string {
	if autoMerged.on {
		return fmt.Sprintf("queueing #%d to merge", autoMerged.number)
	}

	return fmt.Sprintf("cancelling auto-merge on #%d", autoMerged.number)
}
