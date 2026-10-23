// Package documents holds the screen that lists every agent's documents and their types, and steps through a document's writes beside the list.
package documents

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

// document is one document as the listing reported it, without its body.
type document struct {
	id        string
	agentID   string
	taskID    string
	name      string
	typeName  string
	updatedAt time.Time
}

// text is one document's text as it was last read, or why it could not be.
type text struct {
	updatedAt time.Time
	version   int32
	body      string
	pending   bool
	err       error
}

// kind is the kind of resource the documents view lists.
type kind int

const (
	kindDocuments kind = iota
	kindTypes
)

// Deps is what the documents screen is given at launch.
type Deps struct {
	Keys          config.Keybindings
	Register      *vim.Register
	Clients       api.Clients
	Panes         screen.Panes
	Top           *keys.Top
	EditorCommand string
	DraftDir      string
	Glyphs        glyph.Set
}

// New is the documents screen built from what it is given at launch.
func New(deps Deps) Screen {
	return Screen{deps: deps}
}

// Screen lists every agent's documents and renders the one under the cursor beside the list.
type Screen struct {
	deps Deps
	list.Cursor
	kind       kind
	kindFocus  bool
	types      list.Cursor
	items      []document
	revision   int
	built      screen.Derived[rowsKey, []row]
	builtTypes screen.Derived[screen.ListingKey, []typeRow]
	// listedAt is the newest write the list was read at, and listedCount how many were written.
	listedAt    *time.Time
	listedCount int32
	listed      bool
	listing     bool
	err         error
	bodies      map[string]text
	editing     *editing
	renaming    *rename
	history     *history
	// focus says the picker handed the keys to the diff beside the list.
	focus bool
	// agentID is the agent the screen was opened for from its menu.
	agentID string
	// taskID is the task the screen was opened for from its row.
	taskID string
}

// row is one line of the documents list.
type row struct {
	item document
}

func (r row) Key() string {
	return "doc:" + r.item.id
}

// typeRow is one line of the document types list.
type typeRow struct {
	documentType *agentv1.DocumentType
}

func (r typeRow) Key() string {
	return "type:" + r.documentType.GetName()
}

