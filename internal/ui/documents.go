package ui

import (
	"fmt"
	"slices"
	"time"

	"komrad/config"
	"komrad/internal/glyph"
	"komrad/internal/ui/api"
	"komrad/internal/ui/column"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/list"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/tabs"
	"komrad/internal/view"
	"komrad/internal/vim"
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

// documentsDeps is what the documents screen is given at launch.
type documentsDeps struct {
	keys          config.Keybindings
	register      *vim.Register
	clients       api.Clients
	panes         screen.Panes
	top           *keys.Top
	editorCommand string
	draftDir      string
	glyphs        glyph.Set
}

// documentsScreen lists every agent's documents and renders the one under the cursor beside the list.
type documentsScreen struct {
	deps documentsDeps
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

// Enter opens on every agent's documents, filtered down to the task or agent the target names when one is.
func (d *documentsScreen) Enter(in screen.Inputs, target screen.Target) screen.Request {
	var requests screen.Batch
	if !target.Resume {
		d.agentID, d.taskID = target.AgentID, target.TaskID
		if target.AgentID != "" || target.TaskID != "" {
			requests = append(requests, d.closeEdit())
			d.kind, d.kindFocus, d.history, d.focus = documentKindDocuments, false, nil, false
			d.Cursor = list.Cursor{}
		}
		switch {
		case target.TaskID != "":
			requests = append(requests, screen.Filter{Query: taskClause(in.Listing.Tasks[target.TaskID].Slug)})
		case target.AgentID != "":
			requests = append(requests, screen.Filter{Query: agentClause(findAgentName(in.Listing, target.AgentID))})
		}
	}

	return append(requests, screen.StageTerminal{}, screen.Settle{})
}

func (d *documentsScreen) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	d.reconcileRename(in)
	if d.IsRenaming() {
		return d.updateRename(msg)
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	if d.kind != documentKindTypes && d.history != nil && d.history.filter.typing {
		return d.diffKeys().updateFilter(in, key)
	}

	return d.actions(in).DispatchTop(d.deps.top, d, d.deps.keys, key.String())
}

func (d *documentsScreen) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case documentsListedMsg:
		return d.applyListed(in, msg), true
	case documentReadMsg:
		d.applyRead(msg)

		return nil, true
	case documentVersionsListedMsg:
		return d.applyVersionsListed(in, msg), true
	case documentVersionReadMsg:
		d.applyVersionRead(msg)

		return nil, true
	case documentEditorStaged:
		return d.applyEditorStaged(msg), true
	case api.RenameSettled:
		d.settleRename(in, msg.Row, msg.Err)

		return nil, true
	}

	return nil, false
}

func (d *documentsScreen) onDocuments() bool {
	return d.kind == documentKindDocuments
}

func (d *documentsScreen) onDocumentTypes() bool {
	return d.kind == documentKindTypes
}

func (d *documentsScreen) Kinds() tabs.Stops {
	return tabs.Stops{
		Labels: []string{"documents", "types"},
		Focus:  tabs.NoStop,
		Lit:    int(d.kind),
	}
}

func (d *documentsScreen) KindsFocused() *bool {
	return &d.kindFocus
}

// KindsSwitch holds the view on its documents while the user edits one.
func (d *documentsScreen) KindsSwitch() bool {
	return d.editing == nil
}

// StepKind moves the view between documents and their types.
func (d *documentsScreen) StepKind(in screen.Inputs, delta int) screen.Request {
	d.kind = documentKind(tabs.StepStop(int(d.kind), delta, len(d.Kinds().Labels)))
	d.history, d.focus = nil, false

	return screen.Batch{screen.ResetFilter{}, screen.StageTerminal{}, screen.Settle{}}
}

