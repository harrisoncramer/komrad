package ui

import (
	"errors"
	"time"

	"komrad/internal/ui/form"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	forgev1 "komrad/proto/gen/forge/v1"

	"charm.land/huh/v2"
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

var (
	errArchiveArchived = errors.New("an archived task is already archived")
	errNoHeadline      = errors.New("a commit needs a headline")
)

// mergePlannedMsg is how a task's pull request could merge, or the reason the forge would not say.
type mergePlannedMsg struct {
	task   panel.Task
	choice mergeChoice
	plan   *forgev1.PlanForgePullRequestMergeResponse
	err    error
}

// mergeMenu is the ways to merge a task's pull request, opened from the git menu.
func mergeMenu(m *model, task panel.Task) menu.Menu {
	taskID := task.ID
	refused := openPullRequestRefusal(task) != nil
	run := func(choice mergeChoice) func() screen.Request {
		return func() screen.Request { return m.runMerge(taskID, choice) }
	}

	return menu.Menu{
		Title: "merge",
		Entries: []menu.Entry{
			{
				Label: "merge",
				Keys:  m.keys.Merge.Merge,
				Off:   refused,
				After: menu.Hold,
				Pick:  run(mergeChoiceMerge),
			},
			{
				Label: "merge and archive",
				Keys:  m.keys.Merge.MergeArchive,
				Off:   refused || task.ArchivedAt != nil,
				After: menu.Hold,
				Pick:  run(mergeChoiceMergeArchive),
			},
			{
				Label: autoMergeLabel(task),
				Keys:  m.keys.Merge.AutoMerge,
				Off:   autoMergeRefusal(task) != nil,
				After: menu.Hold,
				Pick:  run(mergeChoiceAutoMerge),
			},
			{
				Label: "merge then rebase",
				Keys:  m.keys.Merge.MergeRebase,
				Off:   mergeRebaseRefusal(m.listing, task) != nil,
				After: menu.Hold,
				Pick:  run(mergeChoiceMergeRebase),
			},
		},
	}
}

// runMerge asks the forge how the task's pull request could merge, or says why it cannot, and
// cancels auto-merge at once.
func (m *model) runMerge(taskID string, choice mergeChoice) screen.Request {
	// The list may have moved under the open menu.
	task, ok := m.findGitTask(taskID)
	if !ok {
		return screen.Report{Summary: "Could not act on the task", Err: errTaskGone}
	}

	switch choice {
	case mergeChoiceAutoMerge:
		if err := autoMergeRefusal(task); err != nil {
			return screen.Report{Summary: "Could not change auto-merge on the " + task.PullRequest.Noun() + " of " + task.Slug, Err: err}
		}
		if task.PullRequest.MergeReadiness.AutoMerge {
			return m.autoMergeWrite(task, mergeParams{url: task.PullRequest.URL, headSHA: task.PullRequest.HeadSHA}, false)
		}
	case mergeChoiceMergeRebase:
		if err := mergeRebaseRefusal(m.listing, task); err != nil {
			return screen.Report{Summary: "Could not merge and rebase " + task.Slug, Err: err}
		}
	case mergeChoiceMerge, mergeChoiceMergeArchive:
		if err := openPullRequestRefusal(task); err != nil {
			return screen.Report{Summary: "Could not merge the " + task.PullRequest.Noun() + " of " + task.Slug, Err: err}
		}
		if choice == mergeChoiceMergeArchive && task.ArchivedAt != nil {
			return screen.Report{Summary: "Could not archive " + task.Slug, Err: errArchiveArchived}
		}
	}

	return m.planMerge(task, choice)
}

// planMerge asks the forge how the task's pull request could merge.
func (m *model) planMerge(task panel.Task, choice mergeChoice) screen.Request {
	clients := m.api

	return screen.Write{
		Doing: "planning the merge of " + task.PullRequest.Ref(),
		Send: func() screen.Landed {
			plan, err := clients.planPullRequestMerge(task.PullRequest.URL)

			return screen.Landed{
				Replaces: err == nil,
				Then:     mergePlannedMsg{task: task, choice: choice, plan: plan, err: err},
			}
		},
	}
}

// applyMergePlanned opens the merge form, or says why there is none.
func (m *model) applyMergePlanned(msg mergePlannedMsg) screen.Request {
	if msg.err != nil {
		return screen.Report{Summary: "Could not work out how " + msg.task.PullRequest.Ref() + " of " + msg.task.Slug + " can merge", Err: msg.err}
	}

	return m.openMergeForm(msg.task, msg.choice, msg.plan)
}

// openMergeForm opens the form that picks how the pull request merges and with what message.
func (m *model) openMergeForm(task panel.Task, choice mergeChoice, plan *forgev1.PlanForgePullRequestMergeResponse) screen.Request {
	values := &mergeFormValues{Method: plan.GetDefaultMethod()}
	renderedMethod := values.Method
	var filled *forgev1.CommitMessage

	return form.Open{Form: form.Form{
		Build: func(form.Inputs) (*huh.Form, []string) {
			renderedMethod = values.Method
			filled = values.fill(mergeOptionOf(plan, values.Method), filled)

			return mergeForm(mergeFormParams{
				keys:   fixedKeys(),
				title:  mergeFormTitle(task, choice),
				values: values,
				plan:   plan,
			})
		},
		Reshaped: func() bool { return values.Method != renderedMethod },
		Editable: []form.Editable{{Key: "body", Value: &values.Body}},
		Submit: func(form.Inputs) screen.Request {
			params, err := values.buildMergeParams(task.PullRequest, plan)
			if err != nil {
				return form.Refuse{Err: err}
			}

			return m.mergeWrite(task, choice, *params)
		},
		Narrow:          true,
		ClosesOnRefusal: true,
	}}
}

// mergeOptionOf is the plan's option for the method, nil when the forge offers no such method.
func mergeOptionOf(plan *forgev1.PlanForgePullRequestMergeResponse, method forgev1.MergeMethod) *forgev1.MergeOption {
	for _, option := range plan.GetOptions() {
		if option.GetMethod() == method {
			return option
		}
	}

	return nil
}

// mergeWrite is the write that does what the merge menu entry asked for, the way the form says.
func (m *model) mergeWrite(task panel.Task, choice mergeChoice, params mergeParams) screen.Write {
	switch choice {
	case mergeChoiceAutoMerge:
		return m.autoMergeWrite(task, params, true)
	case mergeChoiceMergeRebase:
		return m.gitOpWrite(mergeRebaseOp(task, params))
	case mergeChoiceMerge, mergeChoiceMergeArchive:
	}

	return m.gitOpWrite(mergeOp(task, choice == mergeChoiceMergeArchive, params))
}

func mergeOp(task panel.Task, archive bool, params mergeParams) gitOp {
	return gitOp{
		running: "merging " + task.PullRequest.Ref(),
		run: func(clients api) gitReport {
			err := clients.mergePullRequest(params)

			return func(m *model) screen.Request {
				return m.reportMerged(task, archive, err)
			}
		},
	}
}

func (m *model) reportMerged(task panel.Task, archive bool, err error) screen.Request {
	standing := task.PullRequest
	if err != nil {
		return screen.Report{Summary: "Could not merge " + standing.Ref() + " of " + task.Slug, Err: err}
	}

	merged := screen.Note{
		Kind:    screen.Info,
		Summary: task.Slug + ": merged " + standing.Ref(),
		Detail:  standing.Title,
	}
	if !archive {
		return merged
	}

	return screen.Batch{merged, m.archiveTasks([]panel.Task{task})}
}

func mergeRebaseOp(task panel.Task, params mergeParams) gitOp {
	taskID, name := task.ID, task.Slug

	return gitOp{
		running: "merging " + task.PullRequest.Ref() + " and rebasing " + name,
		run: func(clients api) gitReport {
			if err := clients.mergePullRequest(params); err != nil {
				return func(m *model) screen.Request {
					return m.reportMerged(task, false, err)
				}
			}

			result, err := clients.rebaseWorktree(taskID, false)

			return func(m *model) screen.Request {
				return screen.Batch{m.reportMerged(task, false, nil), reportRebased(name, result, err)}
			}
		},
	}
}
