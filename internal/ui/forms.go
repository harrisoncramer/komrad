package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/field"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// formState is the form rendered over whichever screen is up, held by value around huh's own
// pointer.
type formState struct {
	form.Form

	// rendered is the huh form on screen.
	rendered *huh.Form

	// isMenu says the form is the space menu.
	isMenu bool

	// crumbs are the steps the form adds to the trail while it is up.
	crumbs []string

	// leading renders what stands to the left of the form within its width and height, nil for nothing.
	leading func(width, height int) []string

	// trailing renders what stands to the right of the form within its width and height, nil for nothing.
	trailing func(width, height int) string

	// back is what the back key does, nil for closing the form.
	back func(*model) tea.Cmd

	// update is the form's own message handler, so the panel need not ask which form is up before
	// handing it a message.
	update func(*model, tea.Msg) tea.Cmd

	// answer is what the answered form does.
	answer func(*model) tea.Cmd

	// order lists the keys of the fields the cursor can reach, in the order they are rendered.
	order []string

	// frozen is the form as the panel rendered it before the key that finished it.
	frozen string

	// refusal is why the last answer was refused.
	refusal string

	// vim is vim's state for the form's text fields, nil when vim is off.
	vim *field.Vim
}

// openedFormState is the form state a form.Open puts on screen.
func openedFormState(f form.Form) formState {
	state := formState{
		Form:   f,
		update: (*model).updateForm,
		answer: (*model).submitForm,
	}
	if len(f.Subforms) > 0 {
		state.update = (*model).updateSubforms
	}

	return state
}

// isActive reports whether a form is up and holding the keys.
func (f formState) isActive() bool {
	return f.rendered != nil
}

// isOpening reports whether a form is on screen or is about to be, which the built form cannot
// answer, a form being built only once its column is settled.
func (f formState) isOpening() bool {
	return f.Built != nil || f.Build != nil
}

// openForm puts a form on screen over the screen the panel is already on, taking the window
// first.
func (m *model) openForm(f formState) tea.Cmd {
	m.form = f
	m.reconcileSolo()
	m.form.vim = field.NewVim(m.register)

	m.form.rendered = m.form.Built
	if m.form.rendered == nil {
		m.form.rendered, m.form.order = m.form.Build(m.formInputs())
	}

	return tea.Batch(m.form.rendered.Init(), formResized())
}

// formInputs is what a form reads of the panel as it is built or answered.
func (m *model) formInputs() form.Inputs {
	return form.Inputs{Listing: m.listing, Inspections: m.inspections}
}

// formColumnFloor is the narrowest a form is worth rendering beside a list at, being a field's
// title and the prose under it without either folding.
const formColumnFloor = 50

// formMaxWidth is the widest a form renders its fields at.
const formMaxWidth = 60

// formColumn is the form rendered beside the list it was opened over, so the row it is about is
// still on screen while it is answered.
func formColumn(narrow bool) columnSpec {
	if narrow {
		return columnSpec{floor: columnFloor}
	}

	return columnSpec{floor: formColumnFloor}
}

// formResizedMsg tells the form on screen how much room it has.
type formResizedMsg struct{}

func formResized() tea.Cmd {
	return func() tea.Msg { return formResizedMsg{} }
}

// formHelpLines is the row the panel keeps back rather than tells the form about.
const formHelpLines = 1

// submittingLines is the notice rendered under a held form.
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

func (m *model) formNoticeLines() int {
	if m.writes.held.active || m.form.refusal != "" {
		return submittingLines
	}

	return 0
}

// formBox is the room a form has on screen.
func (m *model) formBox() (width, height int) {
	held := m.formNoticeLines()

	_, used := m.formLeading()

	floor := 1
	if used > 0 {
		floor = formColumnFloor
	}

	width, height = m.formArea()

	return max(width-used, floor), max(height-formHelpLines-held, 1)
}

// formArea is the columns and rows the form and what it renders to its left share.
func (m *model) formArea() (width, height int) {
	if m.isPopupForm() {
		return m.review.measurePopupInner(m), m.review.measurePopupRoom(m)
	}

	if split, ok := m.columnGeometry(); ok {
		return split.column, split.rows
	}

	return m.term.width, m.term.height
}

// formLeading is what the form renders to its left, and the columns that takes with its dividers.
func (m *model) formLeading() ([]string, int) {
	if m.form.leading == nil {
		return nil, 0
	}

	room, height := m.formArea()
	rows := height - formHelpLines - m.formNoticeLines()

	views := m.form.leading(room, rows)

	used := 0
	for _, view := range views {
		used += lipgloss.Width(view) + dividerCols
	}

	return views, used
}

// holdSubmit answers a form by running what it asked for, holding it up while a write it began is in flight.
func holdSubmit(write func(*model) tea.Cmd) func(*model) tea.Cmd {
	return func(m *model) tea.Cmd {
		began := m.writes.count
		write := write(m)

		if m.writes.count != began && m.form.isActive() {
			return tea.Batch(write, m.holdForm())
		}

		return tea.Batch(write, m.closeForm())
	}
}

// holdForm keeps the form up while the write it was submitted for is in flight, so a refusal
// lands back on the form that caused it.
func (m *model) holdForm() tea.Cmd {
	m.form.refusal = ""
	m.writes.held = heldWrite{
		active: true,
		write:  m.writes.count,
	}

	if !m.form.isActive() {
		return nil
	}
	if m.form.rendered.State == huh.StateCompleted {
		return m.rebuildForm()
	}

	return formResized()
}

