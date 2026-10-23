package ui

import (
	"fmt"

	"komrad/internal/ui/column"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/tabs"
	"komrad/internal/view"

	tea "charm.land/bubbletea/v2"
)

// entriesShown is how many of a directory's entries the document lists before counting the rest.
const entriesShown = 10

// Enter lands on the directory of the agent the target names, remembering it as the focus for
// the listing on its way.
func (w *worktreesScreen) Enter(in screen.Inputs, target screen.Target) screen.Request {
	w.focus = target.AgentID
	if target.AgentID != "" && !w.scope.listsAgent(in.Listing.Worktrees, target.AgentID) {
		w.scope = scopeAll
	}
	w.apply(in, w.list().Hovered(in))

	return screen.Batch{w.Stage(), screen.Settle{}}
}

func (w *worktreesScreen) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return w.actions(in).DispatchTop(w.deps.top, w, w.deps.keys, key.String())
}

func (w *worktreesScreen) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	relisted, ok := msg.(screen.Relisted)
	if !ok {
		return nil, false
	}

	w.apply(in, w.list().Hovered(relisted.Before(in)))

	return nil, true
}

func (w *worktreesScreen) list() keys.ScreenList[worktreeRow] {
	return keys.ScreenList[worktreeRow]{
		Keys:   w.deps.keys,
		Cursor: func() *list.Cursor { return &w.Cursor },
		Rows:   w.rows,
		Name:   worktreeRow.name,
		Refs:   worktreeRefs,
		Header: "repository",
		Placed: func(screen.Inputs) screen.Request { return w.Stage() },
		Moved:  func(screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

func (w *worktreesScreen) hovered(in screen.Inputs) (worktreeRow, bool) {
	row, ok := list.At(w.rows(in), w.Row)
	if !ok || row.header {
		return worktreeRow{}, false
	}

	return row, true
}

func (w *worktreesScreen) Stage() screen.Request {
	return screen.StageTerminal{}
}

// inspect asks the server for the hovered directory's standing.
func (w *worktreesScreen) inspect(in screen.Inputs) screen.Request {
	row, ok := w.hovered(in)
	if !ok {
		return nil
	}

	return screen.Inspect{Dir: row.item.Dir}
}

// inspectUnread asks for the hovered directory's standing the first time, while the screen is up.
func (w *worktreesScreen) inspectUnread(in screen.Inputs) screen.Request {
	if !in.Up {
		return nil
	}

	row, ok := w.hovered(in)
	if !ok {
		return nil
	}
	if _, asked := in.Inspections[row.item.Dir]; asked {
		return nil
	}

	return screen.Inspect{Dir: row.item.Dir}
}

func worktreesTabs() tabs.Strip {
	return tabs.Strip{
		Title:    "f",
		Legend:   "sort",
		Options:  tabs.OptionsOf(worktreeSorts),
		Fallback: "size",
		Grouped:  "repo",
	}
}

func (w *worktreesScreen) Kinds() tabs.Stops {
	return tabs.Stops{
		Labels: worktreeScopeLabels,
		Focus:  tabs.NoStop,
		Lit:    int(w.scope),
	}
}

func (w *worktreesScreen) KindsFocused() *bool {
	return &w.scopeFocus
}

// KindsSwitch holds the scope while any directories are selected.
func (w *worktreesScreen) KindsSwitch() bool {
	return !w.Selected.IsActive()
}

// StepKind moves the listing to another scope, keeping the cursor on its row when the new scope
// lists it.
func (w *worktreesScreen) StepKind(in screen.Inputs, delta int) screen.Request {
	hovered := w.list().Hovered(in)
	w.scope = worktreeScope(tabs.StepStop(int(w.scope), delta, len(worktreeScopeLabels)))
	w.place(in, hovered)

	return screen.Batch{w.Stage(), screen.Settle{}}
}

// tally is how many directories the scope lists, what the measured ones take, and whether they
// are rendered as one list.
func (w *worktreesScreen) tally() string {
	shown := w.shown()
	tally := fmt.Sprintf("%d"+separated+"%s", len(shown), totalLabel(shown))
	if w.sort.Flat {
		tally += separated + tabs.FlatLabel
	}

	return tally
}

func (w *worktreesScreen) ListParams(in screen.Inputs) list.Params {
	return list.Params{
		Tally:    w.tally(),
		Help:     w.help(in),
		Empty:    "no worktrees",
		Rows:     len(w.rows(in)),
		Snapshot: func(pane list.Pane) view.Snapshot { return w.snapshot(in, pane) },
	}
}

func (w *worktreesScreen) help(in screen.Inputs) legend.Help {
	return w.actions(in).Help(w, w.deps.keys)
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

func (w *worktreesScreen) DoesRenderFromListing() bool {
	return true
}

func (w *worktreesScreen) TakesEveryKey() bool {
	return false
}

func (w *worktreesScreen) ActiveList() keys.KeyedList {
	return w.list()
}

func (w *worktreesScreen) HoveredName(in screen.Inputs) string {
	return w.list().HoveredName(in)
}

func (w *worktreesScreen) Tabs() tabs.Strip {
	return worktreesTabs()
}

func (w *worktreesScreen) TabsState() *tabs.State {
	return &w.sort
}

func (w *worktreesScreen) Column(in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: column.Narrowest,
		Read:  column.IsAlwaysRead,
		DocRows: func(width int) []string {
			return paint.DocStyledRows(w.doc(in), max(width, 1), columnStyle())
		},
	}
}

func (w *worktreesScreen) Dispatches() []keys.Table {
	return []keys.Table{w.actions(screen.Inputs{})}
}
