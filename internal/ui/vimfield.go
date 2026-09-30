package ui

import (
	"slices"
	"strings"
	"unicode/utf8"

	"komrad/internal/vim"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// The labels naming a text field that holds the keys.
const (
	editModeLabel       = "  EDIT  "
	editNormalModeLabel = " EDIT:N "
)

// fieldVim is vim's state for the text fields of one form.
type fieldVim struct {
	register *vim.Register
	engines  map[string]*vim.Engine
	// focused is the field the engines last saw the cursor on.
	focused string
}

// newFieldVim is vim for one form's fields, nil when vim is off.
func newFieldVim(register *vim.Register) *fieldVim {
	if register == nil {
		return nil
	}

	return &fieldVim{register: register, engines: map[string]*vim.Engine{}}
}

// engine is the field's engine, put in insert mode when the cursor has come to the field some
// other way than a normal-mode j or k.
func (v *fieldVim) engine(key string, b vim.Buffer, singleLine bool) *vim.Engine {
	e := v.engineFor(key, b, singleLine)
	if v.focused != key {
		v.focused = key
		e.Insert(b)
	}

	return e
}

func (v *fieldVim) engineFor(key string, b vim.Buffer, singleLine bool) *vim.Engine {
	e, ok := v.engines[key]
	if !ok {
		e = vim.New(b, singleLine, v.register)
		v.engines[key] = e
	}

	return e
}

// press hands a key to the engine of the field with the cursor, and reports whether it was a
// text field.
func (v *fieldVim) press(field huh.Field, key string) (vim.Outcome, bool) {
	b, singleLine, ok := readField(field)
	if !ok {
		return vim.Pass, false
	}

	result := v.engine(field.GetKey(), b, singleLine).Key(key, b)
	if result.Outcome == vim.Consumed {
		writeField(field, result.Buffer)
	}

	return result.Outcome, true
}

// arrive puts the field the cursor was moved to by j or k in normal mode.
func (v *fieldVim) arrive(field huh.Field) bool {
	b, singleLine, ok := readField(field)
	if !ok {
		return false
	}

	key := field.GetKey()
	v.focused = key
	writeField(field, v.engineFor(key, b, singleLine).Normal(b))

	return true
}

// label names the mode of the field with the cursor.
func (v *fieldVim) label(key string) string {
	if v == nil || v.focused != key {
		return editModeLabel
	}

	if e, ok := v.engines[key]; ok && e.Mode() == vim.Normal {
		return editNormalModeLabel
	}

	return editModeLabel
}

// isTextField reports whether a field is one the keys type into.
func isTextField(field huh.Field) bool {
	switch field.(type) {
	case *huh.Input, *huh.Text:
		return true
	}

	return false
}

// readField is a text field's buffer, and whether it holds one line.
func readField(field huh.Field) (vim.Buffer, bool, bool) {
	var b vim.Buffer

	switch field := field.(type) {
	case *huh.Input:
		field.Edit(func(t *textinput.Model) { b = readInput(t) })

		return b, true, true
	case *huh.Text:
		field.Edit(func(t *textarea.Model) { b = readArea(t) })

		return b, false, true
	}

	return b, false, false
}

// writeField puts a buffer into a text field.
func writeField(field huh.Field, b vim.Buffer) {
	switch field := field.(type) {
	case *huh.Input:
		field.Edit(func(t *textinput.Model) { writeInput(t, b) })
	case *huh.Text:
		field.Edit(func(t *textarea.Model) { writeArea(t, b) })
	}
}

func readInput(t *textinput.Model) vim.Buffer {
	return vim.Buffer{Text: t.Value(), Cursor: t.Position()}
}

func writeInput(t *textinput.Model, b vim.Buffer) {
	if t.Value() != b.Text {
		t.SetValue(b.Text)
	}
	t.SetCursor(b.Cursor)
}

func readArea(t *textarea.Model) vim.Buffer {
	text := t.Value()
	lines := strings.Split(text, "\n")

	cursor := 0
	for _, line := range lines[:min(t.Line(), len(lines))] {
		cursor += utf8.RuneCountInString(line) + 1
	}

	return vim.Buffer{Text: text, Cursor: cursor + t.Column()}
}

// writeArea puts a buffer into a text area.
func writeArea(t *textarea.Model, b vim.Buffer) {
	if t.Value() != b.Text {
		t.SetValue(b.Text)
	}

	r := []rune(b.Text)
	row, col := 0, 0
	for _, c := range r[:min(b.Cursor, len(r))] {
		if c == '\n' {
			row, col = row+1, 0

			continue
		}
		col++
	}

	t.MoveToBegin()
	for range len(r) + 1 {
		if t.Line() >= row {
			break
		}
		t.CursorDown()
	}
	t.SetCursorColumn(col)
}

// updateFormVim hands a key to vim when the field with the cursor is a text field, and reports
// whether vim kept it.
func (m model) updateFormVim(key tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if m.form.vim == nil {
		return m, nil, false
	}

	focused := m.form.form.GetFocusedField()
	if focused == nil {
		return m, nil, false
	}

	outcome, ok := m.form.vim.press(focused, key.String())
	if !ok {
		return m, nil, false
	}

	switch outcome {
	case vim.Consumed:
		return m, nil, true
	case vim.EdgeDown, vim.EdgeUp:
		return m, m.stepVimField(focused.GetKey(), outcome == vim.EdgeDown), true
	case vim.Pass, vim.Leave:
	}

	return m, nil, false
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

		if m.form.vim.arrive(m.form.form.GetFocusedField()) {
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
