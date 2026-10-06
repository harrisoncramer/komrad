package ui

import (
	"fmt"
	"slices"
	"time"

	"komrad/internal/view"
	"komrad/lib/dates"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// documentItem is one document as the listing reported it, without its body.
type documentItem struct {
	id        string
	agentID   string
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

// documentKind is the kind of resource the documents view lists.
type documentKind int

const (
	documentsKindDocuments documentKind = iota
	documentsKindDocumentTypes
)

// documentsScreen lists documents, every agent's or one agent's, and draws the one under the cursor
// beside the list.
type documentsScreen struct {
	rowCursor
	agentID string
	// openedFrom is the screen one agent's documents were opened from.
	openedFrom    screen
	kind          documentKind
	kindFocus     bool
	documentTypes rowCursor
	items         []documentItem
	// listedAt is the newest write the list was read at, and listedCount how many were written.
	listedAt    *time.Time
	listedCount int32
	listed      bool
	listing     bool
	err         error
	bodies      map[string]documentBody
	editing     *documentEdit
	renaming    *documentRename
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

// documentTypeRow is one line of the document types list.
type documentTypeRow struct {
	documentType *agentv1.DocumentType
}

func (r documentTypeRow) key() string {
	return "type:" + r.documentType.GetName()
}

// enter opens on the documents of the agent the target names, or on every agent's for a target
// naming none, keeping the screen as it was when that is what it already lists.
func (d *documentsScreen) enter(m *model, target screenTarget) tea.Cmd {
	var saved tea.Cmd
	if target.agentID != d.agentID {
		saved = d.closeEdit(m)
		*d = documentsScreen{agentID: target.agentID}
	}
	if target.agentID != "" {
		d.openedFrom = target.openedFrom
	}

	m.stageTerminal()

	return tea.Batch(saved, m.waitToStage())
}

func (d *documentsScreen) update(m *model, msg tea.Msg) tea.Cmd {
	d.reconcileRename(m)
	if d.isRenaming() {
		return d.updateRename(m, msg)
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	if d.kind == documentsKindDocumentTypes {
		return d.documentTypeList().update(m, key, d.actions(m))
	}

	if d.history != nil && d.history.filter.typing {
		return d.diffKeys().updateFilter(m, key)
	}

	if d.isDiffFocused(m) {
		return d.updateDiff(m, key)
	}

	return d.list().update(m, key, d.actions(m))
}

func (d *documentsScreen) onDocuments(*model) bool {
	return d.kind == documentsKindDocuments
}

func (d *documentsScreen) onDocumentTypes(*model) bool {
	return d.kind == documentsKindDocumentTypes
}

func (d *documentsScreen) kinds(*model) stops {
	return stops{
		labels: []string{"documents", "types"},
		focus:  noStop,
		lit:    int(d.kind),
	}
}

// kindsFit draws the row of kinds only over every agent's documents.
func (d *documentsScreen) kindsFit(m *model) bool {
	return d.agentID == "" && m.listFitsTabs(d.listParams(m))
}

func (d *documentsScreen) kindsFocused(*model) *bool {
	return &d.kindFocus
}

// kindsSwitch holds the view on its documents while one is being edited.
func (d *documentsScreen) kindsSwitch(*model) bool {
	return d.agentID == "" && d.editing == nil
}

// stepKind moves the view between documents and their types.
func (d *documentsScreen) stepKind(m *model, delta int) tea.Cmd {
	d.kind = documentKind(stepStop(int(d.kind), delta, len(d.kinds(m).labels)))
	d.history, d.focus = nil, false
	m.stageTerminal()

	return m.waitToStage()
}

func (d *documentsScreen) list() screenList[documentRow] {
	return screenList[documentRow]{
		cursor: func() *rowCursor { return &d.rowCursor },
		rows:   d.rows,
		name:   documentRowName,
		placed: func(m *model) { m.stageTerminal() },
		moved: func(m *model) tea.Cmd {
			d.history, d.focus = nil, false

			return m.waitToStage()
		},
	}
}

func (d *documentsScreen) documentTypeList() screenList[documentTypeRow] {
	return screenList[documentTypeRow]{
		cursor: func() *rowCursor { return &d.documentTypes },
		rows:   d.documentTypeRows,
		name:   documentTypeRowName,
		placed: func(m *model) { m.stageTerminal() },
		moved:  func(m *model) tea.Cmd { return m.waitToStage() },
	}
}

func (d *documentsScreen) rows(m *model) []documentRow {
	return narrowed(m.filter, narrowedParams[documentRow]{
		rows: xslice.Map(d.items, func(item documentItem) documentRow {
			return documentRow{item: item}
		}),
		depth: func(documentRow) int { return 0 },
		name:  documentRowName,
	})
}

func (d *documentsScreen) documentTypeRows(m *model) []documentTypeRow {
	return narrowed(m.filter, narrowedParams[documentTypeRow]{
		rows: xslice.Map(m.listing.documentTypes, func(t *agentv1.DocumentType) documentTypeRow {
			return documentTypeRow{documentType: t}
		}),
		depth: func(documentTypeRow) int { return 0 },
		name:  documentTypeRowName,
	})
}

func documentRowName(row documentRow) string {
	return row.item.name
}

func documentTypeRowName(row documentTypeRow) string {
	return row.documentType.GetName()
}

func (d *documentsScreen) hovered(m *model) (documentItem, bool) {
	if d.kind != documentsKindDocuments {
		return documentItem{}, false
	}

	row, ok := rowAt(d.rows(m), d.cursor)

	return row.item, ok
}

// hoveredDocumentType is the document type the cursor is on, nil for none.
func (d *documentsScreen) hoveredDocumentType(m *model) *agentv1.DocumentType {
	if d.kind != documentsKindDocumentTypes {
		return nil
	}

	row, _ := rowAt(d.documentTypeRows(m), d.documentTypes.cursor)

	return row.documentType
}

func (d *documentsScreen) listParams(m *model) screenListParams {
	if d.kind == documentsKindDocumentTypes {
		return screenListParams{
			help:     d.help,
			empty:    "no document types",
			rows:     len(d.documentTypeRows(m)),
			snapshot: func(pane listPane) view.Snapshot { return d.documentTypeSnapshot(m, pane) },
		}
	}

	return screenListParams{
		help:     d.help,
		empty:    d.emptyLine(),
		rows:     len(d.rows(m)),
		snapshot: func(pane listPane) view.Snapshot { return d.snapshot(m, pane) },
	}
}

// parent is the view one agent's documents were opened from, and the documents view itself for
// every agent's.
func (d *documentsScreen) parent() screen {
	if d.agentID == "" {
		return screenDocuments
	}

	return d.openedFrom
}

// crumbs runs through the agent the documents are of down to the version stepped to, and counts
// the documents while none is.
func (d *documentsScreen) crumbs(m *model) crumbs {
	trail := crumbs{trail: d.crumbRoot(m)}
	if d.kind == documentsKindDocumentTypes {
		trail.tally = fmt.Sprint(len(m.listing.documentTypes))

		return trail
	}
	if item, ok := d.hovered(m); ok && d.history != nil && d.history.item.id == item.id && len(d.history.versions) > 0 {
		current := d.history.versions[d.history.at]
		trail.trail = append(trail.trail, item.name, fmt.Sprintf("v%d of %d", current.version, d.history.versions[0].version))

		return trail
	}
	if d.listed {
		trail.tally = fmt.Sprint(len(d.items))
	}

	return trail
}

// back leaves for the screen one agent's documents were opened from, and for the switcher from
// every agent's.
func (d *documentsScreen) back(m *model) tea.Cmd {
	if d.agentID == "" {
		return m.pickView()
	}

	return m.enter(d.openedFrom, screenTarget{agentID: d.agentID})
}

// crumbRoot is the trail down to the list: the agent the documents are of, if one.
func (d *documentsScreen) crumbRoot(m *model) []string {
	if d.agentID == "" {
		return nil
	}

	return []string{d.agentName(m), "documents"}
}

func (d *documentsScreen) agentName(m *model) string {
	return agentNameOf(m, d.agentID)
}

// agentNameOf is the name of the agent an id names, live or archived, and the id when neither.
func agentNameOf(m *model, agentID string) string {
	if item, ok := m.listing.anyAgent(agentID); ok {
		return item.Agent.Name
	}

	return agentID
}

func (d *documentsScreen) help(m *model) keyMap {
	return d.actions(m).help(m)
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

// documentsWritten is what the agent has written, as its row says, and what every agent has for
// none.
func (m *model) documentsWritten(agentID string) documentsWritten {
	if agentID == "" {
		var written documentsWritten
		for _, item := range slices.Concat(m.listing.agents, m.listing.archivedAgents) {
			written.count += item.Documents
			if item.DocumentsUpdatedAt != nil && (written.newest == nil || item.DocumentsUpdatedAt.After(*written.newest)) {
				written.newest = item.DocumentsUpdatedAt
			}
		}

		return written
	}

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
					agentID:   document.GetAgentId(),
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

	hovered := d.list().hovered(m)
	d.items, d.listedAt, d.listedCount, d.listed, d.err = msg.items, msg.written.newest, msg.written.count, true, nil
	d.keepCursor(m, hovered)

	return m.readDocumentsCmd()
}

// readHoveredDocumentCmd reads the document under the cursor where the text held is older than
// the listing, once at a time.
func (m *model) readHoveredDocumentCmd() tea.Cmd {
	d := &m.documents
	item, ok := d.hovered(m)
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
	_ listFilterer     = (*documentsScreen)(nil)
	_ columnFocuser    = (*documentsScreen)(nil)
	_ crumbDrawer      = (*documentsScreen)(nil)
	_ screenLeaver     = (*documentsScreen)(nil)
	_ rowStager        = (*documentsScreen)(nil)
	_ itemMenuBuilder  = (*documentsScreen)(nil)
	_ lineTyper        = (*documentsScreen)(nil)
	_ renamer          = (*documentsScreen)(nil)
	_ editorReconciler = (*documentsScreen)(nil)
	_ kindSwitcher     = (*documentsScreen)(nil)
)

func (d *documentsScreen) stage(m *model) {
	m.stageTerminal()
}

func (d *documentsScreen) doesDrawFromListing() bool {
	return true
}

func (d *documentsScreen) takesEveryKey() bool {
	return d.isRenaming() || (d.history != nil && d.history.filter.typing)
}

func (d *documentsScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		docRows: func(m *model, width int) []string {
			return m.documents.docRows(m, width)
		},
		holds: func(m *model) bool { return m.documents.editing == nil },
		read:  func(m *model) bool { return !m.documents.isSteppingHovered(m) },
	}
}

func (d *documentsScreen) hoveredKey(m *model) string {
	if d.kind == documentsKindDocumentTypes {
		return d.documentTypeList().hovered(m)
	}

	return d.list().hovered(m)
}

func (d *documentsScreen) placeAt(m *model, key string) {
	if d.kind == documentsKindDocumentTypes {
		d.documentTypeList().place(m, key)

		return
	}

	d.list().place(m, key)
}

func (d *documentsScreen) filterRows(m *model) []filterRow {
	if d.kind == documentsKindDocumentTypes {
		return d.documentTypeList().filterRows(m)
	}

	return d.list().filterRows(m)
}

func (d *documentsScreen) dispatches(m *model) []dispatching {
	return []dispatching{d.listActions(), d.diffActions(), m.docActions(), d.docKeys()}
}
