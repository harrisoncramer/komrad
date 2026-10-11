package ui

import (
	"errors"
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/lib/xptr"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

var errNoWorkflow = errors.New("every agent belongs to a workflow, so create one first")

func openNewTask(m *model, workflow *panel.Workflow) tea.Cmd {
	if workflow == nil {
		m.report("Could not start a task", errNoWorkflow)

		return nil
	}

	values := taskPrefill(*workflow)

	return openTaskForm(m, *workflow, &values)
}

// taskPrefill is the new-task form as the workflow leaves it, before anything is typed.
func taskPrefill(workflow panel.Workflow) taskFormValues {
	return taskFormValues{
		Harness:  workflow.Harness,
		Worktree: inheritWorktree,
	}
}

func openTaskForm(m *model, workflow panel.Workflow, values *taskFormValues) tea.Cmd {
	return m.openForm(rebuildableForm(rebuildableFormParams{
		Kind: formAgent,
		Build: func(m *model) (*huh.Form, []string) {
			return newTaskForm(fixedKeys(), values, workflow.Worktree.Enabled)
		},
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit:   holdSubmit(func(m *model) tea.Cmd { return m.submitTask(workflow, *values) }),
	}))
}

// openNewAgentOnTask opens the form that starts an agent on a task: a reviewer when it has a live
// writer, or else its writer.
func openNewAgentOnTask(m *model, workflow panel.Workflow, task panel.Task) tea.Cmd {
	if writer, ok := m.listing.findLiveWriter(task.ID); ok {
		values := reviewerPrefill(writer)

		return openReviewerForm(m, writer, task, &values)
	}

	values := &agentFormValues{Harness: workflow.Harness}

	return m.openForm(rebuildableForm(rebuildableFormParams{
		Kind: formAgent,
		Build: func(m *model) (*huh.Form, []string) {
			return taskAgentForm(fixedKeys(), values, task.Slug, describeNewWriter(workflow, task))
		},
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit:   holdSubmit(func(m *model) tea.Cmd { return m.submitAgentOnTask(workflow, task, *values) }),
	}))
}

// reviewerPrefill is the reviewer form as the writer leaves it.
func reviewerPrefill(writer panel.Agent) agentFormValues {
	return agentFormValues{Harness: writer.Harness}
}

func openReviewerForm(m *model, writer panel.Agent, task panel.Task, values *agentFormValues) tea.Cmd {
	return m.openForm(rebuildableForm(rebuildableFormParams{
		Kind:     formReviewer,
		Build:    func(m *model) (*huh.Form, []string) { return reviewerAgentForm(fixedKeys(), values, writer, task) },
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit:   holdSubmit(func(m *model) tea.Cmd { return m.submitReviewer(writer, task, *values) }),
	}))
}

// submitReviewer launches a reviewer onto the writer's task.
func (m *model) submitReviewer(writer panel.Agent, task panel.Task, values agentFormValues) tea.Cmd {
	cmd := m.createCmd(createAgentParams{
		Prompt:     values.Prompt,
		Title:      values.Title,
		Harness:    values.Harness,
		Flags:      values.Flags,
		WorkflowID: writer.WorkflowID,
		Task:       task.ID,
	})

	return cmd
}

func openRetryForm(m *model, agent panel.Agent) tea.Cmd {
	values := retryFormValues{
		Prompt:  agent.Prompt,
		Harness: agent.Harness,
		Flags:   agent.Flags,
	}

	return m.openForm(rebuildableForm(rebuildableFormParams{
		Kind:     formAgentRetry,
		Build:    func(m *model) (*huh.Form, []string) { return retryAgentForm(fixedKeys(), &values) },
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit:   holdSubmit(func(m *model) tea.Cmd { return m.submitRetry(agent, values) }),
	}))
}

func (m *model) submitRetry(agent panel.Agent, values retryFormValues) tea.Cmd {
	return m.retryCmd(agent, values)
}

func openAdopt(m *model, workflow *panel.Workflow) tea.Cmd {
	if workflow == nil {
		m.report("Could not adopt", errNoWorkflow)

		return nil
	}

	values := adoptPrefill(*workflow)

	return openAdoptForm(m, *workflow, &values)
}

// adoptPrefill is the adopt form as the workflow leaves it, before anything is typed.
func adoptPrefill(workflow panel.Workflow) adoptFormValues {
	return adoptFormValues{Harness: workflow.Harness}
}

func openAdoptForm(m *model, workflow panel.Workflow, values *adoptFormValues) tea.Cmd {
	return m.openForm(rebuildableForm(rebuildableFormParams{
		Kind:     formAdopt,
		Build:    func(m *model) (*huh.Form, []string) { return adoptAgentForm(fixedKeys(), values) },
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit:   holdSubmit(func(m *model) tea.Cmd { return m.submitAdopt(workflow, *values) }),
	}))
}

func (m *model) submitAdopt(workflow panel.Workflow, values adoptFormValues) tea.Cmd {
	cmd := m.adoptCmd(adoptParams{
		Ref:        values.Ref,
		Prompt:     values.Prompt,
		Title:      values.Title,
		Harness:    values.Harness,
		Flags:      values.Flags,
		WorkflowID: workflow.ID,
	})

	return cmd
}

// submitTask starts the agent along with the task it works on.
func (m *model) submitTask(workflow panel.Workflow, values taskFormValues) tea.Cmd {
	return m.createCmd(createAgentParams{
		Prompt:           values.Prompt,
		Harness:          values.Harness,
		Flags:            values.Flags,
		ProvisionEnabled: worktreeOverride(values.Worktree),
		WorkflowID:       workflow.ID,
	})
}

// submitAgentOnTask starts a task's writer.
func (m *model) submitAgentOnTask(workflow panel.Workflow, task panel.Task, values agentFormValues) tea.Cmd {
	return m.createCmd(createAgentParams{
		Prompt:     values.Prompt,
		Title:      values.Title,
		Harness:    values.Harness,
		Flags:      values.Flags,
		WorkflowID: workflow.ID,
		Task:       task.ID,
	})
}

// worktreeOverride is what the form says about the agent's directory: nil to follow the
// workflow, false to keep the directory it was given.
func worktreeOverride(selected string) *bool {
	if selected == withoutWorktree {
		return xptr.Ptr(false)
	}

	return nil
}

type adoptFormValues struct {
	Ref     string
	Prompt  string
	Title   string
	Harness string
	Flags   string
}

type agentFormValues struct {
	Prompt  string
	Title   string
	Harness string
	Flags   string
}

// The two answers to the only question an agent can override, each a sentinel rather than the
// empty string.
const (
	inheritWorktree = "\x00inherit"
	withoutWorktree = "\x00none"
)

type taskFormValues struct {
	Prompt   string
	Harness  string
	Flags    string
	Worktree string
}

// newTaskForm asks for the brief, and asks about the worktree only in a workflow that makes them.
func newTaskForm(keys config.FixedKeys, values *taskFormValues, worktree bool) (*huh.Form, []string) {
	fields := []huh.Field{
		textField("prompt").Title("Brief").Description(fmt.Sprintf("what the agent is told; %s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("harness").Title("Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
	}

	return formOf(keys, append(fields, worktreeChoice(&values.Worktree, worktree)...)...)
}

// worktreeChoice asks whether an agent takes a worktree of its own, and asks nothing in a workflow
// that makes none.
func worktreeChoice(selected *string, worktree bool) []huh.Field {
	if !worktree {
		return nil
	}

	return []huh.Field{huh.NewSelect[string]().Key("worktree").Title("Worktree").
		Options(
			huh.NewOption("Same as workflow: its own worktree", inheritWorktree),
			huh.NewOption("None: run in the workflow's directory", withoutWorktree),
		).Value(selected)}
}

// taskAgentForm asks for the work a task's writer does, saying where it will run.
func taskAgentForm(keys config.FixedKeys, values *agentFormValues, task, where string) (*huh.Form, []string) {
	fields := []huh.Field{
		textField("prompt").Title(fmt.Sprintf("On task %q", task)).
			Description(fmt.Sprintf("%s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("title").Title("Title").Value(&values.Title),
		huh.NewInput().Key("harness").Title("Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
		huh.NewNote().Description(where),
	}

	return formOf(keys, fields...)
}

// describeNewWriter says where a new writer on a task with no live one will run.
func describeNewWriter(workflow panel.Workflow, task panel.Task) string {
	switch {
	case task.Cwd != "":
		return fmt.Sprintf("Becomes the task's writer, carrying on in its worktree at %s.", task.Cwd)
	case workflow.Worktree.Enabled:
		return "Becomes the task's first writer, in a worktree of its own."
	}

	return "Becomes the task's first writer, in the workflow's directory."
}

// reviewerAgentForm asks for the work and little else.
func reviewerAgentForm(keys config.FixedKeys, values *agentFormValues, writer panel.Agent, task panel.Task) (*huh.Form, []string) {
	fields := []huh.Field{
		textField("prompt").Title(fmt.Sprintf("Reviewer on %q", task.Slug)).
			Description(fmt.Sprintf("%s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("title").Title("Title").Value(&values.Title),
		huh.NewInput().Key("harness").Title("Agent").Placeholder(label.OrDash(writer.Harness)).Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
		huh.NewNote().Description(fmt.Sprintf(
			"Runs in %s, the writer's checkout. No worktree, nothing provisioned, archived with its task.", writer.Cwd)),
	}

	return formOf(keys, fields...)
}

// adoptAgentForm asks for the work to pick up and little else.
func adoptAgentForm(keys config.FixedKeys, values *adoptFormValues) (*huh.Form, []string) {
	fields := []huh.Field{
		huh.NewInput().Key("ref").Title("Adopt").
			Description("a branch already pushed or on disk").Value(&values.Ref),
		textField("prompt").Title("Prompt").
			Description(fmt.Sprintf("what to do with it; blank asks it to report in and wait. %s opens $EDITOR; %s submits",
				keyLabel(keys.Editor), keyLabel(keys.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("title").Title("Title").Description("blank names it after the branch").Value(&values.Title),
		huh.NewInput().Key("harness").Title("Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
	}

	return formOf(keys, fields...)
}

type retryFormValues struct {
	Prompt  string
	Harness string
	Flags   string
}

func retryAgentForm(keys config.FixedKeys, values *retryFormValues) (*huh.Form, []string) {
	return formOf(keys,
		textField("prompt").Title("Prompt").Description(fmt.Sprintf("%s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("harness").Title("Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
	)
}

// promptSubject names what a prompt is about, counting in the reviewers that were not asked
// about.
func promptSubject(agents []panel.Agent, reviewers int) string {
	subject := fmt.Sprintf("%d agents", len(agents))
	if len(agents) == 1 {
		subject = fmt.Sprintf("agent %q", agents[0].Name)
	}

	switch {
	case reviewers == 0:
	case len(agents) > 1:
		subject += " and " + label.Count(reviewers, "reviewer", "reviewers")
	case reviewers == 1:
		subject += " and its reviewer"
	default:
		subject += fmt.Sprintf(" and its %d reviewers", reviewers)
	}

	return subject
}

// rebaseForm asks before a worktree is brought up to date, saying what will happen to the
// branch and to the files.
func rebaseForm(plan rebasePlan) gitConfirmForm {
	return func(keys config.FixedKeys) (*huh.Form, *bool) {
		return confirmForm(
			"Bring the worktree of "+plan.name+" up to date?",
			rebaseDescription(plan),
			rebaseAffirmative(plan),
			keys,
		)
	}
}

// rebaseDescription says what the operation will do, in the order somebody deciding needs it:
// what happens to the branch, then what happens to the files, then what might go wrong.
func rebaseDescription(plan rebasePlan) string {
	lines := []string{}

	// A dirty worktree never reaches here.
	switch {
	case plan.replay == 0 && plan.landed == 0:
		lines = append(lines, fmt.Sprintf("This branch has no commits of its own. It will be moved to %s.", plan.baseRef))
	case plan.replay == 0:
		lines = append(lines, fmt.Sprintf("The base already holds every commit on this branch. It will be moved to %s, "+
			"and %s will leave the branch.", plan.baseRef, label.Count(plan.landed, "commit", "commits")))
	default:
		lines = append(lines, fmt.Sprintf("%s of its own will be replayed onto %s.",
			label.Count(plan.replay, "commit", "commits"), plan.baseRef))
		if plan.landed > 0 {
			lines = append(lines, fmt.Sprintf("Another %d the base already holds will leave the branch.", plan.landed))
		}
	}

	lines = append(lines, "Its files will change under it, and nothing untracked is removed. A copy of the "+
		"worktree is kept first, so this can be read back.")

	if warning := rebaseWarning(plan); warning != "" {
		lines = append(lines, warning)
	}

	return strings.Join(lines, " ")
}

// rebaseAffirmative names the button for what will actually run.
func rebaseAffirmative(plan rebasePlan) string {
	if plan.replay == 0 {
		return "Move"
	}

	return "Rebase"
}

// rebaseWarning is the trouble a rebase would run into that the description cannot say from
// the commit counts alone.
func rebaseWarning(plan rebasePlan) string {
	warnings := []string{}

	// The landed commits are not warned about here.
	if plan.merges > 0 {
		warnings = append(warnings, fmt.Sprintf("A rebase throws away %s and replays what they settled.",
			label.Count(plan.merges, "merge commit", "merge commits")))
	}
	if len(plan.conflictPaths) > 0 {
		warnings = append(warnings, "Both sides changed "+namedPaths(plan.conflictPaths, 3)+".")
	}

	return strings.Join(warnings, " ")
}

// namedPaths is a few paths read out.
func namedPaths(paths []string, most int) string {
	if len(paths) <= most {
		return strings.Join(paths, ", ")
	}

	return fmt.Sprintf("%s and %d more", strings.Join(paths[:most], ", "), len(paths)-most)
}
