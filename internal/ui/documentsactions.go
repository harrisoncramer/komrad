package ui

import (
	"slices"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
)

// actions is the documents screen as one table and the legend that names it.
func (d *documentsScreen) actions(s panelShared) screenActionSpec {
	switch {
	case d.isDiffFocused(s) && d.isDiffDrawn(s):
		return d.diffActions()
	case d.isDiffFocused(s):
		return d.stepActions()
	}

	return d.listActions()
}

// listActions is the keys on the list of documents.
func (d *documentsScreen) listActions() screenActionSpec {
	return screenActionSpec{
		table:  d.table(),
		legend: d.legend(),
	}
}

func (d *documentsScreen) table() []screenAction {
	return append(d.list().actions(),
		screenAction{
			id:   actionDocumentsEdit,
			keys: func(k config.Keybindings) []string { return k.Documents.Edit },
			run: func(h host, s panelShared, _ string) tea.Cmd {
				return d.pressEdit(h, s)
			},
		},
		screenAction{
			id:   actionDocumentsNew,
			keys: func(k config.Keybindings) []string { return k.Documents.New },
			run: func(h host, s panelShared, _ string) tea.Cmd {
				return d.newDocumentForm(h, s)
			},
		},
		screenAction{
			id:   actionDocumentsOlder,
			keys: func(k config.Keybindings) []string { return k.Documents.Older },
			run: func(h host, s panelShared, _ string) tea.Cmd {
				return d.stepOlder(h, s)
			},
		},
		screenAction{
			id:   actionDocumentsNewer,
			keys: func(k config.Keybindings) []string { return k.Documents.Newer },
			run: func(h host, s panelShared, _ string) tea.Cmd {
				return d.stepNewer(h, s)
			},
		},
		screenAction{
			id:   actionDocumentsBack,
			keys: func(k config.Keybindings) []string { return k.Documents.Back },
			run: func(h host, _ panelShared, _ string) tea.Cmd {
				return h.enter(d.from, screenTarget{agentID: d.agentID})
			},
		},
	)
}

func (d *documentsScreen) legend() []screenLegendRow {
	return slices.Concat(screenNavLegend(), []screenLegendRow{
		{
			of:   []actionID{actionFilterOpen},
			desc: filterDescOf,
		},
		{
			of:   []actionID{actionDocumentsEdit},
			desc: func(panelShared) string { return d.editDesc() },
		},
		{
			of:   []actionID{actionDocumentsNew},
			desc: func(panelShared) string { return "new document" },
		},
		{
			of:   []actionID{actionDocumentsOlder},
			desc: func(panelShared) string { return "an earlier version" },
		},
		{
			of:   []actionID{actionDocumentsNewer},
			desc: func(panelShared) string { return "a later version" },
		},
		{
			of:    []actionID{actionFilterClear},
			desc:  filterClearDescOf,
			strip: true,
		},
		{
			of:    []actionID{actionFilterMatch},
			desc:  func(panelShared) string { return "next/prev match" },
			strip: true,
		},
		{
			of:    []actionID{actionDocumentsBack},
			desc:  func(panelShared) string { return "back" },
			strip: true,
		},
	})
}

// diffActions is the keys on a write's diff: the ones every diff pane shares, stepping between
// the writes, and handing the keys back to the list.
func (d *documentsScreen) diffActions() screenActionSpec {
	keys := d.diffKeys()
	step := d.stepActions()

	return screenActionSpec{
		table:  slices.Concat(step.table, keys.table()),
		legend: slices.Concat(keys.legend(), step.legend),
	}
}

// stepActions is the keys on a focused diff with no write drawn yet.
func (d *documentsScreen) stepActions() screenActionSpec {
	return screenActionSpec{
		table: []screenAction{
			{
				id:   actionDocumentsBack,
				keys: func(k config.Keybindings) []string { return k.Documents.Back },
				run: func(host, panelShared, string) tea.Cmd {
					d.focus = false

					return nil
				},
			},
			{
				id:   actionQuit,
				keys: func(k config.Keybindings) []string { return k.Documents.Quit },
				run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.hide() },
			},
			{
				id:   actionDocumentsOlder,
				keys: func(k config.Keybindings) []string { return k.Documents.Older },
				run:  func(h host, s panelShared, _ string) tea.Cmd { return d.stepOlder(h, s) },
			},
			{
				id:   actionDocumentsNewer,
				keys: func(k config.Keybindings) []string { return k.Documents.Newer },
				run:  func(h host, s panelShared, _ string) tea.Cmd { return d.stepNewer(h, s) },
			},
		},
		legend: []screenLegendRow{
			{
				of:   []actionID{actionDocumentsOlder},
				desc: func(panelShared) string { return "an earlier version" },
			},
			{
				of:   []actionID{actionDocumentsNewer},
				desc: func(panelShared) string { return "a later version" },
			},
			{
				of:    []actionID{actionDocumentsBack},
				desc:  func(panelShared) string { return "back to the list" },
				strip: true,
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
		pane:   &h.pane,
		filter: &h.filter,
		notice: &h.said,
		height: d.diffHeight,
	}
}

// updateDiff is the diff taking the keys.
func (d *documentsScreen) updateDiff(h host, s panelShared, key tea.KeyPressMsg) tea.Cmd {
	d.history.said = ""

	return d.actions(s).dispatch(h, s, key.String())
}

// isDiffDrawn reports whether a write's change is drawn as a diff beside the list.
func (d *documentsScreen) isDiffDrawn(s panelShared) bool {
	if _, ok := s.draw.screenColumnSplit(); !ok {
		return false
	}

	return d.isSteppingHovered(s) && d.history.isDrawn(s.isDark)
}

// isDiffFocused reports whether the diff of the write stepped to holds the keys.
func (d *documentsScreen) isDiffFocused(s panelShared) bool {
	return d.focus && d.history != nil && s.draw.isListFocused()
}

// isBlockFocused reports whether the diff of one of its writes holds the keys.
func (d *documentsScreen) isBlockFocused(s panelShared) bool {
	return d.isDiffFocused(s)
}

func (d *documentsScreen) isBlockDrawn(s panelShared) bool {
	return d.isDiffDrawn(s)
}

func (d *documentsScreen) focusBlock(panelShared) {
	d.focus = true
}

func (d *documentsScreen) blurBlock() {
	d.focus = false
}

// leave takes down an editor open on the stage.
func (d *documentsScreen) leave(h host) tea.Cmd {
	return d.closeEdit(h)
}

func (d *documentsScreen) doesScrollColumn(s panelShared) bool {
	return d.isBlockDrawn(s)
}

func (d *documentsScreen) scrollColumn(_ host, s panelShared, rows int) tea.Cmd {
	return d.diffKeys().scrollColumn(s, rows)
}

// docKeys steps through the writes from the document as it stands, handing the keys to the diff.
func (d *documentsScreen) docKeys() screenActionSpec {
	return screenActionSpec{
		table: []screenAction{
			{
				id:   actionDocumentsOlder,
				keys: func(k config.Keybindings) []string { return k.Documents.Older },
				run: func(h host, s panelShared, _ string) tea.Cmd {
					d.focus = true

					return d.stepOlder(h, s)
				},
			},
		},
		legend: []screenLegendRow{
			{
				of:   []actionID{actionDocumentsOlder},
				desc: func(panelShared) string { return "an earlier version" },
			},
		},
	}
}
