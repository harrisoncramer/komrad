package ui

import (
	"fmt"
	"strings"

	"komrad/internal/diff"
	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/lib/dates"

	tea "charm.land/bubbletea/v2"
)

// documentDiffPath is the name a document's diff is highlighted as.
const documentDiffPath = "document.md"

// documentHistory is one document's writes stepped through, drawing what the one stood on changed.
type documentHistory struct {
	item documentItem
	// versions is every write kept, newest first, and at the one stood on.
	versions []historyVersion
	at       int
	listing  bool
	err      error
	reads    map[int32]versionRead
	// pane is the change drawn as a diff.
	pane  diffview.Pane
	drawn drawnChange
	// filter is the pattern being searched for in the diff.
	filter filterState
	// said is what the last key on the diff said.
	said string
}

// historyVersion is one write kept of a document: which it is, who made it where that is known, and
// when.
type historyVersion struct {
	version   int32
	by        string
	createdAt string
}

// versionRead is one write's text as it was read, or why it could not be.
type versionRead struct {
	body    string
	pending bool
	err     error
}

// stepOlder steps back to the write before the one drawn, starting from the newest.
func (d *documentsScreen) stepOlder(h host, s panelShared) tea.Cmd {
	item, ok := d.hovered(s)
	if !ok || d.editing != nil {
		return nil
	}

	if d.history == nil || d.history.item.id != item.id || d.history.err != nil {
		d.history = &documentHistory{item: item}

		return h.waitToStage()
	}

	d.history.forgetFailedReads()
	if d.history.at+1 < len(d.history.versions) {
		d.history.at++
		d.history.pane.Drop()
	}

	return h.waitToStage()
}

// stepNewer steps forward to the write after the one drawn, and past the newest back to the
// document as it stands.
func (d *documentsScreen) stepNewer(h host, s panelShared) tea.Cmd {
	if !d.isSteppingHovered(s) {
		return nil
	}

	if d.history.at == 0 {
		d.history = nil
		if d.focus {
			d.focus = false
			h.focusDoc()
		}

		return nil
	}

	d.history.forgetFailedReads()
	d.history.at--
	d.history.pane.Drop()

	return h.waitToStage()
}

// forgetFailedReads drops the writes that could not be read, so the next step reads them again.
func (h *documentHistory) forgetFailedReads() {
	for version, read := range h.reads {
		if read.err != nil {
			delete(h.reads, version)
		}
	}
}

// isSteppingHovered reports whether the document under the cursor is being stepped through.
func (d *documentsScreen) isSteppingHovered(s panelShared) bool {
	item, ok := d.hovered(s)

	return ok && d.history != nil && d.history.item.id == item.id
}

// docRows is the document drawn beside the list at a width: the write stepped to and what it
// changed, or the document as it stands.
func (d *documentsScreen) docRows(s panelShared, width int) []string {
	item, ok := d.hovered(s)
	if !ok || !d.isSteppingHovered(s) {
		return paint.DocStyledRows(d.doc(s), max(width, 1), columnStyle())
	}

	h := d.history
	head := h.head(item, width, s.glyphs)
	if h.err != nil || h.versions == nil {
		return head
	}

	if !h.isDrawn(s.isDark) {
		return append(head, h.changeRows(width)...)
	}

	held := diffview.DimCursor
	if d.isDiffFocused(s) {
		held = diffview.LiveCursor
	}

	return append(head, h.pane.Render(width, d.diffHeight(s), held)...)
}

// head is the rows over the change: which write it is, the search, and what the last key said.
func (h *documentHistory) head(item documentItem, width int, glyphs glyph.Set) []string {
	notice := "_reading its versions…_"
	switch {
	case h.err != nil:
		notice = "could not list its versions: " + errui.GetReason(h.err)
	case h.versions != nil:
		notice = h.notice()
	}

	rows := paint.DocStyledRows(renderDocument(item.name, notice, "", ""), width, columnStyle())
	if h.err != nil || h.versions == nil {
		return rows
	}

	count := strings.TrimRight(diffMatchCount(&h.pane, h.filter), "\n")

	return append(rows, h.filter.head(count, h.said, width, glyphs)...)
}

// diffHeight is the rows the diff is drawn in, under the rows over it.
func (d *documentsScreen) diffHeight(s panelShared) int {
	split, ok := s.draw.screenColumnSplit()
	item, hovered := d.hovered(s)
	if !ok || !hovered || d.history == nil {
		return 1
	}

	return max(split.rows-len(d.history.head(item, split.column, s.glyphs)), 1)
}

