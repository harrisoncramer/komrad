package ui

import (
	"fmt"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"charm.land/huh/v2"
)

func (a *agentsScreen) openNewTask(workflow *panel.Workflow) screen.Request {
	if workflow == nil {
		return screen.Report{Summary: "Could not start a task", Err: panel.ErrNoWorkflow}
	}

	values := taskPrefill(*workflow)

	return a.openTaskForm(*workflow, &values)
}

// taskPrefill is the new-task form as the workflow leaves it, before the user types anything.
func taskPrefill(workflow panel.Workflow) taskFormValues {
	return taskFormValues{
		Harness:  workflow.Harness,
		Worktree: inheritWorktree,
	}
}

func (a *agentsScreen) openTaskForm(workflow panel.Workflow, values *taskFormValues) screen.Request {
	return form.Open{Form: form.Form{
		Build: func(in form.Inputs) (*huh.Form, []string) {
			return newTaskForm(fixedKeys(), values, workflow.Worktree.Enabled, in.Listing.Tasks.PossibleParents(workflow.ID))
		},
		Editable: []form.Editable{{Key: "prompt", Value: &values.Prompt}},
		Submit:   func(form.Inputs) screen.Request { return a.submitTask(workflow, *values) },
	}}
}

// openNewAgentOnTask opens the form that starts an agent on a task: a reviewer when it has a
// writer, or else its writer.
func (a *agentsScreen) openNewAgentOnTask(in screen.Inputs, workflow panel.Workflow, task panel.Task) screen.Request {
	if writer, ok := in.Listing.FindTaskWriter(task); ok {
		values := reviewerPrefill(writer.Agent)

		return a.openReviewerForm(writer.Agent, task, &values)
	}

	values := &agentFormValues{Harness: workflow.Harness}

	return form.Open{Form: form.Form{
		Build: func(form.Inputs) (*huh.Form, []string) {
			return taskAgentForm(fixedKeys(), values, task.Slug, describeNewWriter(workflow, task))
		},
		Editable: []form.Editable{{Key: "prompt", Value: &values.Prompt}},
		Submit:   func(form.Inputs) screen.Request { return a.submitAgentOnTask(workflow, task, *values) },
	}}
}

// reviewerPrefill is the reviewer form as the writer leaves it.
func reviewerPrefill(writer panel.Agent) agentFormValues {
	return agentFormValues{Harness: writer.Harness}
}

func (a *agentsScreen) openReviewerForm(writer panel.Agent, task panel.Task, values *agentFormValues) screen.Request {
	return form.Open{Form: form.Form{
		Build:    func(form.Inputs) (*huh.Form, []string) { return reviewerAgentForm(fixedKeys(), values, writer, task) },
		Editable: []form.Editable{{Key: "prompt", Value: &values.Prompt}},
		Submit:   func(form.Inputs) screen.Request { return a.submitReviewer(writer, task, *values) },
	}}
}

// submitReviewer launches a reviewer onto the writer's task.
func (a *agentsScreen) submitReviewer(writer panel.Agent, task panel.Task, values agentFormValues) screen.Write {
	return createWrite(a.deps.clients, api.CreateAgentParams{
		Prompt:     values.Prompt,
		Title:      values.Title,
		Harness:    values.Harness,
		Flags:      values.Flags,
		WorkflowID: writer.WorkflowID,
		Task:       task.ID,
	})
}

func (a *agentsScreen) openRetryForm(agent panel.Agent) screen.Request {
	values := retryFormValues{
		Prompt:  agent.Prompt,
		Harness: agent.Harness,
		Flags:   agent.Flags,
	}

	return form.Open{Form: form.Form{
		Build:    func(form.Inputs) (*huh.Form, []string) { return retryAgentForm(fixedKeys(), &values) },
		Editable: []form.Editable{{Key: "prompt", Value: &values.Prompt}},
		Submit:   func(form.Inputs) screen.Request { return retryWrite(a.deps.clients, agent, values) },
	}}
}

func (a *agentsScreen) openAdopt(workflow *panel.Workflow) screen.Request {
	if workflow == nil {
		return screen.Report{Summary: "Could not adopt", Err: panel.ErrNoWorkflow}
	}

	values := adoptPrefill(*workflow)

	return a.openAdoptForm(*workflow, &values)
}

// adoptPrefill is the adopt form as the workflow leaves it, before the user types anything.
func adoptPrefill(workflow panel.Workflow) adoptFormValues {
	return adoptFormValues{Harness: workflow.Harness}
}

func (a *agentsScreen) openAdoptForm(workflow panel.Workflow, values *adoptFormValues) screen.Request {
	return form.Open{Form: form.Form{
		Build:    func(form.Inputs) (*huh.Form, []string) { return adoptAgentForm(fixedKeys(), values) },
		Editable: []form.Editable{{Key: "prompt", Value: &values.Prompt}},
		Submit:   func(form.Inputs) screen.Request { return a.submitAdopt(workflow, *values) },
	}}
}

func (a *agentsScreen) submitAdopt(workflow panel.Workflow, values adoptFormValues) screen.Write {
	return adoptWrite(a.deps.clients, api.AdoptParams{
		Ref:        values.Ref,
		Prompt:     values.Prompt,
		Title:      values.Title,
		Harness:    values.Harness,
		Flags:      values.Flags,
		WorkflowID: workflow.ID,
	})
}

