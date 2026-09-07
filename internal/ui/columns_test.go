package ui

import (
	"strings"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sizedTo gives a model a window big enough to hold both halves and attaches its frame, which is
// what every screen drawing a block of its own needs before it will draw one.
func sizedTo(m model, tmux *recordingTmux) model {
	tmux.width, tmux.height = 200, 40
	m.width, m.height = 200, 40
	m.frame.Attach("%0")

	return m
}

// TestAgentsDrawTheHoveredRowsNotes covers a row with no pane of its own, whose notes are drawn in
// the panel's own frame now rather than paged into the stage.
func TestAgentsDrawTheHoveredRowsNotes(t *testing.T) {
	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)
	m.listing.workflows = []workflowstore.Workflow{{ID: "w1", Name: "backend", Context: "always run the linter"}}

	// The folder header, which has no pane and never did.
	m.tree.cursor = 0
	m.stageSelection()

	assert.True(t, m.frame.IsSolo(), "the panel takes the window when there is no pane to show")

	view := ansi.Strip(m.View())
	assert.Contains(t, view, "always run the linter", "the folder's shared notes stand beside the list")
	assert.Contains(t, view, "backend", "and the list is still there")

	for _, row := range strings.Split(m.View(), "\n") {
		assert.Equal(t, m.width, lipgloss.Width(row), "every row of the frame is squared off")
	}
}

// TestAgentsGiveTheWindowToARunningAgent covers the one row that has a pane of its own, since the
// agent's own terminal is the whole point of the stage beside the list.
func TestAgentsGiveTheWindowToARunningAgent(t *testing.T) {
	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)

	m.tree.cursor = 0
	m.stageSelection()
	require.True(t, m.frame.IsSolo(), "the folder header has no pane")

	// infoModel's one agent is alive with a pane of its own.
	m.tree.cursor = 1
	m.stageSelection()

	assert.False(t, m.frame.IsSolo(), "a running agent's own pane gets the window back")
	assert.Equal(t, m.width, m.paneWidth(), "and the list goes back to the whole of the pane")
	assert.Equal(t, 1, tmux.swaps, "the agent's pane is swapped into the stage")
}

// TestAgentsSayWhyADormantAgentHasNoPane covers the placeholders, which used to be a screenful of
// markdown pushed through a file into a pager.
func TestAgentsSayWhyADormantAgentHasNoPane(t *testing.T) {
	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)
	m.listing.agents = []agentItem{{agent: agentstore.Agent{
		ID:         "a1",
		Name:       "auth",
		WorkflowID: "w1",
	}}}

	m.tree.cursor = 1
	m.stageSelection()

	assert.True(t, m.frame.IsSolo(), "a dormant agent has no pane, so the panel keeps the window")
	assert.Contains(t, ansi.Strip(m.View()), "dormant", "and says so in its own frame")
}

// TestSchedulesDrawTheHoveredRecord covers the schedule's own record standing beside the list.
func TestSchedulesDrawTheHoveredRecord(t *testing.T) {
	m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())
	tmux, ok := m.tmux.(*recordingTmux)
	require.True(t, ok)
	m = sizedTo(m, tmux)

	entered, _ := m.enterSchedules()
	m = asModel(t, entered)

	require.True(t, m.frame.IsSolo(), "the schedules screen draws both halves itself")

	view := ansi.Strip(m.View())
	assert.Contains(t, view, "morning-ci", "the list names the schedule")
	assert.Contains(t, view, "0 9 * * 1-5", "and the record beside it spells out when it fires")
	assert.Empty(t, tmux.respawns, "nothing is pushed into a pane to put it there")
}

// TestMailDrawsTheHoveredMessage covers the body of a message being drawn beside the threads,
// which is what the stage pane was reading out of a file until now.
func TestMailDrawsTheHoveredMessage(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	tmux, ok := m.tmux.(*recordingTmux)
	require.True(t, ok)
	m = sizedTo(m, tmux)
	// The model is built on the screen rather than navigated onto it, so nothing has asked for
	// the window yet.
	m.reconcileSolo()

	m.applyMailLoaded(mailLoadedMsg{
		agentID: "a1",
		inbox:   []*agentv1.Mail{received("1", "emacs-fan", "the linter is unhappy about the new file")},
	})

	require.True(t, m.frame.IsSolo(), "the mail screen draws both halves itself")
	assert.Contains(t, ansi.Strip(m.View()), "the linter is unhappy", "the message body stands beside the threads")
	assert.Empty(t, tmux.respawns, "nothing is pushed into a pane to put it there")
}

// TestColumnOffsetResetsOnANewRow covers the scroll offset belonging to the row it was scrolled
// on, since the offset a reader had is into a different document once the cursor moves.
func TestColumnOffsetResetsOnANewRow(t *testing.T) {
	var state columnState

	state.place("agent:a1")
	state.top = 24
	state.place("agent:a1")
	assert.Equal(t, 24, state.top, "staying on the row keeps the reader where they were")

	state.place("agent:a2")
	assert.Equal(t, 0, state.top, "a different row opens at its own top")
}
