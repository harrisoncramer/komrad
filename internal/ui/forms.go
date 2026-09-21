package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"weave/config"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/paint"
	"weave/internal/ui/paint/label"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// formState is the form drawn over whichever screen is up, held by value around huh's own
// pointer.
type formState struct {
	kind formKind
	form *huh.Form

	// update is the form's own message handler, so the panel need not ask which form is up before
	// handing it a message.
	update func(model, tea.Msg) (tea.Model, tea.Cmd)

	// submit is what the answered form does, closed over the values huh writes through and whatever
	// the form is about.
	submit func(model) (tea.Model, tea.Cmd)

	// rebuild draws the form again around those same values, and lists the fields the cursor can
	// reach in the order they are drawn.
	rebuild func(model) (*huh.Form, []string)

	// editable are the text fields the panel's editor key can open.
	editable []editableText

	// order lists the keys of the fields the cursor can reach, in the order they are drawn.
	order []string
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
	formAgentRefresh
	formAgentRetry
	formAgentRelease
	formGit
	formWorkflow
	formWorkflowArchive
	formSchedule
	formScheduleArchive
	formArchiveDelete
	formArchiveEmpty
	formWorktreesDelete
)

// isActive reports whether a form is up and holding the keys.
func (f formState) isActive() bool {
	return f.form != nil
}

// isOpening reports whether a form is on screen or is about to be, which the kind answers and
// the built form cannot, a form being built only once its column is settled.
func (f formState) isOpening() bool {
	return f.kind != formNone
}

// openForm puts a form on screen over the screen the panel is already on, taking the window
// first.
func (m *model) openForm(f formState) tea.Cmd {
	m.form = f
	m.reconcileSolo()

	if m.form.form == nil {
		m.form.form, m.form.order = m.form.rebuild(*m)
	}

	return tea.Batch(m.form.form.Init(), formResized())
}

// openRedrawnForm puts up a form the panel can draw again from one builder.
func (m model) openRedrawnForm(params openRedrawnFormParams) (tea.Model, tea.Cmd) {
	return m, m.openForm(formState{
		kind:     params.Kind,
		update:   model.updateForm,
		submit:   params.Submit,
		rebuild:  params.Build,
		editable: params.Editable,
	})
}

// openRedrawnFormParams is what a redrawn form is opened from.
type openRedrawnFormParams struct {
	Kind     formKind
	Build    func(model) (*huh.Form, []string)
	Editable []editableText
	Submit   func(model) (tea.Model, tea.Cmd)
}

// formColumnFloor is the narrowest a form is worth drawing beside a list at, being a field's
// title and the prose under it without either folding.
const formColumnFloor = 50

// formColumn is the form drawn beside the list it was opened over, so the row it is about is
// still on screen while it is answered.
func formColumn(kind formKind) (columnSpec, bool) {
	switch kind {
	case formAgent, formChild, formAdopt, formRename, formAgentRetry, formWorkflow, formSchedule:
		return columnSpec{floor: formColumnFloor}, true
	case formGit, formAgentArchive, formAgentRefresh, formAgentRelease, formWorkflowArchive, formScheduleArchive,
		formArchiveDelete, formArchiveEmpty, formWorktreesDelete:
		return columnSpec{floor: columnFloor}, true
	case formNone:
		return columnSpec{}, false
	}

	// The switch carries every kind and no default, so this is what a form added without an arm
	// compiles to until the exhaustive check refuses it.
	return columnSpec{}, false
}

// formResizedMsg tells the form on screen how much room it has.
type formResizedMsg struct{}

func formResized() tea.Cmd {
	return func() tea.Msg { return formResizedMsg{} }
}

// formHelpLines is the row the panel keeps back rather than tells the form about.
const formHelpLines = 1

// submittingLines is the notice drawn under a held form.
const submittingLines = 1

// heldWrite is the write a form is being kept up for: when it was sent, and which write it is.
type heldWrite struct {
	active bool
	since  time.Time
	epoch  int
}

// owns reports whether a landing write is the one the form on screen is waiting on.
func (h heldWrite) owns(epoch int) bool {
	return h.active && h.epoch == epoch
}

