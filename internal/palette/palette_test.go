package palette

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClosingKeys covers the two keys that close the popup, since a popup that will not close
// is the one way this can be in the user's way while it does nothing else.
func TestClosingKeys(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyMsg
	}{
		{
			name: "escape closes",
			key:  tea.KeyMsg{Type: tea.KeyEsc},
		},
		{
			name: "ctrl+c closes",
			key:  tea.KeyMsg{Type: tea.KeyCtrlC},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cmd := newModel().Update(tt.key)
			require.NotNil(t, cmd, "the key left the popup open")

			assert.IsType(t, tea.QuitMsg{}, cmd())
		})
	}
}

func TestTyping(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want string
	}{
		{
			name: "backspace on an empty query types nothing",
			keys: []string{"backspace"},
			want: "",
		},
		{
			name: "backspace takes the last rune back",
			keys: []string{"a", "b", "backspace"},
			want: "a",
		},
		{
			name: "runes and spaces both type",
			keys: []string{"a", "space", "b"},
			want: "a b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shown := press(t, newModel(), tt.keys...)

			assert.Equal(t, tt.want, shown.query)
			assert.Contains(t, shown.View(), tt.want)
		})
	}
}

// press feeds keys to a model the way bubbletea would and returns what they left behind.
func press(t *testing.T, start model, keys ...string) model {
	t.Helper()

	current := tea.Model(start)
	for _, key := range keys {
		next, _ := current.Update(keyMsg(key))
		current = next
	}

	shown, ok := current.(model)
	require.True(t, ok)

	return shown
}

func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.ToLower(key))}
	}
}
