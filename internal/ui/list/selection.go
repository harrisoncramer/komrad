package list

import (
	"slices"

	"komrad/lib/xslice"
)

// Ref is what the selection knows of a row.
type Ref struct {
	Key    string
	Header bool
	Group  string
	// Task is the key of the task the row is a header of or sits under, empty off a task.
	Task string
	// Writer is the key of the writer a task's row stands for, empty on every other row.
	Writer string
}

func (r Ref) isTask() bool {
	return r.Header && r.Task != ""
}

// Selection is the keys of the rows selected in a list.
type Selection struct {
	Keys []string
}

// IsActive reports whether any row is selected.
func (s Selection) IsActive() bool {
	return len(s.Keys) > 0
}

// Has reports whether the row with the key is selected.
func (s Selection) Has(key string) bool {
	return slices.Contains(s.Keys, key)
}

// Toggle is the selection with the row at cursor flipped, or every row under a header at cursor.
func (s Selection) Toggle(refs []Ref, cursor int) Selection {
	ref, ok := At(refs, cursor)
	if !ok {
		return s
	}

	if !ref.Header {
		if s.Has(ref.Key) {
			return s.without(ref.Key)
		}

		return s.with(ref.Key)
	}

	keys := groupKeys(refs, ref)
	if len(keys) == 0 {
		return s
	}

	if s.hasAll(keys) {
		return s.without(keys...)
	}

	return s.with(keys...)
}

// Extend is the selection grown from the nearest selected row to the row at cursor.
func (s Selection) Extend(refs []Ref, cursor int) Selection {
	if ref, ok := At(refs, cursor); !ok || (ref.Header && !ref.isTask()) {
		return s
	}

	first, last := cursor, cursor
	if anchor := s.nearest(refs, cursor); anchor >= 0 {
		first, last = min(anchor, cursor), max(anchor, cursor)
	}

	var keys []string
	for _, r := range refs[first : last+1] {
		switch {
		case !r.Header:
			keys = append(keys, r.Key)
		case r.isTask():
			keys = append(keys, groupKeys(refs, r)...)
		}
	}

	return s.with(keys...)
}

// Pruned is the selection less the rows no longer listed.
func (s Selection) Pruned(refs []Ref) Selection {
	if !s.IsActive() {
		return s
	}

	onScreen := make(map[string]bool, len(refs))
	for _, r := range refs {
		if !r.Header {
			onScreen[r.Key] = true
		}
		if r.Writer != "" {
			onScreen[r.Writer] = true
		}
	}

	kept := xslice.Filter(s.Keys, func(key string) bool { return onScreen[key] })

	return Selection{Keys: kept}
}

func (s Selection) nearest(refs []Ref, cursor int) int {
	closest := -1
	for i, r := range refs {
		if !s.holds(r) {
			continue
		}
		if closest < 0 || rowDistance(i, cursor) < rowDistance(closest, cursor) {
			closest = i
		}
	}

	return closest
}

// holds reports whether the row is selected: an agent's row by its own key and a task's by its writer's.
func (s Selection) holds(r Ref) bool {
	if r.Header {
		return r.Writer != "" && s.Has(r.Writer)
	}

	return s.Has(r.Key)
}

func (s Selection) hasAll(keys []string) bool {
	for _, key := range keys {
		if !s.Has(key) {
			return false
		}
	}

	return true
}

func (s Selection) with(keys ...string) Selection {
	next := slices.Clone(s.Keys)
	for _, key := range keys {
		if !slices.Contains(next, key) {
			next = append(next, key)
		}
	}
	slices.Sort(next)

	return Selection{Keys: next}
}

func (s Selection) without(keys ...string) Selection {
	next := slices.DeleteFunc(slices.Clone(s.Keys), func(key string) bool {
		return slices.Contains(keys, key)
	})

	return Selection{Keys: next}
}

// groupKeys is every row under one header, which for a task header is the rows on its task.
func groupKeys(refs []Ref, header Ref) []string {
	var keys []string
	for _, r := range refs {
		underTask := header.Task != "" && r.Task == header.Task
		underWorkflow := header.Task == "" && r.Group == header.Group
		if !r.Header && (underTask || underWorkflow) {
			keys = append(keys, r.Key)
		}
		if r.Writer != "" && (underTask || underWorkflow) {
			keys = append(keys, r.Writer)
		}
	}

	return keys
}

func rowDistance(a, b int) int {
	return max(a-b, b-a)
}
