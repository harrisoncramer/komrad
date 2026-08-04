package ui

import (
	"testing"

	"weave/config"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
)

// TestJumpKeys covers the two keys that reach the ends of a tree. Top takes two presses the
// way vim's gg does, so a single press has to leave the cursor alone, and a key pressed
// between the two has to drop the first rather than being read as the second.
func TestJumpKeys(t *testing.T) {
	tests := []struct {
		name       string
		cursor     int
		keys       []string
		wantCursor int
	}{
		{
			name:       "one press of the top key waits for the second",
			cursor:     3,
			keys:       []string{"g"},
			wantCursor: 3,
		},
		{
			name:       "a key between the two drops the first press",
			cursor:     3,
			keys:       []string{"g", "k", "g"},
			wantCursor: 2,
		},
		{
			name:       "the top key twice lands on the first row",
			cursor:     3,
			keys:       []string{"g", "g"},
			wantCursor: 0,
		},
		{
			name:       "the bottom key lands on the last row",
			cursor:     1,
			keys:       []string{"G"},
			wantCursor: 5,
		},
		{
			name:       "the top key from the first row stays there",
			keys:       []string{"g", "g"},
			wantCursor: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := selectionTree()
			m.tree.cursor = tt.cursor

			for _, k := range tt.keys {
				m = press(t, m, keyMsg(k))
			}

			assert.Equal(t, tt.wantCursor, m.tree.cursor)
		})
	}
}

// TestJumpKeysOnEmptyTree covers a list with no rows at all, where the last row is index -1
// and jumping to it would put the cursor off the list.
func TestJumpKeysOnEmptyTree(t *testing.T) {
	m := model{
		stage: NewStage(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
	}

	assert.Equal(t, 0, press(t, m, keyMsg("G")).tree.cursor)
}

// TestArchiveJumpKeys covers the same two keys on the other tree, which keeps its own cursor.
func TestArchiveJumpKeys(t *testing.T) {
	t.Run("the top key twice lands on the first row", func(t *testing.T) {
		m := archiveSelectionTree()
		m.archive.cursor = 3

		after := pressArchive(t, m, keyMsg("g"))
		assert.Equal(t, 3, after.archive.cursor, "the first press waits for the second")

		after = pressArchive(t, after, keyMsg("g"))
		assert.Equal(t, 0, after.archive.cursor)
	})

	t.Run("the bottom key lands on the last row", func(t *testing.T) {
		m := archiveSelectionTree()

		after := pressArchive(t, m, keyMsg("G"))
		assert.Equal(t, len(m.archiveRows())-1, after.archive.cursor)
	})

	t.Run("jumping leaves the agents tree alone", func(t *testing.T) {
		m := archiveSelectionTree()
		m.tree.workflows = []workflowstore.Workflow{{ID: "w1", Name: "backend"}}
		m.tree.cursor = 0

		after := pressArchive(t, m, keyMsg("G"))
		assert.Equal(t, 0, after.tree.cursor)
	})
}
