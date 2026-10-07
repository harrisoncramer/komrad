package ui

import (
	"fmt"
	"time"

	"komrad/internal/ui/paint"
	"komrad/internal/view"

	tea "charm.land/bubbletea/v2"
)

// entriesShown is how many of a directory's entries the document lists before counting the rest.
const entriesShown = 10

// enter lands on the directory of the agent the target names, remembering it as the focus for
// the listing on its way.
func (w *worktreesScreen) enter(m *model, target screenTarget) tea.Cmd {
	w.focus = target.agentID
	w.apply(m, m.listing.worktrees, w.list().hovered(m))
	w.stage(m)

	return m.waitToStage()
}

func (w *worktreesScreen) update(m *model, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return w.list().update(m, key, w.actions())
}

func (w *worktreesScreen) list() screenList[worktreeRow] {
	return screenList[worktreeRow]{
		cursor: func() *rowCursor { return &w.rowCursor },
		rows:   w.rows,
		name:   worktreeRow.name,
		refs:   worktreeRefs,
		header: "repository",
		placed: w.stage,
		moved: func(m *model) tea.Cmd {
			return m.waitToStage()
		},
	}
}

func (w *worktreesScreen) hovered(m *model) (worktreeRow, bool) {
	row, ok := rowAt(w.rows(m), w.cursor)
	if !ok || row.header {
		return worktreeRow{}, false
	}

	return row, true
}

func (w *worktreesScreen) stage(m *model) {
	m.stageTerminal()
}

// inspectWorktreeCmd asks the service for the hovered directory's standing.
func (m *model) inspectWorktreeCmd() tea.Cmd {
	row, ok := m.worktrees.hovered(m)
	if !ok {
		return nil
	}

	return m.readWorktreeCmd(row.item.dir)
}

// readWorktreeCmd asks the service for one directory's standing, once at a time.
func (m *model) readWorktreeCmd(dir string) tea.Cmd {
	last, asked := m.inspections[dir]
	if asked && last.pending {
		return nil
	}

	pending := last
	pending.pending = true
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: dir, inspection: pending})
	clients := m.api

	return func() tea.Msg {
		read, err := clients.getWorktree(dir)
		if err != nil {
			// The last answer stays on the row with the refusal beside it.
			last.answered, last.readAt, last.err = true, time.Now(), err

			return worktreeInspectedMsg{dir: dir, inspection: last}
		}

		return worktreeInspectedMsg{dir: dir, inspection: worktreeInspectionFrom(read, time.Now())}
	}
}

// inspectHoveredWorktreeCmd asks for the hovered directory's standing the first time.
func (m *model) inspectHoveredWorktreeCmd() tea.Cmd {
	if m.screen != screenWorktrees {
		return nil
	}

	row, ok := m.worktrees.hovered(m)
	if !ok {
		return nil
	}
	if _, asked := m.inspections[row.item.dir]; asked {
		return nil
	}

	return m.inspectWorktreeCmd()
}

// worktreeInspectedMsg is one directory's standing come back.
type worktreeInspectedMsg struct {
	dir        string
	inspection worktreeInspection
}

func (m *model) applyWorktreeInspection(msg worktreeInspectedMsg) {
	if m.inspections == nil {
		m.inspections = map[string]worktreeInspection{}
	}

	m.inspections[msg.dir] = msg.inspection
}

// applyWorktrees hands the screen a fresh listing and forgets the readings of directories that
// have left it.
func (m *model) applyWorktrees(items []worktreeItem, hovered string) {
	listedDirs := make(map[string]bool, len(items))
	for _, item := range items {
		listedDirs[item.dir] = true
	}
	for dir := range m.inspections {
		if !listedDirs[dir] {
			delete(m.inspections, dir)
		}
	}

	m.worktrees.apply(m, items, hovered)
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

func (w *worktreesScreen) kindsFit(m *model) bool {
	return m.listFitsTabs(w.listParams(m))
}

func (w *worktreesScreen) kindsFocused(*model) *bool {
	return &w.scopeFocus
}

// kindsSwitch holds the scope while any directories are selected.
func (w *worktreesScreen) kindsSwitch(*model) bool {
	return !w.selected.isActive()
}

// stepKind moves the listing to another scope, keeping the cursor on its row when the new scope
// lists it.
func (w *worktreesScreen) stepKind(m *model, delta int) tea.Cmd {
	hovered := w.list().hovered(m)
	w.scope = worktreeScope(stepStop(int(w.scope), delta, len(worktreeScopeLabels)))
	w.place(m, hovered)

	w.stage(m)

	return m.waitToStage()
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

func (w *worktreesScreen) listParams(m *model) screenListParams {
	return screenListParams{
		tally:    w.tally(),
		help:     w.help,
		empty:    "no worktrees",
		rows:     len(w.rows(m)),
		snapshot: func(pane listPane) view.Snapshot { return w.snapshot(m, pane) },
	}
}

func (w *worktreesScreen) help(m *model) keyMap {
	return w.actions().help(m)
}

func (w *worktreesScreen) countUnder(repo string) int {
	return len(itemsUnder(w.shown(), repo))
}

var (
	_ recordRenderer      = (*worktreesScreen)(nil)
	_ listFilterer        = (*worktreesScreen)(nil)
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

func (w *worktreesScreen) detailsFit(m *model) bool {
	return m.listFitsDetails(w.listParams(m))
}

func (w *worktreesScreen) detailsRoom(m *model) int {
	return m.listRecordRoom(w.listParams(m))
}

func (w *worktreesScreen) hoveredKey(m *model) string {
	return w.list().hovered(m)
}

func (w *worktreesScreen) hoveredName(m *model) string {
	return w.list().hoveredName(m)
}

func (w *worktreesScreen) placeAt(m *model, key string) {
	w.list().place(m, key)
}

func (w *worktreesScreen) filterRows(m *model) []filterRow {
	return w.list().filterRows(m)
}

func (w *worktreesScreen) tabs() tabStrip {
	return worktreesTabs()
}

func (w *worktreesScreen) tabsState() *tabsState {
	return &w.sort
}

func (w *worktreesScreen) tabsFit(m *model) bool {
	return m.listFitsTabs(w.listParams(m))
}

func (w *worktreesScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		read:  isAlwaysRead,
		docRows: func(m *model, width int) []string {
			return m.withLastScreen(paint.DocStyledRows(m.worktrees.doc(m), max(width, 1), columnStyle()), width)
		},
	}
}

func (w *worktreesScreen) dispatches(*model) []dispatching {
	return []dispatching{w.actions()}
}
