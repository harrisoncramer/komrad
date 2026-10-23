package gitmenu

import (
	"errors"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	forgev1 "komrad/proto/gen/forge/v1"

	"charm.land/huh/v2"
)

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
func (g Menus) mergeMenu(l panel.Listing, task panel.Task) menu.Menu {
	taskID := task.ID
	refused := openPullRequestRefusal(task) != nil
	run := func(choice mergeChoice) func(screen.Inputs) screen.Request {
		return func(in screen.Inputs) screen.Request { return g.runMerge(in, taskID, choice) }
	}

	return menu.Menu{
		Title: "merge",
		Entries: []menu.Entry{
			{
				Label: "merge",
				Keys:  g.deps.Keys.Merge.Merge,
				Off:   refused,
				After: menu.Hold,
				Pick:  run(mergeChoiceMerge),
			},
			{
				Label: "merge and archive",
				Keys:  g.deps.Keys.Merge.MergeArchive,
				Off:   refused || task.ArchivedAt != nil,
				After: menu.Hold,
				Pick:  run(mergeChoiceMergeArchive),
			},
			{
				Label: autoMergeLabel(task),
				Keys:  g.deps.Keys.Merge.AutoMerge,
				Off:   autoMergeRefusal(task) != nil,
				After: menu.Hold,
				Pick:  run(mergeChoiceAutoMerge),
			},
			{
				Label: "merge then rebase",
				Keys:  g.deps.Keys.Merge.MergeRebase,
				Off:   mergeRebaseRefusal(l, task) != nil,
				After: menu.Hold,
				Pick:  run(mergeChoiceMergeRebase),
			},
		},
	}
}

// runMerge asks the forge how the task's pull request could merge, or says why it cannot, and
// cancels auto-merge at once.
func (g Menus) runMerge(in screen.Inputs, taskID string, choice mergeChoice) screen.Request {
	// The list may have moved under the open menu.
	task, ok := findTask(in.Listing, taskID)
	if !ok {
		return screen.Report{Summary: "Could not act on the task", Err: errTaskGone}
	}

	switch choice {
	case mergeChoiceAutoMerge:
		if err := autoMergeRefusal(task); err != nil {
			return screen.Report{Summary: "Could not change auto-merge on the " + task.PullRequest.Noun() + " of " + task.Slug, Err: err}
		}
		if task.PullRequest.MergeReadiness.AutoMerge {
			return g.autoMergeWrite(task, api.MergeParams{URL: task.PullRequest.URL, HeadSHA: task.PullRequest.HeadSHA}, false)
		}
	case mergeChoiceMergeRebase:
		if err := mergeRebaseRefusal(in.Listing, task); err != nil {
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

	return g.planMerge(task, choice)
}

// planMerge asks the forge how the task's pull request could merge.
func (g Menus) planMerge(task panel.Task, choice mergeChoice) screen.Request {
	clients := g.deps.Clients

	return screen.Write{
		Doing: "planning the merge of " + task.PullRequest.Ref(),
		Send: func() screen.Landed {
			plan, err := clients.PlanPullRequestMerge(task.PullRequest.URL)

			return screen.Landed{
				Replaces: err == nil,
				Then:     mergePlannedMsg{task: task, choice: choice, plan: plan, err: err},
			}
		},
	}
}

// applyMergePlanned opens the merge form, or says why there is none.
func (g Menus) applyMergePlanned(msg mergePlannedMsg) screen.Request {
	if msg.err != nil {
		return screen.Report{Summary: "Could not work out how " + msg.task.PullRequest.Ref() + " of " + msg.task.Slug + " can merge", Err: msg.err}
	}

	return g.openMergeForm(msg.task, msg.choice, msg.plan)
}

// openMergeForm opens the form that picks how the pull request merges and with what message.
func (g Menus) openMergeForm(task panel.Task, choice mergeChoice, plan *forgev1.PlanForgePullRequestMergeResponse) screen.Request {
	values := &mergeFormValues{Method: plan.GetDefaultMethod()}
	renderedMethod := values.Method
	var filled *forgev1.CommitMessage

	return form.Open{Form: form.Form{
		Build: func(form.Inputs) (*huh.Form, []string) {
			renderedMethod = values.Method
			filled = values.fill(mergeOptionOf(plan, values.Method), filled)

			return mergeForm(mergeFormParams{
				keys:   config.Fixed(),
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

			return g.mergeWrite(task, choice, *params)
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
func (g Menus) mergeWrite(task panel.Task, choice mergeChoice, params api.MergeParams) screen.Write {
	switch choice {
	case mergeChoiceAutoMerge:
		return g.autoMergeWrite(task, params, true)
	case mergeChoiceMergeRebase:
		return g.operationWrite(g.mergeRebaseOp(task, params))
	case mergeChoiceMerge, mergeChoiceMergeArchive:
	}

	return g.operationWrite(g.mergeOp(task, choice == mergeChoiceMergeArchive, params))
}

func (g Menus) mergeOp(task panel.Task, archive bool, params api.MergeParams) operation {
	return operation{
		running: "merging " + task.PullRequest.Ref(),
		run: func(clients api.Clients) report {
			err := clients.MergePullRequest(params)

			return func(in screen.Inputs) screen.Request {
				return g.reportMerged(in, task, archive, err)
			}
		},
	}
}

func (g Menus) reportMerged(in screen.Inputs, task panel.Task, archive bool, err error) screen.Request {
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

	tasks := []panel.Task{task}

	return screen.Batch{merged, g.deps.Clients.ArchiveTasksWrite(tasks, in.Listing.AgentsOnTasks(tasks))}
}

func (g Menus) mergeRebaseOp(task panel.Task, params api.MergeParams) operation {
	taskID, name := task.ID, task.Slug

	return operation{
		running: "merging " + task.PullRequest.Ref() + " and rebasing " + name,
		run: func(clients api.Clients) report {
			if err := clients.MergePullRequest(params); err != nil {
				return func(in screen.Inputs) screen.Request {
					return g.reportMerged(in, task, false, err)
				}
			}

			result, err := clients.RebaseWorktree(taskID, false)

			return func(in screen.Inputs) screen.Request {
				return screen.Batch{g.reportMerged(in, task, false, nil), reportRebased(name, result, err)}
			}
		},
	}
}