// Enter opens on every agent's documents, filtered down to the task or agent the target names when one is.
func (d *Screen) Enter(in screen.Inputs, target screen.Target) screen.Request {
	var requests screen.Batch
	if !target.Resume {
		d.agentID, d.taskID = target.AgentID, target.TaskID
		if target.AgentID != "" || target.TaskID != "" {
			requests = append(requests, d.closeEdit())
			d.kind, d.kindFocus, d.history, d.focus = kindDocuments, false, nil, false
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

// Update sends a key to the rename or the diff's search while either is typed into, and to the screen's table otherwise.
func (d *Screen) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	d.reconcileRename(in)
	if d.IsRenaming() {
		return d.updateRename(msg)
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	if d.kind != kindTypes && d.history != nil && d.history.filter.Typing {
		return d.diffKeys().UpdateFilter(in, key)
	}

	return d.actions(in).DispatchTop(d.deps.Top, d, d.deps.Keys, key.String())
}

// Receive takes the documents and versions as they are read, the editor's pane, a settled rename and the listing moving.
func (d *Screen) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case listedMsg:
		return d.applyListed(in, msg), true
	case readMsg:
		d.applyRead(msg)

		return nil, true
	case versionsListedMsg:
		return d.applyVersionsListed(in, msg), true
	case versionReadMsg:
		d.applyVersionRead(msg)

		return nil, true
	case editorStaged:
		return d.applyEditorStaged(msg), true
	case screen.Relisted:
		return screen.Cmd(d.readDocumentsCmd(in)), true
	case api.RenameSettled:
		d.settleRename(in, msg.Row, msg.Err)

		return nil, true
	}

	return nil, false
}

func (d *Screen) onDocuments() bool {
	return d.kind == kindDocuments
}

func (d *Screen) onTypes() bool {
	return d.kind == kindTypes
}

// Kinds is the documents view's row of kinds, documents and types.
func (d *Screen) Kinds() tabs.Stops {
	return tabs.Stops{
		Labels: []string{"documents", "types"},
		Focus:  tabs.NoStop,
		Lit:    int(d.kind),
	}
}

// KindsFocused is whether the row of kinds holds the keys.
func (d *Screen) KindsFocused() *bool {
	return &d.kindFocus
}

// KindsSwitch holds the view on its documents while the user edits one.
func (d *Screen) KindsSwitch() bool {
	return d.editing == nil
}

// StepKind moves the view between documents and their types.
func (d *Screen) StepKind(in screen.Inputs, delta int) screen.Request {
	d.kind = kind(tabs.StepStop(int(d.kind), delta, len(d.Kinds().Labels)))
	d.history, d.focus = nil, false

	return screen.Batch{screen.ResetFilter{}, screen.StageTerminal{}, screen.Settle{}}
}

func (d *Screen) list() keys.ScreenList[row] {
	return keys.ScreenList[row]{
		Keys:   d.deps.Keys,
		Cursor: func() *list.Cursor { return &d.Cursor },
		Rows:   d.rows,
		Name:   rowName,
		Fits:   d.rowFits,
		Placed: func(screen.Inputs) screen.Request { return screen.StageTerminal{} },
		Moved: func(screen.Inputs) screen.Request {
			d.history, d.focus = nil, false

			return screen.Settle{}
		},
	}
}

func (d *Screen) typeList() keys.ScreenList[typeRow] {
	return keys.ScreenList[typeRow]{
		Keys:   d.deps.Keys,
		Cursor: func() *list.Cursor { return &d.types },
		Rows:   d.typeRows,
		Name:   typeRowName,
		Placed: func(screen.Inputs) screen.Request { return screen.StageTerminal{} },
		Moved:  func(screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

type rowsKey struct {
	revision int
	listing  screen.ListingKey
	agentID  string
	taskID   string
}

func (d *Screen) rows(in screen.Inputs) []row {
	key := rowsKey{
		revision: d.revision,
		listing:  in.ListingKey(),
		agentID:  d.agentID,
		taskID:   d.taskID,
	}

	return d.built.Read(key, func() []row {
		return list.Narrowed(in.Room.Pattern, list.NarrowedParams[row]{
			Rows: xslice.Map(d.items, func(item document) row {
				return row{item: item}
			}),
			Depth: func(row) int { return 0 },
			Name:  rowName,
			Fits:  d.rowFits(in),
		})
	})
}

func (d *Screen) typeRows(in screen.Inputs) []typeRow {
	return d.builtTypes.Read(in.ListingKey(), func() []typeRow {
		return list.Narrowed(in.Room.Pattern, list.NarrowedParams[typeRow]{
			Rows: xslice.Map(in.Listing.DocumentTypes, func(t *agentv1.DocumentType) typeRow {
				return typeRow{documentType: t}
			}),
			Depth: func(typeRow) int { return 0 },
			Name:  typeRowName,
		})
	})
}

func rowName(r row) string {
	return r.item.name
}

func typeRowName(r typeRow) string {
	return r.documentType.GetName()
}

func (d *Screen) hovered(in screen.Inputs) (document, bool) {
	if d.kind != kindDocuments {
		return document{}, false
	}

	r, ok := list.At(d.rows(in), d.Row)

	return r.item, ok
}

// hoveredType is the document type the cursor is on, nil for none.
func (d *Screen) hoveredType(in screen.Inputs) *agentv1.DocumentType {
	if d.kind != kindTypes {
		return nil
	}

	r, _ := list.At(d.typeRows(in), d.types.Row)

	return r.documentType
}

// ListParams is the list of documents or types as the root renders it.
func (d *Screen) ListParams(in screen.Inputs) list.Params {
	if d.kind == kindTypes {
		return list.Params{
			Help:     d.help(in),
			Empty:    "no document types",
			Rows:     len(d.typeRows(in)),
			Snapshot: func(pane list.Pane) view.Snapshot { return d.typeSnapshot(in, pane) },
		}
	}

	return list.Params{
		Help:     d.help(in),
		Empty:    d.emptyLine(),
		Rows:     len(d.rows(in)),
		Snapshot: func(pane list.Pane) view.Snapshot { return d.snapshot(in, pane) },
	}
}

// Parent is the documents view, which heads its own trail.
func (d *Screen) Parent() screen.ID {
	return screen.Documents
}

// Crumbs runs down to the version stepped to, and counts the documents while none is.
func (d *Screen) Crumbs(in screen.Inputs) screen.Crumbs {
	var trail screen.Crumbs
	if d.kind == kindTypes {
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

func (d *Screen) back() screen.Request {
	return screen.PickView{}
}

// findAgentName is the name of the agent an id names, live or archived, and the id when neither.
func findAgentName(l panel.Listing, agentID string) string {
	if item, ok := l.AnyAgent(agentID); ok {
		return l.AgentName(item.Agent)
	}

	return agentID
}

func (d *Screen) help(in screen.Inputs) legend.Help {
	return d.actions(in).Help(d, d.deps.Keys)
}

// listedMsg is the documents come back.
type listedMsg struct {
	written writes
	items   []document
	err     error
}

// writes is what an agent's row says it has written: how many, and when the newest was.
type writes struct {
	count  int32
	newest *time.Time
}

// readMsg is one document's text come back.
type readMsg struct {
	id   string
	read text
}

// readDocumentsCmd lists the documents again when any agent has written since they were last
// listed, and otherwise reads the one under the cursor.
func (d *Screen) readDocumentsCmd(in screen.Inputs) tea.Cmd {
	if !in.Up {
		return nil
	}

	written := writtenIn(in.Listing)
	if !d.listed || d.listedCount != written.count || !sameStamp(d.listedAt, written.newest) {
		return d.listDocumentsCmd(written)
	}

	return tea.Batch(d.readHoveredDocumentCmd(in), d.readHistoryCmd(in))
}

// writtenIn is what every agent has written, as their rows say.
func writtenIn(listing panel.Listing) writes {
	var written writes
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
func (d *Screen) listDocumentsCmd(written writes) tea.Cmd {
	if d.listing {
		return nil
	}

	d.listing = true
	clients := d.deps.Clients

	return func() tea.Msg {
		documents, err := clients.ListDocuments()
		if err != nil {
			return listedMsg{
				written: written,
				err:     err,
			}
		}

		return listedMsg{
			written: written,
			items: xslice.Map(documents, func(doc *agentv1.Document) document {
				return document{
					id:        doc.GetId(),
					agentID:   doc.GetAgentId(),
					taskID:    doc.GetTaskId(),
					name:      doc.GetName(),
					typeName:  doc.GetType(),
					updatedAt: dates.Read(doc.GetUpdatedAt()),
				}
			}),
		}
	}
}

func (d *Screen) applyListed(in screen.Inputs, msg listedMsg) screen.Request {
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
func (d *Screen) readHoveredDocumentCmd(in screen.Inputs) tea.Cmd {
	item, ok := d.hovered(in)
	if !ok {
		return nil
	}

	last, asked := d.bodies[item.id]
	if asked && (last.pending || !last.updatedAt.Before(item.updatedAt)) {
		return nil
	}

	if d.bodies == nil {
		d.bodies = map[string]text{}
	}
	pending := last
	pending.pending = true
	d.bodies[item.id] = pending
	clients := d.deps.Clients

	return func() tea.Msg {
		doc, err := clients.GetDocument(item.id)
		if err != nil {
			return readMsg{
				id:   item.id,
				read: text{updatedAt: item.updatedAt, err: err},
			}
		}

		return readMsg{id: item.id, read: text{
			updatedAt: dates.Read(doc.GetUpdatedAt()),
			version:   doc.GetVersion(),
			body:      doc.GetBody(),
		}}
	}
}

func (d *Screen) applyRead(msg readMsg) {
	if d.bodies == nil {
		return
	}

	d.bodies[msg.id] = msg.read
}

// Stage puts the terminal on the stage beside the list.
func (d *Screen) Stage() screen.Request {
	return screen.StageTerminal{}
}

// StageResting stages the terminal and reads the document the cursor came to rest on.
func (d *Screen) StageResting(in screen.Inputs) screen.Request {
	return screen.Batch{d.Stage(), screen.Cmd(d.readDocumentsCmd(in))}
}

// DoesRenderFromListing reports that the screen names agents and tasks from the listing.
func (d *Screen) DoesRenderFromListing() bool {
	return true
}

// TakesEveryKey reports whether a rename or the diff's search is typed into.
func (d *Screen) TakesEveryKey() bool {
	return d.IsRenaming() || d.IsSearchTyping()
}

// IsSearchTyping reports whether the diff's search is typed into.
func (d *Screen) IsSearchTyping() bool {
	return d.history != nil && d.history.filter.Typing
}

// Column is the document or type rendered in the block beside the list.
func (d *Screen) Column(in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: column.Narrowest,
		DocRows: func(width int) []string {
			return d.docRows(in, width)
		},
		Holds: func() bool { return d.editing == nil },
		Read:  func() bool { return !d.isSteppingHovered(in) },
	}
}

// ActiveList is the list of documents or types the filter narrows.
func (d *Screen) ActiveList() keys.KeyedList {
	if d.kind == kindTypes {
		return d.typeList()
	}

	return d.list()
}

// Dispatches is the list's table and the diff's table.
func (d *Screen) Dispatches() []keys.Table {
	return []keys.Table{d.listActions(screen.Inputs{}), d.diffActions(screen.Inputs{})}
}
