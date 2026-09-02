package ui

import (
	"fmt"
	"slices"
	"strings"

	"weave/config"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// formState is the form drawn over whichever screen is up, held by value so that copying the
// model copies nothing but huh's own pointer, which huh writes through either way.
type formState struct {
	kind formKind
	form *huh.Form

	// update is the form's own message handler, which is what saves the panel from asking
	// which form is up before it can hand it a message.
	update func(model, tea.Msg) (tea.Model, tea.Cmd)

	// submit is what the answered form does, closed over the values huh writes through and
	// whatever the form is about, so neither has to be rediscovered from the model.
	submit func(model) (tea.Model, tea.Cmd)

	// rebuild draws the form again around those same values, since huh will not take another
	// key into a form it has already completed.
	rebuild func(model) (*huh.Form, tea.Cmd)
}

// formKind names the form on screen, for the legend and for what closing it means.
type formKind int

const (
	formNone formKind = iota
	formAgent
	formChild
	formAdopt
	formRename
	formAgentArchive
	formWorkflow
	formWorkflowArchive
	formSchedule
	formScheduleArchive
	formArchiveDelete
	formArchiveEmpty
)

// active reports whether a form is up and holding the keys.
func (f formState) active() bool {
	return f.form != nil
}

// openForm puts a form on screen over the screen the panel is already on, which is where
// closing it lands.
func (m *model) openForm(f formState) tea.Cmd {
	m.form = f

	return f.form.Init()
}

// confirmSubmit answers a prompt by running what it asked for and taking it down as it goes,
// since a confirmation is finished the moment it is answered either way.
func confirmSubmit(write func(model) (model, tea.Cmd)) func(model) (tea.Model, tea.Cmd) {
	return func(m model) (tea.Model, tea.Cmd) {
		written, writeCmd := write(m)

		next, back := written.closeForm()

		return next, tea.Batch(writeCmd, back)
	}
}

type adoptFormValues struct {
	Ref     string
	Prompt  string
	Title   string
	Harness string
	Flags   string
}

type agentFormValues struct {
	Prompt            string
	Title             string
	Harness           string
	Dir               string
	Flags             string
	ProvisionAction   string
	DeprovisionAction string
}

const inheritAction = "\x00inherit"

const (
	provisionerNone     = "none"
	provisionerWorktree = "worktree"
	provisionerScript   = "script"
)

type workflowFormValues struct {
	Name                 string
	Context              string
	Harness              string
	Cwd                  string
	Flags                string
	Provisioner          string
	ProvisionAction      string
	DeprovisionAction    string
	PreLaunchHooks       string
	PostLaunchHooks      string
	PostResumeHooks      string
	PostRestoreHooks     string
	WorktreeBaseBranch   string
	WorktreeCopyPaths    string
	WorktreeSymlinkPaths string
	WorktreeDiffExclude  string
	ArchiveOnPRClosed    bool
}

func workflowFormValuesFrom(cfg workflowstore.Config) workflowFormValues {
	provisioner := provisionerNone
	switch {
	case cfg.Worktree.Enabled:
		provisioner = provisionerWorktree
	case cfg.ProvisionAction != "" || cfg.DeprovisionAction != "":
		provisioner = provisionerScript
	}

	return workflowFormValues{
		Name:                 cfg.Name,
		Context:              cfg.Context,
		Harness:              cfg.Harness,
		Cwd:                  cfg.Cwd,
		Flags:                cfg.Flags,
		Provisioner:          provisioner,
		ProvisionAction:      customAction(cfg.ProvisionAction),
		DeprovisionAction:    customAction(cfg.DeprovisionAction),
		PreLaunchHooks:       hookLines(cfg.Hooks, workflowstore.HookPreLaunch),
		PostLaunchHooks:      hookLines(cfg.Hooks, workflowstore.HookPostLaunch),
		PostResumeHooks:      hookLines(cfg.Hooks, workflowstore.HookPostResume),
		PostRestoreHooks:     hookLines(cfg.Hooks, workflowstore.HookPostRestore),
		WorktreeBaseBranch:   cfg.Worktree.BaseBranch,
		WorktreeCopyPaths:    strings.Join(cfg.Worktree.CopyPaths, "\n"),
		WorktreeSymlinkPaths: strings.Join(cfg.Worktree.SymlinkPaths, "\n"),
		WorktreeDiffExclude:  strings.Join(cfg.Worktree.DiffExclude, "\n"),
		ArchiveOnPRClosed:    cfg.ArchiveOnPRClosed,
	}
}

// customAction drops an action weave owns, since the custom fields only ever offer the ones a
// person wrote and an internal name left in them reads as an action that has gone missing.
func customAction(name string) string {
	if isInternalAction(name) {
		return ""
	}

	return name
}

func (v workflowFormValues) toConfig() workflowstore.Config {
	cfg := workflowstore.Config{
		Name:    v.Name,
		Context: v.Context,
		Harness: v.Harness,
		Cwd:     v.Cwd,
		Flags:   v.Flags,
		Worktree: workflowstore.WorktreeConfig{
			Enabled:      v.Provisioner == provisionerWorktree,
			BaseBranch:   v.WorktreeBaseBranch,
			CopyPaths:    splitLines(v.WorktreeCopyPaths),
			SymlinkPaths: splitLines(v.WorktreeSymlinkPaths),
			DiffExclude:  splitLines(v.WorktreeDiffExclude),
		},
		Hooks: slices.Concat(
			hooksFrom(v.PreLaunchHooks, workflowstore.HookPreLaunch),
			hooksFrom(v.PostLaunchHooks, workflowstore.HookPostLaunch),
			hooksFrom(v.PostResumeHooks, workflowstore.HookPostResume),
			hooksFrom(v.PostRestoreHooks, workflowstore.HookPostRestore),
		),
		ArchiveOnPRClosed: v.ArchiveOnPRClosed,
	}

	if v.Provisioner == provisionerScript {
		cfg.ProvisionAction = v.ProvisionAction
		cfg.DeprovisionAction = v.DeprovisionAction
	}

	return cfg
}

// hookLines is one moment's hooks as the form edits them, one action name per line in the
// order they run.
func hookLines(hooks []workflowstore.Hook, event string) string {
	var names []string
	for _, hook := range hooks {
		if hook.Event == event {
			names = append(names, hook.Action)
		}
	}

	return strings.Join(names, "\n")
}

func hooksFrom(text, event string) []workflowstore.Hook {
	return xslice.Map(splitLines(text), func(name string) workflowstore.Hook {
		return workflowstore.Hook{Event: event, Action: name}
	})
}

func splitLines(text string) []string {
	var lines []string
	for line := range strings.SplitSeq(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}

	return lines
}

func blueprintFields(titlePrefix string, harness, dir, flags *string) []huh.Field {
	return []huh.Field{
		huh.NewInput().Key("harness").Title(titlePrefix + "Agent").Placeholder("claude").Value(harness),
		huh.NewInput().Key("dir").Title(titlePrefix + "Directory").Value(dir),
		huh.NewInput().Key("flags").Title(titlePrefix + "Flags").Placeholder("--model opus").Value(flags),
	}
}

func newAgentForm(keys config.FormKeys, values *agentFormValues, available []actionChoice) *huh.Form {
	fields := append([]huh.Field{
		huh.NewText().Key("prompt").Title("Initial prompt").Description(fmt.Sprintf("ctrl+e opens $EDITOR; %s submits", keyLabel(keys.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("title").Title("Title").Value(&values.Title),
	}, blueprintFields("", &values.Harness, &values.Dir, &values.Flags)...)

	fields = append(fields,
		huh.NewSelect[string]().Key("provision").Title("Provision action").
			Value(&values.ProvisionAction).Options(agentActionOptions(available)...),
		huh.NewSelect[string]().Key("deprovision").Title("Deprovision action").
			Value(&values.DeprovisionAction).Options(agentActionOptions(available)...),
	)

	return huh.NewForm(huh.NewGroup(fields...)).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())
}

// childAgentForm asks for the work and little else, since a child takes its directory from the
// parent it is attached to and provisions nothing of its own.
func childAgentForm(keys config.FormKeys, values *agentFormValues, parent agentstore.Agent) *huh.Form {
	fields := []huh.Field{
		huh.NewText().Key("prompt").Title(fmt.Sprintf("Child of %q", parent.Name)).
			Description(fmt.Sprintf("ctrl+e opens $EDITOR; %s submits", keyLabel(keys.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("title").Title("Title").Value(&values.Title),
		huh.NewInput().Key("harness").Title("Agent").Placeholder(orDash(parent.Harness)).Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
		huh.NewNote().Description(fmt.Sprintf(
			"Runs in %s, the parent's own checkout. No worktree, nothing provisioned, archived when the parent is.", parent.Cwd)),
	}

	return huh.NewForm(huh.NewGroup(fields...)).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())
}

// adoptAgentForm asks for the work to pick up and little else, since adoption works out where
// the agent runs.
func adoptAgentForm(keys config.FormKeys, values *adoptFormValues) *huh.Form {
	fields := []huh.Field{
		huh.NewInput().Key("ref").Title("Adopt").
			Description("a pull request url or number, or a branch already pushed or on disk").Value(&values.Ref),
		huh.NewText().Key("prompt").Title("Prompt").
			Description(fmt.Sprintf("what to do with it; blank asks it to report in and wait. ctrl+e opens $EDITOR; %s submits", keyLabel(keys.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("title").Title("Title").Description("blank names it after the pull request or branch").Value(&values.Title),
		huh.NewInput().Key("harness").Title("Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Flags").Placeholder("--model opus").Value(&values.Flags),
	}

	return huh.NewForm(huh.NewGroup(fields...)).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())
}

func renameForm(current string, name *string, keys config.FormKeys) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title(fmt.Sprintf("Rename %q to", current)).Value(name),
		),
	).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())
}

func formKeyMap(keys config.FormKeys) *huh.KeyMap {
	quit := append(append([]string{}, keys.Back...), keys.Quit...)

	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys(quit...), key.WithHelp(keyLabel(keys.Back), "back"))
	return km
}

func multiFieldKeyMap(keys config.FormKeys) *huh.KeyMap {
	submit := key.NewBinding(key.WithKeys(keys.Submit...), key.WithHelp(keyLabel(keys.Submit), "submit"))

	km := formKeyMap(keys)
	km.Input.Submit = submit
	km.Text.Submit = submit
	km.Select.Submit = submit
	km.Confirm.Submit = submit

	return km
}

func confirmForm(title, description, affirmative string, keys config.FormKeys) (*huh.Form, *bool) {
	return confirmFormStartingOn(true, title, description, affirmative, keys)
}

// confirmFormStartingOn builds a confirmation sitting on one of its two answers, so that a
// prompt nothing undoes can start on Cancel and a stray return costs nothing.
func confirmFormStartingOn(confirmed bool, title, description, affirmative string, keys config.FormKeys) (*huh.Form, *bool) {
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

// holdForm keeps the form up while the write it was submitted for is in flight, so a refusal
// lands back on the form that caused it.
func (m *model) holdForm() tea.Cmd {
	m.pendingForm = true

	if m.form.active() && m.form.form.State == huh.StateCompleted {
		return m.rebuildForm()
	}

	return nil
}

func (m *model) rebuildForm() tea.Cmd {
	if m.form.rebuild == nil {
		return nil
	}

	form, cmd := m.form.rebuild(*m)
	m.form.form = form

	return cmd
}

// answerForm runs what the form on screen was filled in to do.
func (m model) answerForm() (tea.Model, tea.Cmd) {
	return m.form.submit(m)
}

// updateForm hands a message to the form on screen, and answers it either when huh says it is
// finished or on the submit key, which the text fields would otherwise take as a character.
func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && isBound(key.String(), m.keys.Form.Submit) {
		return m.answerForm()
	}

	form, cmd := m.form.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form.form = updated
	}

	switch m.form.form.State {
	case huh.StateCompleted:
		return m.answerForm()
	case huh.StateAborted:
		return m.closeForm()
	default:
		return m, cmd
	}
}

// closeForm drops the form and comes back to the screen it was drawn over.
func (m model) closeForm() (tea.Model, tea.Cmd) {
	m.pendingForm = false
	m.form = formState{}

	reenter := m.screen.spec().reenter
	if reenter == nil {
		return m, nil
	}

	return reenter(m)
}

func deleteAgentsForm(agents []agentstore.Agent, children int, keys config.FormKeys) (*huh.Form, *bool) {
	title := "Permanently delete " + promptSubject(agents, children) + "?"

	warning := "Their transcripts go with them and search will not find the conversations again."
	if len(agents) == 1 && children == 0 {
		warning = "Its transcript goes with it and search will not find the conversation again."
	}

	return confirmForm(title, warning, "Delete", keys)
}

func emptyArchiveForm(count int, keys config.FormKeys) (*huh.Form, *bool) {
	title := fmt.Sprintf("Permanently delete all %d archived agents?", count)
	warning := "Their transcripts go with them and search will not find the conversations again. Any archived workflow the sweep leaves with no agents in it is deleted too."
	if count == 1 {
		title = "Permanently delete the one archived agent?"
		warning = "Its transcript goes with it and search will not find the conversation again. Any archived workflow the sweep leaves with no agents in it is deleted too."
	}

	return confirmFormStartingOn(false, title, warning, "Empty", keys)
}

func deleteWorkflowForm(workflow workflowstore.Workflow, agentCount int, keys config.FormKeys) (*huh.Form, *bool) {
	title := fmt.Sprintf("Permanently delete workflow %q?", workflow.Name)

	warning := "It holds no agents."
	if agentCount == 1 {
		warning = "The one agent still archived inside it goes with it, its transcript included."
	} else if agentCount > 1 {
		warning = fmt.Sprintf("The %d agents still archived inside it go with it, their transcripts included.", agentCount)
	}
	warning += " Any archived schedule that launched into it goes too."

	return confirmForm(title, warning, "Delete", keys)
}

type archiveChoice string

const (
	archiveChoiceCancel        archiveChoice = ""
	archiveChoiceArchive       archiveChoice = "archive"
	archiveChoiceArchiveDelete archiveChoice = "archive+delete"
)

// promptSubject names what a prompt is about, counting in the children that were not asked
// about, since they go with their parents whether or not anybody named them.
func promptSubject(agents []agentstore.Agent, children int) string {
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

func archiveAgentsForm(agents []agentstore.Agent, children int, keys config.FormKeys) (*huh.Form, *archiveChoice) {
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
		WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())

	return form, &choice
}

func workflowForm(title string, values *workflowFormValues, keys config.FormKeys) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title(title).Value(&values.Name),
		),
	).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())
}

