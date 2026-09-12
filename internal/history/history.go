package history

import "slices"

// List is the places somebody has been and a cursor saying which one they are at.
type List[T comparable] struct {
	entries []T
	cursor  int
	limit   int
}

// New returns an empty list holding at most limit entries, or any number when limit is below one.
func New[T comparable](limit int) *List[T] {
	return &List[T]{
		entries: nil,
		cursor:  -1,
		limit:   limit,
	}
}

// Visit records arriving somewhere by choice, dropping the forward entries and any earlier visit.
func (l *List[T]) Visit(entry T) {
	if current, ok := l.GetCurrent(); ok && current == entry {
		return
	}

	l.entries = l.entries[:l.cursor+1]
	if at := slices.Index(l.entries, entry); at >= 0 {
		l.entries = slices.Delete(l.entries, at, at+1)
	}

	l.entries = append(l.entries, entry)
	if l.limit > 0 && len(l.entries) > l.limit {
		l.entries = slices.Delete(l.entries, 0, len(l.entries)-l.limit)
	}

	l.cursor = len(l.entries) - 1
}

// GoBack moves the cursor to the previous entry and returns it.
func (l *List[T]) GoBack() (T, bool) {
	if l.cursor <= 0 {
		var zero T
		return zero, false
	}

	l.cursor--

	return l.entries[l.cursor], true
}

// GoForward moves the cursor to the next entry and returns it.
func (l *List[T]) GoForward() (T, bool) {
	if l.cursor < 0 || l.cursor >= len(l.entries)-1 {
		var zero T
		return zero, false
	}

	l.cursor++

	return l.entries[l.cursor], true
}

// GetCurrent returns the entry the cursor is at.
func (l *List[T]) GetCurrent() (T, bool) {
	if l.cursor < 0 || l.cursor >= len(l.entries) {
		var zero T
		return zero, false
	}

	return l.entries[l.cursor], true
}

// Remove forgets an entry, leaving the cursor on whatever takes its place.
func (l *List[T]) Remove(entry T) bool {
	at := slices.Index(l.entries, entry)
	if at < 0 {
		return false
	}

	l.entries = slices.Delete(l.entries, at, at+1)
	if at < l.cursor {
		l.cursor--
	}

	if l.cursor > len(l.entries)-1 {
		l.cursor = len(l.entries) - 1
	}

	return true
}

// GetEntries returns the entries from oldest to most recent.
func (l *List[T]) GetEntries() []T {
	return slices.Clone(l.entries)
}

// Len returns how many entries the list holds.
func (l *List[T]) Len() int {
	return len(l.entries)
}
