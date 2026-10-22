package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/tabs"
	"komrad/lib/xslice"
)

// worktreeScope is which of the directories the worktrees screen lists.
type worktreeScope int

const (
	scopeAll worktreeScope = iota
	scopeInactive
	scopeMerged
)

var worktreeScopeLabels = []string{"all", "inactive", "merged"}

// holds reports whether the scope lists the directory.
func (s worktreeScope) holds(item panel.Worktree) bool {
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
func (s worktreeScope) listsAgent(items []panel.Worktree, id string) bool {
	return slices.ContainsFunc(items, func(item panel.Worktree) bool { return s.holds(item) && item.HoldsAgent(id) })
}

// worktreesDeps is what the worktrees screen is given at launch.
type worktreesDeps struct {
	keys    config.Keybindings
	clients api.Clients
	top     *keys.Top
}

// worktreesScreen is the worktrees screen: the directories the listing holds, the cursor in
// them, which of them are listed, and how they are ordered.
type worktreesScreen struct {
	deps worktreesDeps
	list.Cursor
	// focus is the agent whose directory the next listing puts the cursor on.
	focus      string
	items      []panel.Worktree
	scope      worktreeScope
	scopeFocus bool
	sort       tabs.State
	revision   int
	built      derived[worktreeRowsKey, []worktreeRow]
}

// apply takes a fresh listing, keeping the cursor on the row it was on, or on the agent the
// screen was opened for.
func (w *worktreesScreen) apply(in screen.Inputs, hovered string) {
	w.items = in.Listing.Worktrees
	w.revision++

	focus := w.focus
	w.focus = ""

	rows := w.rows(in)
	if idx := rowIndexOfWorktreeAgent(rows, focus); idx >= 0 {
		hovered = rows[idx].Key()
	}

	w.place(in, hovered)
}

// shown is the directories the scope lists.
func (w *worktreesScreen) shown() []panel.Worktree {
	return xslice.Filter(w.items, w.scope.holds)
}

// place keeps the cursor on the row it was on as the rows under it change.
func (w *worktreesScreen) place(in screen.Inputs, hovered string) {
	rows := w.rows(in)
	w.Selected = w.Selected.Pruned(worktreeRefs(rows))

	w.Row = cursor.Place(list.IndexOf(rows, hovered), w.Row, len(rows))
}
