package ui

import (
	"fmt"
	"slices"

	agentstore "weave/internal/store/agent"
	"weave/lib/xslice"
)

type selection struct {
	ids []string
}

func (s selection) active() bool {
	return len(s.ids) > 0
}

func (s selection) has(id string) bool {
	return slices.Contains(s.ids, id)
}

func (s selection) count() int {
	return len(s.ids)
}

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

	ids := workflowAgentIDs(rows, row.workflow.ID)
	if len(ids) == 0 {
		return s
	}

	if s.hasAll(ids) {
		return s.without(ids...)
	}

	return s.with(ids...)
}

func (s selection) extend(rows []agentRow, cursor int) selection {
	if row, ok := rowAt(rows, cursor); !ok || row.header {
		return s
	}

	first, last := cursor, cursor
	if anchor := s.nearest(rows, cursor); anchor >= 0 {
		first, last = min(anchor, cursor), max(anchor, cursor)
	}

	unheadered := xslice.Filter(rows[first:last+1], func(r agentRow) bool { return !r.header })
	ids := xslice.Map(unheadered, func(r agentRow) string { return r.item.agent.ID })

	return s.with(ids...)
}

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

	kept := xslice.Filter(s.ids, func(id string) bool { return onScreen[id] })

	return selection{ids: kept}
}

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

func (s selection) hasAll(ids []string) bool {
	for _, id := range ids {
		if !s.has(id) {
			return false
		}
	}

	return true
}

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

func (s selection) without(ids ...string) selection {
	next := slices.DeleteFunc(slices.Clone(s.ids), func(id string) bool {
		return slices.Contains(ids, id)
	})

	return selection{ids: next}
}

func (m model) applySelectionKey(k string, sel selection, rows []agentRow, cursor int) (selection, bool) {
	switch {
	case sel.active() && isBound(k, m.keys.Selection.Clear):
		return selection{}, true
	case isBound(k, m.keys.Selection.Toggle):
		return sel.toggle(rows, cursor), true
	case isBound(k, m.keys.Selection.Range):

		if !sel.active() {
			return sel.toggle(rows, cursor), true
		}

		return sel.extend(rows, cursor), true
	}

	return sel, false
}

func chosenAgents(sel selection, rows []agentRow) []agentstore.Agent {
	chosen := xslice.Filter(rows, func(row agentRow) bool {
		return !row.header && sel.has(row.item.agent.ID)
	})

	return xslice.Map(chosen, func(row agentRow) agentstore.Agent { return row.item.agent })
}

func agentIDs(agents []agentstore.Agent) []string {
	return xslice.Map(agents, func(agent agentstore.Agent) string { return agent.ID })
}

func describeAgents(agents []agentstore.Agent) string {
	if len(agents) == 1 {
		return fmt.Sprintf("agent %q", agents[0].Name)
	}

	return fmt.Sprintf("%d agents", len(agents))
}

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
