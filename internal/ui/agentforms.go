package ui

import (
	"errors"
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/lib/xptr"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

var errNoWorkflow = errors.New("every agent belongs to a workflow, so create one first")

var errChildOfChild = errors.New("a child agent may not have children of its own")

func openNewAgent(h host, workflow *panel.Workflow) tea.Cmd {
	if workflow == nil {
		h.report("Could not start an agent", errNoWorkflow)

		return nil
	}

	values := agentPrefill(*workflow)

	return openAgentForm(h, *workflow, &values)
}

// agentPrefill is the new-agent form as the workflow leaves it, before anything is typed.
func agentPrefill(workflow panel.Workflow) agentFormValues {
	return agentFormValues{
		Harness:  workflow.Harness,
		Worktree: inheritWorktree,
	}
}

func openAgentForm(h host, workflow panel.Workflow, values *agentFormValues) tea.Cmd {
	return h.openForm(redrawnForm(redrawnFormParams{
		Kind: formAgent,
		Build: func(m model) (*huh.Form, []string) {
			return newAgentForm(formKeys(), values, workflow.Worktree.Enabled)
		},
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit:   holdSubmit(func(m model) (model, tea.Cmd) { return m.submitAgent(workflow, *values) }),
	}))
}

// openNewChild opens the form that attaches a child to the hovered agent.
func openNewChild(h host, parent panel.Agent) tea.Cmd {
	if parent.ParentAgentID != nil {
		h.report("Could not attach a child", errChildOfChild)

		return nil
	}

	values := childPrefill(parent)

	return openChildForm(h, parent, &values)
}

// childPrefill is the child form as the parent leaves it.
func childPrefill(parent panel.Agent) agentFormValues {
	return agentFormValues{Harness: parent.Harness}
}

func openChildForm(h host, parent panel.Agent, values *agentFormValues) tea.Cmd {
	return h.openForm(redrawnForm(redrawnFormParams{
		Kind:     formChild,
		Build:    func(m model) (*huh.Form, []string) { return childAgentForm(formKeys(), values, parent) },
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit:   holdSubmit(func(m model) (model, tea.Cmd) { return m.submitChild(parent, *values) }),
	}))
}

// submitChild launches the child without a directory or a provisioner.
func (m model) submitChild(parent panel.Agent, values agentFormValues) (model, tea.Cmd) {
	cmd := m.createCmd(createAgentParams{
		Prompt:        values.Prompt,
		Title:         values.Title,
		Harness:       values.Harness,
		Flags:         values.Flags,
		WorkflowID:    parent.WorkflowID,
		ParentAgentID: parent.ID,
	})

	return m, cmd
}

func openRetryForm(h host, agent panel.Agent) tea.Cmd {
	values := retryFormValues{
		Prompt:  agent.Prompt,
		Harness: agent.Harness,
		Flags:   agent.Flags,
	}

	return h.openForm(redrawnForm(redrawnFormParams{
		Kind:     formAgentRetry,
		Build:    func(m model) (*huh.Form, []string) { return retryAgentForm(formKeys(), &values) },
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit:   holdSubmit(func(m model) (model, tea.Cmd) { return m.submitRetry(agent, values) }),
	}))
}

func (m model) submitRetry(agent panel.Agent, values retryFormValues) (model, tea.Cmd) {
	cmd := m.retryCmd(agent, values)

	return m, cmd
}

func openAdopt(h host, workflow *panel.Workflow) tea.Cmd {
	if workflow == nil {
		h.report("Could not adopt", errNoWorkflow)

		return nil
	}

	values := adoptPrefill(*workflow)

	return openAdoptForm(h, *workflow, &values)
}

// adoptPrefill is the adopt form as the workflow leaves it, before anything is typed.
func adoptPrefill(workflow panel.Workflow) adoptFormValues {
	return adoptFormValues{Harness: workflow.Harness}
}

func openAdoptForm(h host, workflow panel.Workflow, values *adoptFormValues) tea.Cmd {
	return h.openForm(redrawnForm(redrawnFormParams{
		Kind:     formAdopt,
		Build:    func(m model) (*huh.Form, []string) { return adoptAgentForm(formKeys(), values) },
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit:   holdSubmit(func(m model) (model, tea.Cmd) { return m.submitAdopt(workflow, *values) }),
	}))
}

func (m model) submitAdopt(workflow panel.Workflow, values adoptFormValues) (model, tea.Cmd) {
	cmd := m.adoptCmd(adoptParams{
		Ref:        values.Ref,
		Prompt:     values.Prompt,
		Title:      values.Title,
		Harness:    values.Harness,
		Flags:      values.Flags,
		WorkflowID: workflow.ID,
	})

	return m, cmd
}

func (m model) submitAgent(workflow panel.Workflow, values agentFormValues) (model, tea.Cmd) {
	cmd := m.createCmd(createAgentParams{
		Prompt:           values.Prompt,
		Title:            values.Title,
		Harness:          values.Harness,
		Flags:            values.Flags,
		ProvisionEnabled: worktreeOverride(values.Worktree),
		WorkflowID:       workflow.ID,
	})

	return m, cmd
}

// worktreeOverride is what the form says about the agent's directory: nil to follow the
// workflow, false to keep the directory it was given.
func worktreeOverride(selected string) *bool {
	if selected == withoutWorktree {
		return xptr.Ptr(false)
	}

	return nil
}

// openRebaseConfirm asks before bringing a worktree up to date.
func (m model) openRebaseConfirm(plan rebasePlan) (tea.Model, tea.Cmd) {
	form, confirmed := rebaseForm(plan, formKeys())

	return m, m.openForm(formState{
		kind:   formAgentRebase,
		form:   form,
		update: model.updateForm,
		submit: holdSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			cmd := m.rebaseCmd(plan.agentID, plan.name)

			return m, cmd
		}),
		rebuild: func(model) (*huh.Form, []string) {
			held, _ := rebaseForm(plan, formKeys())

			return held, nil
		},
	})
}

