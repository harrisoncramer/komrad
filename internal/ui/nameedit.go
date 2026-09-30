package ui

import (
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/view"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// nameEdit is a row's name being typed over where the row is drawn.
type nameEdit struct {
	key   string
	was   string
	typed string
	// saving says the typed name has been sent and the field takes no keys until it lands.
	saving bool
}

// nameEditStep is what one message did to a name being typed.
type nameEditStep int

const (
	nameEditTyping nameEditStep = iota
	nameEditCancelled
	nameEditSubmitted
	nameEditHidden
)

func openNameEdit(key, name string) *nameEdit {
	return &nameEdit{key: key, was: name, typed: name}
}

func (e *nameEdit) take(msg tea.Msg) nameEditStep {
	if e.saving {
		return nameEditTyping
	}

	switch msg := msg.(type) {
	case tea.PasteMsg:
		e.typed += pastedLine(msg)
	case tea.KeyPressMsg:
		switch k := msg.String(); {
		case k == "ctrl+c":
			return nameEditHidden
		case k == "esc":
			return nameEditCancelled
		case k == "enter" || isBound(k, formKeys().Submit):
			return nameEditSubmitted
		case k == "backspace":
			e.typed = paint.TrimLastRune(e.typed)
		case typedText(msg) != "":
			e.typed += typedText(msg)
		}
	}

	return nameEditTyping
}

// submit marks the field saving and gives the name to send, or false when there is nothing to.
func (e *nameEdit) submit() (string, bool) {
	name := strings.TrimSpace(e.typed)
	if name == "" || name == e.was {
		return "", false
	}

	e.typed = name
	e.saving = true

	return name, true
}

// settle says whether the field stays open once the write for row has landed.
func (e *nameEdit) settle(row string, err error) bool {
	if e == nil {
		return false
	}
	if !e.saving || e.key != row {
		return true
	}

	e.saving = false

	return err != nil
}

// drawnOver draws the typed name in place of the row's own.
func (e *nameEdit) drawnOver(row view.Row) view.Row {
	if e == nil || e.key != row.Key {
		return row
	}

	row.Name = e.typed
	row.Matches = nil
	row.Editing = !e.saving

	return row
}

// stampRename says on a write's result which row's rename it carried.
func stampRename(cmd tea.Cmd, row string) tea.Cmd {
	return func() tea.Msg {
		msg := cmd()

		result, ok := msg.(writeResult)
		if !ok {
			return msg
		}

		result.renamed = row

		return result
	}
}

func renameHelp() keyMap {
	save := binding(append([]string{"enter"}, formKeys().Submit...), "save")
	cancel := binding([]string{"esc"}, "cancel")

	return keyMap{short: []key.Binding{save, cancel}, full: []key.Binding{save, cancel}}
}

// renamer is a screen that types over a row's name in its list.
type renamer interface {
	isRenaming() bool
}
