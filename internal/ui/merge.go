package ui

import (
	"errors"
	"fmt"
	"time"

	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

const mergeTimeout = 30 * time.Second

// mergeChoice is one entry of the merge menu.
type mergeChoice int

const (
	mergeChoiceMerge mergeChoice = iota
	mergeChoiceMergeArchive
)

var (
	errAlreadyMerged     = errors.New("its pull request has already merged")
	errPullRequestClosed = errors.New("its pull request was closed without merging")
	errArchiveArchived   = errors.New("an archived agent is already archived")
)

// mergedMsg is a merge come back, with whether the agent is to be archived now.
type mergedMsg struct {
	agentID string
	name    string
	number  int32
	title   string
	archive bool
	err     error
	write   int
}

// mergeMenu is the ways to merge an agent's pull request, opened from the git menu.
func mergeMenu(s panelShared, from screen, item panel.AgentRow) submenu {
	agentID := item.Agent.ID
	refused := openPullRequestRefusal(item) != nil

	return submenu{
		kind:  formMerge,
		title: "merge",
		crumb: "merge",
		entries: []submenuEntry{
			{
				label: "merge",
				keys:  s.keys.Merge.Merge,
				isOff: refused,
			},
			{
				label: "merge and archive",
				keys:  s.keys.Merge.MergeArchive,
				isOff: refused || from == screenArchive,
			},
		},
		answer: func(m model, choice int) (tea.Model, tea.Cmd) {
			return confirmSubmit(func(m model) (model, tea.Cmd) { return m.runMerge(from, agentID, mergeChoice(choice)) })(m)
		},
	}
}

// runMerge asks the forge to merge the agent's pull request, or says why it cannot.
func (m model) runMerge(from screen, agentID string, choice mergeChoice) (model, tea.Cmd) {
	// The list may have moved under the open menu.
	item, ok := m.listing.liveAgent(agentID)
	if from == screenArchive {
		item, ok = m.listing.anyAgent(agentID)
	}

	if !ok {
		m.report("Could not act on the agent", errAgentGone)

		return m, nil
	}

	archive := choice == mergeChoiceMergeArchive
	if err := openPullRequestRefusal(item); err != nil {
		m.report("Could not merge the pull request of "+item.Agent.Name, err)

		return m, nil
	}
	if archive && from == screenArchive {
		m.report("Could not archive "+item.Agent.Name, errArchiveArchived)

		return m, nil
	}

	clients := m.api
	write := m.beginWrite()
	standing := item.PullRequest
	merged := mergedMsg{
		agentID: agentID,
		name:    item.Agent.Name,
		number:  standing.Number,
		title:   standing.Title,
		archive: archive,
		write:   write,
	}

	return m, func() tea.Msg {
		merged.err = clients.mergePullRequest(standing.URL)

		return merged
	}
}

// applyMerged reports the merge, archives the agent when that was asked for, and takes down the
// menu held for it.
func (m model) applyMerged(msg mergedMsg) (tea.Model, tea.Cmd) {
	held := m.landHeld(msg.write)

	var archive tea.Cmd
	if msg.err != nil {
		m.report(fmt.Sprintf("Could not merge #%d of %s", msg.number, msg.name), msg.err)
	} else {
		m.pushMessage(message{
			kind:    messageInfo,
			summary: fmt.Sprintf("%s: merged #%d", msg.name, msg.number),
			detail:  msg.title,
		})

		if item, ok := m.listing.liveAgent(msg.agentID); ok && msg.archive {
			m, archive = m.archiveAgents([]panel.Agent{item.Agent})
		}
	}

	if !held {
		return m, archive
	}

	next, back := m.closeForm()

	return next, tea.Batch(archive, back)
}
