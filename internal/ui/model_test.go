package ui

import (
	"testing"

	"claude-agents/internal/appconfig"

	"claude-agents/internal/store/session"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestIndexOfChat(t *testing.T) {
	tests := []struct {
		name  string
		chats []chatItem
		find  string
		want  int
	}{
		{
			name:  "an empty list",
			chats: nil,
			find:  "a",
			want:  0,
		},
		{
			name:  "a name nobody holds falls back to the first row",
			chats: items("a", "b", "c"),
			find:  "gone",
			want:  0,
		},
		{
			name:  "a chat in the middle",
			chats: items("a", "b", "c"),
			find:  "b",
			want:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, indexOfChat(tt.chats, tt.find))
		})
	}
}

func TestClampCursor(t *testing.T) {
	tests := []struct {
		name   string
		idx    int
		length int
		want   int
	}{
		{
			name:   "an empty list pins to zero",
			idx:    0,
			length: 0,
			want:   0,
		},
		{
			name:   "a negative index pins to the first row",
			idx:    -1,
			length: 3,
			want:   0,
		},
		{
			name:   "an index past the end pins to the last row",
			idx:    9,
			length: 3,
			want:   2,
		},
		{
			name:   "an index inside the list is left alone",
			idx:    2,
			length: 5,
			want:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, clampCursor(tt.idx, tt.length))
		})
	}
}

// TestApplyChats covers what a refresh does to the on-screen list. It keeps the
// cursor on the chat it was on and does not reorder surviving rows, since
// reordering under the cursor defeats an index-based selection.
func TestApplyChats(t *testing.T) {
	tests := []struct {
		name       string
		start      []chatItem
		cursor     int
		fresh      []chatItem
		wantLen    int
		wantCursor string
		wantOrder  []string
	}{
		{
			name:    "a refresh that empties the list stays on the chats screen",
			start:   items("a"),
			fresh:   nil,
			wantLen: 0,
		},
		{
			name:       "the cursor follows its chat rather than its index",
			start:      items("a", "b", "c"),
			cursor:     1,
			fresh:      items("c", "b", "a"),
			wantLen:    3,
			wantCursor: "b",
			wantOrder:  []string{"a", "b", "c"},
		},
		{
			name:       "a new chat is appended rather than sorted in",
			start:      items("a"),
			cursor:     0,
			fresh:      items("b", "a"),
			wantLen:    2,
			wantCursor: "a",
			wantOrder:  []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				screen:     screenChats,
				chats:      tt.start,
				chatCursor: tt.cursor,
				keys:       appconfig.DefaultKeybindings(),
			}

			m.applyChats(tt.fresh)

			assert.Equal(t, screenChats, m.screen, "the panel stays where it was")
			assert.Len(t, m.chats, tt.wantLen)

			if tt.wantLen == 0 {
				// Acting on the empty list must be a safe no-op rather than an index
				// panic, since the handlers resolve a selection through selectedChat.
				_, _ = m.updateChats(tea.KeyMsg{Type: tea.KeyEnter})
				assert.Equal(t, screenChats, m.screen)

				return
			}

			assert.Equal(t, tt.wantCursor, m.chats[m.chatCursor].agent.Name)

			names := make([]string, 0, len(m.chats))
			for _, chat := range m.chats {
				names = append(names, chat.agent.Name)
			}
			assert.Equal(t, tt.wantOrder, names)
		})
	}
}

// items builds a list of chats from names alone, for the cases that only care
// about ordering and selection.
func items(names ...string) []chatItem {
	out := make([]chatItem, 0, len(names))
	for _, name := range names {
		out = append(out, chatItem{agent: session.Agent{Name: name}})
	}

	return out
}
