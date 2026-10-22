package ui

import (
	"slices"

	"komrad/internal/vim"

	tea "charm.land/bubbletea/v2"
)

// updateFormVim hands a key to vim when the field with the cursor is a text field, and reports
// whether vim kept it.
func (m *model) updateFormVim(key tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.form.vim == nil {
		return nil, false
	}

	focused := m.form.form.GetFocusedField()
	if focused == nil {
		return nil, false
	}

	outcome, ok := m.form.vim.Press(m.form.form, focused, key.String())
	if !ok {
		return nil, false
	}

	switch outcome {
	case vim.Consumed:
		return nil, true
	case vim.EdgeDown, vim.EdgeUp:
		return m.stepVimField(focused.GetKey(), outcome == vim.EdgeDown), true
	case vim.Leave:
		return m.leaveFormBack(), true
	case vim.Pass:
	}

	return nil, false
}

// stepVimField moves the cursor to the nearest text field that way, arriving in normal mode,
// and leaves it where it was when there is none.
func (m *model) stepVimField(from string, down bool) tea.Cmd {
	at := slices.Index(m.form.order, from)
	if at < 0 {
		return nil
	}

	step, back, room := m.form.form.NextField, m.form.form.PrevField, len(m.form.order)-1-at
	if !down {
		step, back, room = back, step, at
	}

	cmds := make([]tea.Cmd, 0, room*2)
	for range room {
		cmds = append(cmds, step())

		if m.form.vim.Arrive(m.form.form) {
			return tea.Batch(cmds...)
		}
	}

	for range room {
		cmds = append(cmds, back())
	}

	return tea.Batch(cmds...)
}

// newRegister is the panel's register, nil when vim is off.
func newRegister(on bool) *vim.Register {
	if !on {
		return nil
	}

	return &vim.Register{}
}
