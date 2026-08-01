package ui

import (
	"fmt"
	"slices"

	"weave/internal/appconfig"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"

	"github.com/charmbracelet/bubbles/key"
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
	deleteSchedule   *schedulestore.Schedule
	deleteAgent      *agentstore.Agent
}

type agentFormValues struct {
	Prompt string
	Title  string
	Dir    string
	Flags  string
}

// newAgentForm builds the new-agent form and returns the values its fields write into.
// defaultDir and defaultFlags prefill from the enclosing workflow's config, so an agent
// created inside a workflow starts from its blueprint.
func newAgentForm(keys appconfig.FormKeys, defaultDir, defaultFlags string) (*huh.Form, *agentFormValues) {
	values := &agentFormValues{Dir: defaultDir, Flags: defaultFlags}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewText().Key("prompt").Title("Initial prompt").Description(fmt.Sprintf("ctrl+e opens $EDITOR; %s submits", keyLabel(keys.Submit))).Value(&values.Prompt),
			huh.NewInput().Key("title").Title("Title").Description("optional; named from the prompt if blank").Value(&values.Title),
			huh.NewInput().Key("dir").Title("Directory").Description("blank = current").Value(&values.Dir),
			huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys)).WithTheme(formTheme())

	return form, values
}

// renameForm asks for a new name, starting blank rather than on the current one so the
// old name is there to read while the new one is typed.
func renameForm(current string, keys appconfig.FormKeys) (*huh.Form, *string) {
	var name string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title(fmt.Sprintf("Rename %q to", current)).Value(&name),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys)).WithTheme(formTheme())

	return form, &name
}

// formKeyMap is the default form keymap with the configured back and quit keys bound to
// abort, so esc backs out of a form the same way it leaves other screens.
func formKeyMap(keys appconfig.FormKeys) *huh.KeyMap {
	quit := append(append([]string{}, keys.Back...), keys.Quit...)

	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys(quit...), key.WithHelp(keyLabel(keys.Back), "back"))
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
func confirmForm(title, description, affirmative string, keys appconfig.FormKeys) (*huh.Form, *bool) {
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
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys)).WithTheme(formTheme())

	return form, &confirmed
}

// isForm reports whether a form owns the screen, which decides whether a key belongs to
// the form or the panel's chrome, and whether a form or a list is drawn.
func (s screen) isForm() bool {
	switch s {
	case screenForm, screenRename, screenWorkflowForm, screenWorkflowArchive,
		screenScheduleForm, screenScheduleDelete, screenArchiveDelete:
		return true
	default:
		return false
	}
}

// deleteAgentForm confirms destroying an agent, naming what goes with it since the
// reversible delete on the agents list uses the same key and this one cannot be undone.
func deleteAgentForm(name string, keys appconfig.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Permanently delete agent %q?", name),
		"Its transcript goes with it and search will not find the conversation again.",
		"Delete",
		keys,
	)
}

// workflowForm asks for a name alone, which is what a rename is. It binds the same config
// shape the fuller form does, so one submit path reads either.
func workflowForm(title string, keys appconfig.FormKeys) (*huh.Form, *workflowstore.Config) {
	var values workflowstore.Config

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title(title).Value(&values.Name),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys)).WithTheme(formTheme())

	return form, &values
}

// workflowConfigForm collects a workflow's name, its context, its default config, and the
// actions run around its agents' lives. The harness, dir, and flags become the blueprint
// agents inside it start from, and every field prefills, so it serves create and edit
// alike.
//
// available is what the actions directory holds, which the two action fields are picked
// from rather than typed, so a name matching no file cannot be written from here.
func workflowConfigForm(nameTitle string, cfg workflowstore.Config, available []string, keys appconfig.FormKeys) (*huh.Form, *workflowstore.Config) {
	values := cfg

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title(nameTitle).Value(&values.Name),
			huh.NewText().Key("context").Title("Context").Description("shared notes for this workflow; ctrl+e opens $EDITOR").Value(&values.Context),
			huh.NewInput().Key("harness").Title("Default agent").Description("blank = the configured default").Placeholder("claude").Value(&values.Harness),
			huh.NewInput().Key("dir").Title("Default directory").Description("blank = current").Value(&values.Cwd),
			huh.NewInput().Key("flags").Title("Default flags").Placeholder("--model opus").Value(&values.Flags),
			huh.NewSelect[string]().Key("setup").Title("Setup action").
				Description("runs before each agent starts; a failure stops the launch").
				Options(actionOptions(available, cfg.SetupAction)...).Value(&values.SetupAction),
			huh.NewSelect[string]().Key("cleanup").Title("Cleanup action").
				Description("runs once an agent here is deleted").
				Options(actionOptions(available, cfg.CleanupAction)...).Value(&values.CleanupAction),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys)).WithTheme(formTheme())

	return form, &values
}

// actionOptions is what an action field offers: none, then every action the directory
// holds. A stored name the directory no longer holds is offered too, so opening the form on
// a workflow whose script was renamed shows what is recorded rather than resetting it.
func actionOptions(available []string, current string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(available)+2)
	options = append(options, huh.NewOption("none", ""))
	for _, name := range available {
		options = append(options, huh.NewOption(name, name))
	}
	if current != "" && !slices.Contains(available, current) {
		options = append(options, huh.NewOption(current+"  (missing)", current))
	}

	return options
}

// archiveWorkflowForm confirms an archive. The agents inside go with it, since every agent
// belongs to a workflow and there is nowhere else to leave one.
func archiveWorkflowForm(name string, keys appconfig.FormKeys) (*huh.Form, *bool) {
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
func scheduleForm(cfg schedulestore.Config, workflows []workflowstore.Workflow, keys appconfig.FormKeys) (*huh.Form, *schedulestore.Config) {
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
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys)).WithTheme(formTheme())

	return form, &values
}

// deleteScheduleForm confirms a delete, saying the agents it already launched are kept so
// the choice is not read as bigger than it is.
func deleteScheduleForm(name string, keys appconfig.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Delete schedule %q?", name),
		"The agents it already launched are kept.",
		"Delete",
		keys,
	)
}
