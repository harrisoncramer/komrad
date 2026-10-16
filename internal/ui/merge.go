package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"komrad/internal/ui/panel"
	forgev1 "komrad/proto/gen/forge/v1"

	tea "charm.land/bubbletea/v2"
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
	write  int
}

// mergeValues is the merge form's answers.
type mergeValues struct {
	Method   forgev1.MergeMethod
	Headline string
	Body     string
}

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

// runMerge asks the forge how the task's pull request could merge, or says why it cannot, and
// cancels auto-merge at once.
func (m *model) runMerge(taskID string, choice mergeChoice) tea.Cmd {
	// The list may have moved under the open menu.
	task, ok := m.findGitTask(taskID)
	if !ok {
		m.report("Could not act on the task", errTaskGone)

		return nil
	}

	switch choice {
	case mergeChoiceAutoMerge:
		if err := autoMergeRefusal(task); err != nil {
			m.report("Could not change auto-merge on the "+task.PullRequest.Noun()+" of "+task.Slug, err)

			return nil
		}
		if task.PullRequest.MergeReadiness.AutoMerge {
			return m.setAutoMerge(task, mergeParams{url: task.PullRequest.URL, headSHA: task.PullRequest.HeadSHA}, false)
		}
	case mergeChoiceMergeRebase:
		if err := mergeRebaseRefusal(m.listing, task); err != nil {
			m.report("Could not merge and rebase "+task.Slug, err)

			return nil
		}
	case mergeChoiceMerge, mergeChoiceMergeArchive:
		if err := openPullRequestRefusal(task); err != nil {
			m.report("Could not merge the "+task.PullRequest.Noun()+" of "+task.Slug, err)

			return nil
		}
		if choice == mergeChoiceMergeArchive && task.ArchivedAt != nil {
			m.report("Could not archive "+task.Slug, errArchiveArchived)

			return nil
		}
	}

	return m.planMerge(task, choice)
}

// planMerge asks the forge how the task's pull request could merge.
func (m *model) planMerge(task panel.Task, choice mergeChoice) tea.Cmd {
	clients := m.api
	planned := mergePlannedMsg{task: task, choice: choice}
	planned.write = m.beginWrite("planning the merge of " + task.PullRequest.Ref())

	return func() tea.Msg {
		planned.plan, planned.err = clients.planPullRequestMerge(task.PullRequest.URL)

		return planned
	}
}

// applyMergePlanned opens the merge form, or says why there is none.
func (m *model) applyMergePlanned(msg mergePlannedMsg) tea.Cmd {
	held := m.landHeld(msg.write)

	if msg.err != nil {
		m.report("Could not work out how "+msg.task.PullRequest.Ref()+" of "+msg.task.Slug+" can merge", msg.err)

		if held {
			return m.closeForm()
		}

		return nil
	}

	return m.openMergeForm(msg.task, msg.choice, msg.plan)
}

// openMergeForm opens the form that picks how the pull request merges and with what message.
func (m *model) openMergeForm(task panel.Task, choice mergeChoice, plan *forgev1.PlanForgePullRequestMergeResponse) tea.Cmd {
	values := &mergeValues{Method: plan.GetPick()}
	renderedMethod := values.Method
	var filled *forgev1.CommitMessage

	return m.openForm(rebuildableForm(rebuildableFormParams{
		Kind: formMerge,
		Build: func(m *model) (*huh.Form, []string) {
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
		Editable: []editableText{{key: "body", value: &values.Body}},
		Submit: func(m *model) tea.Cmd {
			params, err := values.mergeParams(task.PullRequest.URL, plan)
			if err != nil {
				m.form.refusal = err.Error()

				return formResized()
			}

			return holdSubmit(func(m *model) tea.Cmd { return m.sendMerge(task, choice, params) })(m)
		},
	}))
}

// fill writes the option's message into the form unless the person has changed what was filled
// before, and answers what the form now holds as filled.
func (v *mergeValues) fill(option *forgev1.MergeOption, filled *forgev1.CommitMessage) *forgev1.CommitMessage {
	if v.Headline != filled.GetHeadline() || v.Body != filled.GetBody() {
		return filled
	}

	message := option.GetMessage()
	v.Headline, v.Body = message.GetHeadline(), message.GetBody()

	return message
}

// mergeParams is the merge the form asks for, carrying a message only when the person changed
// the forge's own.
func (v mergeValues) mergeParams(url string, plan *forgev1.PlanForgePullRequestMergeResponse) (mergeParams, error) {
	option := mergeOptionOf(plan, v.Method)
	if !option.GetAllowed() {
		return mergeParams{}, fmt.Errorf("the base branch does not allow a %s", mergeMethodLabel(v.Method))
	}

	params := mergeParams{
		url:     url,
		method:  v.Method,
		headSHA: plan.GetHeadSha(),
	}
	message := option.GetMessage()
	if message == nil || (v.Headline == message.GetHeadline() && v.Body == message.GetBody()) {
		return params, nil
	}
	if strings.TrimSpace(v.Headline) == "" {
		return mergeParams{}, errNoHeadline
	}

	params.message = &forgev1.CommitMessage{Headline: v.Headline, Body: v.Body}

	return params, nil
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

// sendMerge does what the merge menu entry asked for, the way the form says.
func (m *model) sendMerge(task panel.Task, choice mergeChoice, params mergeParams) tea.Cmd {
	switch choice {
	case mergeChoiceAutoMerge:
		return m.setAutoMerge(task, params, true)
	case mergeChoiceMergeRebase:
		return m.runGitCmd(mergeRebaseOp(task, params))
	case mergeChoiceMerge, mergeChoiceMergeArchive:
	}

	return m.runGitCmd(mergeOp(task, choice == mergeChoiceMergeArchive, params))
}

func mergeOp(task panel.Task, archive bool, params mergeParams) gitOp {
	return gitOp{
		running: "merging " + task.PullRequest.Ref(),
		run: func(clients api) gitReport {
			err := clients.mergePullRequest(params)

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

func mergeRebaseOp(task panel.Task, params mergeParams) gitOp {
	taskID, name := task.ID, task.Slug

	return gitOp{
		running: "merging " + task.PullRequest.Ref() + " and rebasing " + name,
		run: func(clients api) gitReport {
			if err := clients.mergePullRequest(params); err != nil {
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
