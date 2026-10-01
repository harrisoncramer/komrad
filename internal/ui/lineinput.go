package ui

import (
	"komrad/internal/vim"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// lineInput is a one-row text field with a cursor, typed into with bubbles' keys, and with vim's
// when the panel has a register.
type lineInput struct {
	model textinput.Model
	// engine is vim's state for the field, nil until vim is handed a key.
	engine *vim.Engine
}

// lineStep is what one message did to a field.
type lineStep int

const (
	// lineIgnored means the field left the message to whoever handed it over.
	lineIgnored lineStep = iota
	// lineKept means vim took the key and nothing moved.
	lineKept
	// lineChanged means the text or the cursor moved.
	lineChanged
	// lineLeft means esc was pressed in normal mode, which closes the field.
	lineLeft
)

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

// set replaces the text, puts the cursor after it and starts vim over in insert mode.
func (l *lineInput) set(value string) {
	*l = newLineInput(value)
}

// insert puts vim in insert mode with the cursor after the text.
func (l *lineInput) insert() {
	l.model.CursorEnd()
	if l.engine != nil {
		l.engine.Insert(readInput(&l.model))
	}
}

// take hands the field a key or a paste, with vim reading keys first when register is set.
func (l *lineInput) take(msg tea.Msg, register *vim.Register) lineStep {
	if !l.model.Focused() {
		l.set(l.model.Value())
	}
	if paste, ok := msg.(tea.PasteMsg); ok {
		msg = tea.PasteMsg{Content: pastedLine(paste)}
	}

	before := readInput(&l.model)

	if key, ok := msg.(tea.KeyPressMsg); ok && register != nil {
		if l.engine == nil {
			l.engine = vim.New(before, true, register, vimPasses())
		}

		result := l.engine.Key(key.String(), before)
		switch result.Outcome {
		case vim.Leave:
			return lineLeft
		case vim.Consumed:
			writeInput(&l.model, result.Buffer)

			return l.stepFrom(before, lineKept)
		case vim.EdgeUp, vim.EdgeDown:
			return lineKept
		case vim.Pass:
		}
	}

	l.model, _ = l.model.Update(msg)

	return l.stepFrom(before, lineIgnored)
}

// stepFrom is lineChanged when the field has moved off before, and still otherwise.
func (l lineInput) stepFrom(before vim.Buffer, still lineStep) lineStep {
	if readInput(&l.model) != before {
		return lineChanged
	}

	return still
}

// isInserting reports whether the field takes typed text rather than vim commands.
func (l lineInput) isInserting() bool {
	return l.engine == nil || l.engine.Mode() != vim.Normal
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
