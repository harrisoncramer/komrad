package ui

import (
	"errors"
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
	mergeChoiceMergeRebase
)

var errArchiveArchived = errors.New("an archived agent is already archived")

// mergeMenu is the ways to merge an agent's pull request, opened from the git menu.
func mergeMenu(m *model, openedFrom screen, item panel.AgentRow) submenu {
	agentID := item.Agent.ID
	refused := openPullRequestRefusal(item) != nil
	run := func(choice mergeChoice) func(*model) tea.Cmd {
		return func(m *model) tea.Cmd { return m.runMerge(openedFrom, agentID, choice) }
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
				isOff: refused || openedFrom == screenArchive,
				write: run(mergeChoiceMergeArchive),
			},
			{
				label: autoMergeLabel(item),
				keys:  m.keys.Merge.AutoMerge,
				isOff: autoMergeRefusal(item) != nil,
				write: run(mergeChoiceAutoMerge),
			},
			{
				label: "merge then rebase",
				keys:  m.keys.Merge.MergeRebase,
				isOff: mergeRebaseRefusal(m.listing, openedFrom, item) != nil,
				write: run(mergeChoiceMergeRebase),
			},
		},
	}
}

// runMerge asks the forge to merge the agent's pull request, or says why it cannot.
func (m *model) runMerge(openedFrom screen, agentID string, choice mergeChoice) tea.Cmd {
	// The list may have moved under the open menu.
	item, ok := m.listing.liveAgent(agentID)
	if openedFrom == screenArchive {
		item, ok = m.listing.anyAgent(agentID)
	}

	if !ok {
		m.report("Could not act on the agent", errAgentGone)

		return nil
	}

	if choice == mergeChoiceAutoMerge {
		return m.toggleAutoMerge(item)
	}

	if choice == mergeChoiceMergeRebase {
		if err := mergeRebaseRefusal(m.listing, openedFrom, item); err != nil {
			m.report("Could not merge and rebase "+item.Agent.Name, err)

			return nil
		}

		return m.runGitCmd(mergeRebaseOp(item))
	}

	archive := choice == mergeChoiceMergeArchive
	if err := openPullRequestRefusal(item); err != nil {
		m.report("Could not merge the "+item.PullRequest.Noun()+" of "+item.Agent.Name, err)

		return nil
	}
	if archive && openedFrom == screenArchive {
		m.report("Could not archive "+item.Agent.Name, errArchiveArchived)

		return nil
	}

	return m.runGitCmd(mergeOp(item, archive))
}

func mergeOp(item panel.AgentRow, archive bool) gitOp {
	return gitOp{
		running: "merging " + item.PullRequest.Ref(),
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
		m.report("Could not merge "+standing.Ref()+" of "+item.Agent.Name, err)

		return nil
	}

	m.pushMessage(message{
		kind:    messageInfo,
		summary: item.Agent.Name + ": merged " + standing.Ref(),
		detail:  standing.Title,
	})

	live, ok := m.listing.liveAgent(item.Agent.ID)
	if !ok || !archive {
		return nil
	}

	return m.archiveAgents([]panel.Agent{live.Agent})
}

func mergeRebaseOp(item panel.AgentRow) gitOp {
	agentID, name := item.Agent.ID, item.Agent.Name

	return gitOp{
		running: "merging " + item.PullRequest.Ref() + " and rebasing " + name,
		run: func(clients api) gitReport {
			if err := clients.mergePullRequest(item.PullRequest.URL); err != nil {
				return func(m *model) tea.Cmd {
					return m.reportMerged(item, false, err)
				}
			}

			result, err := clients.rebaseWorktree(agentID)

			return func(m *model) tea.Cmd {
				m.reportMerged(item, false, nil)
				m.reportRebased(name, result, err)

				return nil
			}
		},
	}
}
