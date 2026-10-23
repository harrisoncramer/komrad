package field

import (
	"strings"

	"komrad/internal/vim"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// normalKey puts a line in vim's normal mode, the way ctrl+f opens vim's command-line window.
const normalKey = "ctrl+f"

// Line is a one-row text field with a cursor, typed into with bubbles' keys, and with vim's once
// normalKey is pressed while the panel has a register.
type Line struct {
	model textinput.Model
	// engine is vim's state for the field, nil until normalKey is first pressed.
	engine *vim.Engine
	// modal says normalKey was pressed since the line opened, so vim reads the keys and esc
	// leaves insert mode rather than closing the line.
	modal bool
}

// lineStep is what one message did to a line.
type lineStep int

const (
	// LineIgnored means the line left the message to whoever handed it over.
	LineIgnored lineStep = iota
	// LineKept means vim took the key and nothing moved.
	LineKept
	// LineChanged means the text or the cursor moved.
	LineChanged
	// LineLeft means esc was pressed outside vim's insert mode, which closes the line.
	LineLeft
	// LineEntered means enter was pressed and nothing else took it.
	LineEntered
)

// newLine is a focused line holding value, with the cursor after it.
func newLine(value string) Line {
	model := textinput.New()
	model.Prompt = ""
	model.KeyMap.Paste.SetEnabled(false)
	model.KeyMap.AcceptSuggestion.SetEnabled(false)
	model.KeyMap.NextSuggestion.SetEnabled(false)
	model.KeyMap.PrevSuggestion.SetEnabled(false)
	model.SetValue(value)
	model.CursorEnd()
	model.Focus()

	return Line{model: model}
}

// Value is the line's text.
func (l Line) Value() string {
	return l.model.Value()
}

// Set replaces the text, puts the cursor after it and leaves vim until normalKey is pressed.
func (l *Line) Set(value string) {
	*l = newLine(value)
}

// Insert puts the cursor after the text and leaves vim until normalKey is pressed again, as a
// line does when it is opened again.
func (l *Line) Insert() {
	l.model.CursorEnd()
	l.modal = false
	if l.engine != nil {
		l.engine.Insert(readInput(&l.model))
	}
}

// Take hands the line a key or a paste, with vim reading keys first once the line is modal.
func (l *Line) Take(msg tea.Msg, register *vim.Register) lineStep {
	if !l.model.Focused() {
		l.Set(l.model.Value())
	}
	if paste, ok := msg.(tea.PasteMsg); ok {
		msg = tea.PasteMsg{Content: pastedLine(paste)}
	}

	before := readInput(&l.model)

	if key, ok := msg.(tea.KeyPressMsg); ok && register != nil {
		if step, ok := l.takeVim(key.String(), before, register); ok {
			return step
		}
	}

	l.model, _ = l.model.Update(msg)

	step := l.stepFrom(before, LineIgnored)
	if key, ok := msg.(tea.KeyPressMsg); ok && step == LineIgnored {
		switch key.String() {
		case "esc":
			return LineLeft
		case "enter":
			return LineEntered
		}
	}

	return step
}

// takeVim hands a key to vim, and reports whether vim kept it from the line.
func (l *Line) takeVim(key string, before vim.Buffer, register *vim.Register) (lineStep, bool) {
	if !l.modal {
		if key != normalKey {
			return LineIgnored, false
		}

		l.modal = true
		if l.engine == nil {
			l.engine = vim.New(before, true, register, vimPasses())
		}
		writeInput(&l.model, l.engine.Normal(before))

		return l.stepFrom(before, LineKept), true
	}

	result := l.engine.Key(key, before)
	switch result.Outcome {
	case vim.Leave:
		return LineLeft, true
	case vim.Consumed:
		writeInput(&l.model, result.Buffer)

		return l.stepFrom(before, LineKept), true
	case vim.EdgeUp, vim.EdgeDown:
		return LineKept, true
	case vim.Pass:
	}

	return LineIgnored, false
}

// stepFrom is LineChanged when the line has moved off before, and still otherwise.
func (l Line) stepFrom(before vim.Buffer, still lineStep) lineStep {
	if readInput(&l.model) != before {
		return LineChanged
	}

	return still
}

// IsInserting reports whether the line takes typed text rather than vim commands.
func (l Line) IsInserting() bool {
	return !l.modal || l.engine.Mode() != vim.Normal
}

// Split is the text before the cursor and the text after it.
func (l Line) Split() (string, string) {
	r := []rune(l.model.Value())
	at := min(max(l.model.Position(), 0), len(r))

	return string(r[:at]), string(r[at:])
}

// Behind is how many runes follow the cursor.
func (l Line) Behind() int {
	_, after := l.Split()

	return len([]rune(after))
}

// pastedLine is pasted text as it lands in a line of one row.
func pastedLine(paste tea.PasteMsg) string {
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(paste.Content)
}
