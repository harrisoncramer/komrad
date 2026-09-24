package tmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSpellKeyNamesTheKeyAsTmuxDoes pins the respellings between what bubbletea prints for a
// press and what tmux binds.
func TestSpellKeyNamesTheKeyAsTmuxDoes(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "a control key",
			key:  "ctrl+d",
			want: "C-d",
		},
		{
			name: "an alt key",
			key:  "alt+x",
			want: "M-x",
		},
		{
			name: "a key both call the same",
			key:  "F1",
			want: "F1",
		},
		{
			name: "a rune",
			key:  "j",
			want: "j",
		},
		{
			name: "a key tmux names differently",
			key:  "shift+tab",
			want: "BTab",
		},
		{
			name: "the space bar",
			key:  "space",
			want: "Space",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SpellKey(tt.key))
		})
	}
}

// TestIsPanelPassThroughTellsThePanelsBindingFromTheUsers pins the shape the panel's own root
// binding is recognised by, against the user's and against the focus binding.
func TestIsPanelPassThroughTellsThePanelsBindingFromTheUsers(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{
			name:    "the user's own binding",
			command: `run-shell "if ps -o state= -o comm= -t '#{pane_tty}' | grep -q vim; then tmux send-keys C-d; else tmux copy-mode -u; fi"`,
		},
		{
			name:    "the focus binding, which names one pane",
			command: `if-shell -F "#{==:#{pane_id},%3}" "select-pane -t %9" "select-pane -t %3"`,
		},
		{
			name:    "a pass-through as tmux prints it",
			command: `if-shell -F "#{==:#{pane_id},#{@weave_panel_pane}}" send-keys "copy-mode -u"`,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsPanelPassThrough(tt.command))
		})
	}
}