// rerenderOpenForm renders the form on screen again from the listing as it stands now, leaving the
// cursor on the field it was on.
func (m *model) rerenderOpenForm() tea.Cmd {
	if !m.form.isActive() || m.writes.held.active {
		return nil
	}

	key := ""
	if focused := m.form.rendered.GetFocusedField(); focused != nil {
		key = focused.GetKey()
	}

	return m.rerenderFormOn(key)
}

// rerenderWorktreeConfirm renders a delete confirm again once git answers for a worktree it
// describes.
func (m *model) rerenderWorktreeConfirm(dir string) tea.Cmd {
	if !slices.Contains(m.form.Worktrees, dir) {
		return nil
	}

	return m.rerenderOpenForm()
}

func (m *model) rebuildForm() tea.Cmd {
	if m.form.Build == nil {
		return nil
	}

	built, order := m.form.Build(m.formInputs())
	m.form.rendered = built
	m.form.order = order

	return tea.Batch(built.Init(), formResized())
}

// detachForm gives the keyboard back while the write the form was held for runs on, and
// remembers which write that was.
func (m *model) detachForm() tea.Cmd {
	if m.writes.held.write != 0 {
		if m.writes.detached == nil {
			m.writes.detached = map[int]struct{}{}
		}
		m.writes.detached[m.writes.held.write] = struct{}{}
	}

	return m.closeForm()
}

// answerForm runs what the form on screen was filled in to do.
func (m *model) answerForm() tea.Cmd {
	return m.form.answer(m)
}

// submitForm runs what the form was filled in to ask for, holding it up while a write that began is
// in flight.
func (m *model) submitForm() tea.Cmd {
	if m.form.Submit == nil {
		return m.closeForm()
	}

	request := m.form.Submit(m.formInputs())
	if refused, ok := request.(form.Refuse); ok {
		return m.refuseForm(refused)
	}

	return holdSubmit(func(m *model) tea.Cmd { return m.apply(request) })(m)
}

// refuseForm leaves the form up with why its answer was refused, rendering it again on the field
// the refusal names or when huh had finished it.
func (m *model) refuseForm(refused form.Refuse) tea.Cmd {
	m.form.refusal = refused.Err.Error()
	if refused.Field != "" {
		return m.rerenderFormOn(refused.Field)
	}
	if m.form.rendered.State == huh.StateCompleted {
		return m.rebuildForm()
	}

	return formResized()
}

// updateForm hands a message to the form on screen, and answers it either when huh says it is
// finished or on the submit key.
func (m *model) updateForm(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok && keys.IsBound(key.String(), fixedKeys().Submit) {
		return m.answerForm()
	}

	if key, ok := msg.(tea.KeyPressMsg); ok {
		if cmd, kept := m.updateFormVim(key); kept {
			return cmd
		}

		if m.form.back != nil && keys.IsBound(key.String(), fixedKeys().Back) {
			return m.form.back(m)
		}
	}

	cmd := m.stepForm(msg)

	switch m.form.rendered.State {
	case huh.StateCompleted:
		if m.writes.held.active {
			return cmd
		}

		return m.answerForm()
	case huh.StateAborted:
		return m.closeForm()
	case huh.StateNormal:
	}

	if m.form.Reshaped != nil && m.form.Reshaped() {
		key := ""
		if focused := m.form.rendered.GetFocusedField(); focused != nil {
			key = focused.GetKey()
		}

		return tea.Batch(cmd, m.rerenderFormOn(key))
	}

	return cmd
}

// stepForm hands a message to huh, keeping the form as rendered before a key that may finish it.
func (m *model) stepForm(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(tea.KeyPressMsg); ok && m.form.Build == nil {
		m.form.frozen = m.form.rendered.View()
	}

	form, cmd := m.form.rendered.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form.rendered = updated
	}

	return cmd
}

// formSizeOf turns a resize into the room the form has rather than the room the panel has, and
// leaves every other message alone.
func (m *model) formSizeOf(msg tea.Msg) tea.Msg {
	switch msg.(type) {
	case formResizedMsg, tea.WindowSizeMsg:
		width, height := m.formBox()

		return tea.WindowSizeMsg{Width: min(width, formMaxWidth), Height: height}
	default:
		return msg
	}
}

// leaveForm drops the form for an answer that goes on to another screen, which settles the window
// itself, so the screen the form was rendered over is not rendered again on the way.
func (m *model) leaveForm() {
	m.writes.held = heldWrite{}
	m.form = formState{}
}

// leaveFormBack does what the back key does on the form on screen.
func (m *model) leaveFormBack() tea.Cmd {
	if m.form.back != nil {
		return m.form.back(m)
	}

	return m.closeForm()
}

// closeForm drops the form and comes back to the screen it was rendered over.
func (m *model) closeForm() tea.Cmd {
	m.frame.Begin()
	defer m.frame.Flush()

	m.leaveForm()
	m.reconcileSolo()

	return m.enter(m.screen, screen.Target{Resume: true})
}

// fixedKeys is what every screen, menu and form answers.
func fixedKeys() config.FixedKeys {
	return config.Fixed()
}
