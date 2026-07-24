package panel

import (
	"testing"

	"claude-agents/internal/appconfig"
	"claude-agents/internal/store"

	tea "github.com/charmbracelet/bubbletea"
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

// TestApplyChatsEmptyStaysOnChatsScreen confirms an async refresh that empties
// the chats list leaves the chats screen open, so a chat can be created from
// there. The action handlers must stay panic-safe on the empty list, since they
// resolve the selection through selectedChat rather than indexing m.chats.
func TestApplyChatsEmptyStaysOnChatsScreen(t *testing.T) {
	m := model{screen: screenChats, chats: items("a"), chatCursor: 0, keys: appconfig.DefaultKeybindings()}
	m.applyChats(nil)

	if m.screen != screenChats {
		t.Fatalf("want screenChats after chats emptied, got %v", m.screen)
	}
	if len(m.chats) != 0 {
		t.Fatalf("want empty chats, got %d", len(m.chats))
	}

	// Open on the empty list must be a safe no-op, not an index panic.
	if _, _ = m.updateChats(tea.KeyMsg{Type: tea.KeyEnter}); m.screen != screenChats {
		t.Fatalf("open on empty list changed screen to %v", m.screen)
	}
}

// TestApplyChatsPreservesSelection confirms a refresh keeps the cursor on the
// same chat by name and does not reorder the surviving rows.
func TestApplyChatsPreservesSelection(t *testing.T) {
	m := model{screen: screenChats, chats: items("a", "b", "c"), chatCursor: 1}
	m.applyChats(items("c", "b", "a"))

	if m.screen != screenChats {
		t.Fatalf("want to stay on chats screen, got %v", m.screen)
	}
	if got := m.chats[m.chatCursor].agent.Name; got != "b" {
		t.Fatalf("cursor should stay on b, got %q", got)
	}
	if m.chats[0].agent.Name != "a" || m.chats[2].agent.Name != "c" {
		t.Fatalf("existing order should be preserved, got %v", []string{m.chats[0].agent.Name, m.chats[1].agent.Name, m.chats[2].agent.Name})
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
