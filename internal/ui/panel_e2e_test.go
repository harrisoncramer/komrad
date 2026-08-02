package ui

import (
	"testing"

	"weave/internal/appconfig"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPanelArchivesThroughTheService covers a keypress reaching the database. Every other
// test here answers the panel from a fake client, which says what the panel asked for and
// nothing about whether asking for it does anything, so the archive key is exercised once
// against the real service with a real agent running in a real pane behind it.
func TestPanelArchivesThroughTheService(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	agent := created.Msg.GetAgent()
	require.NotEmpty(t, instance.GetOwnedPane(agent.GetId()))

	keys := appconfig.DefaultKeybindings()
	tmux := &recordingTmux{}
	panel := model{
		api: api{
			agents:    instance.Agents,
			workflows: instance.Workflows,
			schedules: instance.Schedules,
			actions:   instance.Actions,
		},
		tmux:   tmux,
		popup:  NewPopupWindow(tmux, "test-session"),
		stage:  NewStage(tmux, appconfig.DefaultSplit),
		keys:   keys,
		screen: screenAgents,
	}

	panel = settle(t, panel, panel.refreshCmd())
	require.Equal(t, []string{"header:backend", "agent:login"}, describeRows(panel.agentRows()))

	// The cursor sits on the agent rather than the folder it is nested under, since the
	// same key on the header archives the whole workflow.
	panel.tree.cursor = 1

	updated, cmd := panel.Update(keyMsg(keys.Agents.Archive[0]))
	pressed, ok := updated.(model)
	require.True(t, ok)
	panel = settle(t, pressed, cmd)

	listed, err := instance.Agents.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{Archived: true}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetAgents(), 1)
	assert.Equal(t, agent.GetId(), listed.Msg.GetAgents()[0].GetId())

	assert.Empty(t, instance.GetOwnedPane(agent.GetId()), "archiving from the panel stops what was running")

	panel = settle(t, panel, panel.refreshCmd())
	assert.Equal(t, []string{"header:backend"}, describeRows(panel.agentRows()),
		"and the row goes off the panel's own list, leaving the folder it was in")
}
