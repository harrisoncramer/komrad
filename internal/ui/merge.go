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

var errArchiveArchived = errors.New("an archived task is already archived")

// mergeMenu is the ways to merge a task's pull request, opened from the git menu.
func mergeMenu(m *model, task panel.Task) submenu {
	taskID := task.ID
	refused := openPullRequestRefusal(task) != nil
	run := func(choice mergeChoice) func(*model) tea.Cmd {
		return func(m *model) tea.Cmd { return m.runMerge(taskID, choice) }
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
				isOff: refused || task.ArchivedAt != nil,
				write: run(mergeChoiceMergeArchive),
			},
			{
				label: autoMergeLabel(task),
				keys:  m.keys.Merge.AutoMerge,
				isOff: autoMergeRefusal(task) != nil,
				write: run(mergeChoiceAutoMerge),
			},
			{
				label: "merge then rebase",
				keys:  m.keys.Merge.MergeRebase,
				isOff: mergeRebaseRefusal(m.listing, task) != nil,
				write: run(mergeChoiceMergeRebase),
			},
		},
	}
}

// runMerge asks the forge to merge the task's pull request, or says why it cannot.
func (m *model) runMerge(taskID string, choice mergeChoice) tea.Cmd {
	// The list may have moved under the open menu.
	task, ok := m.findGitTask(taskID)
	if !ok {
		m.report("Could not act on the task", errTaskGone)

		return nil
	}

	if choice == mergeChoiceAutoMerge {
		return m.toggleAutoMerge(task)
	}

	if choice == mergeChoiceMergeRebase {
		if err := mergeRebaseRefusal(m.listing, task); err != nil {
			m.report("Could not merge and rebase "+task.Slug, err)

			return nil
		}

		return m.runGitCmd(mergeRebaseOp(task))
	}

	archive := choice == mergeChoiceMergeArchive
	if err := openPullRequestRefusal(task); err != nil {
		m.report("Could not merge the "+task.PullRequest.Noun()+" of "+task.Slug, err)

		return nil
	}
	if archive && task.ArchivedAt != nil {
		m.report("Could not archive "+task.Slug, errArchiveArchived)

		return nil
	}

	return m.runGitCmd(mergeOp(task, archive))
}

func mergeOp(task panel.Task, archive bool) gitOp {
	return gitOp{
		running: "merging " + task.PullRequest.Ref(),
		run: func(clients api) gitReport {
			err := clients.mergePullRequest(task.PullRequest.URL)

			return func(m *model) tea.Cmd {
				return m.reportMerged(task, archive, err)
			}
		},
	}
}

func (m *model) reportMerged(task panel.Task, archive bool, err error) tea.Cmd {
	standing := task.PullRequest
	if err != nil {
		m.report("Could not merge "+standing.Ref()+" of "+task.Slug, err)

		return nil
	}

	m.pushMessage(message{
		kind:    messageInfo,
		summary: task.Slug + ": merged " + standing.Ref(),
		detail:  standing.Title,
	})

	if !archive {
		return nil
	}

	return m.archiveTasks([]panel.Task{task})
}

func mergeRebaseOp(task panel.Task) gitOp {
	taskID, name := task.ID, task.Slug

	return gitOp{
		running: "merging " + task.PullRequest.Ref() + " and rebasing " + name,
		run: func(clients api) gitReport {
			if err := clients.mergePullRequest(task.PullRequest.URL); err != nil {
				return func(m *model) tea.Cmd {
					return m.reportMerged(task, false, err)
				}
			}

			result, err := clients.rebaseWorktree(taskID)

			return func(m *model) tea.Cmd {
				m.reportMerged(task, false, nil)
				m.reportRebased(name, result, err)

				return nil
			}
		},
	}
}
