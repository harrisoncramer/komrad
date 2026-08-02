package ui

import (
	"testing"

	"weave/internal/appconfig"
	"weave/internal/tmux"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBelowActionKeys covers the keys that run one of the panel's own actions. What each
// fires and where it fires it are the whole point, so the pane the panel opened for the
// script is asserted alongside the call.
func TestBelowActionKeys(t *testing.T) {
	tests := []struct {
		name      string
		cursor    int
		key       string
		wantFired []string
		wantPane  string
	}{
		{
			name:   "nothing under the cursor has no agent to act on",
			cursor: -1,
			key:    "e",
		},
		{
			name:      "the editor key fires the editor action on the hovered agent",
			cursor:    1,
			key:       "e",
			wantFired: []string{"a1/_editor"},
			wantPane:  "%2",
		},
		{
			name:      "the terminal key fires the terminal action",
			cursor:    1,
			key:       "t",
			wantFired: []string{"a1/_terminal"},
			wantPane:  "%2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &recordingTmux{}
			client := &fakeActions{}
			m := belowActionModel(recorder, client, tt.cursor)

			next, cmd := m.updateAgents(keyMsg(tt.key))
			pressed, ok := next.(model)
			require.True(t, ok)

			got := settle(t, pressed, cmd)

			assert.Equal(t, tt.wantFired, client.fired)
			assert.Equal(t, tt.wantPane, recorder.globals[tmux.SecondaryPaneOption])
			assert.Nil(t, got.banner, "nothing here was refused")
		})
	}
}

// TestBelowActionKeyClosesOnSecondPress covers the key that opened the pane taking it away
// again, which is the only way back to a full-height stage.
func TestBelowActionKeyClosesOnSecondPress(t *testing.T) {
	recorder := &recordingTmux{}
	client := &fakeActions{}

	opened, cmd := belowActionModel(recorder, client, 1).updateAgents(keyMsg("e"))
	pressed, ok := opened.(model)
	require.True(t, ok)

	settled := settle(t, pressed, cmd)
	require.Equal(t, belowAction, settled.tree.below)

	closed, _ := settled.updateAgents(keyMsg("e"))
	got, ok := closed.(model)
	require.True(t, ok)

	assert.Equal(t, belowNothing, got.tree.below)
	assert.Equal(t, []string{"a1/_editor"}, client.fired, "the second press runs nothing")
	assert.Contains(t, recorder.killed, "%2", "the pane goes with it")
}

// belowActionModel builds the panel on a list of one agent under one workflow header, with
// the cursor where the test puts it. Row 0 is the header and row 1 the agent.
func belowActionModel(recorder *recordingTmux, client *fakeActions, cursor int) model {
	stage := NewStage(recorder, appconfig.DefaultLayout())
	stage.Init("%0")

	return model{
		api:    api{actions: client},
		tmux:   recorder,
		popup:  NewPopupWindow(recorder, "test-session"),
		stage:  stage,
		keys:   appconfig.DefaultKeybindings(),
		screen: screenAgents,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{agent: agentstore.Agent{ID: "a1", Name: "auth", Pane: "%9", Cwd: "/repo", WorkflowID: "w1"}, alive: true},
			},
			cursor: cursor,
		},
	}
}
