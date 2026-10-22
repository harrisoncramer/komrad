package ui

import (
	"slices"
	"time"

	"komrad/internal/errui"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/list"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
)

// worktreeEntry is one top-level entry of a directory.
type worktreeEntry struct {
	path  string
	bytes int64
	dir   bool
}

// worktreeCommit is one of a branch's commits since its base.
type worktreeCommit struct {
	sha         string
	subject     string
	committedAt time.Time
}

// worktreeInspection is one directory read through git, or why it could not be.
type worktreeInspection struct {
	// pending says a fresh reading is on its way, and answered that one has come at all.
	pending  bool
	answered bool
	// readAt is when the answer came.
	readAt time.Time
	// item is the directory as the listing reports it, measured afresh by the read.
	item      panel.Worktree
	entries   []worktreeEntry
	modified  []string
	untracked []string
	baseRef   string
	// baseError is why git could not name a base, when baseRef is empty for that reason.
	baseError string
	ahead     int
	behind    int
	pushed    bool
	unpushed  int
	unpulled  int
	// unpulledLanded says the base already holds every unpulled commit.
	unpulledLanded bool
	// landed is how many of the branch's own commits the base already holds whole.
	landed int
	// sequencing says a merge, a rebase or a cherry-pick is part-way through.
	sequencing  bool
	keepsBranch bool
	commits     []worktreeCommit
	err         error
}

// isStale reports whether the answer is old enough to ask for again.
func (r worktreeInspection) isStale(now time.Time) bool {
	return !r.answered || (!r.isSettled() && now.Sub(r.readAt) > worktreeReadTTL)
}

// isRead reports whether git has answered for the directory.
func (r worktreeInspection) isRead() bool {
	return r.answered && (r.err == nil || r.item.Dir != "")
}

// unpushedCommits is how many commits in the worktree no remote holds, and false where nothing
// was pushed and git cannot name the base to count from.
func (r worktreeInspection) unpushedCommits() (int, bool) {
	switch {
	case r.pushed:
		return r.unpushed, true
	case r.baseRef == "":
		return 0, false
	}

	return r.ahead, true
}

// isSettled reports whether the answer is one that will never change.
func (r worktreeInspection) isSettled() bool {
	return r.answered && errui.IsNotFound(r.err)
}

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

// worktreesScreen is the worktrees screen: the directories the listing holds, the cursor in
// them, which of them are listed, and how they are ordered.
type worktreesScreen struct {
	list.Cursor
	// focus is the agent whose directory the next listing puts the cursor on.
	focus      string
	items      []panel.Worktree
	scope      worktreeScope
	scopeFocus bool
	sort       tabsState
	revision   int
	built      derived[worktreeRowsKey, []worktreeRow]
}

// apply takes a fresh listing, keeping the cursor on the row it was on, or on the agent the
// screen was opened for.
func (w *worktreesScreen) apply(m *model, items []panel.Worktree, hovered string) {
	w.items = items
	w.revision++

	focus := w.focus
	w.focus = ""

	rows := w.rows(m)
	if idx := rowIndexOfWorktreeAgent(rows, focus); idx >= 0 {
		hovered = rows[idx].Key()
	}

	w.place(m, hovered)
}

// shown is the directories the scope lists.
func (w *worktreesScreen) shown() []panel.Worktree {
	return xslice.Filter(w.items, w.scope.holds)
}

// place keeps the cursor on the row it was on as the rows under it change.
func (w *worktreesScreen) place(m *model, hovered string) {
	rows := w.rows(m)
	w.Selected = w.Selected.Pruned(worktreeRefs(rows))

	w.Row = cursor.Place(list.IndexOf(rows, hovered), w.Row, len(rows))
}
