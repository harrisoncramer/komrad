package ui

import (
	"testing"

	"weave/config"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	"github.com/stretchr/testify/assert"
)

// TestJumpKeys pins the two-press top key, where one press leaves the cursor alone and a key
// pressed between the two drops the first.
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
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
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
		m.listing.workflows = []workflowstore.Workflow{{ID: "w1", Name: "backend"}}
		m.tree.cursor = 0

		after := pressArchive(t, m, keyMsg("G"))
		assert.Equal(t, 0, after.tree.cursor)
	})
}

// TestEnterFocusesTheStagedAgent covers the open key over a live agent, whose own pane is
// already swapped into the stage, so the keyboard is what is left to hand over.
func TestEnterFocusesTheStagedAgent(t *testing.T) {
	tmux := &recordingTmux{}
	m := infoModel(tmux)
	m.stageSelection()

	press(t, m, keyMsg("enter"))

	assert.Equal(t, []string{"%9"}, tmux.selected)
}

// TestEnterLeavesAnUnstagedAgentAlone covers a live agent the stage is not showing, where
// selecting its pane would move focus somewhere the panel is not.
func TestEnterLeavesAnUnstagedAgentAlone(t *testing.T) {
	tmux := &recordingTmux{}
	m := infoModel(tmux)

	press(t, m, keyMsg("enter"))

	assert.Empty(t, tmux.selected)
}

// TestResumeFocusesTheAgentItPutsBack covers the open key over a dormant agent, where the pane
// the resume made is handed the keyboard the way a live agent's already is.
func TestResumeFocusesTheAgentItPutsBack(t *testing.T) {
	tmux := &recordingTmux{}
	m := infoModel(tmux)

	landed, _ := m.applyWrite(writeResult{written: &written{
		row:   agentRowKey("a1"),
		pane:  "%9",
		focus: true,
	}})
	got := asModel(t, landed)

	assert.Equal(t, "%9", got.frame.GuestPane())
	assert.Equal(t, []string{"%9"}, tmux.selected)
}

// TestCreateLeavesTheKeyboardOnThePanel covers the other writes that show a pane, which stage it
// without taking the reader off the list.
func TestCreateLeavesTheKeyboardOnThePanel(t *testing.T) {
	tmux := &recordingTmux{}
	m := infoModel(tmux)

	landed, _ := m.applyWrite(writeResult{written: &written{row: agentRowKey("a1"), pane: "%9"}})
	got := asModel(t, landed)

	assert.Equal(t, "%9", got.frame.GuestPane())
	assert.Empty(t, tmux.selected)
}

// TestResumeFocusesOnceTheHarnessDraws covers a resume whose pane takes the keys only once it is
// shown.
func TestResumeFocusesOnceTheHarnessDraws(t *testing.T) {
	tmux := &recordingTmux{}
	frames := &drawnFrames{}
	m := infoModel(tmux)
	m.harnessFrames = frames

	landed, _ := m.applyWrite(writeResult{written: &written{
		row:   agentRowKey("a1"),
		pane:  "%9",
		focus: true,
	}})
	m = asModel(t, landed)

	assert.Empty(t, tmux.selected, "a pane still at its shell is not given the keys")
	assert.Equal(t, "%9", m.warm.pending)

	frames.drawn = true

	m, _ = m.applyWarmupTick()

	assert.Equal(t, "%9", m.frame.GuestPane())
	assert.Equal(t, []string{"%9"}, tmux.selected)
}

// drawnFrames answers whether a pane is showing its harness yet.
type drawnFrames struct {
	drawn bool
}

func (f *drawnFrames) DoesFrameShowHarness(string, string) bool { return f.drawn }

// TestResumeLeavesTheKeyboardWhenTheStageIsRefused covers the narrow window, where tmux refuses
// the split and the agent's pane is never shown.
func TestResumeLeavesTheKeyboardWhenTheStageIsRefused(t *testing.T) {
	tmux := &unsplittableTmux{}
	m := infoModel(&tmux.recordingTmux)
	m.tmux = tmux
	m.frame = layout.New(tmux, config.DefaultLayout())
	m.frame.Attach("%0")

	landed, _ := m.applyWrite(writeResult{written: &written{
		row:   agentRowKey("a1"),
		pane:  "%9",
		focus: true,
	}})
	got := asModel(t, landed)

	assert.Empty(t, got.frame.GuestPane())
	assert.Empty(t, tmux.selected, "the keys stay on the panel rather than moving to a pane nobody can see")
}

// TestRevealLeavesTheKeyboardWhenTheStageIsRefused covers the same refusal on the arm that waits
// for the harness to draw before it shows the pane.
func TestRevealLeavesTheKeyboardWhenTheStageIsRefused(t *testing.T) {
	tmux := &unsplittableTmux{}
	m := infoModel(&tmux.recordingTmux)
	m.tmux = tmux
	m.frame = layout.New(tmux, config.DefaultLayout())
	m.frame.Attach("%0")

	m, _ = m.revealCreated("%9", true)

	assert.Empty(t, m.frame.GuestPane())
	assert.Empty(t, tmux.selected)
}

// unsplittableTmux is a tmux that will not make the pane the stage needs.
type unsplittableTmux struct {
	recordingTmux
}

func (t *unsplittableTmux) SplitRight(string, string, string) string { return "" }
func (t *unsplittableTmux) SplitLeft(string, string, string) string  { return "" }
