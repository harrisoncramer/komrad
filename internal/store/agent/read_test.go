package agent

import (
	"testing"

	"komrad/internal/store"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetInWorkflowAnswersNotFoundForANameNobodyHolds pins store.ErrNotFound for a miss.
func TestGetInWorkflowAnswersNotFoundForANameNobodyHolds(t *testing.T) {
	st := open(t)

	_, err := st.GetInWorkflow(t.Context(), st.defaultWorkflow, "nobody")

	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestCountLiveReviewers pins the reviewers counted against a task's ceiling.
func TestCountLiveReviewers(t *testing.T) {
	f := open(t)
	ctx := t.Context()

	require.NoError(t, f.Create(ctx, Agent{Name: "writer", State: agentv1.AgentState_AGENT_STATE_IDLE}))
	writer, err := f.Get(ctx, "writer")
	require.NoError(t, err)
	require.NoError(t, f.Create(ctx, Agent{Name: "other", State: agentv1.AgentState_AGENT_STATE_IDLE}))
	other, err := f.Get(ctx, "other")
	require.NoError(t, err)

	for _, reviewer := range []Agent{
		{Name: "first", TaskID: writer.TaskID},
		{Name: "second", TaskID: writer.TaskID},
		{Name: "archived", TaskID: writer.TaskID},
		{Name: "elsewhere", TaskID: other.TaskID},
	} {
		reviewer.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER
		reviewer.State = agentv1.AgentState_AGENT_STATE_IDLE
		require.NoError(t, f.Create(ctx, reviewer))
	}
	require.NoError(t, f.archiveByName(ctx, "archived"))

	count, err := f.CountLiveReviewers(ctx, *writer.TaskID)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "the writer, an archived reviewer and one on another task are left out")
}

// TestCountLiveTasksStartedBy pins the tasks counted against an orchestrator's ceiling.
func TestCountLiveTasksStartedBy(t *testing.T) {
	f := open(t)
	ctx := t.Context()

	require.NoError(t, f.Create(ctx, Agent{
		Name:  "orchestrator",
		Role:  agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR,
		State: agentv1.AgentState_AGENT_STATE_IDLE,
	}))
	orchestrator, err := f.Get(ctx, "orchestrator")
	require.NoError(t, err)
	require.NoError(t, f.Create(ctx, Agent{Name: "writer", State: agentv1.AgentState_AGENT_STATE_IDLE}))
	writer, err := f.Get(ctx, "writer")
	require.NoError(t, err)

	for _, agent := range []Agent{
		{
			Name:             "live",
			State:            agentv1.AgentState_AGENT_STATE_IDLE,
			SpawnedByAgentID: &orchestrator.ID,
		},
		{
			Name:             "archived",
			State:            agentv1.AgentState_AGENT_STATE_IDLE,
			SpawnedByAgentID: &orchestrator.ID,
		},
		{
			Name:             "failed",
			State:            agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
			SpawnedByAgentID: &orchestrator.ID,
		},
		{
			Name:             "reviewer",
			State:            agentv1.AgentState_AGENT_STATE_IDLE,
			SpawnedByAgentID: &orchestrator.ID,
			Role:             agentv1.AgentRole_AGENT_ROLE_REVIEWER,
			TaskID:           writer.TaskID,
		},
		{
			Name:             "started-by-writer",
			State:            agentv1.AgentState_AGENT_STATE_IDLE,
			SpawnedByAgentID: &writer.ID,
		},
	} {
		require.NoError(t, f.Create(ctx, agent))
	}
	require.NoError(t, f.archiveByName(ctx, "archived"))

	count, err := f.CountLiveTasksStartedBy(ctx, orchestrator.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "only the live writer it started counts")
}
