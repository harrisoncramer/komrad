package ui

import (
	"fmt"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/panes"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// traceTmux records every tmux call in order, since each one is a fork and exec and the ones
// that move panes make tmux repaint the whole window.
type traceTmux struct {
	recordingTmux
	trace []string
}

func (t *traceTmux) note(format string, args ...any) {
	t.trace = append(t.trace, fmt.Sprintf(format, args...))
}

func (t *traceTmux) IsPaneAlive(pane string) bool {
	t.note("q alive %s", pane)
	return t.recordingTmux.IsPaneAlive(pane)
}

func (t *traceTmux) GetPaneWidth(pane string) int {
	t.note("q width %s", pane)
	return t.recordingTmux.GetPaneWidth(pane)
}

func (t *traceTmux) GetPaneHeight(pane string) int {
	t.note("q height %s", pane)
	return t.recordingTmux.GetPaneHeight(pane)
}

func (t *traceTmux) GetPaneFocus(pane string) (bool, bool) {
	t.note("q focus %s", pane)
	return t.recordingTmux.GetPaneFocus(pane)
}

func (t *traceTmux) GetPaneOwner(pane string) string {
	t.note("q owner %s", pane)
	return t.recordingTmux.GetPaneOwner(pane)
}

func (t *traceTmux) GetSiblingPanes(pane string) []string {
	t.note("q siblings %s", pane)
	return t.recordingTmux.GetSiblingPanes(pane)
}

func (t *traceTmux) MovePanes(moves []panes.Move) {
	t.note("PAINT %s", describeMoves(moves))
	t.recordingTmux.MovePanes(moves)
}

// describeMoves names what one invocation of tmux carried, since it is the invocations that are
// repaints and the moves inside one that are free.
func describeMoves(moves []panes.Move) string {
	described := make([]string, 0, len(moves))
	for _, move := range moves {
		switch move.Kind {
		case panes.KindZoom:
			described = append(described, fmt.Sprintf("zoom %s %v", move.Pane, move.Zoomed))
		case panes.KindSwap:
			described = append(described, fmt.Sprintf("swap %s <-> %s", move.Pane, move.Other))
		case panes.KindRespawn:
			described = append(described, fmt.Sprintf("respawn %s [%s]", move.Pane, strings.Join(move.Argv, " ")))
		}
	}

	return strings.Join(described, " ; ")
}

func (t *traceTmux) ResizePane(pane, width string) {
	t.note("MUT resize-pane %s %s", pane, width)
	t.recordingTmux.ResizePane(pane, width)
}

func (t *traceTmux) ResizeWindow(pane string, width, height int) {
	t.note("MUT resize-window %s %dx%d", pane, width, height)
	t.recordingTmux.ResizeWindow(pane, width, height)
}

func (t *traceTmux) KillPane(pane string) {
	t.note("MUT kill %s", pane)
	t.recordingTmux.KillPane(pane)
}

func (t *traceTmux) SplitRight(a, d, s string) string {
	t.note("MUT split-right %s", a)
	return t.recordingTmux.SplitRight(a, d, s)
}

func (t *traceTmux) SplitLeft(a, d, s string) string {
	t.note("MUT split-left %s", a)
	return t.recordingTmux.SplitLeft(a, d, s)
}

func (t *traceTmux) take() []string {
	out := t.trace
	t.trace = nil
	return out
}

func flickerPanel(t *testing.T) (model, *traceTmux) {
	t.Helper()

	tmux := &traceTmux{recordingTmux: recordingTmux{width: 200, height: 50}}
	m := model{
		tmux:   tmux,
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		width:  200,
		height: 50,
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "weave"}},
			agents: []agentItem{
				{agent: agentstore.Agent{ID: "a1", Name: "one", WorkflowID: "w1", State: agentstore.StateIdle, Pane: "%9"}, alive: true},
				{agent: agentstore.Agent{ID: "a2", Name: "two", WorkflowID: "w1", State: agentstore.StateIdle, Pane: "%8"}, alive: true},
			},
		},
	}
	m.frame.Attach("%0")
	tmux.take()

	return m, tmux
}

// paints is what each invocation of tmux carried, one entry per invocation, which is one entry
// per repaint and so per frame the reader sees go by for one keypress.
func paints(trace []string) []string {
	var painted []string
	for _, line := range trace {
		if after, ok := strings.CutPrefix(line, "PAINT "); ok {
			painted = append(painted, after)
		}
	}

	return painted
}

// TestCursorMovesPaintOnce pins what one move of the cursor costs the window, since every pane
// tmux moves is a repaint and a reader watching two or three go by reads it as a flicker.
func TestCursorMovesPaintOnce(t *testing.T) {
	m, tmux := flickerPanel(t)

	m.tree.cursor = 1
	m.stageSelection()
	tmux.take()

	m.tree.cursor = 0
	m.stageSelection()
	assert.Equal(t, []string{"zoom %0 true"}, paints(tmux.take()),
		"landing on a folder header covers the stage and leaves what is parked in it alone")

	m.tree.cursor = 1
	m.stageSelection()
	assert.Equal(t, []string{"zoom %0 false"}, paints(tmux.take()),
		"and coming back to the agent it was parked on only uncovers it again")
}

// TestCoveredStageKeepsItsGuest pins that the pane parked in the stage stays there while the
// panel is drawn over it, since swapping it out and back is work nobody can see the result of.
func TestCoveredStageKeepsItsGuest(t *testing.T) {
	m, tmux := flickerPanel(t)

	m.tree.cursor = 1
	m.stageSelection()
	require.Equal(t, "%9", m.frame.GuestPane())

	m.tree.cursor = 0
	m.stageSelection()
	tmux.take()

	assert.Equal(t, "%9", m.frame.GuestPane(), "the agent's pane is still in the stage, covered")

	m.stageSelection()
	assert.Empty(t, tmux.take(), "and a refresh pass on the same row asks tmux for nothing")
}

// TestCoveredStageReleasesADeadGuest covers the agent behind the panel going away, whose pane
// takes the stage's own pane with it unless the two are swapped back first.
func TestCoveredStageReleasesADeadGuest(t *testing.T) {
	m, tmux := flickerPanel(t)

	m.tree.cursor = 1
	m.stageSelection()
	require.Equal(t, "%9", m.frame.GuestPane())

	m.tree.cursor = 0
	m.stageSelection()
	tmux.take()

	m.listing.agents[0].alive = false
	m.stageSelection()

	assert.Empty(t, m.frame.GuestPane(), "the dead agent's pane is put back rather than left holding the stage")
	assert.Equal(t, []string{"swap %1 <-> %9 ; respawn %1 [] ; zoom %0 true"}, paints(tmux.take()),
		"and putting it back, blanking the stage and holding the zoom is still one repaint")
}
