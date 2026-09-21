package ui

import (
	"time"

	"weave/internal/errui"
	agentstore "weave/internal/store/agent"
)

// worktreeAgent is what the listing says of an agent that holds a directory.
type worktreeAgent struct {
	id         string
	name       string
	state      agentstore.State
	archivedAt *time.Time
}

// worktreeSize is what a directory took when it was last walked, or why the walk failed.
type worktreeSize struct {
	bytes      int64
	measuredAt time.Time
	// err is why the last walk failed, empty when it did not.
	err string
}

// worktreeItem is one directory as the listing reported it.
type worktreeItem struct {
	dir      string
	repoDir  string
	branch   string
	readable bool
	agent    *worktreeAgent
	// occupants are the other agents working in the directory.
	occupants []worktreeAgent
	// size is the last measurement, nil until the loop has reached the directory.
	size         *worktreeSize
	lastActiveAt time.Time
}

// isMeasured reports whether the directory carries a size.
func (item worktreeItem) isMeasured() bool {
	return item.size != nil && item.size.err == ""
}

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
	item      worktreeItem
	entries   []worktreeEntry
	modified  []string
	untracked []string
	baseRef   string
	ahead     int
	behind    int
	// landed is how many of the branch's own commits the base already holds whole.
	landed int
	// sequencing says a merge, a rebase or a cherry-pick is part-way through.
	sequencing  bool
	keepsBranch bool
	commits     []worktreeCommit
	err         error
}

// isStale reports whether the answer is old enough to ask for again.
func (read worktreeInspection) isStale(now time.Time) bool {
	return !read.answered || (!read.isSettled() && now.Sub(read.readAt) > worktreeReadTTL)
}

// isSettled reports whether the answer is one that will never change, which the service saying
// the directory is not a worktree it holds is.
func (read worktreeInspection) isSettled() bool {
	return read.answered && errui.IsNotFound(read.err)
}

type worktreesState struct {
	rowCursor
	items []worktreeItem
	sort  tabsState
	// inspected is what get_worktree answered for each directory, kept until the directory
	// leaves the listing.
	inspected map[string]worktreeInspection
}

func (m *model) applyWorktrees(items []worktreeItem, hovered string) {
	m.worktrees.items = items

	listedDirs := make(map[string]bool, len(items))
	for _, item := range items {
		listedDirs[item.dir] = true
	}
	for dir := range m.worktrees.inspected {
		if !listedDirs[dir] {
			delete(m.worktrees.inspected, dir)
		}
	}

	rows := m.worktreeRows()
	m.worktrees.selected = m.worktrees.selected.pruned(worktreeRefs(rows))

	focus := m.worktrees.focus
	m.worktrees.focus = ""
	if idx := rowIndexOfWorktreeAgent(rows, focus); focus != "" && idx >= 0 {
		m.landWorktreeCursor(rows, idx)

		return
	}

	if idx := rowIndexOfKey(rows, hovered); idx >= 0 {
		m.landWorktreeCursor(rows, idx)

		return
	}

	m.landWorktreeCursor(rows, clampCursor(m.worktrees.cursor, len(rows)))
}

// landWorktreeCursor puts the cursor on a row by index and names the row to the column.
func (m *model) landWorktreeCursor(rows []worktreeRow, idx int) {
	m.worktrees.cursor = idx
	m.placeColumn(screenWorktrees, rowKeyAt(rows, idx))
}
