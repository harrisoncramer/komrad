package ui

import (
	"fmt"
	"slices"

	"weave/config"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// formValues are what the open form writes into, each nil while no form of that shape is
// up. huh records a field into its own results only as the cursor leaves it, so a submit
// from partway down would read every field below as blank. Binding the fields here instead
// means a submit reads what is on screen however it was reached.
type formValues struct {
	agent     *agentFormValues
	rename    *string
	workflow  *workflowstore.Config
	schedule  *schedulestore.Config
	confirmed *bool
}

// formTarget is the record the open form is working on, nil while none is, which for the
// create forms is the whole time. renameAgent is a value rather than a pointer, since a
// rename form is only opened on a row that is on screen.
type formTarget struct {
	renameAgent      agentstore.Agent
	newAgentWorkflow *workflowstore.Workflow
	renameWorkflow   *workflowstore.Workflow
	editWorkflow     *workflowstore.Workflow
	archiveWorkflow  *workflowstore.Workflow
	editSchedule     *schedulestore.Schedule
	archiveSchedule  *schedulestore.Schedule
	deleteAgents     []agentstore.Agent
	archiveAgents    []agentstore.Agent
}

type agentFormValues struct {
	Prompt  string
	Title   string
	Harness string
	Dir     string
	Flags   string
}

// blueprintFields is the harness, directory, and flags inputs shared by the new-agent form
// and the workflow form, since both are setting the same three things: which CLI runs, where
// it starts, and what flags it takes. titlePrefix and the two descriptions distinguish a
// workflow setting its own blueprint from an agent overriding one it was prefilled with.
func blueprintFields(titlePrefix, harnessDesc, dirDesc string, harness, dir, flags *string) []huh.Field {
	return []huh.Field{
		huh.NewInput().Key("harness").Title(titlePrefix + "Agent").Description(harnessDesc).Placeholder("claude").Value(harness),
		huh.NewInput().Key("dir").Title(titlePrefix + "Directory").Description(dirDesc).Value(dir),
		huh.NewInput().Key("flags").Title(titlePrefix + "Flags").Placeholder("--model opus").Value(flags),
	}
}

// newAgentForm builds the new-agent form and returns the values its fields write into.
// prefill is the enclosing workflow's blueprint (harness, directory, flags), so an agent
// created inside a workflow starts from it and can override any part, or everything a
// refused submit is putting back.
func newAgentForm(keys config.FormKeys, prefill agentFormValues) (*huh.Form, *agentFormValues) {
	values := &prefill

	fields := append([]huh.Field{
		huh.NewText().Key("prompt").Title("Initial prompt").Description(fmt.Sprintf("ctrl+e opens $EDITOR; %s submits", keyLabel(keys.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("title").Title("Title").Description("optional; named from the prompt if blank").Value(&values.Title),
	}, blueprintFields("", "blank = the workflow's default", "blank = the workflow's directory", &values.Harness, &values.Dir, &values.Flags)...)

	form := huh.NewForm(huh.NewGroup(fields...)).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())

	return form, values
}

// renameForm asks for a new name, starting blank rather than on the current one so the
// old name is there to read while the new one is typed. prefill is what the field opens
// on, blank except when a refused submit is putting it back.
func renameForm(current, prefill string, keys config.FormKeys) (*huh.Form, *string) {
	name := prefill

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title(fmt.Sprintf("Rename %q to", current)).Value(&name),
		),
	).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())

	return form, &name
}

// formKeyMap is the default form keymap with the configured back and quit keys bound to
// abort, so esc backs out of a form the same way it leaves other screens.
func formKeyMap(keys config.FormKeys) *huh.KeyMap {
	quit := append(append([]string{}, keys.Back...), keys.Quit...)

	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys(quit...), key.WithHelp(keyLabel(keys.Back), "back"))
	return km
}

// multiFieldKeyMap is every form's keymap, binding submit to the configured key alone so
// enter never doubles as a send.
func multiFieldKeyMap(keys config.FormKeys) *huh.KeyMap {
	submit := key.NewBinding(key.WithKeys(keys.Submit...), key.WithHelp(keyLabel(keys.Submit), "submit"))

	km := formKeyMap(keys)
	km.Input.Submit = submit
	km.Text.Submit = submit
	km.Select.Submit = submit
	km.Confirm.Submit = submit

	return km
}

// wasConfirmed reports whether the open prompt was answered yes. A prompt that is not open
// reads as no, so a destructive half only ever runs off an answer somebody gave.
func (m model) wasConfirmed() bool {
	return m.values.confirmed != nil && *m.values.confirmed
}