// submitTask starts the agent along with the task it works on.
func (a *agentsScreen) submitTask(workflow panel.Workflow, values taskFormValues) screen.Write {
	return createWrite(a.deps.clients, api.CreateAgentParams{
		Prompt:           values.Prompt,
		Harness:          values.Harness,
		Flags:            values.Flags,
		ProvisionEnabled: worktreeOverride(values.Worktree),
		WorkflowID:       workflow.ID,
		DependsOn:        values.DependsOn,
	})
}

// submitAgentOnTask starts a task's writer.
func (a *agentsScreen) submitAgentOnTask(workflow panel.Workflow, task panel.Task, values agentFormValues) screen.Write {
	return createWrite(a.deps.clients, api.CreateAgentParams{
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
	Prompt    string
	Harness   string
	Flags     string
	Worktree  string
	DependsOn string
}

// newTaskForm asks for the brief, and asks about the worktree and the parent only in a workflow that makes worktrees.
func newTaskForm(fixed config.FixedKeys, values *taskFormValues, worktree bool, possibleParents []panel.Task) (*huh.Form, []string) {
	fields := []huh.Field{
		form.Text("prompt").Title("Brief").Description(fmt.Sprintf("what the agent is told; %s opens $EDITOR; %s submits", keys.Label(fixed.Editor), keys.Label(fixed.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("harness").Title("Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
	}

	fields = append(fields, worktreeChoice(&values.Worktree, worktree)...)

	return form.Of(fixed, append(fields, parentChoice(&values.DependsOn, worktree, possibleParents)...)...)
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

// parentChoice asks which task a new task depends on, and asks nothing where no task has a branch to depend on.
func parentChoice(selected *string, worktree bool, possibleParents []panel.Task) []huh.Field {
	if !worktree || len(possibleParents) == 0 {
		return nil
	}

	options := append([]huh.Option[string]{huh.NewOption("The workflow's base branch", "")}, xslice.Map(possibleParents, func(task panel.Task) huh.Option[string] {
		return huh.NewOption(task.Slug, task.ID)
	})...)

	return []huh.Field{huh.NewSelect[string]().Key("depends_on").Title("Depends on").Options(options...).Value(selected)}
}

// taskAgentForm asks for the work a task's writer does, saying where it will run.
func taskAgentForm(fixed config.FixedKeys, values *agentFormValues, task, where string) (*huh.Form, []string) {
	fields := []huh.Field{
		form.Text("prompt").Title(fmt.Sprintf("On task %q", task)).
			Description(fmt.Sprintf("%s opens $EDITOR; %s submits", keys.Label(fixed.Editor), keys.Label(fixed.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("title").Title("Title").Value(&values.Title),
		huh.NewInput().Key("harness").Title("Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
		huh.NewNote().Description(where),
	}

	return form.Of(fixed, fields...)
}

// describeNewWriter says where a new writer on a task with no live one will run.
func describeNewWriter(workflow panel.Workflow, task panel.Task) string {
	switch {
	case task.Cwd != "":
		return fmt.Sprintf("The agent becomes the task's writer and carries on in its worktree at %s.", task.Cwd)
	case workflow.Worktree.Enabled:
		return "The agent becomes the task's first writer and gets a worktree of its own."
	}

	return "The agent becomes the task's first writer and runs in the workflow's directory."
}

// reviewerAgentForm asks for the work and little else.
func reviewerAgentForm(fixed config.FixedKeys, values *agentFormValues, writer panel.Agent, task panel.Task) (*huh.Form, []string) {
	fields := []huh.Field{
		form.Text("prompt").Title(fmt.Sprintf("Reviewer on %q", task.Slug)).
			Description(fmt.Sprintf("%s opens $EDITOR; %s submits", keys.Label(fixed.Editor), keys.Label(fixed.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("title").Title("Title").Value(&values.Title),
		huh.NewInput().Key("harness").Title("Agent").Placeholder(label.OrDash(writer.Harness)).Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
		huh.NewNote().Description(fmt.Sprintf(
			"The reviewer runs in %s, the writer's checkout. Komrad provisions no worktree for it and archives it with its task.", writer.Cwd)),
	}

	return form.Of(fixed, fields...)
}

// adoptAgentForm asks for the work to pick up and little else.
func adoptAgentForm(fixed config.FixedKeys, values *adoptFormValues) (*huh.Form, []string) {
	fields := []huh.Field{
		huh.NewInput().Key("ref").Title("Adopt").
			Description("a branch already pushed or on disk").Value(&values.Ref),
		form.Text("prompt").Title("Prompt").
			Description(fmt.Sprintf("what to do with it; leave blank to have it report in and wait. %s opens $EDITOR; %s submits",
				keys.Label(fixed.Editor), keys.Label(fixed.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("title").Title("Title").Description("leave blank to name it after the branch").Value(&values.Title),
		huh.NewInput().Key("harness").Title("Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
	}

	return form.Of(fixed, fields...)
}

type retryFormValues struct {
	Prompt  string
	Harness string
	Flags   string
}

func retryAgentForm(fixed config.FixedKeys, values *retryFormValues) (*huh.Form, []string) {
	return form.Of(fixed,
		form.Text("prompt").Title("Prompt").Description(fmt.Sprintf("%s opens $EDITOR; %s submits", keys.Label(fixed.Editor), keys.Label(fixed.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("harness").Title("Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
	)
}