// notice names the write stepped to, who made it and how long ago.
func (h *documentHistory) notice() string {
	current := h.versions[h.at]
	written := "written"
	if current.by != "" {
		written += " by " + current.by
	}

	notice := fmt.Sprintf("version %d of %d, %s %s ago",
		current.version, h.versions[0].version, written, label.Age(dates.Parse(current.createdAt)))
	if h.isOldestKeptLate() {
		notice += ", and the writes before it were not kept"
	}

	return "_" + notice + "_"
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

// changeRows is what stands in place of the diff while the change is not drawn as one.
func (h *documentHistory) changeRows(width int) []string {
	indent := strings.Repeat(" ", docMargin())

	before, after, ok := h.readChange()
	switch {
	case !ok:
		return []string{fitRow(indent+dimStyle.Render("reading…"), width)}
	case after.err != nil:
		return []string{fitRow(indent+"could not read: "+errui.GetReason(after.err), width)}
	case before.err != nil:
		return []string{fitRow(indent+"could not read: "+errui.GetReason(before.err), width)}
	case h.isOldestKeptLate():
		return paint.DocStyledRows(after.body+"\n", width, columnStyle())
	}

	return []string{fitRow(indent+dimStyle.Render("no change to the text"), width)}
}

// drawnChange is the two writes a change view was built between, and the background it was
// highlighted for.
type drawnChange struct {
	before int32
	after  int32
	isDark bool
}

// isDrawn reports whether the change is drawn as a diff, building it once per pair of writes when
// it is.
func (h *documentHistory) isDrawn(isDark bool) bool {
	if h.err != nil || h.versions == nil || h.isOldestKeptLate() {
		return false
	}

	before, after, ok := h.readChange()
	if !ok || after.err != nil || before.err != nil {
		return false
	}

	drawn := drawnChange{after: h.versions[h.at].version, isDark: isDark}
	if h.at+1 < len(h.versions) {
		drawn.before = h.versions[h.at+1].version
	}

	if h.pane.View() == nil || h.drawn != drawn {
		view := diffview.NewView(&diff.Patch{Files: []diff.File{diff.FromTexts(documentDiffPath, before.body, after.body)}}, isDark)
		view.SetHeadless(true)
		view.SetPattern(h.filter.pattern)
		h.pane, h.drawn = diffview.NewPane(view), drawn
	}

	return h.pane.View().Len() > 0
}

// documentVersionsListedMsg is the writes kept of a document come back, to the screen that asked.
type documentVersionsListedMsg struct {
	on       screen
	id       string
	versions []historyVersion
	err      error
}

// documentVersionReadMsg is one write's text come back, to the screen that asked.
type documentVersionReadMsg struct {
	on      screen
	id      string
	version int32
	read    versionRead
}

// readHistoryCmd lists the writes of the document being stepped through on a screen, then reads
// the one stood on and the one before it.
func (m *model) readHistoryCmd(on screen) tea.Cmd {
	d := m.documentsOn(on)
	if !d.isSteppingHovered(m.shared()) {
		return nil
	}

	h, clients, shelf := d.history, m.api, d.shelf
	if h.versions == nil {
		if h.listing || h.err != nil {
			return nil
		}

		h.listing = true
		id := h.item.id

		return func() tea.Msg {
			versions, err := shelf.listVersions(clients, id)

			return documentVersionsListedMsg{
				on:       on,
				id:       id,
				versions: versions,
				err:      err,
			}
		}
	}

	var cmds []tea.Cmd
	for _, at := range []int{h.at, h.at + 1} {
		if at < len(h.versions) {
			cmds = append(cmds, m.readVersionCmd(on, h.versions[at].version))
		}
	}

	return tea.Batch(cmds...)
}

// readVersionCmd reads one write of the document being stepped through on a screen, once.
func (m *model) readVersionCmd(on screen, version int32) tea.Cmd {
	d := m.documentsOn(on)
	h, clients, shelf := d.history, m.api, d.shelf
	if _, asked := h.reads[version]; asked {
		return nil
	}

	if h.reads == nil {
		h.reads = map[int32]versionRead{}
	}
	h.reads[version] = versionRead{pending: true}
	item := h.item

	return func() tea.Msg {
		body, err := shelf.readVersion(clients, item, version)
		if err != nil {
			return documentVersionReadMsg{
				on:      on,
				id:      item.id,
				version: version,
				read:    versionRead{err: err},
			}
		}

		return documentVersionReadMsg{
			on:      on,
			id:      item.id,
			version: version,
			read:    versionRead{body: body},
		}
	}
}

func (m *model) applyDocumentVersionsListed(msg documentVersionsListedMsg) tea.Cmd {
	h := m.documentsOn(msg.on).history
	if h == nil || h.item.id != msg.id {
		return nil
	}

	h.listing = false
	if msg.err != nil {
		h.err = msg.err

		return nil
	}

	h.versions, h.at = msg.versions, 0

	return m.readHistoryCmd(msg.on)
}

func (m *model) applyDocumentVersionRead(msg documentVersionReadMsg) {
	h := m.documentsOn(msg.on).history
	if h == nil || h.item.id != msg.id || h.reads == nil {
		return
	}

	h.reads[msg.version] = msg.read
}