const provisionerKey = "provisioner"

func provisionerOptions() []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption("None: agents keep the directory they are given", provisionerNone),
		huh.NewOption("Worktree: a git worktree per agent", provisionerWorktree),
		huh.NewOption("Script: your own provision and deprovision actions", provisionerScript),
	}
}

func workflowConfigForm(nameTitle string, values *workflowFormValues, available []actionChoice, keys config.FormKeys) (form *huh.Form, focusFieldIndex int) {
	if values.Provisioner == "" {
		values.Provisioner = provisionerWorktree
	}

	fields := append([]huh.Field{
		huh.NewInput().Key("name").Title(nameTitle).Value(&values.Name),
		huh.NewText().Key("context").Title("Context").Description("shared notes for this workflow; ctrl+e opens $EDITOR").Lines(2).Value(&values.Context),
	}, blueprintFields("Default ", &values.Harness, &values.Cwd, &values.Flags)...)

	toggleIndex := len(fields)
	fields = append(fields,

		huh.NewSelect[string]().Key(provisionerKey).Title("Provisioner").
			Options(provisionerOptions()...).Value(&values.Provisioner),
	)

	switch values.Provisioner {
	case provisionerNone:
	case provisionerScript:
		// Options comes before Value so huh reads the zero value and leaves the list scrolled
		// to the top, which holds only while None, carrying the empty action, leads it.
		fields = append(fields,
			huh.NewSelect[string]().Key("provision").Title("Provision action").
				Options(actionOptions(available, values.ProvisionAction)...).Value(&values.ProvisionAction),
			huh.NewSelect[string]().Key("deprovision").Title("Deprovision action").
				Options(actionOptions(available, values.DeprovisionAction)...).Value(&values.DeprovisionAction),
		)
	default:
		fields = append(fields, worktreeDetailFields(values)...)
	}

	fields = append(fields, hookFields(values)...)
	fields = append(fields, archiveOnPRClosedField(values))

	built := huh.NewForm(huh.NewGroup(fields...)).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())

	return built, toggleIndex
}