// formBox is the room a form has on screen.
func (m model) formBox() (width, height int) {
	held := 0
	if m.held.active {
		held = submittingLines
	}

	if split, ok := m.columnGeometry(); ok {
		return split.column, max(split.rows-formHelpLines-held, 1)
	}

	return m.width, max(m.height-formHelpLines-held, 1)
}

// confirmSubmit answers a prompt by running what it asked for and taking it down as it goes.
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
	Prompt   string
	Title    string
	Harness  string
	Dir      string
	Flags    string
	Worktree string
}

// The two answers to the only question an agent can override, each a sentinel rather than the
// empty string.
const (
	inheritWorktree = "\x00inherit"
	withoutWorktree = "\x00none"
)

const (
	provisionerNone     = "none"
	provisionerWorktree = "worktree"
)

type workflowFormValues struct {
	Name                 string
	Context              string
	Harness              string
	Cwd                  string
	Flags                string
	Provisioner          string
	PreLaunchHook        string
	PostLaunchHook       string
	PostResumeHook       string
	PostRestoreHook      string
	PreTeardownHook      string
	WorktreeBaseBranch   string
	WorktreeCopyPaths    string
	WorktreeSymlinkPaths string
	WorktreeDiffExclude  string
	WorktreeDir          string
}

func workflowFormValuesFrom(cfg workflowstore.Config) workflowFormValues {
	provisioner := provisionerNone
	if cfg.Worktree.Enabled {
		provisioner = provisionerWorktree
	}

	return workflowFormValues{
		Name:                 cfg.Name,
		Context:              cfg.Context,
		Harness:              cfg.Harness,
		Cwd:                  cfg.Cwd,
		Flags:                cfg.Flags,
		Provisioner:          provisioner,
		PreLaunchHook:        hookNamed(cfg.Hooks, workflowstore.HookPreLaunch),
		PostLaunchHook:       hookNamed(cfg.Hooks, workflowstore.HookPostLaunch),
		PostResumeHook:       hookNamed(cfg.Hooks, workflowstore.HookPostResume),
		PostRestoreHook:      hookNamed(cfg.Hooks, workflowstore.HookPostRestore),
		PreTeardownHook:      hookNamed(cfg.Hooks, workflowstore.HookPreTeardown),
		WorktreeBaseBranch:   cfg.Worktree.BaseBranch,
		WorktreeCopyPaths:    strings.Join(cfg.Worktree.CopyPaths, "\n"),
		WorktreeSymlinkPaths: strings.Join(cfg.Worktree.SymlinkPaths, "\n"),
		WorktreeDiffExclude:  strings.Join(cfg.Worktree.DiffExclude, "\n"),
		WorktreeDir:          cfg.Worktree.Dir,
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
			Enabled:      v.Provisioner == provisionerWorktree,
			BaseBranch:   v.WorktreeBaseBranch,
			CopyPaths:    splitLines(v.WorktreeCopyPaths),
			SymlinkPaths: splitLines(v.WorktreeSymlinkPaths),
			DiffExclude:  splitLines(v.WorktreeDiffExclude),
			Dir:          strings.TrimSpace(v.WorktreeDir),
		},
		Hooks: slices.Concat(
			hookOn(v.PreLaunchHook, workflowstore.HookPreLaunch),
			hookOn(v.PostLaunchHook, workflowstore.HookPostLaunch),
			hookOn(v.PostResumeHook, workflowstore.HookPostResume),
			hookOn(v.PostRestoreHook, workflowstore.HookPostRestore),
			hookOn(v.PreTeardownHook, workflowstore.HookPreTeardown),
		),
	}

	return cfg
}

// hookNamed is the hook a workflow hangs on one moment, and the empty string for a moment
// nothing hangs on.
func hookNamed(hooks []workflowstore.Hook, event string) string {
	for _, hook := range hooks {
		if hook.Event == event {
			return hook.Script
		}
	}

	return ""
}

// hookOn is a moment's hook as the store takes it, which is one entry or none.
func hookOn(name, event string) []workflowstore.Hook {
	if name == "" {
		return nil
	}

	return []workflowstore.Hook{{Event: event, Script: name}}
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

// formOf draws a group of fields as one form, and lists the keys of the fields the cursor can
// reach, in the order they are drawn.
func formOf(keys config.FormKeys, fields ...huh.Field) (*huh.Form, []string) {
	form := huh.NewForm(huh.NewGroup(fields...)).
		WithShowHelp(true).
		WithKeyMap(multiFieldKeyMap(keys)).
		WithTheme(paint.Panel.FormTheme())

	reached := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.Skip() {
			continue
		}

		reached = append(reached, field.GetKey())
	}

	return form, reached
}

