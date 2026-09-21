package ui

import (
	"time"

	"weave/config"

	"weave/internal/ui/paint"

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
	formAgentRebase
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

// redrawnForm is a form the panel can draw again from one builder.
func redrawnForm(params redrawnFormParams) formState {
	return formState{
		kind:     params.Kind,
		update:   model.updateForm,
		submit:   params.Submit,
		rebuild:  params.Build,
		editable: params.Editable,
	}
}

// redrawnFormParams is what a redrawn form is opened from.
type redrawnFormParams struct {
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
	case formGit, formAgentArchive, formAgentRebase, formAgentRelease, formWorkflowArchive, formScheduleArchive,
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

	return m.active().open(m, screenTarget{})
}

const (
	archiveChoiceCancel        archiveChoice = ""
	archiveChoiceArchive       archiveChoice = "archive"
	archiveChoiceArchiveDelete archiveChoice = "archive+delete"
)
