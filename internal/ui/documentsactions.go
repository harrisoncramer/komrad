package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

// actions is the documents screen as one table and the legend that names it.
func (d *documentsScreen) actions(m *model) keys.Spec[*model] {
	in := m.inputs()
	switch {
	case d.isDiffFocused(in) && d.isDiffRendered(m, in):
		return d.diffActions()
	case d.isDiffFocused(in):
		return d.stepActions()
	}

	return d.listActions()
}

// listActions is the keys on the list of documents.
func (d *documentsScreen) listActions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table:  d.table(),
		Legend: d.legend(),
	}
}

func (d *documentsScreen) table() []keys.Action[*model] {
	return slices.Concat(
		guarded(d.onDocuments, d.documentTable()),
		guarded(d.onDocumentTypes, d.documentTypeTable()),
	)
}

// guarded narrows each action to answer only while when holds.
func guarded(when func(*model) bool, actions []keys.Action[*model]) []keys.Action[*model] {
	return xslice.Map(actions, func(action keys.Action[*model]) keys.Action[*model] {
		inner := action.When
		action.When = func(m *model) bool { return when(m) && (inner == nil || inner(m)) }

		return action
	})
}

func (d *documentsScreen) documentTypeTable() []keys.Action[*model] {
	return append(d.documentTypeList().actions(),
		keys.Action[*model]{
			ID:   actionDocumentsBack,
			Keys: keys.FixedBack,
			Nav:  func(m *model, _ string) screen.Request { return d.back(m) },
		},
	)
}

func (d *documentsScreen) documentTable() []keys.Action[*model] {
	return slices.Concat(d.list().actions(), []keys.Action[*model]{
		{
			ID:   actionDocumentsOlder,
			Keys: func(k config.Keybindings) []string { return k.Documents.Older },
			Nav: func(m *model, _ string) screen.Request {
				return d.stepOlder(m, m.inputs())
			},
		},
		{
			ID:   actionDocumentsNewer,
			Keys: func(k config.Keybindings) []string { return k.Documents.Newer },
			Nav: func(m *model, _ string) screen.Request {
				return d.stepNewer(m, m.inputs())
			},
		},
		{
			ID:   actionDocumentsBack,
			Keys: keys.FixedBack,
			Nav: func(m *model, _ string) screen.Request {
				return d.back(m)
			},
		},
	})
}

func (d *documentsScreen) legend() []keys.LegendRow[*model] {
	return slices.Concat(keys.NavLegend[*model](), []keys.LegendRow[*model]{
		{
			Of:   []keys.ID{keys.FilterOpen},
			Desc: filterDescOf,
		},
		{
			Of:   []keys.ID{actionDocumentsOlder},
			Desc: func(*model) string { return "an earlier version" },
			Show: d.isOnDocument,
		},
		{
			Of:   []keys.ID{actionDocumentsNewer},
			Desc: func(*model) string { return "a later version" },
			Show: d.isOnDocument,
		},
		{
			Of:    []keys.ID{keys.FilterClear},
			Desc:  filterClearDescOf,
			Strip: true,
		},
		{
			Of:    []keys.ID{keys.FilterMatch},
			Desc:  func(*model) string { return "next/prev match" },
			Strip: true,
		},
		{
			Of:    []keys.ID{actionDocumentsBack},
			Desc:  func(*model) string { return "back" },
			Strip: true,
		},
	})
}

// diffActions is the keys on a write's diff: the ones every diff pane shares, stepping between
// the writes, and handing the keys back to the list.
func (d *documentsScreen) diffActions() keys.Spec[*model] {
	diff := d.diffKeys()
	step := d.stepActions()

	return keys.Spec[*model]{
		Table:  slices.Concat(step.Table, diff.table(), diff.shapeTable()),
		Legend: slices.Concat(diff.legend(), step.Legend),
	}
}

