package documents

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
)

const (
	actionBack keys.ID = keys.Own + iota
	actionOlder
	actionNewer
)

// actions is the documents screen as one table and the legend that names it.
func (d *Screen) actions(in screen.Inputs) keys.Spec[*Screen] {
	switch {
	case d.isDiffFocused(in) && d.isDiffRendered(in):
		return d.diffActions(in)
	case d.isDiffFocused(in):
		return d.stepActions(in)
	}

	return d.listActions(in)
}

// listActions is the keys on the list of documents.
func (d *Screen) listActions(in screen.Inputs) keys.Spec[*Screen] {
	return keys.Spec[*Screen]{
		Table:  d.table(in),
		Legend: d.legend(in),
	}
}

func (d *Screen) table(in screen.Inputs) []keys.Action[*Screen] {
	return slices.Concat(
		guarded((*Screen).onDocuments, d.documentTable(in)),
		guarded((*Screen).onTypes, d.typeTable(in)),
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

func (d *Screen) typeTable(in screen.Inputs) []keys.Action[*Screen] {
	return append(keys.ListActions[*Screen](d.typeList(), in),
		keys.Action[*Screen]{
			ID:   actionBack,
			Keys: keys.FixedBack,
			Nav:  func(*Screen, string) screen.Request { return d.back() },
		},
	)
}

func (d *Screen) documentTable(in screen.Inputs) []keys.Action[*Screen] {
	return slices.Concat(keys.ListActions[*Screen](d.list(), in), []keys.Action[*Screen]{
		{
			ID:   actionOlder,
			Keys: func(k config.Keybindings) []string { return k.Documents.Older },
			Nav: func(*Screen, string) screen.Request {
				return d.stepOlder(in)
			},
		},
		{
			ID:   actionNewer,
			Keys: func(k config.Keybindings) []string { return k.Documents.Newer },
			Nav: func(*Screen, string) screen.Request {
				return d.stepNewer(in)
			},
		},
		{
			ID:   actionBack,
			Keys: keys.FixedBack,
			Nav: func(*Screen, string) screen.Request {
				return d.back()
			},
		},
	})
}

func (d *Screen) legend(in screen.Inputs) []keys.LegendRow[*Screen] {
	documents := d.list()
	onDocument := func(*Screen) bool { return d.isOnDocument(in) }

	return slices.Concat(keys.SelectionLegend[*Screen](documents, in), []keys.LegendRow[*Screen]{
		{
			ActionIDs: []keys.ID{keys.SelectToggle},
			Desc:      func(*Screen) string { return documents.ToggleDesc(in) },
			Show:      func(*Screen) bool { return !documents.IsSelecting() && d.isOnDocument(in) },
		},
	}, keys.NavLegend[*Screen](), []keys.LegendRow[*Screen]{
		{
			ActionIDs: []keys.ID{keys.FilterOpen},
			Desc:      keys.FilterDescOf[*Screen](in),
		},
		{
			ActionIDs: []keys.ID{actionOlder},
			Desc:      func(*Screen) string { return "an earlier version" },
			Show:      onDocument,
		},
		{
			ActionIDs: []keys.ID{actionNewer},
			Desc:      func(*Screen) string { return "a later version" },
			Show:      onDocument,
		},
		{
			ActionIDs: []keys.ID{keys.FilterClear},
			Desc:      keys.FilterClearDescOf[*Screen](in),
			Strip:     true,
		},
		{
			ActionIDs: []keys.ID{keys.FilterMatch},
			Desc:      func(*Screen) string { return "next/prev match" },
			Strip:     true,
		},
		{
			ActionIDs: []keys.ID{actionBack},
			Desc:      func(*Screen) string { return "back" },
			Strip:     true,
		},
	})
}

// diffActions is the keys on a write's diff: the ones every diff pane shares, stepping between
// the writes, and handing the keys back to the list.
func (d *Screen) diffActions(in screen.Inputs) keys.Spec[*Screen] {
	diff := d.diffKeys()
	step := d.stepActions(in)

	return keys.Spec[*Screen]{
		Table:  slices.Concat(step.Table, keys.DiffTable[*Screen](diff, in), keys.DiffShapeTable[*Screen](diff, in)),
		Legend: slices.Concat(keys.DiffLegend[*Screen](diff, in), step.Legend),
	}
}

// stepActions is the keys on a focused diff with no write rendered yet.
func (d *Screen) stepActions(in screen.Inputs) keys.Spec[*Screen] {
	return keys.Spec[*Screen]{
		Table: []keys.Action[*Screen]{
			{
				ID:   actionBack,
				Keys: keys.FixedBack,
				Nav: func(*Screen, string) screen.Request {
					d.focus = false

					return nil
				},
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(*Screen, string) screen.Request { return screen.Hide{} },
			},
			{
				ID:   actionOlder,
				Keys: func(k config.Keybindings) []string { return k.Documents.Older },
				Nav:  func(*Screen, string) screen.Request { return d.stepOlder(in) },
			},
			{
				ID:   actionNewer,
				Keys: func(k config.Keybindings) []string { return k.Documents.Newer },
				Nav:  func(*Screen, string) screen.Request { return d.stepNewer(in) },
			},
		},
		Legend: []keys.LegendRow[*Screen]{
			{
				ActionIDs: []keys.ID{actionOlder},
				Desc:      func(*Screen) string { return "an earlier version" },
			},
			{
				ActionIDs: []keys.ID{actionNewer},
				Desc:      func(*Screen) string { return "a later version" },
			},
			{
				ActionIDs: []keys.ID{actionBack},
				Desc:      func(*Screen) string { return "back to the list" },
				Strip:     true,
			},
		},
	}
}

// diffKeys is the diff of the write stepped to under the keys every diff pane shares.
func (d *Screen) diffKeys() keys.Diff {
	h := d.history
	if h == nil {
		h = &history{}
	}

	return keys.Diff{
		Keys:     d.deps.Keys,
		Register: d.deps.Register,
		Pane:     &h.pane,
		Filter:   &h.filter,
		Window:   d.diffWindow,
	}
}

// isDiffRendered reports whether the panel renders a write's change as a diff beside the list.
func (d *Screen) isDiffRendered(in screen.Inputs) bool {
	if !in.Room.OwnBlockSplit.IsDivided {
		return false
	}

	return d.isSteppingHovered(in) && d.history.isRendered(in.Room.IsDarkBackground)
}

// isDiffFocused reports whether the diff of the write stepped to holds the keys.
func (d *Screen) isDiffFocused(in screen.Inputs) bool {
	return d.focus && d.history != nil && in.Room.IsListFocused
}

// IsBlockFocused reports whether the diff of one of its writes holds the keys.
func (d *Screen) IsBlockFocused(in screen.Inputs) bool {
	return d.isDiffFocused(in)
}

// IsBlockRendered reports whether the block beside the list holds a write's diff.
func (d *Screen) IsBlockRendered(in screen.Inputs) bool {
	return d.isDiffRendered(in)
}

// FocusBlock hands the keys to the diff.
func (d *Screen) FocusBlock() {
	d.focus = true
}

// BlurBlock hands the keys back to the list.
func (d *Screen) BlurBlock() {
	d.focus = false
}

// LeaveScreen takes down an editor open on the stage.
func (d *Screen) LeaveScreen() screen.Request {
	d.renaming = nil

	return d.closeEdit()
}

// DoesScrollColumn reports whether a write's diff is rendered beside the list to scroll.
func (d *Screen) DoesScrollColumn(in screen.Inputs) bool {
	return d.IsBlockRendered(in)
}

// ScrollColumn scrolls the diff by a number of rows.
func (d *Screen) ScrollColumn(in screen.Inputs, rows int) screen.Request {
	return d.diffKeys().MoveBy(in, rows)
}

// docSpec steps through the writes from the document as it stands, handing the keys to the diff.
func (d *Screen) docSpec(in screen.Inputs) keys.Spec[*Screen] {
	return keys.Spec[*Screen]{
		Table: []keys.Action[*Screen]{
			{
				ID:   actionOlder,
				Keys: func(k config.Keybindings) []string { return k.Documents.Older },
				Nav: func(*Screen, string) screen.Request {
					d.focus = true

					return d.stepOlder(in)
				},
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(*Screen, string) screen.Request { return screen.Hide{} },
			},
		},
		Legend: []keys.LegendRow[*Screen]{
			{
				ActionIDs: []keys.ID{actionOlder},
				Desc:      func(*Screen) string { return "an earlier version" },
			},
		},
	}
}

// DocKeys is the table on the document as it stands.
func (d *Screen) DocKeys(in screen.Inputs) keys.Table {
	return d.docSpec(in)
}

// PressDoc sends a key to the table on the document as it stands.
func (d *Screen) PressDoc(in screen.Inputs, pressed string) screen.Request {
	return d.docSpec(in).Dispatch(d, d.deps.Keys, pressed)
}

// DocHelp is the legend of the table on the document as it stands.
func (d *Screen) DocHelp(in screen.Inputs) legend.Help {
	return d.docSpec(in).Help(d, d.deps.Keys)
}

// isOnDocument reports whether the cursor is on a document.
func (d *Screen) isOnDocument(in screen.Inputs) bool {
	_, ok := d.hovered(in)

	return ok
}

// ClickDiff puts the diff's reader on the row a click marked with key.
func (d *Screen) ClickDiff(in screen.Inputs, key string) screen.Request {
	return d.diffKeys().Click(in, key)
}
