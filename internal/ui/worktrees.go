package ui

import (
	"time"

	agentstore "weave/internal/store/agent"
)

// worktreeAgent is what the listing says of an agent that holds a directory.
type worktreeAgent struct {
	id         string
	name       string
	state      agentstore.State
	archivedAt *time.Time
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
	// bytes is the last count, nil until one has reached the directory.
	bytes *int64
}

// worktreeEntry is one top-level entry of a directory as of its last count.
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

// worktreeInspection is one directory read through git, why it could not be, or the fact that
// the reading is still on its way.
type worktreeInspection struct {
	pending     bool
	entries     []worktreeEntry
	modified    []string
	untracked   []string
	baseRef     string
	ahead       int
	behind      int
	keepsBranch bool
	commits     []worktreeCommit
	err         error
}

type worktreesState struct {
	rowCursor
	items []worktreeItem
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
