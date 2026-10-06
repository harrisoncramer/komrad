package ui

import (
	"slices"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
)

// actions is the documents screen as one table and the legend that names it.
func (d *documentsScreen) actions(m *model) screenActionSpec {
	switch {
	case d.isDiffFocused(m) && d.isDiffDrawn(m):
		return d.diffActions()
	case d.isDiffFocused(m):
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
	return slices.Concat(
		guarded(d.onDocuments, d.documentTable()),
		guarded(d.onDocumentTypes, d.documentTypeTable()),
	)
}

func (d *documentsScreen) documentTypeTable() []screenAction {
	return append(d.documentTypeList().actions(),
		screenAction{
			id:   actionDocumentsBack,
			keys: fixedBack,
			nav:  func(m *model, _ string) tea.Cmd { return d.back(m) },
		},
	)
}

func (d *documentsScreen) documentTable() []screenAction {
	return slices.Concat(d.list().actions(), []screenAction{
		{
			id:   actionDocumentsJumpToAgent,
			keys: func(k config.Keybindings) []string { return k.Documents.JumpToAgent },
			when: d.holdsADocument,
			nav:  func(m *model, _ string) tea.Cmd { return d.jumpToAgent(m) },
		},
		{
			id:   actionDocumentsOlder,
			keys: func(k config.Keybindings) []string { return k.Documents.Older },
			nav: func(m *model, _ string) tea.Cmd {
				return d.stepOlder(m)
			},
		},
		{
			id:   actionDocumentsNewer,
			keys: func(k config.Keybindings) []string { return k.Documents.Newer },
			nav: func(m *model, _ string) tea.Cmd {
				return d.stepNewer(m)
			},
		},
		{
			id:   actionDocumentsBack,
			keys: fixedBack,
			nav: func(m *model, _ string) tea.Cmd {
				return d.back(m)
			},
		},
	})
}

func (d *documentsScreen) legend() []screenLegendRow {
	return slices.Concat(screenNavLegend(), []screenLegendRow{
		{
			of:   []actionID{actionFilterOpen},
			desc: filterDescOf,
		},
		{
			of:   []actionID{actionDocumentsOlder},
			desc: func(*model) string { return "an earlier version" },
			show: d.holdsADocument,
		},
		{
			of:   []actionID{actionDocumentsNewer},
			desc: func(*model) string { return "a later version" },
			show: d.holdsADocument,
		},
		{
			of:    []actionID{actionDocumentsJumpToAgent},
			desc:  func(*model) string { return "jump to agent" },
			show:  d.holdsADocument,
			strip: true,
		},
		{
			of:    []actionID{actionFilterClear},
			desc:  filterClearDescOf,
			strip: true,
		},
		{
			of:    []actionID{actionFilterMatch},
			desc:  func(*model) string { return "next/prev match" },
			strip: true,
		},
		{
			of:    []actionID{actionDocumentsBack},
			desc:  func(*model) string { return "back" },
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
		table:  slices.Concat(step.table, keys.table(), keys.shapeTable()),
		legend: slices.Concat(keys.legend(), step.legend),
	}
}

// stepActions is the keys on a focused diff with no write drawn yet.
func (d *documentsScreen) stepActions() screenActionSpec {
	return screenActionSpec{
		table: []screenAction{
			{
				id:   actionDocumentsBack,
				keys: fixedBack,
				nav: func(*model, string) tea.Cmd {
					d.focus = false

					return nil
				},
			},
			{
				id:   actionQuit,
				keys: fixedQuit,
				nav:  func(m *model, _ string) tea.Cmd { return m.hide() },
			},
			{
				id:   actionDocumentsOlder,
				keys: func(k config.Keybindings) []string { return k.Documents.Older },
				nav:  func(m *model, _ string) tea.Cmd { return d.stepOlder(m) },
			},
			{
				id:   actionDocumentsNewer,
				keys: func(k config.Keybindings) []string { return k.Documents.Newer },
				nav:  func(m *model, _ string) tea.Cmd { return d.stepNewer(m) },
			},
		},
		legend: []screenLegendRow{
			{
				of:   []actionID{actionDocumentsOlder},
				desc: func(*model) string { return "an earlier version" },
			},
			{
				of:   []actionID{actionDocumentsNewer},
				desc: func(*model) string { return "a later version" },
			},
			{
				of:    []actionID{actionDocumentsBack},
				desc:  func(*model) string { return "back to the list" },
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
		window: d.diffWindow,
	}
}

// updateDiff is the diff taking the keys.
func (d *documentsScreen) updateDiff(m *model, key tea.KeyPressMsg) tea.Cmd {
	return d.actions(m).dispatchTop(m, key.String())
}

// isDiffDrawn reports whether a write's change is drawn as a diff beside the list.
func (d *documentsScreen) isDiffDrawn(m *model) bool {
	if _, ok := m.screenColumnSplit(); !ok {
		return false
	}

	return d.isSteppingHovered(m) && d.history.isDrawn(m.term.isDark)
}

// isDiffFocused reports whether the diff of the write stepped to holds the keys.
func (d *documentsScreen) isDiffFocused(m *model) bool {
	return d.focus && d.history != nil && m.isListFocused()
}

// isBlockFocused reports whether the diff of one of its writes holds the keys.
func (d *documentsScreen) isBlockFocused(m *model) bool {
	return d.isDiffFocused(m)
}

func (d *documentsScreen) isBlockDrawn(m *model) bool {
	return d.isDiffDrawn(m)
}

func (d *documentsScreen) focusBlock(*model) {
	d.focus = true
}

func (d *documentsScreen) blurBlock() {
	d.focus = false
}

// leaveScreen takes down an editor open on the stage.
func (d *documentsScreen) leaveScreen(m *model) tea.Cmd {
	d.renaming = nil

	return d.closeEdit(m)
}

func (d *documentsScreen) doesScrollColumn(m *model) bool {
	return d.isBlockDrawn(m)
}

func (d *documentsScreen) scrollColumn(m *model, rows int) tea.Cmd {
	return d.diffKeys().moveBy(m, rows)
}

// docKeys steps through the writes from the document as it stands, handing the keys to the diff.
func (d *documentsScreen) docKeys() screenActionSpec {
	return screenActionSpec{
		table: []screenAction{
			{
				id:   actionDocumentsOlder,
				keys: func(k config.Keybindings) []string { return k.Documents.Older },
				nav: func(m *model, _ string) tea.Cmd {
					d.focus = true

					return d.stepOlder(m)
				},
			},
			{
				id:   actionQuit,
				keys: fixedQuit,
				nav:  func(m *model, _ string) tea.Cmd { return m.hide() },
			},
		},
		legend: []screenLegendRow{
			{
				of:   []actionID{actionDocumentsOlder},
				desc: func(*model) string { return "an earlier version" },
			},
		},
	}
}

// holdsADocument reports whether the cursor is on a document.
func (d *documentsScreen) holdsADocument(m *model) bool {
	_, ok := d.hovered(m)

	return ok
}

// jumpToAgent lands the agents list or the archive on the agent that wrote the hovered document.
func (d *documentsScreen) jumpToAgent(m *model) tea.Cmd {
	item, ok := d.hovered(m)
	if !ok {
		return nil
	}

	target := screenTarget{agentID: item.agentID}
	if _, live := m.listing.liveAgent(target.agentID); live {
		return m.enter(screenAgents, target)
	}

	return m.enter(screenArchive, target)
}
