package ui

import (
	"fmt"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/view"

	tea "charm.land/bubbletea/v2"
)

// entriesShown is how many of a directory's entries the document lists before counting the rest.
const entriesShown = 10

// enter lands on the directory of the agent the target names, remembering it as the focus for
// the listing on its way.
func (w *worktreesScreen) enter(m *model, in screen.Inputs, target screen.Target) screen.Request {
	w.focus = target.AgentID
	if target.AgentID != "" && !w.scope.listsAgent(in.Listing.Worktrees, target.AgentID) {
		w.scope = scopeAll
	}
	w.apply(m, in, in.Listing.Worktrees, w.list().hovered(m, in))

	return screen.Batch{w.stage(m, in), screen.Settle{}}
}

func (w *worktreesScreen) update(m *model, in screen.Inputs, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return w.list().update(m, key, w.actions())
}

func (w *worktreesScreen) receive(m *model, in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	relisted, ok := msg.(screen.Relisted)
	if !ok {
		return nil, false
	}

	w.apply(m, in, in.Listing.Worktrees, w.list().hovered(m, relisted.Before(in)))

	return nil, true
}

func (w *worktreesScreen) list() screenList[worktreeRow] {
	return screenList[worktreeRow]{
		keys:   w.deps.keys,
		top:    w.deps.top,
		cursor: func() *list.Cursor { return &w.Cursor },
		rows:   w.rows,
		name:   worktreeRow.name,
		refs:   worktreeRefs,
		header: "repository",
		placed: w.stage,
		moved:  func(*model, screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

func (w *worktreesScreen) hovered(m *model, in screen.Inputs) (worktreeRow, bool) {
	row, ok := list.At(w.rows(m, in), w.Row)
	if !ok || row.header {
		return worktreeRow{}, false
	}

	return row, true
}

func (w *worktreesScreen) stage(*model, screen.Inputs) screen.Request {
	return screen.StageTerminal{}
}

// inspectWorktree asks the server for the hovered directory's standing.
func (m *model) inspectWorktree() screen.Request {
	row, ok := m.worktrees.hovered(m, m.inputs())
	if !ok {
		return nil
	}

	return screen.Inspect{Dir: row.item.Dir}
}

// readWorktreeCmd asks the server for one directory's standing, once at a time.
func (m *model) readWorktreeCmd(dir string) tea.Cmd {
	last, asked := m.inspections[dir]
	if asked && last.Pending {
		return nil
	}

	pending := last
	pending.Pending = true
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: dir, inspection: pending})
	clients, clock := m.api, m.clock

	return func() tea.Msg {
		read, err := clients.GetWorktree(dir)
		if err != nil {
			// The last answer stays on the row with the refusal beside it.
			last.Answered, last.ReadAt, last.Err = true, clock.Now(), err

			return worktreeInspectedMsg{dir: dir, inspection: last}
		}

		return worktreeInspectedMsg{dir: dir, inspection: worktreeInspectionFrom(read, clock.Now())}
	}
}

// inspectHoveredWorktree asks for the hovered directory's standing the first time.
func (m *model) inspectHoveredWorktree() screen.Request {
	if m.screen != screen.Worktrees {
		return nil
	}

	row, ok := m.worktrees.hovered(m, m.inputs())
	if !ok {
		return nil
	}
	if _, asked := m.inspections[row.item.Dir]; asked {
		return nil
	}

	return m.inspectWorktree()
}

// worktreeInspectedMsg is one directory's standing come back.
type worktreeInspectedMsg struct {
	dir        string
	inspection panel.WorktreeInspection
}

func (m *model) applyWorktreeInspection(msg worktreeInspectedMsg) {
	if m.inspections == nil {
		m.inspections = map[string]panel.WorktreeInspection{}
	}

	m.inspections[msg.dir] = msg.inspection
}

// forgetUnlistedInspections forgets the readings of directories that have left the listing.
func (m *model) forgetUnlistedInspections() {
	listedDirs := make(map[string]bool, len(m.listing.Worktrees))
	for _, item := range m.listing.Worktrees {
		listedDirs[item.Dir] = true
	}
	for dir := range m.inspections {
		if !listedDirs[dir] {
			delete(m.inspections, dir)
		}
	}
}

func worktreesTabs() tabStrip {
	return tabStrip{
		title:    "f",
		legend:   "sort",
		options:  tabOptionsOf(worktreeSorts),
		fallback: "size",
		grouped:  "repo",
	}
}

func (w *worktreesScreen) kinds(*model) stops {
	return stops{
		labels: worktreeScopeLabels,
		focus:  noStop,
		lit:    int(w.scope),
	}
}

func (w *worktreesScreen) kindsFit(m *model, in screen.Inputs) bool {
	return m.listFitsTabs(w.listParams(m, in))
}

func (w *worktreesScreen) kindsFocused(*model) *bool {
	return &w.scopeFocus
}

// kindsSwitch holds the scope while any directories are selected.
func (w *worktreesScreen) kindsSwitch(*model) bool {
	return !w.Selected.IsActive()
}

// stepKind moves the listing to another scope, keeping the cursor on its row when the new scope
// lists it.
func (w *worktreesScreen) stepKind(m *model, in screen.Inputs, delta int) screen.Request {
	hovered := w.list().hovered(m, in)
	w.scope = worktreeScope(stepStop(int(w.scope), delta, len(worktreeScopeLabels)))
	w.place(m, in, hovered)

	return screen.Batch{w.stage(m, in), screen.Settle{}}
}

// tally is how many directories the scope lists, what the measured ones take, and whether they
// are rendered as one list.
func (w *worktreesScreen) tally() string {
	shown := w.shown()
	tally := fmt.Sprintf("%d"+separated+"%s", len(shown), totalLabel(shown))
	if w.sort.flat {
		tally += separated + flatLabel
	}

	return tally
}

func (w *worktreesScreen) listParams(m *model, in screen.Inputs) screenListParams {
	return screenListParams{
		tally:    w.tally(),
		help:     w.help,
		empty:    "no worktrees",
		rows:     len(w.rows(m, in)),
		snapshot: func(pane list.Pane) view.Snapshot { return w.snapshot(m, in, pane) },
	}
}

func (w *worktreesScreen) help(m *model) keyMap {
	return m.helpOf(w.actions())
}

func (w *worktreesScreen) countUnder(repo string) int {
	return len(itemsUnder(w.shown(), repo))
}

var (
	_ recordRenderer      = (*worktreesScreen)(nil)
	_ activeLister        = (*worktreesScreen)(nil)
	_ kindSwitcher        = (*worktreesScreen)(nil)
	_ tabSwitcher         = (*worktreesScreen)(nil)
	_ rowStager           = (*worktreesScreen)(nil)
	_ hoveredNamer        = (*worktreesScreen)(nil)
	_ itemMenuBuilder     = (*worktreesScreen)(nil)
	_ savedScreenRenderer = (*worktreesScreen)(nil)
)

func (w *worktreesScreen) doesRenderFromListing() bool {
	return true
}

func (w *worktreesScreen) takesEveryKey() bool {
	return false
}

func (w *worktreesScreen) detailsFit(m *model, in screen.Inputs) bool {
	return m.listFitsDetails(w.listParams(m, in))
}

func (w *worktreesScreen) detailsRoom(m *model, in screen.Inputs) int {
	return m.listRecordRoom(w.listParams(m, in))
}

func (w *worktreesScreen) activeList() keyedList {
	return w.list()
}

func (w *worktreesScreen) hoveredName(m *model, in screen.Inputs) string {
	return w.list().hoveredName(m, in)
}

func (w *worktreesScreen) tabs() tabStrip {
	return worktreesTabs()
}

func (w *worktreesScreen) tabsState() *tabsState {
	return &w.sort
}

func (w *worktreesScreen) tabsFit(m *model, in screen.Inputs) bool {
	return m.listFitsTabs(w.listParams(m, in))
}

func (w *worktreesScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		read:  isAlwaysRead,
		docRows: func(m *model, in screen.Inputs, width int) []string {
			return m.withLastScreen(paint.DocStyledRows(m.worktrees.doc(m, in), max(width, 1), columnStyle()), width)
		},
	}
}

func (w *worktreesScreen) dispatches(*model) []keys.Table {
	return []keys.Table{w.actions()}
}