// textField is a text field whose editor the panel opens rather than huh.
func textField(key string) *huh.Text {
	return huh.NewText().Key(key).ExternalEditor(false)
}

func blueprintFields(titlePrefix string, harness, dir, flags *string) []huh.Field {
	return []huh.Field{
		huh.NewInput().Key("harness").Title(titlePrefix + "Agent").Placeholder("claude").Value(harness),
		huh.NewInput().Key("dir").Title(titlePrefix + "Directory").Value(dir),
		huh.NewInput().Key("flags").Title(titlePrefix + "Flags").Placeholder("--model opus").Value(flags),
	}
}

// newAgentForm asks for the work, and asks about the worktree only in a workflow that makes
// them.
func newAgentForm(keys config.FormKeys, values *agentFormValues, worktree bool) (*huh.Form, []string) {
	fields := append([]huh.Field{
		textField("prompt").Title("Initial prompt").Description(fmt.Sprintf("%s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).Value(&values.Prompt),
		huh.NewInput().Key("title").Title("Title").Value(&values.Title),
	}, blueprintFields("", &values.Harness, &values.Dir, &values.Flags)...)

	if worktree {
		fields = append(fields, huh.NewSelect[string]().Key("worktree").Title("Worktree").
			Options(
				huh.NewOption("Same as workflow: its own worktree", inheritWorktree),
				huh.NewOption("None: run in the directory above", withoutWorktree),
			).Value(&values.Worktree))
	}

	return formOf(keys, fields...)
}

// childAgentForm asks for the work and little else.
func childAgentForm(keys config.FormKeys, values *agentFormValues, parent agentstore.Agent) (*huh.Form, []string) {
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

func renameForm(current string, name *string, keys config.FormKeys) (*huh.Form, []string) {
	return formOf(keys, huh.NewInput().Key("name").Title(fmt.Sprintf("Rename %q to", current)).Value(name))
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

// confirmFormStartingOn builds a confirmation sitting on one of its two answers.
func confirmFormStartingOn(confirmed bool, title, description, affirmative string, keys config.FormKeys) (*huh.Form, *bool) { //nolint:revive // a params struct is the follow-up
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Key("confirm").
				Title(title).
				Description(description).
				Affirmative(affirmative).
				Negative("Cancel").
				WithButtonAlignment(lipgloss.Left).
				Value(&confirmed),
		),
	).WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(paint.Panel.FormTheme())

	return form, &confirmed
}

// holdForm keeps the form up while the write it was submitted for is in flight, so a refusal
// lands back on the form that caused it.
func (m *model) holdForm() tea.Cmd {
	m.held = heldWrite{
		active: true,
		since:  time.Now(),
		epoch:  m.epoch,
	}

	if m.form.isActive() && m.form.form.State == huh.StateCompleted {
		return m.rebuildForm()
	}

	return nil
}

// redrawOpenForm draws the form on screen again from the listing as it stands now, leaving the
// cursor on the field it was on.
func (m *model) redrawOpenForm() tea.Cmd {
	if !m.form.isActive() || m.held.active {
		return nil
	}

	key := ""
	if focused := m.form.form.GetFocusedField(); focused != nil {
		key = focused.GetKey()
	}

	return m.redrawFormOn(key)
}

func (m *model) rebuildForm() tea.Cmd {
	if m.form.rebuild == nil {
		return nil
	}

	form, order := m.form.rebuild(*m)
	m.form.form = form
	m.form.order = order

	return tea.Batch(form.Init(), formResized())
}

// detachForm gives the keyboard back while the write the form was held for runs on, and
// remembers which write that was.
func (m model) detachForm() (tea.Model, tea.Cmd) {
	if m.held.epoch != 0 {
		if m.detached == nil {
			m.detached = map[int]struct{}{}
		}
		m.detached[m.held.epoch] = struct{}{}
	}

	return m.closeForm()
}

// answerForm runs what the form on screen was filled in to do.
func (m model) answerForm() (tea.Model, tea.Cmd) {
	return m.form.submit(m)
}

// updateForm hands a message to the form on screen, and answers it either when huh says it is
// finished or on the submit key.
func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && isBound(key.String(), formKeys().Submit) {
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
	case huh.StateNormal:
	}

	return m, cmd
}

// formSizeOf turns a resize into the room the form has rather than the room the panel has, and
// leaves every other message alone.
func (m model) formSizeOf(msg tea.Msg) tea.Msg {
	switch msg.(type) {
	case formResizedMsg, tea.WindowSizeMsg:
		width, height := m.formBox()

		return tea.WindowSizeMsg{Width: width, Height: height}
	default:
		return msg
	}
}

// closeForm drops the form and comes back to the screen it was drawn over.
func (m model) closeForm() (tea.Model, tea.Cmd) {
	m.held = heldWrite{}
	m.form = formState{}
	m.reconcileSolo()

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

func releaseInSilenceForm(name string, keys config.FormKeys) (*huh.Form, *bool) {
	title := fmt.Sprintf("Unpause %q without saying anything?", name)
	warning := "Nothing is typed at it, so an agent the pause cut mid-turn sits where it is until you tell it something. Use this when unpausing keeps being refused because its input box will not take the words."

	return confirmFormStartingOn(false, title, warning, "Unpause", keys)
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

// deleteWorktreesForm asks before a deletion, saying what goes through the agent delete, what the
// remover takes on its own, and where a lone branch stands.
func deleteWorktreesForm(deletion worktreeDeletion, keepsBranch *bool, keys config.FormKeys) (*huh.Form, *bool) {
	count := len(deletion.agents) + len(deletion.unowned)
	subject := label.Count(count, "worktree", "worktrees")
	if count == 1 {
		subject = "the worktree of " + worktreeRow{item: slices.Concat(deletion.agents, deletion.unowned)[0]}.name()
	}
	title := fmt.Sprintf("Delete %s?", subject)

	var lines []string
	switch n := len(deletion.agents); {
	case n == 1:
		lines = append(lines, "The directory goes through the agent delete, which runs the workflow's teardown hooks and the cleanup; the agent leaves the archive and its transcript goes with it.")
	case n > 1:
		lines = append(lines, fmt.Sprintf("%d directories go through the agent delete, which runs the workflow's teardown hooks and the cleanup; the agents leave the archive and their transcripts go with them.", n))
	}
	switch n := len(deletion.unowned); {
	case n == 1:
		lines = append(lines, "The directory nothing answers for is removed on its own, and no branch is touched.")
	case n > 1:
		lines = append(lines, fmt.Sprintf("%d directories nothing answers for are removed on their own, and no branch is touched.", n))
	}
	switch {
	case keepsBranch == nil:
	case *keepsBranch:
		lines = append(lines, "The branch is kept, since its commits are on no other ref.")
	default:
		lines = append(lines, "The branch goes too; every one of its commits is elsewhere.")
	}
	lines = append(lines, "Uncommitted changes go with the directories.")

	return confirmFormStartingOn(false, title, strings.Join(lines, " "), "Delete", keys)
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
// about.
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
		WithShowHelp(true).WithKeyMap(multiFieldKeyMap(keys)).WithTheme(paint.Panel.FormTheme())

	return form, &choice
}

// refreshForm asks before a worktree is brought up to date, saying what will happen to the
// branch and to the files.
func refreshForm(plan refreshPlan, keys config.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		"Bring the worktree of "+plan.name+" up to date?",
		refreshDescription(plan),
		refreshAffirmative(plan),
		keys,
	)
}

// refreshDescription says what the operation will do, in the order somebody deciding needs it:
// what happens to the branch, then what happens to the files, then what might go wrong.
func refreshDescription(plan refreshPlan) string {
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

	if warning := refreshWarning(plan); warning != "" {
		lines = append(lines, warning)
	}

	return strings.Join(lines, " ")
}

// refreshAffirmative names the button for what will actually run.
func refreshAffirmative(plan refreshPlan) string {
	if plan.replay == 0 {
		return "Move"
	}

	return "Rebase"
}

// refreshWarning is the trouble a rebase would run into that the description cannot say from
// the commit counts alone.
func refreshWarning(plan refreshPlan) string {
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

func workflowForm(title string, values *workflowFormValues, keys config.FormKeys) (*huh.Form, []string) {
	return formOf(keys, huh.NewInput().Key("name").Title(title).Value(&values.Name))
}

const provisionerKey = "provisioner"

func provisionerOptions() []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption("None: agents keep the directory they are given", provisionerNone),
		huh.NewOption("Worktree: a git worktree per agent", provisionerWorktree),
	}
}

// workflowConfigParams is what the workflow form is drawn from: the values it edits, and the
// hooks it offers.
type workflowConfigParams struct {
	NameTitle string
	Values    *workflowFormValues
	Hooks     []hookChoice
	Keys      config.FormKeys
}

func workflowConfigForm(params workflowConfigParams) (form *huh.Form, order []string) {
	values, keys := params.Values, params.Keys
	if values.Provisioner == "" {
		values.Provisioner = provisionerWorktree
	}

	fields := append([]huh.Field{
		huh.NewInput().Key("name").Title(params.NameTitle).Value(&values.Name),
		textField("context").Title("Context").
			Description(fmt.Sprintf("shared notes for this workflow; %s opens $EDITOR", keyLabel(keys.Editor))).
			Lines(2).Value(&values.Context),
	}, blueprintFields("Default ", &values.Harness, &values.Cwd, &values.Flags)...)

	fields = append(fields,
		huh.NewSelect[string]().Key(provisionerKey).Title("Provisioner").
			Options(provisionerOptions()...).Value(&values.Provisioner),
	)

	if values.Provisioner == provisionerWorktree {
		fields = append(fields, worktreeDetailFields(values)...)
	}

	fields = append(fields, hookFields(values, params.Hooks)...)

	// The form is taller than most terminals now, so it takes its height from the resize the panel
	// sends it and huh scrolls it around whichever field has the cursor.
	return formOf(keys, fields...)
}

func worktreeDetailFields(values *workflowFormValues) []huh.Field {
	return []huh.Field{
		huh.NewInput().Key("worktree_base_branch").Title("Worktree base branch").
			Placeholder("repo's default branch").Value(&values.WorktreeBaseBranch),
		huh.NewInput().Key("worktree_dir").Title("Worktree parent directory").
			Description("where this workflow's worktrees are made, for a repo that expects them somewhere of its own").
			Placeholder("the configured worktrees directory").Value(&values.WorktreeDir),
		textField("worktree_copy_paths").Title("Worktree copy paths").
			Description("one repo-relative path per line, copied fresh into every worktree, such as .env").
			Lines(2).Value(&values.WorktreeCopyPaths),
		textField("worktree_symlink_paths").Title("Worktree symlink paths").
			Description("one repo-relative path per line, symlinked back instead of duplicated, such as node_modules").
			Lines(2).Value(&values.WorktreeSymlinkPaths),
		textField("worktree_diff_exclude").Title("Diff excludes").
			Description("one path or glob per line, left out of the diff the review screen draws, such as proto/gen or *.pb.go").
			Lines(2).Value(&values.WorktreeDiffExclude),
	}
}

// hookOptionsShown bounds one moment's list, so five moments and everything above them still
// leave the form scrollable.
const hookOptionsShown = 4

// hookFieldChrome is the title and description above a moment's options, which huh takes out of
// the height it is given before sizing the list.
const hookFieldChrome = 2

// hookFields are the scripts hung on a moment rather than on the directory, one select each
// since a moment holds one hook.
func hookFields(values *workflowFormValues, available []hookChoice) []huh.Field {
	moments := []struct {
		key         string
		title       string
		description string
		chosen      *string
	}{
		{
			key:         "pre_launch_hook",
			title:       "Pre-launch hook",
			description: "runs in the agent's directory before its pane opens, and a failure abandons the create",
			chosen:      &values.PreLaunchHook,
		},
		{
			key:         "post_launch_hook",
			title:       "Post-launch hook",
			description: "runs once the agent has been launched into its pane",
			chosen:      &values.PostLaunchHook,
		},
		{
			key:         "post_resume_hook",
			title:       "Post-resume hook",
			description: "runs once a dormant agent is back in a pane",
			chosen:      &values.PostResumeHook,
		},
		{
			key:         "post_restore_hook",
			title:       "Post-restore hook",
			description: "runs when an archived agent's row comes back",
			chosen:      &values.PostRestoreHook,
		},
		{
			key:         "pre_teardown_hook",
			title:       "Pre-teardown hook",
			description: "runs in the agent's directory as it is deleted, before the worktree is given back",
			chosen:      &values.PreTeardownHook,
		},
	}

	fields := make([]huh.Field, 0, len(moments))
	for _, moment := range moments {
		options := hookOptions(available, *moment.chosen)
		scrolls := len(options) > hookOptionsShown

		// Options comes before Value so huh reads the zero value and leaves the list scrolled to
		// the top, which holds only while None, carrying the empty name, leads it.
		field := huh.NewSelect[string]().Key(moment.key).Title(moment.title).
			Description(hookFieldDescription(moment.description, available, scrolls)).
			Options(options...).
			Value(moment.chosen)
		// Only a list too long for the room gets a height.
		if scrolls {
			field = field.Height(hookOptionsShown + hookFieldChrome)
		}

		fields = append(fields, field)
	}

	return fields
}

// hookFieldDescription leads with the empty case rather than appending it.
func hookFieldDescription(description string, available []hookChoice, scrolls bool) string {
	if len(available) == 0 {
		return "none found in your hooks directory; " + description
	}

	if scrolls {
		return "↑/↓ for more; " + description
	}

	return description
}

// hookOptions offers None, every hook in the directory, and the one this workflow already names
// if the directory no longer holds it.
func hookOptions(available []hookChoice, chosen string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(available)+2)
	options = append(options, huh.NewOption("None", ""))

	for _, hook := range available {
		options = append(options, huh.NewOption(hook.name, hook.name))
	}

	offered := slices.ContainsFunc(available, func(a hookChoice) bool { return a.name == chosen })
	if chosen != "" && !offered {
		options = append(options, huh.NewOption(chosen+"  (missing)", chosen))
	}

	return options
}

