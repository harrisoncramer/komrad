package ui

import (
	"fmt"
	"strings"
	"time"

	"komrad/config"
	"komrad/internal/errui"
	"komrad/internal/ui/paint/label"
	"komrad/internal/view"
	"komrad/lib/dates"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// documentItem is one document as the listing reported it, without its body.
type documentItem struct {
	id        string
	name      string
	kind      agentv1.DocumentKind
	updatedAt time.Time
}

// documentBody is one document's text as it was last read, or why it could not be.
type documentBody struct {
	updatedAt time.Time
	version   int32
	body      string
	pending   bool
	err       error
}

// documentsScreen lists the documents one agent has written and draws the one under the cursor
// beside the list.
type documentsScreen struct {
	rowCursor
	agentID string
	items   []documentItem
	// listedAt is the agent's newest write the list was read at, and listedCount how many it had
	// written.
	listedAt    *time.Time
	listedCount int32
	listed      bool
	listing     bool
	err         error
	bodies      map[string]documentBody
	editing     *documentEdit
	history     *documentHistory
}

// documentRow is one line of the documents list.
type documentRow struct {
	item documentItem
}

func (r documentRow) key() string {
	return "doc:" + r.item.id
}

// enter opens on the documents of the agent the target names, forgetting another agent's.
func (d *documentsScreen) enter(h host, _ panelShared, target screenTarget) tea.Cmd {
	var saved tea.Cmd
	if target.agentID != d.agentID {
		saved = d.closeEdit(h)
		*d = documentsScreen{agentID: target.agentID}
	}

	h.stageInfo()

	return tea.Batch(saved, h.waitToStage())
}

func (d *documentsScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return d.list().update(h, s, key, d.actions())
}

func (d *documentsScreen) list() screenList[documentRow] {
	return screenList[documentRow]{
		cursor: func() *rowCursor { return &d.rowCursor },
		rows:   d.rows,
		name:   documentRowName,
		placed: func(h host, _ panelShared) { h.stageInfo() },
		moved: func(h host, _ panelShared) tea.Cmd {
			d.history = nil

			return h.waitToStage()
		},
		quit: func(k config.Keybindings) []string { return k.Documents.Quit },
		back: func(k config.Keybindings) []string { return k.Documents.Back },
	}
}

func (d *documentsScreen) rows(s panelShared) []documentRow {
	return narrowed(s.chrome.filter, narrowedParams[documentRow]{
		rows: xslice.Map(d.items, func(item documentItem) documentRow {
			return documentRow{item: item}
		}),
		depth: func(documentRow) int { return 0 },
		name:  documentRowName,
	})
}

func documentRowName(row documentRow) string {
	return row.item.name
}

func (d *documentsScreen) hovered(s panelShared) (documentItem, bool) {
	row, ok := rowAt(d.rows(s), d.cursor)

	return row.item, ok
}

func (d *documentsScreen) view(s panelShared) string {
	return s.draw.columnListView(d.listParams(s))
}

func (d *documentsScreen) listParams(s panelShared) screenListParams {
	return screenListParams{
		title:    screenTitle(view.GlyphDocument, "Documents"),
		tally:    d.tally(s),
		help:     d.help,
		empty:    d.emptyLine(),
		rows:     len(d.rows(s)),
		snapshot: func(pane listPane) view.Snapshot { return d.snapshot(s, pane) },
	}
}

// tally names the agent the documents are of, and how many it has written.
func (d *documentsScreen) tally(s panelShared) string {
	name := d.agentName(s)
	if !d.listed {
		return name
	}

	return fmt.Sprintf("%s · %d", name, len(d.items))
}

func (d *documentsScreen) emptyLine() string {
	switch {
	case d.err != nil:
		return "could not list: " + errui.GetReason(d.err)
	case !d.listed:
		return "reading…"
	}

	return "no documents"
}

func (d *documentsScreen) agentName(s panelShared) string {
	if item, ok := s.listing.liveAgent(d.agentID); ok {
		return item.Agent.Name
	}

	return d.agentID
}

func (d *documentsScreen) help(s panelShared) keyMap {
	return d.actions().help(s)
}

// documentColumns is the gutter of the documents list: how long ago each was last written.
func documentColumns() view.Columns {
	return view.Columns{{Kind: view.ColumnText, Width: ageColumns}}
}

func (d *documentsScreen) snapshot(s panelShared, pane listPane) view.Snapshot {
	rows := d.rows(s)
	now := time.Now()

	return view.Snapshot{
		Rows: xslice.Map(rows, func(r documentRow) view.Row {
			return view.Row{
				Key:     r.key(),
				Kind:    view.KindResult,
				Name:    r.item.name,
				Matches: matchesIn(s.chrome.filter.pattern, r.item.name),
				Process: view.ProcessNone,
				Cells:   []view.Cell{mutedCell(label.AgeAt(r.item.updatedAt, now))},
			}
		}),
		Caret:         pane.caret,
		BarBlinkedOut: pane.blinkedOut,
		Focused:       rowKeyAt(rows, d.cursor),
		Pane:          pane.width,
		Columns:       documentColumns(),
	}
}

// documentKindLabel is a document's kind as the screen names it.
func documentKindLabel(kind agentv1.DocumentKind) string {
	switch kind {
	case agentv1.DocumentKind_DOCUMENT_KIND_RFC:
		return "RFC"
	case agentv1.DocumentKind_DOCUMENT_KIND_REPORT:
		return "report"
	case agentv1.DocumentKind_DOCUMENT_KIND_EXPLAINER:
		return "explainer"
	case agentv1.DocumentKind_DOCUMENT_KIND_NOTES:
		return "notes"
	case agentv1.DocumentKind_DOCUMENT_KIND_UNSPECIFIED:
	}

	return "document"
}

// doc is the markdown drawn beside the list for the document the cursor is on.
func (d *documentsScreen) doc(s panelShared) string {
	item, ok := d.hovered(s)
	if !ok {
		return renderDocument(d.agentName(s), "_"+d.emptyLine()+"_", "", "")
	}

	notice := fmt.Sprintf("_%s, written %s ago_", documentKindLabel(item.kind), label.Age(item.updatedAt))
	read, asked := d.bodies[item.id]
	switch {
	case !asked || (read.pending && read.body == ""):
		return renderDocument(item.name, notice, "_reading…_", "")
	case read.err != nil:
		return renderDocument(item.name, notice, "could not read: "+errui.GetReason(read.err), "")
	}

	return renderDocument(item.name, notice, documentProse(item.name, read.body), "")
}

// documentProse is a document's text without the heading that opens it when that heading only
// repeats the name.
func documentProse(name, body string) string {
	body = strings.TrimSpace(body)
	first, rest, _ := strings.Cut(body, "\n")
	heading, isHeading := strings.CutPrefix(first, "# ")
	if isHeading && strings.TrimSpace(heading) == strings.TrimSpace(name) {
		return strings.TrimSpace(rest)
	}

	return body
}

// documentsListedMsg is one agent's documents come back.
type documentsListedMsg struct {
	agentID string
	written documentsWritten
	items   []documentItem
	err     error
}

// documentsWritten is what an agent's row says it has written: how many, and when the newest was.
type documentsWritten struct {
	count  int32
	newest *time.Time
}

// documentReadMsg is one document's text come back.
type documentReadMsg struct {
	agentID string
	id      string
	read    documentBody
}

// readDocumentsCmd lists the documents again when the agent has written since they were last
// listed, and otherwise reads the one under the cursor.
func (m *model) readDocumentsCmd() tea.Cmd {
	if m.screen != screenDocuments {
		return nil
	}

	d := &m.documents
	written := m.documentsWritten(d.agentID)
	if !d.listed || d.listedCount != written.count || !sameStamp(d.listedAt, written.newest) {
		return m.listDocumentsCmd(written)
	}

	return tea.Batch(m.readHoveredDocumentCmd(), m.readHistoryCmd())
}

// documentsWritten is what the agent has written, as its row says.
func (m *model) documentsWritten(agentID string) documentsWritten {
	item, ok := m.listing.liveAgent(agentID)
	if !ok {
		return documentsWritten{}
	}

	return documentsWritten{count: item.Documents, newest: item.DocumentsUpdatedAt}
}

func sameStamp(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.Equal(*b)
}

// listDocumentsCmd asks for the agent's documents, once at a time.
func (m *model) listDocumentsCmd(written documentsWritten) tea.Cmd {
	d := &m.documents
	if d.listing {
		return nil
	}

	d.listing = true
	agentID, clients := d.agentID, m.api

	return func() tea.Msg {
		documents, err := clients.listDocuments(agentID)
		if err != nil {
			return documentsListedMsg{agentID: agentID, written: written, err: err}
		}

		return documentsListedMsg{
			agentID: agentID,
			written: written,
			items: xslice.Map(documents, func(document *agentv1.Document) documentItem {
				return documentItem{
					id:        document.GetId(),
					name:      document.GetName(),
					kind:      document.GetKind(),
					updatedAt: dates.Parse(document.GetUpdatedAt()),
				}
			}),
		}
	}
}

func (m *model) applyDocumentsListed(msg documentsListedMsg) tea.Cmd {
	d := &m.documents
	if msg.agentID != d.agentID {
		return nil
	}

	d.listing = false
	if msg.err != nil {
		d.err = msg.err

		return nil
	}

	hovered := d.list().hovered(m.shared())
	d.items, d.listedAt, d.listedCount, d.listed, d.err = msg.items, msg.written.newest, msg.written.count, true, nil
	rows := d.rows(m.shared())
	d.cursor = placeCursor(rowIndexOfKey(rows, hovered), d.cursor, len(rows))

	return m.readDocumentsCmd()
}

// readHoveredDocumentCmd reads the document under the cursor where the text held is older than
// the listing, once at a time.
func (m *model) readHoveredDocumentCmd() tea.Cmd {
	d := &m.documents
	item, ok := d.hovered(m.shared())
	if !ok {
		return nil
	}

	last, asked := d.bodies[item.id]
	if asked && (last.pending || !last.updatedAt.Before(item.updatedAt)) {
		return nil
	}

	if d.bodies == nil {
		d.bodies = map[string]documentBody{}
	}
	pending := last
	pending.pending = true
	d.bodies[item.id] = pending
	agentID, clients := d.agentID, m.api

	return func() tea.Msg {
		document, err := clients.getDocument(item.id)
		if err != nil {
			return documentReadMsg{agentID: agentID, id: item.id, read: documentBody{updatedAt: item.updatedAt, err: err}}
		}

		return documentReadMsg{agentID: agentID, id: item.id, read: documentBody{
			updatedAt: dates.Parse(document.GetUpdatedAt()),
			version:   document.GetVersion(),
			body:      document.GetBody(),
		}}
	}
}

func (m *model) applyDocumentRead(msg documentReadMsg) {
	d := &m.documents
	if msg.agentID != d.agentID || d.bodies == nil {
		return
	}

	d.bodies[msg.id] = msg.read
}

var (
	_ columned = (*documentsScreen)(nil)
	_ filtered = (*documentsScreen)(nil)
	_ staged   = (*documentsScreen)(nil)
)

func (d *documentsScreen) stage(h host, _ panelShared) {
	h.stageInfo()
}

func (d *documentsScreen) doesDrawFromListing() bool {
	return true
}

func (d *documentsScreen) takesEveryKey() bool {
	return false
}

func (d *documentsScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		docRows: func(m model, width int) []string {
			return m.documents.docRows(m.shared(), width)
		},
		holds: func(m model) bool { return m.documents.editing == nil },
	}
}

func (d *documentsScreen) hoveredKey(s panelShared) string {
	return d.list().hovered(s)
}

func (d *documentsScreen) placeAt(h host, s panelShared, key string) {
	d.list().place(h, s, key)
}

func (d *documentsScreen) filterRows(s panelShared) []filterRow {
	return d.list().filterRows(s)
}
