package ui

import (
	"errors"
	"testing"

	"weave/internal/appconfig"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

// TestEditorKey covers the key that opens an agent's directory in the user's editor.
// Where the window lands and whether the popup comes down behind it are the whole
// point of the key, so both are asserted rather than the call alone.
func TestEditorKey(t *testing.T) {
	tests := []struct {
		name        string
		cursor      int
		editor      string
		openErr     error
		wantWindows []string
		wantDetach  int
		wantBanner  bool
	}{
		{
			name:   "nothing under the cursor has no directory to open",
			cursor: -1,
			editor: "nvim",
		},
		{
			name:       "a failed open keeps the popup up so its banner can be read",
			cursor:     1,
			editor:     "nvim",
			openErr:    errors.New("tmux knows no client"),
			wantBanner: true,
		},
		{
			name:        "an editor of nothing leaves a shell in the directory",
			cursor:      1,
			editor:      "",
			wantWindows: []string{"/repo"},
			wantDetach:  1,
		},
		{
			name:        "an editor carrying its own flags keeps them",
			cursor:      1,
			editor:      "nvim -u NONE",
			wantWindows: []string{"/repo nvim -u NONE /repo"},
			wantDetach:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &recordingTmux{openWindowErr: tt.openErr}
			m := editorModel(tmux, tt.editor, tt.cursor)

			next, cmd := m.updateAgents(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
			if cmd != nil {
				cmd()
			}

			updated, ok := next.(model)
			assert.True(t, ok, "want a panel model back, got %T", next)
			assert.Equal(t, tt.wantWindows, tmux.windows)
			assert.Equal(t, tt.wantDetach, tmux.detaches)

			if tt.wantBanner {
				assert.NotNil(t, updated.banner)
			} else {
				assert.Nil(t, updated.banner)
			}
		})
	}
}

// editorModel builds the panel on a list of one agent under one workflow header, with
// the cursor where the test puts it. Row 0 is the header and row 1 the agent.
func editorModel(tmux *recordingTmux, editor string, cursor int) model {
	stage := NewStage(tmux, appconfig.DefaultSplit)
	stage.Init()

	return model{
		tmux:   tmux,
		popup:  NewPopup(tmux, "test-session"),
		stage:  stage,
		keys:   appconfig.DefaultKeybindings(),
		editor: editor,
		screen: screenAgents,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{agent: agentstore.Agent{Name: "auth", Pane: "%9", Cwd: "/repo", WorkflowID: "w1"}, alive: true},
			},
			cursor: cursor,
		},
	}
}
