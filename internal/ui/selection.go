package ui

import (
	"fmt"
	"slices"

	agentstore "weave/internal/store/agent"
	"weave/lib/xslice"
)

// rowRef is what the selection knows of a row.
type rowRef struct {
	key    string
	header bool
	group  string
}

type selection struct {
	keys []string
}

func (s selection) isActive() bool {
	return len(s.keys) > 0
}

func (s selection) has(key string) bool {
	return slices.Contains(s.keys, key)
}

func (s selection) toggle(refs []rowRef, cursor int) selection {
	ref, ok := rowAt(refs, cursor)
	if !ok {
		return s
	}

	if !ref.header {
		if s.has(ref.key) {
			return s.without(ref.key)
		}

		return s.with(ref.key)
	}

	keys := groupKeys(refs, ref.group)
	if len(keys) == 0 {
		return s
	}

	if s.hasAll(keys) {
		return s.without(keys...)
	}

	return s.with(keys...)
}

func (s selection) extend(refs []rowRef, cursor int) selection {
	if ref, ok := rowAt(refs, cursor); !ok || ref.header {
		return s
	}

	first, last := cursor, cursor
	if anchor := s.nearest(refs, cursor); anchor >= 0 {
		first, last = min(anchor, cursor), max(anchor, cursor)
	}

	unheadered := xslice.Filter(refs[first:last+1], func(r rowRef) bool { return !r.header })
	keys := xslice.Map(unheadered, func(r rowRef) string { return r.key })

	return s.with(keys...)
}

func (s selection) pruned(refs []rowRef) selection {
	if !s.isActive() {
		return s
	}

	onScreen := make(map[string]bool, len(refs))
	for _, r := range refs {
		if !r.header {
			onScreen[r.key] = true
		}
	}

	kept := xslice.Filter(s.keys, func(key string) bool { return onScreen[key] })

	return selection{keys: kept}
}

func (s selection) nearest(refs []rowRef, cursor int) int {
	closest := -1
	for i, r := range refs {
		if r.header || !s.has(r.key) {
			continue
		}
		if closest < 0 || rowDistance(i, cursor) < rowDistance(closest, cursor) {
			closest = i
		}
	}

	return closest
}

func (s selection) hasAll(keys []string) bool {
	for _, key := range keys {
		if !s.has(key) {
			return false
		}
	}

	return true
}

func (s selection) with(keys ...string) selection {
	next := slices.Clone(s.keys)
	for _, key := range keys {
		if !slices.Contains(next, key) {
			next = append(next, key)
		}
	}
	slices.Sort(next)

	return selection{keys: next}
}

func (s selection) without(keys ...string) selection {
	next := slices.DeleteFunc(slices.Clone(s.keys), func(key string) bool {
		return slices.Contains(keys, key)
	})

	return selection{keys: next}
}

// groupKeys is every row under one header.
func groupKeys(refs []rowRef, group string) []string {
	var keys []string
	for _, r := range refs {
		if !r.header && r.group == group {
			keys = append(keys, r.key)
		}
	}

	return keys
}

// refsOf is the tree as the selection sees it.
func refsOf(rows []agentRow) []rowRef {
	return xslice.Map(rows, func(row agentRow) rowRef {
		return rowRef{
			key:    rowKey(row),
			header: row.header,
			group:  workflowRowKey(row.workflow.ID),
		}
	})
}

func chosenAgents(sel selection, rows []agentRow) []agentstore.Agent {
	chosen := xslice.Filter(rows, func(row agentRow) bool {
		return !row.header && sel.has(rowKey(row))
	})

	return xslice.Map(chosen, func(row agentRow) agentstore.Agent { return row.item.agent })
}

func describeAgents(agents []agentstore.Agent) string {
	if len(agents) == 1 {
		return fmt.Sprintf("agent %q", agents[0].Name)
	}

	return fmt.Sprintf("%d agents", len(agents))
}

func rowDistance(a, b int) int {
	return max(a-b, b-a)
}
