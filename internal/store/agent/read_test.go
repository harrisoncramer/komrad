package agent

import (
	"testing"

	"komrad/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListByIDIgnoresAnIDNoRowCouldHold pins the guard that keeps a malformed id from Postgres.
func TestListByIDIgnoresAnIDNoRowCouldHold(t *testing.T) {
	got, err := open(t).ListByID(t.Context(), []string{"not-a-uuid", ""})

	require.NoError(t, err, "an id no row could hold is nothing to do rather than something to report")
	assert.Empty(t, got, "no row answers to either of those")
}

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

	require.NoError(t, f.Create(ctx, Agent{Name: "writer", State: StateIdle}))
	writer, err := f.Get(ctx, "writer")
	require.NoError(t, err)
	require.NoError(t, f.Create(ctx, Agent{Name: "other", State: StateIdle}))
	other, err := f.Get(ctx, "other")
	require.NoError(t, err)

	for _, reviewer := range []Agent{
		{Name: "first", TaskID: writer.TaskID},
		{Name: "second", TaskID: writer.TaskID},
		{Name: "archived", TaskID: writer.TaskID},
		{Name: "elsewhere", TaskID: other.TaskID},
	} {
		reviewer.Role = RoleReviewer
		reviewer.State = StateIdle
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
		Role:  RoleOrchestrator,
		State: StateIdle,
	}))
	orchestrator, err := f.Get(ctx, "orchestrator")
	require.NoError(t, err)
	require.NoError(t, f.Create(ctx, Agent{Name: "writer", State: StateIdle}))
	writer, err := f.Get(ctx, "writer")
	require.NoError(t, err)

	for _, agent := range []Agent{
		{
			Name:             "live",
			State:            StateIdle,
			SpawnedByAgentID: &orchestrator.ID,
		},
		{
			Name:             "archived",
			State:            StateIdle,
			SpawnedByAgentID: &orchestrator.ID,
		},
		{
			Name:             "failed",
			State:            StateFailedToCreate,
			SpawnedByAgentID: &orchestrator.ID,
		},
		{
			Name:             "reviewer",
			State:            StateIdle,
			SpawnedByAgentID: &orchestrator.ID,
			Role:             RoleReviewer,
			TaskID:           writer.TaskID,
		},
		{
			Name:             "started-by-writer",
			State:            StateIdle,
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
