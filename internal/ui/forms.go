package ui

import (
	"fmt"
	"slices"
	"strings"

	"weave/config"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

type formValues struct {
	agent     *agentFormValues
	rename    *string
	workflow  *workflowFormValues
	schedule  *schedulestore.Config
	confirmed *bool
	archive   *archiveChoice
}

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
	Prompt        string
	Title         string
	Harness       string
	Dir           string
	Flags         string
	SetupAction   string
	CleanupAction string
}

const inheritAction = "\x00inherit"

const (
	setupTypeWorktree = "worktree"
	setupTypeCustom   = "custom"
)

type workflowFormValues struct {
	Name    string
	Context string
	Harness string
	Cwd     string
	Flags   string

	SetupType     string
	SetupAction   string
	CleanupAction string

	WorktreeBaseBranch string

	WorktreeCopyPaths    string
	WorktreeSymlinkPaths string
}

func workflowFormValuesFrom(cfg workflowstore.Config) workflowFormValues {
	setupType := setupTypeCustom
	if cfg.Worktree.Enabled {
		setupType = setupTypeWorktree
	}

	return workflowFormValues{
		Name:                 cfg.Name,
		Context:              cfg.Context,
		Harness:              cfg.Harness,
		Cwd:                  cfg.Cwd,
		Flags:                cfg.Flags,
		SetupType:            setupType,
		SetupAction:          cfg.SetupAction,
		CleanupAction:        cfg.CleanupAction,
		WorktreeBaseBranch:   cfg.Worktree.BaseBranch,
		WorktreeCopyPaths:    strings.Join(cfg.Worktree.CopyPaths, "\n"),
		WorktreeSymlinkPaths: strings.Join(cfg.Worktree.SymlinkPaths, "\n"),
	}
}

func (v workflowFormValues) toConfig() workflowstore.Config {
	cfg := workflowstore.Config{
		Name:    v.Name,
		Context: v.Context,
		Harness: v.Harness,
		Cwd:     v.Cwd,
		Flags:   v.Flags,
		Worktree: workflowstore.WorktreeConfig{
			Enabled:      v.SetupType == setupTypeWorktree,
			BaseBranch:   v.WorktreeBaseBranch,
			CopyPaths:    splitLines(v.WorktreeCopyPaths),
			SymlinkPaths: splitLines(v.WorktreeSymlinkPaths),
		},
	}

	if v.SetupType == setupTypeCustom {
		cfg.SetupAction = v.SetupAction
		cfg.CleanupAction = v.CleanupAction
	}

	return cfg
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

func newAgentForm(keys config.FormKeys, prefill agentFormValues, available []actionChoice) (*huh.Form, *agentFormValues) {
	values := &prefill

	fields := append([]huh.Field{
		huh.NewText().Key("prompt").Title("Initial prompt").Description(fmt.Sprintf("ctrl+e opens $EDITOR; %s submits", keyLabel(keys.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("title").Title("Title").Value(&values.Title),
	}, blueprintFields("", &values.Harness, &values.Dir, &values.Flags)...)

	fields = append(fields,
		huh.NewSelect[string]().Key("setup").Title("Setup action").
			Value(&values.SetupAction).Options(agentActionOptions(available)...),
		huh.NewSelect[string]().Key("cleanup").Title("Cleanup action").
			Value(&values.CleanupAction).Options(agentActionOptions(available)...),
	)

	form := huh.NewForm(huh.NewGroup(fields...)).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())

	return form, values
}

func renameForm(current, prefill string, keys config.FormKeys) (*huh.Form, *string) {
	name := prefill

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title(fmt.Sprintf("Rename %q to", current)).Value(&name),
		),
	).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())

	return form, &name
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

func (m model) wasConfirmed() bool {
	return m.values.confirmed != nil && *m.values.confirmed
}

func confirmForm(title, description, affirmative string, keys config.FormKeys) (*huh.Form, *bool) {
	confirmed := true

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

func (m *model) holdForm() tea.Cmd {
	m.pendingForm = true

	if m.form != nil && m.form.State == huh.StateCompleted {
		return m.rebuildForm()
	}

	return nil
}

func (m *model) rebuildForm() tea.Cmd {
	switch {
	case m.screen == screenForm && m.values.agent != nil:
		m.form, m.values.agent = newAgentForm(m.keys.Form, *m.values.agent, m.getAvailableActions())
	case m.screen == screenRename && m.values.rename != nil:
		m.form, m.values.rename = renameForm(m.target.renameAgent.Name, *m.values.rename, m.keys.Form)
	case m.screen == screenWorkflowForm && m.values.workflow != nil:
		m.form, m.values.workflow, _ = m.workflowFormFor(*m.values.workflow)
	case m.screen == screenScheduleForm && m.values.schedule != nil:
		m.form, m.values.schedule = scheduleForm(*m.values.schedule, m.listing.workflows, m.keys.Form)
	default:
		return nil
	}

	return m.form.Init()
}

func (m model) updateSubmitForm(msg tea.Msg, submit func(model) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && isBound(key.String(), m.keys.Form.Submit) {
		return submit(m)
	}

	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		return submit(m)
	case huh.StateAborted:
		return m.closeForm()
	default:
		return m, cmd
	}
}

type confirmFormActions struct {
	write    func(model) tea.Cmd
	clear    func(model) model
	returnTo func(model) (tea.Model, tea.Cmd)
}

func (m model) updateConfirmForm(msg tea.Msg, actions confirmFormActions) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		writeCmd := actions.write(m)
		next, refresh := actions.returnTo(actions.clear(m))

		return next, tea.Batch(writeCmd, refresh)
	case huh.StateAborted:
		return actions.returnTo(actions.clear(m))
	default:
		return m, cmd
	}
}

