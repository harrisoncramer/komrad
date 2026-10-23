package worktrees

import (
	"os"
	"slices"

	"komrad/config"
	"komrad/internal/glyph"
	"komrad/internal/ui/api"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/tabs"
	"komrad/lib/xslice"
)

const separated = " " + glyph.Separator + " "

// scope is which of the directories the worktrees screen lists.
type scope int

const (
	scopeAll scope = iota
	scopeInactive
	scopeMerged
)

var scopeLabels = []string{"all", "inactive", "merged"}

// holds reports whether the scope lists the directory.
func (s scope) holds(item panel.Worktree) bool {
	switch s {
	case scopeInactive:
		return item.IsInactive()
	case scopeMerged:
		return item.IsInactive() && item.IsMerged()
	case scopeAll:
	}

	return true
}

// listsAgent reports whether the scope lists a directory the agent holds.
func (s scope) listsAgent(items []panel.Worktree, id string) bool {
	return slices.ContainsFunc(items, func(item panel.Worktree) bool { return s.holds(item) && item.HoldsAgent(id) })
}

// Deps is what the worktrees screen is given at launch.
type Deps struct {
	Keys    config.Keybindings
	Clients api.Clients
	Top     *keys.Top
}

// Screen is the worktrees screen: the directories the listing holds, the cursor in
// them, which of them are listed, and how they are ordered.
type Screen struct {
	deps Deps
	list.Cursor
	// focus is the agent whose directory the next listing puts the cursor on.
	focus      string
	items      []panel.Worktree
	scope      scope
	scopeFocus bool
	sort       tabs.State
	revision   int
	built      screen.Derived[rowsKey, []row]
}

// New is the worktrees screen built from what it is given at launch, listing every directory as one list.
func New(deps Deps) Screen {
	return Screen{
		deps: deps,
		sort: tabs.State{Flat: true},
	}
}

// apply takes a fresh listing, keeping the cursor on the row it was on, or on the agent the
// screen was opened for.
func (w *Screen) apply(in screen.Inputs, hovered string) {
	w.items = in.Listing.Worktrees
	w.revision++

	focus := w.focus
	w.focus = ""

	rows := w.rows(in)
	if idx := rowIndexOfAgent(rows, focus); idx >= 0 {
		hovered = rows[idx].Key()
	}

	w.place(in, hovered)
}

// shown is the directories the scope lists.
func (w *Screen) shown() []panel.Worktree {
	return xslice.Filter(w.items, w.scope.holds)
}

// place keeps the cursor on the row it was on as the rows under it change.
func (w *Screen) place(in screen.Inputs, hovered string) {
	rows := w.rows(in)
	w.Selected = w.Selected.Pruned(refs(rows))

	w.Row = cursor.Place(list.IndexOf(rows, hovered), w.Row, len(rows))
}

// homeDir is the user's home directory, which shortens the paths under it.
func homeDir() string {
	dir, _ := os.UserHomeDir()

	return dir
}
