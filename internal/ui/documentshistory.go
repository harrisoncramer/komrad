package ui

import (
	"komrad/internal/diff"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// documentDiffPath is the name the panel highlights a document's diff as.
const documentDiffPath = "document.md"

// documentHistory is one document's writes stepped through, rendering what the one stood on changed.
type documentHistory struct {
	item documentItem
	// versions is every write kept, newest first, and at the one stood on.
	versions []historyVersion
	at       int
	listing  bool
	err      error
	reads    map[int32]versionRead
	// pane is the change rendered as a diff.
	pane     diffview.Pane
	rendered renderedChange
	// filter is the pattern the user searches the diff for.
	filter filterState
}

// historyVersion is one write kept of a document: which it is, the name it had and who made it where
// those are known, and when.
type historyVersion struct {
	version   int32
	name      string
	by        string
	createdAt string
}

// versionRead is one write's text as it was read, or why it could not be.
type versionRead struct {
	body    string
	pending bool
	err     error
}

// stepOlder steps back to the write before the one rendered, starting from the newest.
func (d *documentsScreen) stepOlder(m *model) screen.Request {
	item, ok := d.hovered(m)
	if !ok || d.editing != nil {
		return nil
	}

	if d.history == nil || d.history.item.id != item.id || d.history.err != nil {
		d.history = &documentHistory{item: item}

		return screen.Settle{}
	}

	d.history.forgetFailedReads()
	if d.history.at+1 < len(d.history.versions) {
		d.history.at++
		d.history.pane.Drop()
	}

	return screen.Settle{}
}

// stepNewer steps forward to the write after the one rendered, and past the newest back to the
// document as it stands.
func (d *documentsScreen) stepNewer(m *model) screen.Request {
	if !d.isSteppingHovered(m) {
		return nil
	}

	if d.history.at == 0 {
		d.history = nil
		if d.focus {
			d.focus = false

			return screen.FocusDoc{}
		}

		return nil
	}

	d.history.forgetFailedReads()
	d.history.at--
	d.history.pane.Drop()

	return screen.Settle{}
}

// forgetFailedReads drops the writes that could not be read, so the next step reads them again.
func (h *documentHistory) forgetFailedReads() {
	for version, read := range h.reads {
		if read.err != nil {
			delete(h.reads, version)
		}
	}
}

// isSteppingHovered reports whether the user steps through the document under the cursor.
func (d *documentsScreen) isSteppingHovered(m *model) bool {
	item, ok := d.hovered(m)

	return ok && d.history != nil && d.history.item.id == item.id
}

// isOldestKeptLate reports whether the write stood on is the oldest kept but not the first
// written, as for a document written before its writes were kept.
func (h *documentHistory) isOldestKeptLate() bool {
	return h.at+1 >= len(h.versions) && h.versions[h.at].version > 1
}

// readChange is the write stepped to and the one kept before it, and false while either is still
// being read.
func (h *documentHistory) readChange() (versionRead, versionRead, bool) {
	after, ok := h.reads[h.versions[h.at].version]
	before := versionRead{}
	if h.at+1 < len(h.versions) {
		var held bool
		before, held = h.reads[h.versions[h.at+1].version]
		ok = ok && held
	}

	return before, after, ok && !after.pending && !before.pending
}

// renderedChange is the two writes a change view was built between, and the background it was
// highlighted for.
type renderedChange struct {
	before int32
	after  int32
	isDark bool
}

// isRendered reports whether the panel renders the change as a diff.
func (h *documentHistory) isRendered(isDark bool) bool {
	rendered, _, _, ok := h.change(isDark)

	return ok && h.pane.View() != nil && h.rendered == rendered && h.pane.View().Len() > 0
}

// keepDiff builds the diff of the change once per pair of writes.
func (h *documentHistory) keepDiff(isDark bool) {
	rendered, before, after, ok := h.change(isDark)
	if !ok || (h.pane.View() != nil && h.rendered == rendered) {
		return
	}

	view := diffview.NewView(&diff.Patch{Files: []diff.File{diff.FromTexts(documentDiffPath, before.body, after.body)}}, isDark)
	view.SetHeadless(true)
	view.SetPattern(h.filter.pattern)
	h.pane, h.rendered = diffview.NewPane(view), rendered
}

// change is the two writes the change is rendered between and what they hold, and false when it is
// not rendered as a diff.
func (h *documentHistory) change(isDark bool) (renderedChange, versionRead, versionRead, bool) {
	if h.err != nil || h.versions == nil || h.isOldestKeptLate() {
		return renderedChange{}, versionRead{}, versionRead{}, false
	}

	before, after, ok := h.readChange()
	if !ok || after.err != nil || before.err != nil {
		return renderedChange{}, versionRead{}, versionRead{}, false
	}

	rendered := renderedChange{after: h.versions[h.at].version, isDark: isDark}
	if h.at+1 < len(h.versions) {
		rendered.before = h.versions[h.at+1].version
	}

	return rendered, before, after, true
}

// documentVersionsListedMsg is the writes kept of a document come back.
type documentVersionsListedMsg struct {
	id       string
	versions []historyVersion
	err      error
}

// documentVersionReadMsg is one write's text come back.
type documentVersionReadMsg struct {
	id      string
	version int32
	read    versionRead
}

// readHistoryCmd lists the writes of the document being stepped through, then reads
// the one stood on and the one before it.
func (m *model) readHistoryCmd() tea.Cmd {
	d := &m.documents
	if !d.isSteppingHovered(m) {
		return nil
	}

	h, clients := d.history, d.deps.clients
	if h.versions == nil {
		if h.listing || h.err != nil {
			return nil
		}

		h.listing = true
		id := h.item.id

		return func() tea.Msg {
			versions, err := listVersions(clients, id)

			return documentVersionsListedMsg{
				id:       id,
				versions: versions,
				err:      err,
			}
		}
	}

	var cmds []tea.Cmd
	for _, at := range []int{h.at, h.at + 1} {
		if at < len(h.versions) {
			cmds = append(cmds, m.readVersionCmd(h.versions[at].version))
		}
	}

	return tea.Batch(cmds...)
}

// readVersionCmd reads one write of the document being stepped through, once.
func (m *model) readVersionCmd(version int32) tea.Cmd {
	d := &m.documents
	h, clients := d.history, d.deps.clients
	if _, asked := h.reads[version]; asked {
		return nil
	}

	if h.reads == nil {
		h.reads = map[int32]versionRead{}
	}
	h.reads[version] = versionRead{pending: true}
	item := h.item

	return func() tea.Msg {
		body, err := readVersion(clients, item, version)
		if err != nil {
			return documentVersionReadMsg{
				id:      item.id,
				version: version,
				read:    versionRead{err: err},
			}
		}

		return documentVersionReadMsg{
			id:      item.id,
			version: version,
			read:    versionRead{body: body},
		}
	}
}

func (d *documentsScreen) applyVersionsListed(m *model, msg documentVersionsListedMsg) screen.Request {
	h := d.history
	if h == nil || h.item.id != msg.id {
		return nil
	}

	h.listing = false
	if msg.err != nil {
		h.err = msg.err

		return nil
	}

	h.versions, h.at = msg.versions, 0

	return screen.Cmd(m.readHistoryCmd())
}

func (d *documentsScreen) applyVersionRead(msg documentVersionReadMsg) {
	h := d.history
	if h == nil || h.item.id != msg.id || h.reads == nil {
		return
	}

	h.reads[msg.version] = msg.read
}

// buildFrame builds the diff of the write stepped to once it is rendered beside the list.
func (d *documentsScreen) buildFrame(m *model) {
	if m.screenColumnSplit().On && d.isSteppingHovered(m) {
		d.history.keepDiff(m.term.isDark)
	}
}

// diffWindow is the size the panel renders the diff at, under the rows over it.
func (d *documentsScreen) diffWindow(m *model) diffview.Window {
	split := m.screenColumnSplit()
	item, hovered := d.hovered(m)
	if !split.On || !hovered || d.history == nil {
		return diffview.Window{Height: 1, Wrap: m.diffWrap}
	}

	return diffview.Window{
		Width:  split.Column,
		Height: max(split.Rows-len(d.history.head(m, item, split.Column)), 1),
		Wrap:   m.diffWrap,
	}
}
