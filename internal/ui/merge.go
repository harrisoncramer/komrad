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
	mergeChoiceAutoMerge
)

var (
	errAlreadyMerged     = errors.New("its pull request has already merged")
	errPullRequestClosed = errors.New("its pull request was closed without merging")
	errArchiveArchived   = errors.New("an archived agent is already archived")
)

// mergeMenu is the ways to merge an agent's pull request, opened from the git menu.
func mergeMenu(m *model, from screen, item panel.AgentRow) submenu {
	agentID := item.Agent.ID
	refused := openPullRequestRefusal(item) != nil
	run := func(choice mergeChoice) func(model) (model, tea.Cmd) {
		return func(m model) (model, tea.Cmd) { return m.runMerge(from, agentID, choice) }
	}

	return submenu{
		kind:  formMerge,
		title: "merge",
		entries: []submenuEntry{
			{
				label: "merge",
				keys:  m.keys.Merge.Merge,
				isOff: refused,
				write: run(mergeChoiceMerge),
			},
			{
				label: "merge and archive",
				keys:  m.keys.Merge.MergeArchive,
				isOff: refused || from == screenArchive,
				write: run(mergeChoiceMergeArchive),
			},
			{
				label: autoMergeLabel(item),
				keys:  m.keys.Merge.AutoMerge,
				isOff: autoMergeRefusal(item) != nil,
				write: run(mergeChoiceAutoMerge),
			},
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

	if choice == mergeChoiceAutoMerge {
		return m.toggleAutoMerge(item)
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

	cmd := m.runGitCmd(mergeOp(item, archive))

	return m, cmd
}

func mergeOp(item panel.AgentRow, archive bool) gitOp {
	return gitOp{
		running: fmt.Sprintf("merging #%d", item.PullRequest.Number),
		run: func(clients api) gitReport {
			err := clients.mergePullRequest(item.PullRequest.URL)

			return func(m *model) tea.Cmd {
				return m.reportMerged(item, archive, err)
			}
		},
	}
}

func (m *model) reportMerged(item panel.AgentRow, archive bool, err error) tea.Cmd {
	standing := item.PullRequest
	if err != nil {
		m.report(fmt.Sprintf("Could not merge #%d of %s", standing.Number, item.Agent.Name), err)

		return nil
	}

	m.pushMessage(message{
		kind:    messageInfo,
		summary: fmt.Sprintf("%s: merged #%d", item.Agent.Name, standing.Number),
		detail:  standing.Title,
	})

	live, ok := m.listing.liveAgent(item.Agent.ID)
	if !ok || !archive {
		return nil
	}

	archived, archiving := m.archiveAgents([]panel.Agent{live.Agent})
	*m = archived

	return archiving
}
