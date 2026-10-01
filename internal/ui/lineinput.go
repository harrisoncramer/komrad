package ui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// lineInput is a one-row text field with a cursor, typed into with bubbles' keys.
type lineInput struct {
	model textinput.Model
}

func newLineInput(value string) lineInput {
	model := textinput.New()
	model.Prompt = ""
	model.KeyMap.Paste.SetEnabled(false)
	model.KeyMap.AcceptSuggestion.SetEnabled(false)
	model.KeyMap.NextSuggestion.SetEnabled(false)
	model.KeyMap.PrevSuggestion.SetEnabled(false)
	model.SetValue(value)
	model.CursorEnd()
	model.Focus()

	return lineInput{model: model}
}

func (l lineInput) value() string {
	return l.model.Value()
}

// set replaces the text and puts the cursor after it.
func (l *lineInput) set(value string) {
	*l = newLineInput(value)
}

// take hands the field a key or a paste, and reports whether it moved the cursor or changed the text.
func (l *lineInput) take(msg tea.Msg) bool {
	if !l.model.Focused() {
		l.set(l.model.Value())
	}
	if paste, ok := msg.(tea.PasteMsg); ok {
		msg = tea.PasteMsg{Content: pastedLine(paste)}
	}

	value, at := l.model.Value(), l.model.Position()
	l.model, _ = l.model.Update(msg)

	return l.model.Value() != value || l.model.Position() != at
}

// split is the text before the cursor and the text after it.
func (l lineInput) split() (string, string) {
	r := []rune(l.model.Value())
	at := min(max(l.model.Position(), 0), len(r))

	return string(r[:at]), string(r[at:])
}

// behind is how many runes follow the cursor.
func (l lineInput) behind() int {
	_, after := l.split()

	return len([]rune(after))
}
