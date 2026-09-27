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
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// documentDiffPath is the name a document's diff is highlighted as.
const documentDiffPath = "document.md"

// documentHistory is one document's writes stepped through, drawing what the one stood on changed.
type documentHistory struct {
	id string
	// versions is every write kept, newest first, and at the one stood on.
	versions []*agentv1.DocumentVersion
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

// versionRead is one write's text as it was read, or why it could not be.
type versionRead struct {
	body    string
	pending bool
	err     error
}

// drawnChange is the two writes a change view was built between, and the background it was
// highlighted for.
type drawnChange struct {
	before int32
	after  int32
	isDark bool
}

// documentVersionsListedMsg is the writes kept of a document come back.
type documentVersionsListedMsg struct {
	id       string
	versions []*agentv1.DocumentVersion
	err      error
}

// documentVersionReadMsg is one write's text come back.
type documentVersionReadMsg struct {
	id      string
	version int32
	read    versionRead
}

// stepOlder steps back to the write before the one drawn, starting from the newest.
func (d *documentsScreen) stepOlder(h host, s panelShared) tea.Cmd {
	item, ok := d.hovered(s)
	if !ok || d.editing != nil {
		return nil
	}

	if d.history == nil || d.history.id != item.id || d.history.err != nil {
		d.history = &documentHistory{id: item.id}

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
		d.history, d.diffFocus = nil, false

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

	return ok && d.history != nil && d.history.id == item.id
}

// docRows is the document drawn beside the list at a width: the write stepped to and what it
// changed, or the document as it stands.
func (d *documentsScreen) docRows(s panelShared, width int) []string {
	item, ok := d.hovered(s)
	if !ok || !d.isSteppingHovered(s) {
		return paint.DocStyledRows(d.doc(s), width, columnStyle())
	}

	history := d.history
	head := history.head(item, width, s.glyphs)
	if history.err != nil || history.versions == nil {
		return head
	}

	if !history.ensureDrawn(s.isDark) {
		return append(head, history.changeRows(width)...)
	}

	held := diffview.DimCursor
	if d.isDiffFocused(s) {
		held = diffview.LiveCursor
	}

	return append(head, history.pane.Render(width, d.diffHeight(s), held)...)
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

	indent := strings.Repeat(" ", docMargin())
	if h.filter.typing || h.filter.isStanding() {
		count := strings.TrimRight(diffMatchCount(&h.pane, h.filter), "\n")
		rows = append(rows, fitRow(indent+h.filter.line(paint.Panel, glyphs)+count, width))
	}

	if h.said != "" {
		rows = append(rows, fitRow(indent+dimStyle.Render(h.said), width))
	}

	return append(rows, "")
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
	by := "the agent"
	if current.GetByPerson() {
		by = "you"
	}

	notice := fmt.Sprintf("version %d of %d, written by %s %s ago",
		current.GetVersion(), h.versions[0].GetVersion(), by, label.Age(dates.Parse(current.GetCreatedAt())))
	if h.isOldestKeptLate() {
		notice += ", and the writes before it were not kept"
	}

	return "_" + notice + "_"
}

// isOldestKeptLate reports whether the write stood on is the oldest kept but not the first
// written, as for a document written before its writes were kept.
func (h *documentHistory) isOldestKeptLate() bool {
	return h.at+1 >= len(h.versions) && h.versions[h.at].GetVersion() > 1
}

// getChange is the write stepped to and the one kept before it, and false while either is still
// being read.
func (h *documentHistory) getChange() (before, after versionRead, ok bool) {
	after, ok = h.reads[h.versions[h.at].GetVersion()]
	if h.at+1 < len(h.versions) {
		var held bool
		before, held = h.reads[h.versions[h.at+1].GetVersion()]
		ok = ok && held
	}

	return before, after, ok && !after.pending && !before.pending
}

// changeRows is what stands in place of the diff while the change is not drawn as one.
func (h *documentHistory) changeRows(width int) []string {
	indent := strings.Repeat(" ", docMargin())

	before, after, ok := h.getChange()
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

// ensureDrawn builds the change as a diff once per pair of writes and background, and reports
// whether there is one to draw.
func (h *documentHistory) ensureDrawn(isDark bool) bool {
	if h.err != nil || h.versions == nil || h.isOldestKeptLate() {
		return false
	}

	before, after, ok := h.getChange()
	if !ok || after.err != nil || before.err != nil {
		return false
	}

	drawn := drawnChange{after: h.versions[h.at].GetVersion(), isDark: isDark}
	if h.at+1 < len(h.versions) {
		drawn.before = h.versions[h.at+1].GetVersion()
	}

	if h.pane.GetView() == nil || h.drawn != drawn {
		view := diffview.NewView(&diff.Patch{Files: []diff.File{diff.CompareTexts(documentDiffPath, before.body, after.body)}}, isDark)
		view.SetHeadless(true)
		view.SetPattern(h.filter.pattern)
		h.pane, h.drawn = diffview.NewPane(view), drawn
	}

	return h.pane.GetView().Len() > 0
}

// readHistoryCmd lists the writes of the document being stepped through, then reads the one
// stood on and the one before it.
func (m *model) readHistoryCmd() tea.Cmd {
	docs := &m.documents
	if !docs.isSteppingHovered(m.shared()) {
		return nil
	}

	history, clients := docs.history, m.api
	if history.versions == nil {
		if history.listing || history.err != nil {
			return nil
		}

		history.listing = true
		id := history.id

		return func() tea.Msg {
			versions, err := clients.listDocumentVersions(id)

			return documentVersionsListedMsg{
				id:       id,
				versions: versions,
				err:      err,
			}
		}
	}

	var cmds []tea.Cmd
	for _, at := range []int{history.at, history.at + 1} {
		if at < len(history.versions) {
			cmds = append(cmds, m.readVersionCmd(history.versions[at].GetVersion()))
		}
	}

	return tea.Batch(cmds...)
}

// readVersionCmd reads one write of the document being stepped through, once.
func (m *model) readVersionCmd(version int32) tea.Cmd {
	history, clients := m.documents.history, m.api
	if _, asked := history.reads[version]; asked {
		return nil
	}

	if history.reads == nil {
		history.reads = map[int32]versionRead{}
	}
	history.reads[version] = versionRead{pending: true}
	id := history.id

	return func() tea.Msg {
		document, err := clients.getDocumentVersion(id, version)
		if err != nil {
			return documentVersionReadMsg{
				id:      id,
				version: version,
				read:    versionRead{err: err},
			}
		}

		return documentVersionReadMsg{
			id:      id,
			version: version,
			read:    versionRead{body: document.GetBody()},
		}
	}
}

func (m *model) applyDocumentVersionsListed(msg documentVersionsListedMsg) tea.Cmd {
	history := m.documents.history
	if history == nil || history.id != msg.id {
		return nil
	}

	history.listing = false
	if msg.err != nil {
		history.err = msg.err

		return nil
	}

	history.versions, history.at = msg.versions, 0

	return m.readHistoryCmd()
}

func (m *model) applyDocumentVersionRead(msg documentVersionReadMsg) {
	history := m.documents.history
	if history == nil || history.id != msg.id || history.reads == nil {
		return
	}

	history.reads[msg.version] = msg.read
}
