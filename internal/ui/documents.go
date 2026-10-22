package ui

import (
	"fmt"
	"slices"
	"time"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/screen"
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
	taskID    string
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
	documentKindDocuments documentKind = iota
	documentKindTypes
)

// documentsScreen lists every agent's documents and renders the one under the cursor beside the list.
type documentsScreen struct {
	list.Cursor
	kind          documentKind
	kindFocus     bool
	documentTypes list.Cursor
	items         []documentItem
	revision      int
	built         derived[documentRowsKey, []documentRow]
	builtTypes    derived[listingKey, []documentTypeRow]
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
	// agentID is the agent the screen was opened for from its menu.
	agentID string
	// taskID is the task the screen was opened for from its row.
	taskID string
}

// documentRow is one line of the documents list.
type documentRow struct {
	item documentItem
}

func (r documentRow) Key() string {
	return "doc:" + r.item.id
}

// documentTypeRow is one line of the document types list.
type documentTypeRow struct {
	documentType *agentv1.DocumentType
}

func (r documentTypeRow) Key() string {
	return "type:" + r.documentType.GetName()
}

// enter opens on every agent's documents, filtered down to the task or agent the target names when one is.
func (d *documentsScreen) enter(m *model, target screen.Target) tea.Cmd {
	var saved tea.Cmd
	if !target.Resume {
		d.agentID, d.taskID = target.AgentID, target.TaskID
		if target.AgentID != "" || target.TaskID != "" {
			saved = d.closeEdit(m)
			d.kind, d.kindFocus, d.history, d.focus = documentKindDocuments, false, nil, false
			d.Cursor = list.Cursor{}
		}
		switch {
		case target.TaskID != "":
			m.filterDocuments(taskClause(m.listing.Tasks[target.TaskID].Slug))
		case target.AgentID != "":
			m.filterDocuments(agentClause(findAgentName(m, target.AgentID)))
		}
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

	if d.kind == documentKindTypes {
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
	return d.kind == documentKindDocuments
}

func (d *documentsScreen) onDocumentTypes(*model) bool {
	return d.kind == documentKindTypes
}

func (d *documentsScreen) kinds(*model) stops {
	return stops{
		labels: []string{"documents", "types"},
		focus:  noStop,
		lit:    int(d.kind),
	}
}

func (d *documentsScreen) kindsFit(m *model) bool {
	return m.listFitsTabs(d.listParams(m))
}

func (d *documentsScreen) kindsFocused(*model) *bool {
	return &d.kindFocus
}

// kindsSwitch holds the view on its documents while the user edits one.
func (d *documentsScreen) kindsSwitch(*model) bool {
	return d.editing == nil
}

// stepKind moves the view between documents and their types.
func (d *documentsScreen) stepKind(m *model, delta int) tea.Cmd {
	d.kind = documentKind(stepStop(int(d.kind), delta, len(d.kinds(m).labels)))
	d.history, d.focus = nil, false
	m.filter = filterState{}
	m.stageTerminal()

	return m.waitToStage()
}

func (d *documentsScreen) list() screenList[documentRow] {
	return screenList[documentRow]{
		cursor: func() *list.Cursor { return &d.Cursor },
		rows:   d.rows,
		name:   documentRowName,
		fits:   (*model).documentRowFits,
		placed: func(m *model) { m.stageTerminal() },
		moved: func(m *model) tea.Cmd {
			d.history, d.focus = nil, false

			return m.waitToStage()
		},
	}
}

func (d *documentsScreen) documentTypeList() screenList[documentTypeRow] {
	return screenList[documentTypeRow]{
		cursor: func() *list.Cursor { return &d.documentTypes },
		rows:   d.documentTypeRows,
		name:   documentTypeRowName,
		placed: func(m *model) { m.stageTerminal() },
		moved:  func(m *model) tea.Cmd { return m.waitToStage() },
	}
}

type documentRowsKey struct {
	revision int
	listing  listingKey
	agentID  string
	taskID   string
}

func (d *documentsScreen) rows(m *model) []documentRow {
	key := documentRowsKey{
		revision: d.revision,
		listing:  m.listingKey(),
		agentID:  d.agentID,
		taskID:   d.taskID,
	}

	return d.built.read(key, func() []documentRow {
		return narrowed(m.filter, narrowedParams[documentRow]{
			rows: xslice.Map(d.items, func(item documentItem) documentRow {
				return documentRow{item: item}
			}),
			depth: func(documentRow) int { return 0 },
			name:  documentRowName,
			fits:  m.documentRowFits,
		})
	})
}

func (d *documentsScreen) documentTypeRows(m *model) []documentTypeRow {
	return d.builtTypes.read(m.listingKey(), func() []documentTypeRow {
		return narrowed(m.filter, narrowedParams[documentTypeRow]{
			rows: xslice.Map(m.listing.DocumentTypes, func(t *agentv1.DocumentType) documentTypeRow {
				return documentTypeRow{documentType: t}
			}),
			depth: func(documentTypeRow) int { return 0 },
			name:  documentTypeRowName,
		})
	})
}

func documentRowName(row documentRow) string {
	return row.item.name
}

func documentTypeRowName(row documentTypeRow) string {
	return row.documentType.GetName()
}

func (d *documentsScreen) hovered(m *model) (documentItem, bool) {
	if d.kind != documentKindDocuments {
		return documentItem{}, false
	}

	row, ok := list.At(d.rows(m), d.Row)

	return row.item, ok
}

// hoveredDocumentType is the document type the cursor is on, nil for none.
func (d *documentsScreen) hoveredDocumentType(m *model) *agentv1.DocumentType {
	if d.kind != documentKindTypes {
		return nil
	}

	row, _ := list.At(d.documentTypeRows(m), d.documentTypes.Row)

	return row.documentType
}

func (d *documentsScreen) listParams(m *model) screenListParams {
	if d.kind == documentKindTypes {
		return screenListParams{
			help:     d.help,
			empty:    "no document types",
			rows:     len(d.documentTypeRows(m)),
			snapshot: func(pane list.Pane) view.Snapshot { return d.documentTypeSnapshot(m, pane) },
		}
	}

	return screenListParams{
		help:     d.help,
		empty:    d.emptyLine(),
		rows:     len(d.rows(m)),
		snapshot: func(pane list.Pane) view.Snapshot { return d.snapshot(m, pane) },
	}
}

func (d *documentsScreen) parent() screen.ID {
	return screen.Documents
}

// crumbs runs down to the version stepped to, and counts the documents while none is.
func (d *documentsScreen) crumbs(m *model) crumbs {
	var trail crumbs
	if d.kind == documentKindTypes {
		trail.tally = fmt.Sprint(len(m.listing.DocumentTypes))

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

func (d *documentsScreen) back(m *model) tea.Cmd {
	return m.pickView()
}

// findAgentName is the name of the agent an id names, live or archived, and the id when neither.
func findAgentName(m *model, agentID string) string {
	if item, ok := m.listing.AnyAgent(agentID); ok {
		return m.listing.AgentName(item.Agent)
	}

	return agentID
}

func (d *documentsScreen) help(m *model) keyMap {
	return m.helpOf(d.actions(m))
}

// documentsListedMsg is the documents come back.
type documentsListedMsg struct {
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
	id   string
	read documentBody
}

// readDocumentsCmd lists the documents again when any agent has written since they were last
// listed, and otherwise reads the one under the cursor.
func (m *model) readDocumentsCmd() tea.Cmd {
	if m.screen != screen.Documents {
		return nil
	}

	d := &m.documents
	written := m.documentsWritten()
	if !d.listed || d.listedCount != written.count || !sameStamp(d.listedAt, written.newest) {
		return m.listDocumentsCmd(written)
	}

	return tea.Batch(m.readHoveredDocumentCmd(), m.readHistoryCmd())
}

// documentsWritten is what every agent has written, as their rows say.
func (m *model) documentsWritten() documentsWritten {
	var written documentsWritten
	for _, item := range slices.Concat(m.listing.Agents, m.listing.ArchivedAgents) {
		written.count += item.Documents
		if item.DocumentsUpdatedAt != nil && (written.newest == nil || item.DocumentsUpdatedAt.After(*written.newest)) {
			written.newest = item.DocumentsUpdatedAt
		}
	}

	return written
}

func sameStamp(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.Equal(*b)
}

// listDocumentsCmd asks for the documents, once at a time.
func (m *model) listDocumentsCmd(written documentsWritten) tea.Cmd {
	d := &m.documents
	if d.listing {
		return nil
	}

	d.listing = true
	clients := m.api

	return func() tea.Msg {
		documents, err := clients.listDocuments()
		if err != nil {
			return documentsListedMsg{
				written: written,
				err:     err,
			}
		}

		return documentsListedMsg{
			written: written,
			items: xslice.Map(documents, func(document *agentv1.Document) documentItem {
				return documentItem{
					id:        document.GetId(),
					agentID:   document.GetAgentId(),
					taskID:    document.GetTaskId(),
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
	d.listing = false
	if msg.err != nil {
		d.err = msg.err

		return nil
	}

	hovered := d.list().hovered(m)
	d.items, d.listedAt, d.listedCount, d.listed, d.err = msg.items, msg.written.newest, msg.written.count, true, nil
	d.revision++
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
	clients := m.api

	return func() tea.Msg {
		document, err := clients.getDocument(item.id)
		if err != nil {
			return documentReadMsg{
				id:   item.id,
				read: documentBody{updatedAt: item.updatedAt, err: err},
			}
		}

		return documentReadMsg{id: item.id, read: documentBody{
			updatedAt: dates.Parse(document.GetUpdatedAt()),
			version:   document.GetVersion(),
			body:      document.GetBody(),
		}}
	}
}

func (m *model) applyDocumentRead(msg documentReadMsg) {
	d := &m.documents
	if d.bodies == nil {
		return
	}

	d.bodies[msg.id] = msg.read
}

var (
	_ activeLister     = (*documentsScreen)(nil)
	_ columnFocuser    = (*documentsScreen)(nil)
	_ crumbRenderer    = (*documentsScreen)(nil)
	_ screenLeaver     = (*documentsScreen)(nil)
	_ rowStager        = (*documentsScreen)(nil)
	_ itemMenuBuilder  = (*documentsScreen)(nil)
	_ lineTyper        = (*documentsScreen)(nil)
	_ renamer          = (*documentsScreen)(nil)
	_ editorReconciler = (*documentsScreen)(nil)
	_ kindSwitcher     = (*documentsScreen)(nil)
	_ recordRenderer   = (*documentsScreen)(nil)
)

func (d *documentsScreen) stage(m *model) {
	m.stageTerminal()
}

func (d *documentsScreen) doesRenderFromListing() bool {
	return true
}

func (d *documentsScreen) takesEveryKey() bool {
	return d.isRenaming() || d.isSearchTyping()
}

func (d *documentsScreen) isSearchTyping() bool {
	return d.history != nil && d.history.filter.typing
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

func (d *documentsScreen) activeList() keyedList {
	if d.kind == documentKindTypes {
		return d.documentTypeList()
	}

	return d.list()
}

func (d *documentsScreen) dispatches(m *model) []keys.Table {
	return []keys.Table{d.listActions(), d.diffActions(), m.docActions(), d.docKeys()}
}

func (d *documentsScreen) detailsFit(m *model) bool {
	return m.listFitsDetails(d.listParams(m))
}

func (d *documentsScreen) detailsRoom(m *model) int {
	return m.listRecordRoom(d.listParams(m))
}
