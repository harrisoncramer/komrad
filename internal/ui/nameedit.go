package ui

import (
	"fmt"
	"slices"
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
	// gone says the row was taken out of the list while its name was being typed.
	gone  bool
	noted bool
	// saving says the typed name has been sent and the field takes no keys until it lands.
	saving bool
	// landed says the typed name was written and is drawn until the listing shows it.
	landed bool
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

func (e *nameEdit) take(h host, msg tea.Msg) nameEditStep {
	if e.saving || e.landed {
		return nameEditTyping
	}
	if e.gone {
		return e.takeGone(h, msg)
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

// takeGone holds the keys of a field whose row is gone until it is let go.
func (e *nameEdit) takeGone(h host, msg tea.Msg) nameEditStep {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nameEditTyping
	}

	switch k := key.String(); {
	case k == "ctrl+c":
		return nameEditHidden
	case k == "esc" || k == "enter" || isBound(k, formKeys().Submit):
		return nameEditCancelled
	}

	if !e.noted {
		e.noted = true
		h.note(messageWarning, fmt.Sprintf("%q is gone, so there is nothing to rename; esc to stop", e.was))
	}

	return nameEditTyping
}

// reconcileNameEdit says whether a field stays open over rows whose cursor is on hovered.
func reconcileNameEdit[T keyed](e *nameEdit, rows []T, hovered string, name func(T) string) bool {
	at := slices.IndexFunc(rows, func(r T) bool { return r.key() == e.key })
	if e.landed {
		return at >= 0 && name(rows[at]) == e.was
	}
	if e.saving || e.gone || hovered == e.key {
		return true
	}
	if at >= 0 {
		return false
	}

	e.gone = true

	return true
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
	e.landed = err == nil

	return true
}

// drawnOver draws the typed name in place of the row's own.
func (e *nameEdit) drawnOver(row view.Row) view.Row {
	if e == nil || e.key != row.Key {
		return row
	}

	row.Name = e.typed
	row.Matches = nil
	row.Editing = !e.saving && !e.landed

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
