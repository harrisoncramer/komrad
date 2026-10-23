package schedules

import (
	"fmt"

	"komrad/internal/ui/api"
	"komrad/internal/ui/field"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// rename is a schedule's name as the user types over it in the list.
type rename struct {
	*field.NameEdit
	schedule panel.Schedule
}

func (w *Screen) renamingField() *field.NameEdit {
	if w.renaming == nil {
		return nil
	}

	return w.renaming.NameEdit
}

func (w *Screen) openRename(in screen.Inputs) screen.Request {
	if item, ok := w.selected(in); ok {
		w.renaming = &rename{NameEdit: field.NewNameEdit(item.Key(), item.schedule.Name), schedule: item.schedule}
	}

	return nil
}

// reconcileRename drops an open rename once the cursor leaves its row.
func (w *Screen) reconcileRename(in screen.Inputs) {
	if w.renaming != nil && !field.ReconcileNameEdit(w.renaming.NameEdit, w.listRows(in), w.list().Hovered(in), rowName) {
		w.renaming = nil
	}
}

func (w *Screen) updateRename(msg tea.Msg) screen.Request {
	switch w.renaming.Take(msg, w.deps.Register) {
	case field.NameGone:
		return screen.NewPostMessage(screen.Warning, w.renaming.GoneWarning())
	case field.NameHidden:
		return screen.Hide{}
	case field.NameCancelled:
		w.renaming = nil
	case field.NameSubmitted:
		return w.saveRename()
	case field.NameTyping:
	}

	return nil
}

func (w *Screen) saveRename() screen.Request {
	name, ok := w.renaming.Submit()
	if !ok {
		w.renaming = nil

		return nil
	}

	renamed := prefill(w.renaming.schedule)
	renamed.Name = name
	id := w.renaming.schedule.ID

	return w.deps.Clients.RenameWrite(api.RenameWriteParams{
		Doing:          "renaming " + w.renaming.Was(),
		FailureSummary: fmt.Sprintf("Could not rename schedule %q", w.renaming.Was()),
		Row:            w.renaming.Key(),
		Write:          func(a api.Clients) error { return a.UpdateSchedule(id, renamed) },
	})
}

// settleRename marks the field saved once its write has landed, or open for another try when it
// failed.
func (w *Screen) settleRename(in screen.Inputs, key string, err error) {
	if w.renaming == nil {
		return
	}
	w.renaming.Settle(key, err)

	w.reconcileRename(in)
}

// IsRenaming reports whether a schedule's name is being typed over.
func (w *Screen) IsRenaming() bool {
	return w.renaming != nil && !w.renaming.IsLanded()
}

// TypedLine is the name being typed over, while a rename is open.
func (w *Screen) TypedLine() (field.Line, bool) {
	if !w.IsRenaming() {
		return field.Line{}, false
	}

	return w.renaming.Input(), true
}