func worktreeDetailFields(values *workflowFormValues) []huh.Field {
	return []huh.Field{
		huh.NewInput().Key("worktree_base_branch").Title("Worktree base branch").
			Placeholder("repo's default branch").Value(&values.WorktreeBaseBranch),
		huh.NewText().Key("worktree_copy_paths").Title("Worktree copy paths").
			Description("one repo-relative path per line, copied fresh into every worktree, such as .env").
			Lines(2).Value(&values.WorktreeCopyPaths),
		huh.NewText().Key("worktree_symlink_paths").Title("Worktree symlink paths").
			Description("one repo-relative path per line, symlinked back instead of duplicated, such as node_modules").
			Lines(2).Value(&values.WorktreeSymlinkPaths),
		huh.NewText().Key("worktree_diff_exclude").Title("Diff excludes").
			Description("one path or glob per line, left out of the diff the review screen draws, such as proto/gen or *.pb.go").
			Lines(2).Value(&values.WorktreeDiffExclude),
	}
}

// hookFields are the scripts hung on a moment rather than on the directory, so they are asked
// for whatever the provisioner is.
func hookFields(values *workflowFormValues) []huh.Field {
	return []huh.Field{
		huh.NewText().Key("pre_launch_hooks").Title("Pre-launch hooks").
			Description("one action name per line, run in the agent's directory before its pane opens, such as install-deps").
			Lines(2).Value(&values.PreLaunchHooks),
		huh.NewText().Key("post_launch_hooks").Title("Post-launch hooks").
			Description("one action name per line, run once the agent has been launched into its pane").
			Lines(2).Value(&values.PostLaunchHooks),
		huh.NewText().Key("post_resume_hooks").Title("Post-resume hooks").
			Description("one action name per line, run once a dormant agent is back in a pane").
			Lines(2).Value(&values.PostResumeHooks),
		huh.NewText().Key("post_restore_hooks").Title("Post-restore hooks").
			Description("one action name per line, run when an archived agent's row comes back").
			Lines(2).Value(&values.PostRestoreHooks),
	}
}

