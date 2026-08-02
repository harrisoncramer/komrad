package ui

import (
	"slices"
)

// Multi-select over a tree of folders and agents. Both trees the panel draws use it, so it
// works over rows and a cursor rather than over either screen's state. What acts on a
// selection lives with the key that does it, and this is the choosing alone.

// selection is the agents chosen on one tree, held by id and sorted. Ids rather than row
// indexes, since a listing rebuilds the tree between one keypress and the next.
type selection struct {
	ids []string
}

// active reports whether the screen drawing this is in multi-select mode.
func (s selection) active() bool {
	return len(s.ids) > 0
}

// has reports whether an agent is in the selection.
func (s selection) has(id string) bool {
	return slices.Contains(s.ids, id)
}

// count is how many agents are held.
func (s selection) count() int {
	return len(s.ids)
}

// toggle takes the row under the cursor into the selection or back out of it. A workflow
// header is its whole folder: one not already wholly selected is taken in whole, and one
// that is drops, so the key is a checkbox over the folder.
func (s selection) toggle(rows []agentRow, cursor int) selection {
	row, ok := rowAt(rows, cursor)
	if !ok {
		return s
	}

	if !row.header {
		if s.has(row.item.agent.ID) {
			return s.without(row.item.agent.ID)
		}

		return s.with(row.item.agent.ID)
	}

	// An empty folder has nothing to select, and reading it as wholly selected would put the
	// screen into a mode with nothing in it.
	ids := workflowAgentIDs(rows, row.workflow.ID)
	if len(ids) == 0 {
		return s
	}

	if s.hasAll(ids) {
		return s.without(ids...)
	}

	return s.with(ids...)
}

// extend takes in every agent between the cursor and the nearest selected row, which is how
// a run of agents is chosen without pressing the toggle down the whole of it. Headers inside
// the range are passed over, since only agents are selectable. A range with no row to reach
// back to falls back to the agent under the cursor.
func (s selection) extend(rows []agentRow, cursor int) selection {
	if row, ok := rowAt(rows, cursor); !ok || row.header {
		return s
	}

	first, last := cursor, cursor
	if anchor := s.nearest(rows, cursor); anchor >= 0 {
		first, last = min(anchor, cursor), max(anchor, cursor)
	}

	var ids []string
	for _, r := range rows[first : last+1] {
		if !r.header {
			ids = append(ids, r.item.agent.ID)
		}
	}

	return s.with(ids...)
}

// pruned drops the agents that no longer have a row, so one archived from elsewhere or put
// away by the hidden toggle cannot hold a screen in multi-select mode over something nobody
// can see.
func (s selection) pruned(rows []agentRow) selection {
	if !s.active() {
		return s
	}

	onScreen := make(map[string]bool, len(rows))
	for _, r := range rows {
		if !r.header {
			onScreen[r.item.agent.ID] = true
		}
	}

	kept := make([]string, 0, len(s.ids))
	for _, id := range s.ids {
		if onScreen[id] {
			kept = append(kept, id)
		}
	}

	return selection{ids: kept}
}

// nearest is the selected row closest to the cursor, or -1 when nothing is selected. A
// cursor sitting the same distance from a selection above and one below takes the one above,
// so the key answers the same whichever way the cursor arrived.
func (s selection) nearest(rows []agentRow, cursor int) int {
	closest := -1
	for i, r := range rows {
		if r.header || !s.has(r.item.agent.ID) {
			continue
		}
		if closest < 0 || rowDistance(i, cursor) < rowDistance(closest, cursor) {
			closest = i
		}
	}

	return closest
}

// hasAll reports whether every one of the given agents is already selected, which is what
// makes a folder's key a toggle.
func (s selection) hasAll(ids []string) bool {
	for _, id := range ids {
		if !s.has(id) {
			return false
		}
	}

	return true
}

// with is the selection with ids added, sorted so one compares by what is in it rather than
// by the order the rows were pressed in. It copies rather than appending in place, since the
// caller still holds the value this came off.
func (s selection) with(ids ...string) selection {
	next := slices.Clone(s.ids)
	for _, id := range ids {
		if !slices.Contains(next, id) {
			next = append(next, id)
		}
	}
	slices.Sort(next)

	return selection{ids: next}
}

// without is the selection with ids removed.
func (s selection) without(ids ...string) selection {
	next := slices.DeleteFunc(slices.Clone(s.ids), func(id string) bool {
		return slices.Contains(ids, id)
	})

	return selection{ids: next}
}

// applySelectionKey answers a key against a selection over the given rows, reporting whether
// it took it. Both trees route their keys through this, so the gesture cannot drift apart.
//
// The clear key is taken only while the mode is up, since it is the same key each screen
// leaves by and a screen at rest should still leave.
func (m model) applySelectionKey(k string, sel selection, rows []agentRow, cursor int) (selection, bool) {
	switch {
	case sel.active() && isBound(k, m.keys.Selection.Clear):
		return selection{}, true
	case isBound(k, m.keys.Selection.Toggle):
		return sel.toggle(rows, cursor), true
	case isBound(k, m.keys.Selection.Range):
		// From a tree at rest a range has nothing to reach back to, so the two keys only
		// part once the mode is up.
		if !sel.active() {
			return sel.toggle(rows, cursor), true
		}

		return sel.extend(rows, cursor), true
	}

	return sel, false
}

// workflowAgentIDs are the agents drawn under a folder. It reads the rows rather than a
// listing, so an agent that is not on screen is not selected out from under the user.
func workflowAgentIDs(rows []agentRow, workflowID string) []string {
	var ids []string
	for _, r := range rows {
		if !r.header && r.workflow.ID == workflowID {
			ids = append(ids, r.item.agent.ID)
		}
	}

	return ids
}

func rowDistance(a, b int) int {
	return max(a-b, b-a)
}
