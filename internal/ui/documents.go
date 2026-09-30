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
	typeName  string
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

// documentsScreen lists a shelf of documents, the ones one agent has written or the document
// types, and draws the one under the cursor beside the list.
type documentsScreen struct {
	rowCursor
	shelf   documentShelf
	agentID string
	// from is the screen the screen was opened from, and under the view that one sits under.
	from  screen
	under screen
	items []documentItem
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
	// focus says the picker handed the keys to the diff beside the list.
	focus bool
}

// documentRow is one line of the documents list.
type documentRow struct {
	item documentItem
}

func (r documentRow) key() string {
	return "doc:" + r.item.id
}

// enter opens on the documents of the agent the target names, forgetting another agent's, and
// keeps where the documents were opened from when coming back from the document types.
func (d *documentsScreen) enter(h host, _ panelShared, target screenTarget) tea.Cmd {
	var saved tea.Cmd
	if target.agentID != d.agentID {
		saved = d.closeEdit(h)
		*d = documentsScreen{shelf: d.shelf, agentID: target.agentID}
	}
	switch {
	case target.from == screenDocumentTypes:
	case d.shelf == shelfDocumentTypes || target.agentID != "":
		d.from, d.under = target.from, target.under
	}

	h.stageInfo()

	return tea.Batch(saved, h.waitToStage())
}

func (d *documentsScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	if d.history != nil && d.history.filter.typing {
		return d.diffKeys().updateFilter(h, s, key)
	}

	if d.isDiffFocused(s) {
		return d.updateDiff(h, s, key)
	}

	return d.list().update(h, s, key, d.actions(s))
}

func (d *documentsScreen) list() screenList[documentRow] {
	return screenList[documentRow]{
		cursor: func() *rowCursor { return &d.rowCursor },
		rows:   d.rows,
		name:   documentRowName,
		placed: func(h host, _ panelShared) { h.stageInfo() },
		moved: func(h host, _ panelShared) tea.Cmd {
			d.history, d.focus = nil, false

			return h.waitToStage()
		},
		quit: func(k config.Keybindings) []string { return d.shelf.keys(k).Quit },
		back: func(k config.Keybindings) []string { return d.shelf.keys(k).Back },
	}
}