func archiveOnPRClosedField(values *workflowFormValues) huh.Field {
	return huh.NewConfirm().Key("archive_on_pr_closed").Title("Archive once the pull request closes?").
		Description("an idle agent is archived when the pull request on its branch merges or closes; nothing is deleted").
		Affirmative("Archive").Negative("Leave").Value(&values.ArchiveOnPRClosed).
		WithButtonAlignment(lipgloss.Left)
}

type actionChoice struct {
	name string
}

func actionOptions(available []actionChoice, current string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(available)+2)
	options = append(options, huh.NewOption("None", ""))

	for _, action := range available {
		options = append(options, huh.NewOption(actionChoiceLabel(action), action.name))
	}

	offered := slices.ContainsFunc(available, func(a actionChoice) bool { return a.name == current })
	if current != "" && !offered {
		options = append(options, huh.NewOption(current+"  (missing)", current))
	}

	return options
}

func agentActionOptions(available []actionChoice) []huh.Option[string] {
	label := "Same as workflow"
	options := []huh.Option[string]{huh.NewOption(label, inheritAction)}
	return append(options, actionOptions(available, "")...)
}

func actionChoiceLabel(action actionChoice) string {
	return action.name
}

func archiveWorkflowForm(name string, keys config.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Archive workflow %q and its agents?", name),
		"Their transcripts are kept and stay searchable.",
		"Archive",
		keys,
	)
}

func scheduleForm(values *schedulestore.Config, workflows []workflowstore.Workflow, keys config.FormKeys) *huh.Form {
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
			huh.NewSelect[string]().Key("workflow").Title("Workflow").Description("supplies the directory, flags, and context").Value(&values.WorkflowID).Options(options...),

			huh.NewConfirm().Key("enabled").Title("Armed?").Affirmative("Armed").Negative("Paused").Value(&values.Enabled).WithButtonAlignment(lipgloss.Left),
			huh.NewConfirm().Key("hide_agents").Title("Hide its runs?").Description("they still run, they just take no row").Affirmative("Hidden").Negative("Listed").Value(&values.HideAgents).WithButtonAlignment(lipgloss.Left),
		),
	).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())

	return form
}

func archiveScheduleForm(name string, keys config.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Archive schedule %q?", name),
		"The agents it already launched are kept, and nothing brings the schedule back.",
		"Archive",
		keys,
	)
}