type hookChoice struct {
	name string
}

func archiveWorkflowForm(name string, keys config.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Archive workflow %q and its agents?", name),
		"Their transcripts are kept and stay searchable.",
		"Archive",
		keys,
	)
}

func scheduleForm(values *schedulestore.Config, workflows []workflowstore.Workflow, keys config.FormKeys) (*huh.Form, []string) {
	options := make([]huh.Option[string], 0, len(workflows)+1)
	for _, w := range workflows {
		options = append(options, huh.NewOption(w.Name+"  "+label.OrDash(w.Cwd), w.ID))
	}

	// A select whose value names none of its options selects the first one and writes it back,
	// which would move the schedule to a workflow nobody chose.
	if values.WorkflowID != "" && !slices.ContainsFunc(options, func(o huh.Option[string]) bool { return o.Value == values.WorkflowID }) {
		options = append(options, huh.NewOption(values.WorkflowID+"  gone", values.WorkflowID))
	}

	return formOf(keys,
		huh.NewInput().Key("name").Title("Name").Description("also names the agents it launches").Value(&values.Name),
		huh.NewInput().Key("cron").Title("Cron").Placeholder("0 9 * * 1-5").Description("minute hour day month weekday").Value(&values.Cron),
		huh.NewInput().Key("timezone").Title("Timezone").Placeholder("America/New_York").Description("blank = this machine's zone").Value(&values.Timezone),
		textField("prompt").Title("Prompt").Description("what each launched agent is asked to do").Value(&values.Prompt),
		// Options comes before Value so huh leaves the list scrolled to the top.
		huh.NewSelect[string]().Key("workflow").Title("Workflow").Description("supplies the directory, flags, and context").Options(options...).Value(&values.WorkflowID),

		huh.NewConfirm().Key("enabled").Title("Armed?").Affirmative("Armed").Negative("Paused").Value(&values.Enabled).WithButtonAlignment(lipgloss.Left),
		huh.NewConfirm().Key("hide_agents").Title("Hide its runs?").Description("they still run, they just take no row").Affirmative("Hidden").Negative("Listed").Value(&values.HideAgents).WithButtonAlignment(lipgloss.Left),
	)
}

func archiveScheduleForm(name string, keys config.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Archive schedule %q?", name),
		"The agents it already launched are kept, and nothing brings the schedule back.",
		"Archive",
		keys,
	)
}
