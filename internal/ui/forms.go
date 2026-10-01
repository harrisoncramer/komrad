package ui

import (
	"komrad/config"

	"komrad/internal/ui/paint"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// formState is the form drawn over whichever screen is up, held by value around huh's own
// pointer.
type formState struct {
	kind formKind
	form *huh.Form

	// crumbs are the steps the form adds to the trail while it is up.
	crumbs []string

	// leading draws what stands to the left of the form within its width and height, nil for nothing.
	leading func(width, height int) []string

	// back is what the back key does, nil for closing the form.
	back func(model) (tea.Model, tea.Cmd)

	// update is the form's own message handler, so the panel need not ask which form is up before
	// handing it a message.
	update func(model, tea.Msg) (tea.Model, tea.Cmd)

	// submit is what the answered form does, closed over the values huh writes through and whatever
	// the form is about.
	submit func(model) (tea.Model, tea.Cmd)

	// rebuild draws the form again around those same values, and lists the fields the cursor can
	// reach in the order they are drawn.
	rebuild func(model) (*huh.Form, []string)

	// reshaped reports whether an answer has changed which fields the form draws.
	reshaped func() bool

	// editable are the text fields the panel's editor key can open.
	editable []editableText

	// order lists the keys of the fields the cursor can reach, in the order they are drawn.
	order []string

	// frozen is the form as it was drawn before the key that finished it.
	frozen string

	// keepsOnRefusal says a refused write leaves the form up to be fixed and submitted again.
	keepsOnRefusal bool

	// vim is vim's state for the form's text fields, nil when vim is off.
	vim *fieldVim
}

// formKind names the form on screen, for the legend and for what closing it means.
type formKind int

const (
	formNone formKind = iota
	formAgent
	formChild
	formAdopt
	formAgentArchive
	formAgentRebase
	formAgentPush
	formAgentAbort
	formMerge
	formAgentRetry
	formGit
	formArchiveMenu
	formHideMenu
	formDocumentsMenu
	formDocumentTypesMenu
	formMenu
	formWorkflow
	formWorkflowArchive
	formSchedule
	formScheduleArchive
	formDocument
	formDocumentType
	formDocumentTypeDelete
	formArchiveDelete
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
	m.form.vim = newFieldVim(m.register)

	if m.form.form == nil {
		m.form.form, m.form.order = m.form.rebuild(*m)
	}

	return tea.Batch(m.form.form.Init(), formResized())
}

// redrawnForm is a form the panel can draw again from one builder.
func redrawnForm(params redrawnFormParams) formState {
	return formState{
		kind:           params.Kind,
		keepsOnRefusal: true,
		update:         model.updateForm,
		submit:         params.Submit,
		rebuild:        params.Build,
		reshaped:       params.Reshaped,
		editable:       params.Editable,
	}
}

// redrawnFormParams is what a redrawn form is opened from.
type redrawnFormParams struct {
	Kind     formKind
	Build    func(model) (*huh.Form, []string)
	Reshaped func() bool
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
	case formAgent, formChild, formAdopt, formAgentRetry, formWorkflow, formSchedule,
		formDocument, formDocumentType:
		return columnSpec{floor: formColumnFloor}, true
	case formGit, formArchiveMenu, formHideMenu, formDocumentsMenu, formDocumentTypesMenu, formMerge, formMenu, formAgentArchive, formAgentRebase, formAgentPush, formAgentAbort, formWorkflowArchive, formScheduleArchive,
		formArchiveDelete, formWorktreesDelete, formDocumentTypeDelete:
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

// heldWrite is the write a form is being kept up for.
type heldWrite struct {
	active bool
	write  int
}

// owns reports whether a landing write is the one the form on screen is waiting on.
func (h heldWrite) owns(write int) bool {
	return h.active && h.write == write
}

// formBox is the room a form has on screen.
func (m model) formBox() (width, height int) {
	held := 0
	if m.held.active {
		held = submittingLines
	}

	_, used := m.formLeading()

	if split, ok := m.columnGeometry(); ok {
		return max(split.column-used, 1), max(split.rows-formHelpLines-held, 1)
	}

	return max(m.width-used, 1), max(m.height-formHelpLines-held, 1)
}

// formLeading is what the form draws to its left, and the columns that takes with its dividers.
func (m model) formLeading() ([]string, int) {
	if m.form.leading == nil {
		return nil, 0
	}

	room, rows := m.width, m.height-formHelpLines
	if split, ok := m.columnGeometry(); ok {
		room, rows = split.column, split.rows-formHelpLines
	}
	if m.held.active {
		rows -= submittingLines
	}

	views := m.form.leading(room, rows)

	used := 0
	for _, view := range views {
		used += lipgloss.Width(view) + dividerCols
	}

	return views, used
}

// holdSubmit answers a form by running what it asked for, holding it up while a write it began is in flight.
func holdSubmit(write func(model) (model, tea.Cmd)) func(model) (tea.Model, tea.Cmd) {
	return func(m model) (tea.Model, tea.Cmd) {
		began := m.writes
		written, writeCmd := write(m)

		if written.writes != began && written.form.isActive() {
			return written, tea.Batch(writeCmd, written.holdForm())
		}

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

	next := key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next"))

	km := formKeyMap(keys)
	km.Input.Next = next
	km.Input.Submit = submit
	km.Text.Next = next
	km.Text.NewLine = key.NewBinding(key.WithKeys("enter", "ctrl+j", "alt+enter"), key.WithHelp("enter", "new line"))
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
		write:  m.writes,
	}

	if !m.form.isActive() {
		return nil
	}
	if m.form.form.State == huh.StateCompleted {
		return m.rebuildForm()
	}

	return formResized()
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
	m.warm.closesForm = false

	if m.held.write != 0 {
		if m.detached == nil {
			m.detached = map[int]struct{}{}
		}
		m.detached[m.held.write] = struct{}{}
	}

	return m.closeForm()
}

// landHeld counts a write back in, and reports whether the form on screen was held for it.
func (m *model) landHeld(write int) bool {
	m.landWrite(write)
	delete(m.detached, write)

	if !m.held.owns(write) {
		return false
	}

	m.held = heldWrite{}

	return true
}

// answerForm runs what the form on screen was filled in to do.
func (m model) answerForm() (tea.Model, tea.Cmd) {
	return m.form.submit(m)
}

// updateForm hands a message to the form on screen, and answers it either when huh says it is
// finished or on the submit key.
func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && isBound(key.String(), formKeys().Submit) {
		return m.answerForm()
	}

	if key, ok := msg.(tea.KeyPressMsg); ok {
		if next, cmd, kept := m.updateFormVim(key); kept {
			return next, cmd
		}

		if m.form.back != nil && isBound(key.String(), formKeys().Back) {
			return m.leaveFormBack()
		}
	}

	cmd := m.stepForm(msg)

	switch m.form.form.State {
	case huh.StateCompleted:
		return m.answerForm()
	case huh.StateAborted:
		return m.closeForm()
	case huh.StateNormal:
	}

	if m.form.reshaped != nil && m.form.reshaped() {
		key := ""
		if focused := m.form.form.GetFocusedField(); focused != nil {
			key = focused.GetKey()
		}

		return m, tea.Batch(cmd, m.redrawFormOn(key))
	}

	return m, cmd
}

// stepForm hands a message to huh, keeping the form as drawn before a key that may finish it.
func (m *model) stepForm(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(tea.KeyPressMsg); ok && m.form.rebuild == nil {
		m.form.frozen = m.form.form.View()
	}

	form, cmd := m.form.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form.form = updated
	}

	return cmd
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

// leaveForm drops the form for an answer that goes on to another screen, which settles the window
// itself, so the screen the form was drawn over is not drawn again on the way.
func (m *model) leaveForm() {
	m.held = heldWrite{}
	m.form = formState{}
}

// leaveFormBack does what the back key does on the form on screen.
func (m model) leaveFormBack() (tea.Model, tea.Cmd) {
	if m.form.back != nil {
		return m.form.back(m)
	}

	return m.closeForm()
}

// closeForm drops the form and comes back to the screen it was drawn over.
func (m model) closeForm() (tea.Model, tea.Cmd) {
	m.leaveForm()
	m.reconcileSolo()

	cmd := m.enter(m.screen, screenTarget{})

	return m, cmd
}

const (
	archiveChoiceCancel        archiveChoice = ""
	archiveChoiceArchive       archiveChoice = "archive"
	archiveChoiceArchiveDelete archiveChoice = "archive+delete"
)
