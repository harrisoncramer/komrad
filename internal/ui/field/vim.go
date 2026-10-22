package field

import (
	"slices"
	"strings"
	"unicode/utf8"

	"komrad/config"
	"komrad/internal/vim"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/huh/v2"
)

// Vim is vim's state for the text fields of one form.
type Vim struct {
	register *vim.Register
	engines  map[string]*vim.Engine
	// focused is the field the engines last saw the cursor on.
	focused string
}

// NewVim is vim for one form's fields, nil when vim is off.
func NewVim(register *vim.Register) *Vim {
	if register == nil {
		return nil
	}

	return &Vim{register: register, engines: map[string]*vim.Engine{}}
}

// engine is the field's engine, put in insert mode when the cursor has come to the field some
// other way than a normal-mode j or k.
func (v *Vim) engine(key string, b vim.Buffer, singleLine bool) *vim.Engine {
	e := v.engineFor(key, b, singleLine)
	if v.focused != key {
		v.focused = key
		e.Insert(b)
	}

	return e
}

func (v *Vim) engineFor(key string, b vim.Buffer, singleLine bool) *vim.Engine {
	e, ok := v.engines[key]
	if !ok {
		e = vim.New(b, singleLine, v.register, vimPasses())
		v.engines[key] = e
	}

	return e
}

// vimPasses is the keys normal mode leaves to the form.
func vimPasses() []string {
	keys := config.Fixed()

	return slices.Concat([]string{"tab", "shift+tab", "pgup", "pgdown"}, keys.Submit, keys.Editor, keys.Quit)
}

// Press hands a key to the engine of the field with the cursor, and reports whether it was a
// text field.
func (v *Vim) Press(form *huh.Form, field huh.Field, key string) (vim.Outcome, bool) {
	b, singleLine, ok := readField(field)
	if !ok {
		return vim.Pass, false
	}

	result := v.engine(field.GetKey(), b, singleLine).Key(key, b)
	if result.Outcome == vim.Consumed {
		writeField(field, result.Buffer)
		rerenderForm(form)
	}

	return result.Outcome, true
}

// Arrive puts the field the cursor was moved to by j or k in normal mode.
func (v *Vim) Arrive(form *huh.Form) bool {
	field := form.GetFocusedField()
	b, singleLine, ok := readField(field)
	if !ok {
		return false
	}

	key := field.GetKey()
	v.focused = key
	writeField(field, v.engineFor(key, b, singleLine).Normal(b))
	rerenderForm(form)

	return true
}

// rerenderMsg asks a form to render its fields again.
type rerenderMsg struct{}

// rerenderForm renders a form's fields again after vim wrote to one of them.
func rerenderForm(form *huh.Form) {
	_, _ = form.Update(rerenderMsg{})
}

// IsInserting reports whether the field with the cursor takes typed text rather than vim commands.
func (v *Vim) IsInserting(key string) bool {
	if v == nil || v.focused != key {
		return true
	}

	e, ok := v.engines[key]

	return !ok || e.Mode() != vim.Normal
}

// IsText reports whether a field is one the keys type into.
func IsText(field huh.Field) bool {
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