func (d *documentsScreen) list() screenList[documentRow] {
	return screenList[documentRow]{
		keys:   d.deps.keys,
		top:    d.deps.top,
		cursor: func() *list.Cursor { return &d.Cursor },
		rows:   d.rows,
		name:   documentRowName,
		fits:   d.documentRowFits,
		placed: func(screen.Inputs) screen.Request { return screen.StageTerminal{} },
		moved: func(screen.Inputs) screen.Request {
			d.history, d.focus = nil, false

			return screen.Settle{}
		},
	}
}

func (d *documentsScreen) documentTypeList() screenList[documentTypeRow] {
	return screenList[documentTypeRow]{
		keys:   d.deps.keys,
		top:    d.deps.top,
		cursor: func() *list.Cursor { return &d.documentTypes },
		rows:   d.documentTypeRows,
		name:   documentTypeRowName,
		placed: func(screen.Inputs) screen.Request { return screen.StageTerminal{} },
		moved:  func(screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

type documentRowsKey struct {
	revision int
	listing  listingKey
	agentID  string
	taskID   string
}

func (d *documentsScreen) rows(in screen.Inputs) []documentRow {
	key := documentRowsKey{
		revision: d.revision,
		listing:  listingKeyOf(in),
		agentID:  d.agentID,
		taskID:   d.taskID,
	}

	return d.built.read(key, func() []documentRow {
		return list.Narrowed(in.Room.Pattern, list.NarrowedParams[documentRow]{
			Rows: xslice.Map(d.items, func(item documentItem) documentRow {
				return documentRow{item: item}
			}),
			Depth: func(documentRow) int { return 0 },
			Name:  documentRowName,
			Fits:  d.documentRowFits(in),
		})
	})
}

func (d *documentsScreen) documentTypeRows(in screen.Inputs) []documentTypeRow {
	return d.builtTypes.read(listingKeyOf(in), func() []documentTypeRow {
		return list.Narrowed(in.Room.Pattern, list.NarrowedParams[documentTypeRow]{
			Rows: xslice.Map(in.Listing.DocumentTypes, func(t *agentv1.DocumentType) documentTypeRow {
				return documentTypeRow{documentType: t}
			}),
			Depth: func(documentTypeRow) int { return 0 },
			Name:  documentTypeRowName,
		})
	})
}

func documentRowName(row documentRow) string {
	return row.item.name
}

func documentTypeRowName(row documentTypeRow) string {
	return row.documentType.GetName()
}

func (d *documentsScreen) hovered(in screen.Inputs) (documentItem, bool) {
	if d.kind != documentKindDocuments {
		return documentItem{}, false
	}

	row, ok := list.At(d.rows(in), d.Row)

	return row.item, ok
}

// hoveredDocumentType is the document type the cursor is on, nil for none.
func (d *documentsScreen) hoveredDocumentType(in screen.Inputs) *agentv1.DocumentType {
	if d.kind != documentKindTypes {
		return nil
	}

	row, _ := list.At(d.documentTypeRows(in), d.documentTypes.Row)

	return row.documentType
}

func (d *documentsScreen) ListParams(in screen.Inputs) list.Params {
	if d.kind == documentKindTypes {
		return list.Params{
			Help:     d.help(in),
			Empty:    "no document types",
			Rows:     len(d.documentTypeRows(in)),
			Snapshot: func(pane list.Pane) view.Snapshot { return d.documentTypeSnapshot(in, pane) },
		}
	}

	return list.Params{
		Help:     d.help(in),
		Empty:    d.emptyLine(),
		Rows:     len(d.rows(in)),
		Snapshot: func(pane list.Pane) view.Snapshot { return d.snapshot(in, pane) },
	}
}

func (d *documentsScreen) Parent() screen.ID {
	return screen.Documents
}

// Crumbs runs down to the version stepped to, and counts the documents while none is.
func (d *documentsScreen) Crumbs(in screen.Inputs) screen.Crumbs {
	var trail screen.Crumbs
	if d.kind == documentKindTypes {
		trail.Tally = fmt.Sprint(len(in.Listing.DocumentTypes))

		return trail
	}
	if item, ok := d.hovered(in); ok && d.history != nil && d.history.item.id == item.id && len(d.history.versions) > 0 {
		current := d.history.versions[d.history.at]
		trail.Trail = append(trail.Trail, item.name, fmt.Sprintf("v%d of %d", current.version, d.history.versions[0].version))

		return trail
	}
	if d.listed {
		trail.Tally = fmt.Sprint(len(d.items))
	}

	return trail
}

func (d *documentsScreen) back() screen.Request {
	return screen.PickView{}
}

// findAgentName is the name of the agent an id names, live or archived, and the id when neither.
func findAgentName(l panel.Listing, agentID string) string {
	if item, ok := l.AnyAgent(agentID); ok {
		return l.AgentName(item.Agent)
	}

	return agentID
}

func (d *documentsScreen) help(in screen.Inputs) legend.Help {
	return d.actions(in).Help(d, d.deps.keys)
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
func (d *documentsScreen) readDocumentsCmd(in screen.Inputs) tea.Cmd {
	if !in.Up {
		return nil
	}

	written := documentsWrittenIn(in.Listing)
	if !d.listed || d.listedCount != written.count || !sameStamp(d.listedAt, written.newest) {
		return d.listDocumentsCmd(written)
	}

	return tea.Batch(d.readHoveredDocumentCmd(in), d.readHistoryCmd(in))
}

// documentsWrittenIn is what every agent has written, as their rows say.
func documentsWrittenIn(listing panel.Listing) documentsWritten {
	var written documentsWritten
	for _, item := range slices.Concat(listing.Agents, listing.ArchivedAgents) {
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
func (d *documentsScreen) listDocumentsCmd(written documentsWritten) tea.Cmd {
	if d.listing {
		return nil
	}

	d.listing = true
	clients := d.deps.clients

	return func() tea.Msg {
		documents, err := clients.ListDocuments()
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

func (d *documentsScreen) applyListed(in screen.Inputs, msg documentsListedMsg) screen.Request {
	d.listing = false
	if msg.err != nil {
		d.err = msg.err

		return nil
	}

	hovered := d.list().Hovered(in)
	d.items, d.listedAt, d.listedCount, d.listed, d.err = msg.items, msg.written.newest, msg.written.count, true, nil
	d.revision++
	d.keepCursor(in, hovered)

	return screen.Cmd(d.readDocumentsCmd(in))
}

// readHoveredDocumentCmd reads the document under the cursor where the text held is older than
// the listing, once at a time.
func (d *documentsScreen) readHoveredDocumentCmd(in screen.Inputs) tea.Cmd {
	item, ok := d.hovered(in)
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
	clients := d.deps.clients

	return func() tea.Msg {
		document, err := clients.GetDocument(item.id)
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

func (d *documentsScreen) applyRead(msg documentReadMsg) {
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

func (d *documentsScreen) Stage() screen.Request {
	return screen.StageTerminal{}
}

func (d *documentsScreen) DoesRenderFromListing() bool {
	return true
}

func (d *documentsScreen) TakesEveryKey() bool {
	return d.IsRenaming() || d.IsSearchTyping()
}

func (d *documentsScreen) IsSearchTyping() bool {
	return d.history != nil && d.history.filter.typing
}

func (d *documentsScreen) Column(in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: column.Narrowest,
		DocRows: func(width int) []string {
			return d.docRows(in, width)
		},
		Holds: func() bool { return d.editing == nil },
		Read:  func() bool { return !d.isSteppingHovered(in) },
	}
}

func (d *documentsScreen) ActiveList() keyedList {
	if d.kind == documentKindTypes {
		return d.documentTypeList()
	}

	return d.list()
}

func (d *documentsScreen) Dispatches() []keys.Table {
	return []keys.Table{d.listActions(screen.Inputs{}), d.diffActions(screen.Inputs{})}
}
