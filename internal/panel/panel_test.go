package panel

import (
	"testing"

	"claude-agents/internal/store"
)

func items(names ...string) []chatItem {
	out := make([]chatItem, 0, len(names))
	for _, n := range names {
		out = append(out, chatItem{agent: store.Agent{Name: n}})
	}
	return out
}

func TestIndexOfChat(t *testing.T) {
	list := items("a", "b", "c")

	if got := indexOfChat(list, "b"); got != 1 {
		t.Fatalf("want 1, got %d", got)
	}
	// A vanished selection falls back to the first row rather than a stale index.
	if got := indexOfChat(list, "gone"); got != 0 {
		t.Fatalf("want 0 for missing, got %d", got)
	}
	if got := indexOfChat(nil, "a"); got != 0 {
		t.Fatalf("want 0 for empty, got %d", got)
	}
}

func TestClampCursor(t *testing.T) {
	cases := []struct{ idx, length, want int }{
		{2, 5, 2},
		{9, 3, 2},
		{-1, 3, 0},
		{0, 0, 0},
	}
	for _, c := range cases {
		if got := clampCursor(c.idx, c.length); got != c.want {
			t.Fatalf("clampCursor(%d,%d): want %d, got %d", c.idx, c.length, c.want, got)
		}
	}
}
