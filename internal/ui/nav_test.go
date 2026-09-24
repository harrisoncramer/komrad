package ui

import (
	"testing"

	"weave/config"
	"weave/internal/ui/layout"

	"github.com/stretchr/testify/assert"
)

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