func (m model) closeForm() (tea.Model, tea.Cmd) {
	m.pendingForm = false
	m.values = formValues{}
	m.target = formTarget{}

	if m.screen == screenScheduleForm {
		return m.enterSchedules()
	}

	return m.returnToAgents()
}

func (s screen) isForm() bool {
	switch s {
	case screenForm, screenRename, screenAgentArchive, screenWorkflowForm, screenWorkflowArchive,
		screenScheduleForm, screenScheduleArchive, screenArchiveDelete:
		return true
	default:
		return false
	}
}

func deleteAgentsForm(agents []agentstore.Agent, keys config.FormKeys) (*huh.Form, *bool) {
	title := fmt.Sprintf("Permanently delete %d agents?", len(agents))
	warning := "Their transcripts go with them and search will not find the conversations again."
	if len(agents) == 1 {
		title = fmt.Sprintf("Permanently delete agent %q?", agents[0].Name)
		warning = "Its transcript goes with it and search will not find the conversation again."
	}

	return confirmForm(title, warning, "Delete", keys)
}

type archiveChoice string

const (
	archiveChoiceCancel        archiveChoice = ""
	archiveChoiceArchive       archiveChoice = "archive"
	archiveChoiceArchiveDelete archiveChoice = "archive+delete"
)

func archiveAgentsForm(agents []agentstore.Agent, keys config.FormKeys) (*huh.Form, *archiveChoice) {
	title := fmt.Sprintf("Archive %d agents?", len(agents))
	if len(agents) == 1 {
		title = fmt.Sprintf("Archive agent %q?", agents[0].Name)
	}

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

func (m model) archiveAnswer() archiveChoice {
	if m.values.archive == nil {
		return archiveChoiceCancel
	}

	return *m.values.archive
}

func workflowForm(title string, values workflowFormValues, keys config.FormKeys) (*huh.Form, *workflowFormValues) {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title(title).Value(&values.Name),
		),
	).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())

	return form, &values
}

const setupTypeKey = "setup_type"

func setupTypeOptions() []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption("Worktree: a git worktree per agent", setupTypeWorktree),
		huh.NewOption("Custom: your own setup and cleanup actions", setupTypeCustom),
	}
}

func workflowConfigForm(nameTitle string, values workflowFormValues, available []actionChoice, keys config.FormKeys) (form *huh.Form, out *workflowFormValues, focusFieldIndex int) {
	if values.SetupType == "" {
		values.SetupType = setupTypeWorktree
	}

	fields := append([]huh.Field{
		huh.NewInput().Key("name").Title(nameTitle).Value(&values.Name),
		huh.NewText().Key("context").Title("Context").Description("shared notes for this workflow; ctrl+e opens $EDITOR").Lines(2).Value(&values.Context),
	}, blueprintFields("Default ", &values.Harness, &values.Cwd, &values.Flags)...)

	toggleIndex := len(fields)
	fields = append(fields,

		huh.NewSelect[string]().Key(setupTypeKey).Title("Setup type").
			Options(setupTypeOptions()...).Value(&values.SetupType),
	)

	switch values.SetupType {
	case setupTypeCustom:
		fields = append(fields,
			huh.NewSelect[string]().Key("setup").Title("Setup action").
				Value(&values.SetupAction).Options(actionOptions(available, values.SetupAction)...),
			huh.NewSelect[string]().Key("cleanup").Title("Cleanup action").
				Value(&values.CleanupAction).Options(actionOptions(available, values.CleanupAction)...),
		)
	default:
		fields = append(fields, worktreeDetailFields(&values)...)
	}

	built := huh.NewForm(huh.NewGroup(fields...)).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())

	return built, &values, toggleIndex
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
	}
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
			huh.NewSelect[string]().Key("workflow").Title("Workflow").Description("supplies the directory, flags, and context").Value(&values.WorkflowID).Options(options...),

			huh.NewConfirm().Key("enabled").Title("Armed?").Affirmative("Armed").Negative("Paused").Value(&values.Enabled).WithButtonAlignment(lipgloss.Left),
			huh.NewConfirm().Key("hide_agents").Title("Hide its runs?").Description("they still run, they just take no row").Affirmative("Hidden").Negative("Listed").Value(&values.HideAgents).WithButtonAlignment(lipgloss.Left),
		),
	).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(formTheme())

	return form, &values
}

func archiveScheduleForm(name string, keys config.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Archive schedule %q?", name),
		"The agents it already launched are kept, and nothing brings the schedule back.",
		"Archive",
		keys,
	)
}