// confirmForm asks a yes-or-no question and returns the answer its buttons write into.
// Every prompt in the panel goes through this, so only the wording differs. affirmative
// names what saying yes does, since one prompt archives and the others destroy.
func confirmForm(title, description, affirmative string, keys config.FormKeys) (*huh.Form, *bool) {
	var confirmed bool

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Key("confirm").
				Title(title).
				Description(description).
				Affirmative(affirmative).
				Negative("Cancel").
				Value(&confirmed),
		),
	).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())

	return form, &confirmed
}

// holdForm keeps a submitted form on screen until its write lands, so a refusal comes back
// to the filled-in fields rather than closing and reporting over the list. A form the
// submit completed is built again, since huh cannot resume one.
func (m *model) holdForm() tea.Cmd {
	m.pendingForm = true

	if m.form != nil && m.form.State == huh.StateCompleted {
		return m.rebuildForm()
	}

	return nil
}

// rebuildForm builds the open form again around the values the panel holds, which are
// everything that was typed into it.
func (m *model) rebuildForm() tea.Cmd {
	switch {
	case m.screen == screenForm && m.values.agent != nil:
		m.form, m.values.agent = newAgentForm(m.keys.Form, *m.values.agent)
	case m.screen == screenRename && m.values.rename != nil:
		m.form, m.values.rename = renameForm(m.target.renameAgent.Name, *m.values.rename, m.keys.Form)
	case m.screen == screenWorkflowForm && m.values.workflow != nil:
		m.form, m.values.workflow = m.workflowFormFor(*m.values.workflow)
	case m.screen == screenScheduleForm && m.values.schedule != nil:
		m.form, m.values.schedule = scheduleForm(*m.values.schedule, m.tree.workflows, m.keys.Form)
	default:
		return nil
	}

	return m.form.Init()
}

// closeForm leaves the open form and lands on the screen it was opened from, dropping
// what it was working on.
func (m model) closeForm() (tea.Model, tea.Cmd) {
	m.pendingForm = false
	m.values = formValues{}
	m.target = formTarget{}

	if m.screen == screenScheduleForm {
		return m.enterSchedules()
	}

	return m.returnToAgents()
}

// isForm reports whether a form owns the screen, which decides whether a key belongs to
// the form or the panel's chrome, and whether a form or a list is drawn.
func (s screen) isForm() bool {
	switch s {
	case screenForm, screenRename, screenAgentArchive, screenWorkflowForm, screenWorkflowArchive,
		screenScheduleForm, screenScheduleArchive, screenArchiveDelete:
		return true
	default:
		return false
	}
}

// deleteAgentsForm confirms destroying agents, naming what goes with them since the
// reversible delete on the agents list uses the same key and this one cannot be undone.
func deleteAgentsForm(agents []agentstore.Agent, keys config.FormKeys) (*huh.Form, *bool) {
	title := fmt.Sprintf("Permanently delete %d agents?", len(agents))
	warning := "Their transcripts go with them and search will not find the conversations again."
	if len(agents) == 1 {
		title = fmt.Sprintf("Permanently delete agent %q?", agents[0].Name)
		warning = "Its transcript goes with it and search will not find the conversation again."
	}

	return confirmForm(title, warning, "Delete", keys)
}

// archiveAgentsForm confirms putting agents away. It is reversible from the archive screen,
// so the wording says so rather than warning the way the delete prompt does.
func archiveAgentsForm(agents []agentstore.Agent, keys config.FormKeys) (*huh.Form, *bool) {
	title := fmt.Sprintf("Archive %d agents?", len(agents))
	if len(agents) == 1 {
		title = fmt.Sprintf("Archive agent %q?", agents[0].Name)
	}

	return confirmForm(title, "Their transcripts are kept and stay searchable; restore them from the archive.", "Archive", keys)
}

// workflowForm asks for a name alone, which is what a rename is. It binds the same config
// shape the fuller form does, so one submit path reads either.
func workflowForm(title string, cfg workflowstore.Config, keys config.FormKeys) (*huh.Form, *workflowstore.Config) {
	values := cfg

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title(title).Value(&values.Name),
		),
	).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())

	return form, &values
}

