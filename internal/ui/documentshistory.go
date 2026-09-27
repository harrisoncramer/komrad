package ui

import (
	"fmt"
	"strings"

	"komrad/internal/diff"
	"komrad/internal/errui"
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
	// pan is how far the diff is scrolled sideways.
	pan   int
	view  *diffview.View
	drawn drawnChange
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

	if d.history == nil || d.history.id != item.id || d.history.err != nil {
		d.history = &documentHistory{id: item.id}

		return h.waitToStage()
	}

	d.history.forgetFailedReads()
	if d.history.at+1 < len(d.history.versions) {
		d.history.at++
		d.history.pan = 0
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

		return nil
	}

	d.history.forgetFailedReads()
	d.history.at--
	d.history.pan = 0

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

	return d.history.rows(item, width, s.isDark)
}

func (h *documentHistory) rows(item documentItem, width int, isDark bool) []string {
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

	return append(append(rows, ""), h.changeRows(width, isDark)...)
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

// changeRows is what the write stepped to changed from the one kept before it.
func (h *documentHistory) changeRows(width int, isDark bool) []string {
	indent := strings.Repeat(" ", docMargin())

	after, ok := h.reads[h.versions[h.at].GetVersion()]
	before := versionRead{}
	if h.at+1 < len(h.versions) {
		var held bool
		before, held = h.reads[h.versions[h.at+1].GetVersion()]
		ok = ok && held
	}

	switch {
	case !ok || after.pending || before.pending:
		return []string{fitRow(indent+dimStyle.Render("reading…"), width)}
	case after.err != nil:
		return []string{fitRow(indent+"could not read: "+errui.GetReason(after.err), width)}
	case before.err != nil:
		return []string{fitRow(indent+"could not read: "+errui.GetReason(before.err), width)}
	case h.isOldestKeptLate():
		return paint.DocStyledRows(after.body+"\n", width, columnStyle())
	}

	view := h.changeView(before.body, after.body, isDark)
	if view.Len() == 0 {
		return []string{fitRow(indent+dimStyle.Render("no change to the text"), width)}
	}

	rows := make([]string, 0, view.Len())
	for y := range view.Len() {
		rows = append(rows, view.RenderRow(y, diffview.NoCursor, width, h.pan))
	}

	return rows
}

// drawnChange is the two writes a change view was built between, and the background it was
// highlighted for.
type drawnChange struct {
	before int32
	after  int32
	isDark bool
}

// changeView is the diff between the write stepped to and the one before it, built once per pair.
func (h *documentHistory) changeView(before, after string, isDark bool) *diffview.View {
	drawn := drawnChange{after: h.versions[h.at].GetVersion(), isDark: isDark}
	if h.at+1 < len(h.versions) {
		drawn.before = h.versions[h.at+1].GetVersion()
	}

	if h.view != nil && h.drawn == drawn {
		return h.view
	}

	view := diffview.NewView(&diff.Patch{Files: []diff.File{diff.FromTexts(documentDiffPath, before, after)}}, isDark)
	view.SetHeadless(true)
	h.view, h.drawn = view, drawn

	return view
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

// readHistoryCmd lists the writes of the document being stepped through, then reads the one
// stood on and the one before it.
func (m *model) readHistoryCmd() tea.Cmd {
	d := &m.documents
	if !d.isSteppingHovered(m.shared()) {
		return nil
	}

	h, clients := d.history, m.api
	if h.versions == nil {
		if h.listing || h.err != nil {
			return nil
		}

		h.listing = true
		id := h.id

		return func() tea.Msg {
			versions, err := clients.listDocumentVersions(id)

			return documentVersionsListedMsg{id: id, versions: versions, err: err}
		}
	}

	var cmds []tea.Cmd
	for _, at := range []int{h.at, h.at + 1} {
		if at < len(h.versions) {
			cmds = append(cmds, m.readVersionCmd(h.versions[at].GetVersion()))
		}
	}

	return tea.Batch(cmds...)
}

// readVersionCmd reads one write of the document being stepped through, once.
func (m *model) readVersionCmd(version int32) tea.Cmd {
	h, clients := m.documents.history, m.api
	if _, asked := h.reads[version]; asked {
		return nil
	}

	if h.reads == nil {
		h.reads = map[int32]versionRead{}
	}
	h.reads[version] = versionRead{pending: true}
	id := h.id

	return func() tea.Msg {
		document, err := clients.getDocumentVersion(id, version)
		if err != nil {
			return documentVersionReadMsg{id: id, version: version, read: versionRead{err: err}}
		}

		return documentVersionReadMsg{id: id, version: version, read: versionRead{body: document.GetBody()}}
	}
}

func (m *model) applyDocumentVersionsListed(msg documentVersionsListedMsg) tea.Cmd {
	h := m.documents.history
	if h == nil || h.id != msg.id {
		return nil
	}

	h.listing = false
	if msg.err != nil {
		h.err = msg.err

		return nil
	}

	h.versions, h.at = msg.versions, 0

	return m.readHistoryCmd()
}

func (m *model) applyDocumentVersionRead(msg documentVersionReadMsg) {
	h := m.documents.history
	if h == nil || h.id != msg.id || h.reads == nil {
		return
	}

	h.reads[msg.version] = msg.read
}