type adoptFormValues struct {
	Ref     string
	Prompt  string
	Title   string
	Harness string
	Flags   string
}

type agentFormValues struct {
	Prompt   string
	Title    string
	Harness  string
	Flags    string
	Worktree string
}

// The two answers to the only question an agent can override, each a sentinel rather than the
// empty string.
const (
	inheritWorktree = "\x00inherit"
	withoutWorktree = "\x00none"
)

// newAgentForm asks for the work, and asks about the worktree only in a workflow that makes
// them.
func newAgentForm(keys config.FormKeys, values *agentFormValues, worktree bool) (*huh.Form, []string) {
	fields := []huh.Field{
		textField("prompt").Title("Initial prompt").Description(fmt.Sprintf("%s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("title").Title("Title").Value(&values.Title),
		huh.NewInput().Key("harness").Title("Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
	}

	if worktree {
		fields = append(fields, huh.NewSelect[string]().Key("worktree").Title("Worktree").
			Options(
				huh.NewOption("Same as workflow: its own worktree", inheritWorktree),
				huh.NewOption("None: run in the workflow's directory", withoutWorktree),
			).Value(&values.Worktree))
	}

	return formOf(keys, fields...)
}

// childAgentForm asks for the work and little else.
func childAgentForm(keys config.FormKeys, values *agentFormValues, parent panel.Agent) (*huh.Form, []string) {
	fields := []huh.Field{
		textField("prompt").Title(fmt.Sprintf("Child of %q", parent.Name)).
			Description(fmt.Sprintf("%s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("title").Title("Title").Value(&values.Title),
		huh.NewInput().Key("harness").Title("Agent").Placeholder(label.OrDash(parent.Harness)).Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
		huh.NewNote().Description(fmt.Sprintf(
			"Runs in %s, the parent's own checkout. No worktree, nothing provisioned, archived when the parent is.", parent.Cwd)),
	}

	return formOf(keys, fields...)
}

// adoptAgentForm asks for the work to pick up and little else.
func adoptAgentForm(keys config.FormKeys, values *adoptFormValues) (*huh.Form, []string) {
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

func retryAgentForm(keys config.FormKeys, values *retryFormValues) (*huh.Form, []string) {
	return formOf(keys,
		textField("prompt").Title("Prompt").Description(fmt.Sprintf("%s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("harness").Title("Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
	)
}

type archiveChoice string

// promptSubject names what a prompt is about, counting in the children that were not asked
// about.
func promptSubject(agents []panel.Agent, children int) string {
	subject := fmt.Sprintf("%d agents", len(agents))
	if len(agents) == 1 {
		subject = fmt.Sprintf("agent %q", agents[0].Name)
	}

	switch {
	case children == 0:
	case len(agents) > 1:
		subject += fmt.Sprintf(" and %d children", children)
	case children == 1:
		subject += " and its child"
	default:
		subject += fmt.Sprintf(" and its %d children", children)
	}

	return subject
}

// confirmArchiveAgents asks before archiving agents, counting the children they hold among the
// live agents, and offers to delete them too.
func confirmArchiveAgents(h host, live []panel.Agent, agents []panel.Agent) tea.Cmd {
	if len(agents) == 0 {
		return nil
	}

	form, answer := archiveAgentsForm(agents, unnamedChildren(live, agents), formKeys())

	return h.openForm(formState{
		kind:   formAgentArchive,
		form:   form,
		update: model.updateForm,
		submit: holdSubmit(func(m model) (model, tea.Cmd) {
			switch *answer {
			case archiveChoiceArchive:
				return m.archiveAgents(agents)
			case archiveChoiceArchiveDelete:
				return m.archiveAndDeleteAgents(agents)
			case archiveChoiceCancel:
				return m, nil
			}

			return m, nil
		}),
	})
}

func archiveAgentsForm(agents []panel.Agent, children int, keys config.FormKeys) (*huh.Form, *archiveChoice) {
	title := "Archive " + promptSubject(agents, children) + "?"

	choice := archiveChoiceArchive

	field := huh.NewSelect[archiveChoice]().
		Key("archive").
		Title(title).
		Value(&choice).
		Options(
			huh.NewOption("Archive", archiveChoiceArchive),
			huh.NewOption("Archive and delete", archiveChoiceArchiveDelete),
			huh.NewOption("Cancel", archiveChoiceCancel),
		)

	form := huh.NewForm(huh.NewGroup(field)).
		WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(paint.Panel.FormTheme())

	return form, &choice
}

// rebaseForm asks before a worktree is brought up to date, saying what will happen to the
// branch and to the files.
func rebaseForm(plan rebasePlan, keys config.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		"Bring the worktree of "+plan.name+" up to date?",
		rebaseDescription(plan),
		rebaseAffirmative(plan),
		keys,
	)
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
