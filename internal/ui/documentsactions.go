package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
)

// actions is the documents screen as one table and the legend that names it.
func (d *documentsScreen) actions(in screen.Inputs) keys.Spec[*documentsScreen] {
	switch {
	case d.isDiffFocused(in) && d.isDiffRendered(in):
		return d.diffActions(in)
	case d.isDiffFocused(in):
		return d.stepActions(in)
	}

	return d.listActions(in)
}

// listActions is the keys on the list of documents.
func (d *documentsScreen) listActions(in screen.Inputs) keys.Spec[*documentsScreen] {
	return keys.Spec[*documentsScreen]{
		Table:  d.table(in),
		Legend: d.legend(in),
	}
}

func (d *documentsScreen) table(in screen.Inputs) []keys.Action[*documentsScreen] {
	return slices.Concat(
		guarded((*documentsScreen).onDocuments, d.documentTable(in)),
		guarded((*documentsScreen).onDocumentTypes, d.documentTypeTable(in)),
	)
}

// guarded narrows each action to answer only while when holds.
func guarded[S any](when func(S) bool, actions []keys.Action[S]) []keys.Action[S] {
	return xslice.Map(actions, func(action keys.Action[S]) keys.Action[S] {
		inner := action.When
		action.When = func(s S) bool { return when(s) && (inner == nil || inner(s)) }

		return action
	})
}

func (d *documentsScreen) documentTypeTable(in screen.Inputs) []keys.Action[*documentsScreen] {
	return append(listActions[*documentsScreen](d.documentTypeList(), in),
		keys.Action[*documentsScreen]{
			ID:   actionDocumentsBack,
			Keys: keys.FixedBack,
			Nav:  func(*documentsScreen, string) screen.Request { return d.back() },
		},
	)
}

func (d *documentsScreen) documentTable(in screen.Inputs) []keys.Action[*documentsScreen] {
	return slices.Concat(listActions[*documentsScreen](d.list(), in), []keys.Action[*documentsScreen]{
		{
			ID:   actionDocumentsOlder,
			Keys: func(k config.Keybindings) []string { return k.Documents.Older },
			Nav: func(*documentsScreen, string) screen.Request {
				return d.stepOlder(in)
			},
		},
		{
			ID:   actionDocumentsNewer,
			Keys: func(k config.Keybindings) []string { return k.Documents.Newer },
			Nav: func(*documentsScreen, string) screen.Request {
				return d.stepNewer(in)
			},
		},
		{
			ID:   actionDocumentsBack,
			Keys: keys.FixedBack,
			Nav: func(*documentsScreen, string) screen.Request {
				return d.back()
			},
		},
	})
}

func (d *documentsScreen) legend(in screen.Inputs) []keys.LegendRow[*documentsScreen] {
	onDocument := func(*documentsScreen) bool { return d.isOnDocument(in) }

	return slices.Concat(keys.NavLegend[*documentsScreen](), []keys.LegendRow[*documentsScreen]{
		{
			Of:   []keys.ID{keys.FilterOpen},
			Desc: filterDescOf[*documentsScreen](in),
		},
		{
			Of:   []keys.ID{actionDocumentsOlder},
			Desc: func(*documentsScreen) string { return "an earlier version" },
			Show: onDocument,
		},
		{
			Of:   []keys.ID{actionDocumentsNewer},
			Desc: func(*documentsScreen) string { return "a later version" },
			Show: onDocument,
		},
		{
			Of:    []keys.ID{keys.FilterClear},
			Desc:  filterClearDescOf[*documentsScreen](in),
			Strip: true,
		},
		{
			Of:    []keys.ID{keys.FilterMatch},
			Desc:  func(*documentsScreen) string { return "next/prev match" },
			Strip: true,
		},
		{
			Of:    []keys.ID{actionDocumentsBack},
			Desc:  func(*documentsScreen) string { return "back" },
			Strip: true,
		},
	})
}

// diffActions is the keys on a write's diff: the ones every diff pane shares, stepping between
// the writes, and handing the keys back to the list.
func (d *documentsScreen) diffActions(in screen.Inputs) keys.Spec[*documentsScreen] {
	diff := d.diffKeys()
	step := d.stepActions(in)

	return keys.Spec[*documentsScreen]{
		Table:  slices.Concat(step.Table, diffTable[*documentsScreen](diff, in), diffShapeTable[*documentsScreen](diff, in)),
		Legend: slices.Concat(diffLegend[*documentsScreen](diff, in), step.Legend),
	}
}

// stepActions is the keys on a focused diff with no write rendered yet.
func (d *documentsScreen) stepActions(in screen.Inputs) keys.Spec[*documentsScreen] {
	return keys.Spec[*documentsScreen]{
		Table: []keys.Action[*documentsScreen]{
			{
				ID:   actionDocumentsBack,
				Keys: keys.FixedBack,
				Nav: func(*documentsScreen, string) screen.Request {
					d.focus = false

					return nil
				},
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(*documentsScreen, string) screen.Request { return screen.Hide{} },
			},
			{
				ID:   actionDocumentsOlder,
				Keys: func(k config.Keybindings) []string { return k.Documents.Older },
				Nav:  func(*documentsScreen, string) screen.Request { return d.stepOlder(in) },
			},
			{
				ID:   actionDocumentsNewer,
				Keys: func(k config.Keybindings) []string { return k.Documents.Newer },
				Nav:  func(*documentsScreen, string) screen.Request { return d.stepNewer(in) },
			},
		},
		Legend: []keys.LegendRow[*documentsScreen]{
			{
				Of:   []keys.ID{actionDocumentsOlder},
				Desc: func(*documentsScreen) string { return "an earlier version" },
			},
			{
				Of:   []keys.ID{actionDocumentsNewer},
				Desc: func(*documentsScreen) string { return "a later version" },
			},
			{
				Of:    []keys.ID{actionDocumentsBack},
				Desc:  func(*documentsScreen) string { return "back to the list" },
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
		window:   d.diffWindow,
	}
}

// isDiffRendered reports whether the panel renders a write's change as a diff beside the list.
func (d *documentsScreen) isDiffRendered(in screen.Inputs) bool {
	if !in.Room.Split.On {
		return false
	}

	return d.isSteppingHovered(in) && d.history.isRendered(in.Room.Dark)
}

// isDiffFocused reports whether the diff of the write stepped to holds the keys.
func (d *documentsScreen) isDiffFocused(in screen.Inputs) bool {
	return d.focus && d.history != nil && in.Room.ListFocused
}

// isBlockFocused reports whether the diff of one of its writes holds the keys.
func (d *documentsScreen) isBlockFocused(in screen.Inputs) bool {
	return d.isDiffFocused(in)
}

func (d *documentsScreen) isBlockRendered(in screen.Inputs) bool {
	return d.isDiffRendered(in)
}

func (d *documentsScreen) focusBlock() {
	d.focus = true
}

func (d *documentsScreen) blurBlock() {
	d.focus = false
}

// leaveScreen takes down an editor open on the stage.
func (d *documentsScreen) leaveScreen(m *model) screen.Request {
	d.renaming = nil

	return d.closeEdit()
}

func (d *documentsScreen) doesScrollColumn(in screen.Inputs) bool {
	return d.isBlockRendered(in)
}

func (d *documentsScreen) scrollColumn(m *model, rows int) screen.Request {
	return d.diffKeys().moveBy(m.inputs(), rows)
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

					return d.stepOlder(m.inputs())
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
func (d *documentsScreen) isOnDocument(in screen.Inputs) bool {
	_, ok := d.hovered(in)

	return ok
}
