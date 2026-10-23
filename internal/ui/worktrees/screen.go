package worktrees

import (
	"fmt"

	"komrad/internal/ui/column"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/tabs"
	"komrad/internal/view"

	tea "charm.land/bubbletea/v2"
)

// entriesShown is how many of a directory's entries the document lists before counting the rest.
const entriesShown = 10

// Enter lands on the directory of the agent the target names, remembering it as the focus for
// the listing on its way.
func (w *Screen) Enter(in screen.Inputs, target screen.Target) screen.Request {
	w.focus = target.AgentID
	if target.AgentID != "" && !w.scope.listsAgent(in.Listing.Worktrees, target.AgentID) {
		w.scope = scopeAll
	}
	w.apply(in, w.list().Hovered(in))

	return screen.Batch{w.Stage(), screen.Settle{}}
}

// Update sends a key to the screen's table.
func (w *Screen) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return w.actions(in).DispatchTop(w.deps.Top, w, w.deps.Keys, key.String())
}

// Receive takes a fresh listing and asks for the hovered directory's standing the first time.
func (w *Screen) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	relisted, ok := msg.(screen.ListingChanged)
	if !ok {
		return nil, false
	}

	w.apply(in, w.list().Hovered(relisted.WithPreviousListing(in)))

	return w.inspectUnread(in), true
}

func (w *Screen) list() keys.ScreenList[row] {
	return keys.ScreenList[row]{
		Keys:   w.deps.Keys,
		Cursor: func() *list.Cursor { return &w.Cursor },
		Rows:   w.rows,
		Name:   row.name,
		Refs:   refs,
		Header: "repository",
		Placed: func(screen.Inputs) screen.Request { return w.Stage() },
		Moved:  func(screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

func (w *Screen) hovered(in screen.Inputs) (row, bool) {
	hovered, ok := list.At(w.rows(in), w.Row)
	if !ok || hovered.header {
		return row{}, false
	}

	return hovered, true
}

// Stage puts the terminal on the stage beside the list.
func (w *Screen) Stage() screen.Request {
	return screen.StageTerminal{}
}

// StageResting stages the row the cursor came to rest on, with its standing and its agent's saved screen.
func (w *Screen) StageResting(in screen.Inputs) screen.Request {
	return screen.Batch{w.Stage(), w.inspect(in), screen.ReadSavedScreen{}}
}

// inspect asks the server for the hovered directory's standing.
func (w *Screen) inspect(in screen.Inputs) screen.Request {
	row, ok := w.hovered(in)
	if !ok {
		return nil
	}

	return screen.InspectWorktree{Dir: row.item.Dir}
}

// inspectUnread asks for the hovered directory's standing the first time, while the screen is up.
func (w *Screen) inspectUnread(in screen.Inputs) screen.Request {
	if !in.IsActive {
		return nil
	}

	row, ok := w.hovered(in)
	if !ok {
		return nil
	}
	if _, asked := in.WorktreeInspections[row.item.Dir]; asked {
		return nil
	}

	return screen.InspectWorktree{Dir: row.item.Dir}
}

// ScreenedAgentID is the stopped agent working in the hovered directory, whose saved screen is rendered beside the list.
func (w *Screen) ScreenedAgentID(in screen.Inputs) (string, bool) {
	row, ok := w.hovered(in)
	if !ok {
		return "", false
	}

	occupant, ok := row.item.FirstOccupant()
	if !ok {
		return "", false
	}

	if live, ok := in.Listing.LiveAgent(occupant.ID); ok && live.Alive {
		return "", false
	}

	return occupant.ID, true
}

// ScreenNotice is the line over a stopped agent's saved screen.
func (w *Screen) ScreenNotice(screen.Inputs) string {
	return "not running"
}

func sortStrip() tabs.Strip {
	return tabs.Strip{
		Title:        "f",
		Legend:       "sort",
		Options:      tabs.OptionsOf(sorts),
		Fallback:     "size",
		GroupedLabel: "repo",
	}
}

// Kinds is the worktrees screen's row of scopes.
func (w *Screen) Kinds() tabs.Stops {
	return tabs.Stops{
		Labels:      scopeLabels,
		FocusedStop: tabs.NoStop,
		ChosenStop:  int(w.scope),
	}
}

// KindsFocused is whether the row of scopes holds the keys.
func (w *Screen) KindsFocused() *bool {
	return &w.scopeFocus
}

// KindsSwitch holds the scope while any directories are selected.
func (w *Screen) KindsSwitch() bool {
	return !w.Selected.IsActive()
}

// StepKind moves the listing to another scope, keeping the cursor on its row when the new scope
// lists it.
func (w *Screen) StepKind(in screen.Inputs, delta int) screen.Request {
	hovered := w.list().Hovered(in)
	w.scope = scope(tabs.StepStop(int(w.scope), delta, len(scopeLabels)))
	w.place(in, hovered)

	return screen.Batch{w.Stage(), screen.Settle{}}
}

// tally is how many directories the scope lists, what the measured ones take, and whether they
// are rendered as one list.
func (w *Screen) tally() string {
	shown := w.shown()
	tally := fmt.Sprintf("%d"+separated+"%s", len(shown), totalLabel(shown))
	if w.sort.AreHeadingsOff {
		tally += separated + tabs.FlatLabel
	}

	return tally
}

// ListParams is the worktrees list as the root renders it.
func (w *Screen) ListParams(in screen.Inputs) list.Params {
	return list.Params{
		Tally:    w.tally(),
		Help:     w.help(in),
		Empty:    "no worktrees",
		Rows:     len(w.rows(in)),
		Snapshot: func(pane list.Pane) view.Snapshot { return w.snapshot(in, pane) },
	}
}

func (w *Screen) help(in screen.Inputs) legend.Help {
	return w.actions(in).Help(w, w.deps.Keys)
}

func (w *Screen) countUnder(repo string) int {
	return len(itemsUnder(w.shown(), repo))
}

// DoesRenderFromListing reports that the screen renders the watched directories.
func (w *Screen) DoesRenderFromListing() bool {
	return true
}

// TakesEveryKey reports that nothing on the screen takes every key.
func (w *Screen) TakesEveryKey() bool {
	return false
}

// ActiveList is the worktrees list the filter narrows.
func (w *Screen) ActiveList() keys.KeyedList {
	return w.list()
}

// HoveredName is the name of the row the cursor is on.
func (w *Screen) HoveredName(in screen.Inputs) string {
	return w.list().HoveredName(in)
}

// Tabs is the strip of columns the list can be ordered by.
func (w *Screen) Tabs() tabs.Strip {
	return sortStrip()
}

// TabsState is where the screen keeps the column chosen and whether the strip holds the keys.
func (w *Screen) TabsState() *tabs.State {
	return &w.sort
}

// Column is the hovered directory's document rendered in the block beside the list.
func (w *Screen) Column(in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: column.Narrowest,
		Read:  column.IsAlwaysRead,
		DocRows: func(width int) []string {
			return paint.DocStyledRows(w.doc(in), max(width, 1), paint.Panel.Stage())
		},
	}
}

// Dispatches is the screen's one table.
func (w *Screen) Dispatches() []keys.Table {
	return []keys.Table{w.actions(screen.Inputs{})}
}