// workflowConfigForm collects a workflow's name, its context, its default config, and the
// actions run around its agents' lives. The harness, dir, and flags become the blueprint
// agents inside it start from, and every field prefills, so it serves create and edit
// alike.
//
// available is what the actions directory holds, which the two action fields are picked
// from rather than typed, so a name matching no file cannot be written from here.
func workflowConfigForm(nameTitle string, cfg workflowstore.Config, available []actionChoice, keys config.FormKeys) (*huh.Form, *workflowstore.Config) {
	values := cfg

	fields := append([]huh.Field{
		huh.NewInput().Key("name").Title(nameTitle).Value(&values.Name),
		huh.NewText().Key("context").Title("Context").Description("shared notes for this workflow; ctrl+e opens $EDITOR").Value(&values.Context),
	}, blueprintFields("Default ", "blank = the configured default", "blank = current", &values.Harness, &values.Cwd, &values.Flags)...)

	fields = append(fields,
		huh.NewSelect[string]().Key("setup").Title("Setup action").
			Description("runs before each agent starts; a failure stops the launch").
			Options(actionOptions(available, cfg.SetupAction)...).Value(&values.SetupAction),
		huh.NewSelect[string]().Key("cleanup").Title("Cleanup action").
			Description("runs once an agent here is deleted").
			Options(actionOptions(available, cfg.CleanupAction)...).Value(&values.CleanupAction),
	)

	form := huh.NewForm(huh.NewGroup(fields...)).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())

	return form, &values
}

// actionChoice is one action an action field may be set to, with what its header says it does
// so the picker is not a list of bare filenames.
type actionChoice struct {
	name        string
	description string
}

// actionOptions is what an action field offers: none, then every action the directory
// holds. A stored name the directory no longer holds is offered too, so opening the form on
// a workflow whose script was renamed shows what is recorded rather than resetting it.
//
// An option's value is the bare name, which is what the workflow stores. Only the label
// carries the description.
func actionOptions(available []actionChoice, current string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(available)+2)
	options = append(options, huh.NewOption("none", ""))

	for _, action := range available {
		options = append(options, huh.NewOption(actionChoiceLabel(action), action.name))
	}

	offered := slices.ContainsFunc(available, func(a actionChoice) bool { return a.name == current })
	if current != "" && !offered {
		options = append(options, huh.NewOption(current+"  (missing)", current))
	}

	return options
}

// actionChoiceLabel is how one action reads in a picker. The description is clipped so that
// no row grows taller than the others.
func actionChoiceLabel(action actionChoice) string {
	if action.description == "" {
		return action.name
	}

	return action.name + "  " + clip(action.description, descriptionWidth)
}

// archiveWorkflowForm confirms an archive. The agents inside go with it, since every agent
// belongs to a workflow and there is nowhere else to leave one.
func archiveWorkflowForm(name string, keys config.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Archive workflow %q and its agents?", name),
		"Their transcripts are kept and stay searchable.",
		"Archive",
		keys,
	)
}

// scheduleForm collects everything a schedule holds, prefilled so it serves create and
// edit alike. There is no directory or flags field, since those come off the workflow when
// the schedule fires, which is why picking the workflow picks where the agent runs.
func scheduleForm(cfg schedulestore.Config, workflows []workflowstore.Workflow, keys config.FormKeys) (*huh.Form, *schedulestore.Config) {
	values := cfg

	options := make([]huh.Option[string], 0, len(workflows))
	for _, w := range workflows {
		options = append(options, huh.NewOption(w.Name+"  "+orDash(w.Cwd), w.ID))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title("Name").Description("also names the agents it launches").Value(&values.Name),
			huh.NewInput().Key("cron").Title("Cron").Placeholder("0 9 * * 1-5").Description("minute hour day month weekday").Value(&values.Cron),
			huh.NewInput().Key("timezone").Title("Timezone").Placeholder("America/New_York").Description("blank = this machine's zone").Value(&values.Timezone),
			huh.NewText().Key("prompt").Title("Prompt").Description("what each launched agent is asked to do").Value(&values.Prompt),
			huh.NewSelect[string]().Key("workflow").Title("Workflow").Description("supplies the directory, flags, and context").Options(options...).Value(&values.WorkflowID),
			// huh centres the buttons under the field's own prompt, and a field with a
			// description is as wide as that description, so its buttons would sit
			// indented from the ones above by however long the sentence is.
			huh.NewConfirm().Key("enabled").Title("Armed?").Affirmative("Armed").Negative("Paused").Value(&values.Enabled).WithButtonAlignment(lipgloss.Left),
			huh.NewConfirm().Key("hide_agents").Title("Hide its runs?").Description("they still run, they just take no row").Affirmative("Hidden").Negative("Listed").Value(&values.HideAgents).WithButtonAlignment(lipgloss.Left),
		),
	).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())

	return form, &values
}

// archiveScheduleForm confirms an archive, saying the agents it already launched are kept
// so the choice is not read as bigger than it is.
func archiveScheduleForm(name string, keys config.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Archive schedule %q?", name),
		"The agents it already launched are kept, and nothing brings the schedule back.",
		"Archive",
		keys,
	)
}