func (d *documentsScreen) rows(s panelShared) []documentRow {
	return narrowed(s.chrome.filter, narrowedParams[documentRow]{
		rows: xslice.Map(d.itemsOf(s), func(item documentItem) documentRow {
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
		help:     d.help,
		empty:    d.emptyLine(),
		rows:     len(d.rows(s)),
		snapshot: func(pane listPane) view.Snapshot { return d.snapshot(s, pane) },
	}
}

// parent is the view the documents were opened from, or the one the documents the document types
// were opened from sit under.
func (d *documentsScreen) parent() screen {
	if d.shelf == shelfDocumentTypes {
		return d.under
	}

	return d.from
}

// crumbs runs through the agent the documents are of down to the version stepped to, and counts
// the documents while none is.
func (d *documentsScreen) crumbs(s panelShared) crumbs {
	trail := crumbs{trail: d.crumbRoot(s)}
	if item, ok := d.hovered(s); ok && d.history != nil && d.history.item.id == item.id && len(d.history.versions) > 0 {
		current := d.history.versions[d.history.at]
		trail.trail = append(trail.trail, item.name, fmt.Sprintf("v%d of %d", current.version, d.history.versions[0].version))

		return trail
	}
	if d.isListed() {
		trail.tally = fmt.Sprint(len(d.itemsOf(s)))
	}

	return trail
}

// crumbRoot is the trail down to the shelf: the agent the documents are of, and the document types
// under the documents they were opened from.
func (d *documentsScreen) crumbRoot(s panelShared) []string {
	if d.shelf != shelfDocumentTypes {
		return []string{d.agentName(s), "documents"}
	}

	if d.from == screenDocuments {
		return []string{d.agentName(s), "documents", "types"}
	}

	return []string{"document types"}
}

func (d *documentsScreen) emptyLine() string {
	switch {
	case d.err != nil:
		return "could not list: " + errui.GetReason(d.err)
	case !d.isListed():
		return "reading…"
	case d.shelf == shelfDocumentTypes:
		return "no document types"
	}

	return "no documents"
}

func (d *documentsScreen) agentName(s panelShared) string {
	if item, ok := s.listing.anyAgent(d.agentID); ok {
		return item.Agent.Name
	}

	return d.agentID
}

func (d *documentsScreen) help(s panelShared) keyMap {
	return d.actions(s).help(s)
}

// documentColumns is the gutter of the documents list: how long ago each was last written.
func documentColumns() view.Columns {
	return view.Columns{{Kind: view.ColumnText, Width: ageColumns}}
}

func (d *documentsScreen) snapshot(s panelShared, pane listPane) view.Snapshot {
	rows := d.rows(s)
	now := time.Now()
	caret := pane.caret
	if d.isDiffFocused(s) {
		caret = view.CaretDrawn
	}

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
		Caret:      caret,
		BlinkedOut: pane.blinkedOut,
		Focused:    rowKeyAt(rows, d.cursor),
		Pane:       pane.width,
		Columns:    documentColumns(),
	}
}

// doc is the markdown drawn beside the list for the document the cursor is on.
func (d *documentsScreen) doc(s panelShared) string {
	item, ok := d.hovered(s)
	if !ok {
		return renderDocument(d.emptyTitle(s), "_"+d.emptyLine()+"_", "", "")
	}

	read, asked := d.bodyOf(s, item.id)
	notice := fmt.Sprintf("_%s, written %s ago_", item.typeName, label.Age(item.updatedAt))
	if d.shelf == shelfDocumentTypes {
		notice = fmt.Sprintf("_version %d, written %s ago_", read.version, label.Age(item.updatedAt))
	}
	switch {
	case !asked || (read.pending && read.body == ""):
		return renderDocument(item.name, notice, "_reading…_", "")
	case read.err != nil:
		return renderDocument(item.name, notice, "could not read: "+errui.GetReason(read.err), "")
	}

	return renderDocument(item.name, notice, documentProse(item.name, read.body), "")
}

// emptyTitle heads the block beside an empty list.
func (d *documentsScreen) emptyTitle(s panelShared) string {
	if d.shelf == shelfDocumentTypes {
		return "Document types"
	}

	return d.agentName(s)
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

	return tea.Batch(m.readHoveredDocumentCmd(), m.readHistoryCmd(screenDocuments))
}

// documentsWritten is what the agent has written, as its row says.
func (m *model) documentsWritten(agentID string) documentsWritten {
	item, ok := m.listing.anyAgent(agentID)
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
			return documentsListedMsg{
				agentID: agentID,
				written: written,
				err:     err,
			}
		}

		return documentsListedMsg{
			agentID: agentID,
			written: written,
			items: xslice.Map(documents, func(document *agentv1.Document) documentItem {
				return documentItem{
					id:        document.GetId(),
					name:      document.GetName(),
					typeName:  document.GetType(),
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
	d.keepCursor(m.shared(), hovered)

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
			return documentReadMsg{
				agentID: agentID,
				id:      item.id,
				read:    documentBody{updatedAt: item.updatedAt, err: err},
			}
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
	_ columned       = (*documentsScreen)(nil)
	_ filtered       = (*documentsScreen)(nil)
	_ staged         = (*documentsScreen)(nil)
	_ columnScroller = (*documentsScreen)(nil)
	_ blocked        = (*documentsScreen)(nil)
	_ leaving        = (*documentsScreen)(nil)
)

func (d *documentsScreen) stage(h host, _ panelShared) {
	h.stageInfo()
}

func (d *documentsScreen) doesDrawFromListing() bool {
	return true
}

func (d *documentsScreen) takesEveryKey() bool {
	return d.history != nil && d.history.filter.typing
}

func (d *documentsScreen) column() columnSpec {
	on := d.shelf.screen()

	return columnSpec{
		floor: columnFloor,
		docRows: func(m model, width int) []string {
			return m.documentsOn(on).docRows(m.shared(), width)
		},
		holds: func(m model) bool { return m.documentsOn(on).editing == nil },
		read:  func(m model) bool { return !m.documentsOn(on).isSteppingHovered(m.shared()) },
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