// stepActions is the keys on a focused diff with no write rendered yet.
func (d *documentsScreen) stepActions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table: []keys.Action[*model]{
			{
				ID:   actionDocumentsBack,
				Keys: keys.FixedBack,
				Nav: func(*model, string) screen.Request {
					d.focus = false

					return nil
				},
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(m *model, _ string) screen.Request { return screen.Hide{} },
			},
			{
				ID:   actionDocumentsOlder,
				Keys: func(k config.Keybindings) []string { return k.Documents.Older },
				Nav:  func(m *model, _ string) screen.Request { return d.stepOlder(m, m.inputs()) },
			},
			{
				ID:   actionDocumentsNewer,
				Keys: func(k config.Keybindings) []string { return k.Documents.Newer },
				Nav:  func(m *model, _ string) screen.Request { return d.stepNewer(m, m.inputs()) },
			},
		},
		Legend: []keys.LegendRow[*model]{
			{
				Of:   []keys.ID{actionDocumentsOlder},
				Desc: func(*model) string { return "an earlier version" },
			},
			{
				Of:   []keys.ID{actionDocumentsNewer},
				Desc: func(*model) string { return "a later version" },
			},
			{
				Of:    []keys.ID{actionDocumentsBack},
				Desc:  func(*model) string { return "back to the list" },
				Strip: true,
			},
		},
	}
}

// diffKeys is the diff of the write stepped to under the keys every diff pane shares.
func (d *documentsScreen) diffKeys() diffKeys {
	h := d.history
	if h == nil {
		h = &documentHistory{}
	}

	return diffKeys{
		keys:     d.deps.keys,
		register: d.deps.register,
		pane:     &h.pane,
		filter:   &h.filter,
		window:   func(m *model) diffview.Window { return d.diffWindow(m, m.inputs()) },
	}
}

// updateDiff is the diff taking the keys.
func (d *documentsScreen) updateDiff(m *model, key tea.KeyPressMsg) screen.Request {
	return d.actions(m).DispatchTop(d.deps.top, m, d.deps.keys, key.String())
}

// isDiffRendered reports whether the panel renders a write's change as a diff beside the list.
func (d *documentsScreen) isDiffRendered(m *model, in screen.Inputs) bool {
	if !in.Room.Split.On {
		return false
	}

	return d.isSteppingHovered(m, in) && d.history.isRendered(in.Room.Dark)
}

// isDiffFocused reports whether the diff of the write stepped to holds the keys.
func (d *documentsScreen) isDiffFocused(in screen.Inputs) bool {
	return d.focus && d.history != nil && in.Room.ListFocused
}

// isBlockFocused reports whether the diff of one of its writes holds the keys.
func (d *documentsScreen) isBlockFocused(m *model, in screen.Inputs) bool {
	return d.isDiffFocused(in)
}

func (d *documentsScreen) isBlockRendered(m *model, in screen.Inputs) bool {
	return d.isDiffRendered(m, in)
}

func (d *documentsScreen) focusBlock(*model) {
	d.focus = true
}

func (d *documentsScreen) blurBlock() {
	d.focus = false
}

// leaveScreen takes down an editor open on the stage.
func (d *documentsScreen) leaveScreen(m *model) screen.Request {
	d.renaming = nil

	return d.closeEdit(m)
}

func (d *documentsScreen) doesScrollColumn(m *model, in screen.Inputs) bool {
	return d.isBlockRendered(m, in)
}

func (d *documentsScreen) scrollColumn(m *model, rows int) screen.Request {
	return d.diffKeys().moveBy(m, rows)
}

// docKeys steps through the writes from the document as it stands, handing the keys to the diff.
func (d *documentsScreen) docKeys() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table: []keys.Action[*model]{
			{
				ID:   actionDocumentsOlder,
				Keys: func(k config.Keybindings) []string { return k.Documents.Older },
				Nav: func(m *model, _ string) screen.Request {
					d.focus = true

					return d.stepOlder(m, m.inputs())
				},
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(m *model, _ string) screen.Request { return screen.Hide{} },
			},
		},
		Legend: []keys.LegendRow[*model]{
			{
				Of:   []keys.ID{actionDocumentsOlder},
				Desc: func(*model) string { return "an earlier version" },
			},
		},
	}
}

// isOnDocument reports whether the cursor is on a document.
func (d *documentsScreen) isOnDocument(m *model) bool {
	_, ok := d.hovered(m, m.inputs())

	return ok
}
