package field

import (
	"fmt"
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/view"
	"komrad/internal/vim"

	tea "charm.land/bubbletea/v2"
)

// NameEdit is a row's name being typed over the place where the row renders.
type NameEdit struct {
	key   string
	was   string
	input Line
	// gone says the row left the list while the user typed its name.
	gone  bool
	noted bool
	// saving says the typed name has been sent and the field takes no keys until it lands.
	saving bool
	// landed says Komrad wrote the typed name and renders it until the listing shows it.
	landed bool
}

// nameStep is what one message did to a name being typed.
type nameStep int

const (
	// NameTyping means the name is still being typed.
	NameTyping nameStep = iota
	// NameCancelled means esc closed the field.
	NameCancelled
	// NameSubmitted means enter asked for the typed name to be saved.
	NameSubmitted
	// NameHidden means ctrl+c asked for the panel to hide.
	NameHidden
	// NameGone means a key first reached a field whose row is gone, which GoneWarning says.
	NameGone
)

// NewNameEdit opens a field over the row with key, holding its name.
func NewNameEdit(key, name string) *NameEdit {
	return &NameEdit{
		key:   key,
		was:   name,
		input: newLine(name),
	}
}

// Key is the key of the row whose name is typed over.
func (e *NameEdit) Key() string {
	return e.key
}

// Was is the row's name when the field opened.
func (e *NameEdit) Was() string {
	return e.was
}

// Input is the line the name is typed into.
func (e *NameEdit) Input() Line {
	return e.input
}

// IsLanded reports whether Komrad wrote the typed name.
func (e *NameEdit) IsLanded() bool {
	return e.landed
}

// Take hands the field a key or a paste, with vim reading keys first when register is set.
func (e *NameEdit) Take(msg tea.Msg, register *vim.Register) nameStep {
	if e.saving || e.landed {
		return NameTyping
	}
	if e.gone {
		return e.takeGone(msg)
	}

	switch msg := msg.(type) {
	case tea.PasteMsg:
		e.input.Take(msg, register)
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return NameHidden
		}

		switch e.input.Take(msg, register) {
		case LineLeft:
			return NameCancelled
		case LineEntered:
			return NameSubmitted
		case LineIgnored, LineKept, LineChanged:
		}
	}

	return NameTyping
}

// takeGone holds the keys of a field whose row is gone until it is let go.
func (e *NameEdit) takeGone(msg tea.Msg) nameStep {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return NameTyping
	}

	switch k := key.String(); {
	case k == "ctrl+c":
		return NameHidden
	case k == "esc" || slices.Contains(config.Fixed().Submit, k):
		return NameCancelled
	}

	if !e.noted {
		e.noted = true

		return NameGone
	}

	return NameTyping
}

// GoneWarning says the row being renamed is gone.
func (e *NameEdit) GoneWarning() string {
	return fmt.Sprintf("%q is gone, so there is nothing to rename; esc to stop", e.was)
}

// Reconcile says whether the field stays open while the cursor is on hovered, given the name its
// row is listed under and whether the row is listed at all.
func (e *NameEdit) Reconcile(hovered, name string, listed bool) bool {
	if e.landed {
		return listed && name == e.was
	}
	if e.saving || e.gone || hovered == e.key {
		return true
	}
	if listed {
		return false
	}

	e.gone = true

	return true
}

// ReconcileNameEdit says whether a field stays open over rows whose cursor is on hovered.
func ReconcileNameEdit[T interface{ Key() string }](e *NameEdit, rows []T, hovered string, name func(T) string) bool {
	at := slices.IndexFunc(rows, func(r T) bool { return r.Key() == e.Key() })
	if at < 0 {
		return e.Reconcile(hovered, "", false)
	}

	return e.Reconcile(hovered, name(rows[at]), true)
}

// Submit marks the field saving and gives the name to send, or false when there is nothing to.
func (e *NameEdit) Submit() (string, bool) {
	name := strings.TrimSpace(e.input.Value())
	if name == "" || name == e.was {
		return "", false
	}

	e.input.Set(name)
	e.saving = true

	return name, true
}

// Settle marks the field saved, or open for another try, once the write for row has landed.
func (e *NameEdit) Settle(row string, err error) {
	if !e.saving || e.key != row {
		return
	}

	e.saving = false
	e.landed = err == nil
}

// RenderedOver renders the typed name in place of the row's own.
func (e *NameEdit) RenderedOver(row view.Row) view.Row {
	if e == nil || e.key != row.Key {
		return row
	}

	row.Name = e.input.Value()
	row.Matches = nil
	row.Editing = !e.saving && !e.landed
	row.Behind = e.input.Behind()

	return row
}
