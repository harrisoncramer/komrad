package task_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	"komrad/internal/storetest"
)

// TestClaimEmpty pins which tasks a cleanup can take, and that only one cleanup takes each.
func TestClaimEmpty(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	live := stores.SeedTask(t, taskstore.CreateParams{Slug: "live"})
	busy := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: live.WorkflowID, Slug: "busy"})
	stores.SeedAgent(t, agentstore.Agent{
		Name:       "worker",
		WorkflowID: live.WorkflowID,
		TaskID:     &busy.ID,
	})
	taken := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: live.WorkflowID, Slug: "taken"})
	empty := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: live.WorkflowID, Slug: "empty"})
	require.NoError(t, stores.Tasks.MarkDeleting(ctx, []string{busy.ID, taken.ID, empty.ID}))
	first, err := stores.Tasks.ClaimEmpty(ctx, taken.ID)
	require.NoError(t, err)
	require.True(t, first)

	tests := []struct {
		name string
		id   string
		want bool
	}{
		{
			name: "a ref that is not an id is not taken",
			id:   "taken",
			want: false,
		},
		{
			name: "a task no delete claimed is not taken",
			id:   live.ID,
			want: false,
		},
		{
			name: "a task an agent is still on is not taken",
			id:   busy.ID,
			want: false,
		},
		{
			name: "a task another cleanup took is not taken again",
			id:   taken.ID,
			want: false,
		},
		{
			name: "an empty task being deleted is taken",
			id:   empty.ID,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stores.Tasks.ClaimEmpty(ctx, tt.id)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestClearDeleting pins a released task being claimable again by a later delete.
func TestClearDeleting(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	task := stores.SeedTask(t, taskstore.CreateParams{Slug: "work"})
	require.NoError(t, stores.Tasks.MarkDeleting(ctx, []string{task.ID}))
	claimed, err := stores.Tasks.ClaimEmpty(ctx, task.ID)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, stores.Tasks.ClearDeleting(ctx, task.ID))

	released, err := stores.Tasks.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Nil(t, released.DeletingAt)
	require.NoError(t, stores.Tasks.MarkDeleting(ctx, []string{task.ID}))
	again, err := stores.Tasks.ClaimEmpty(ctx, task.ID)
	require.NoError(t, err)
	assert.True(t, again)
}

// TestRecordCleanupFailure pins a failed cleanup releasing the task with its reason, and the next delete
// clearing that reason.
func TestRecordCleanupFailure(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	task := stores.SeedTask(t, taskstore.CreateParams{Slug: "work"})
	require.NoError(t, stores.Tasks.MarkDeleting(ctx, []string{task.ID}))
	claimed, err := stores.Tasks.ClaimEmpty(ctx, task.ID)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, stores.Tasks.RecordCleanupFailure(ctx, task.ID, errors.New("teardown exited 1")))

	failed, err := stores.Tasks.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Nil(t, failed.DeletingAt)
	assert.Equal(t, "teardown exited 1", failed.FailureReason)
	require.NoError(t, stores.Tasks.MarkDeleting(ctx, []string{task.ID}))
	retried, err := stores.Tasks.ClaimEmpty(ctx, task.ID)
	require.NoError(t, err)
	assert.True(t, retried)
	require.NoError(t, stores.Tasks.ClearDeleting(ctx, task.ID))
	cleared, err := stores.Tasks.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Empty(t, cleared.FailureReason)
}

// TestClearAbandonedDeleting pins that only a task an agent delete still holds keeps its claim.
func TestClearAbandonedDeleting(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	held := stores.SeedTask(t, taskstore.CreateParams{Slug: "held"})
	leaving := stores.SeedAgent(t, agentstore.Agent{
		Name:       "leaving",
		WorkflowID: held.WorkflowID,
		TaskID:     &held.ID,
	})
	_, err := stores.Agents.MarkDeleting(ctx, []string{leaving.ID})
	require.NoError(t, err)
	staying := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: held.WorkflowID, Slug: "staying"})
	stores.SeedAgent(t, agentstore.Agent{
		Name:       "staying",
		WorkflowID: held.WorkflowID,
		TaskID:     &staying.ID,
	})
	empty := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: held.WorkflowID, Slug: "empty"})
	require.NoError(t, stores.Tasks.MarkDeleting(ctx, []string{held.ID, staying.ID, empty.ID}))

	require.NoError(t, stores.Tasks.ClearAbandonedDeleting(ctx))

	tests := []struct {
		name         string
		id           string
		wantDeleting bool
	}{
		{
			name:         "a task whose only agent is not being deleted is released",
			id:           staying.ID,
			wantDeleting: false,
		},
		{
			name:         "a task no agent is on is released",
			id:           empty.ID,
			wantDeleting: false,
		},
		{
			name:         "a task an agent delete holds keeps its claim",
			id:           held.ID,
			wantDeleting: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stores.Tasks.Get(ctx, tt.id)
			require.NoError(t, err)
			assert.Equal(t, tt.wantDeleting, got.DeletingAt != nil)
		})
	}
}

// TestMarkDeletingHidesTheTask pins a task being deleted leaving both listings.
func TestMarkDeletingHidesTheTask(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	task := stores.SeedTask(t, taskstore.CreateParams{Slug: "work"})
	archived := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: task.WorkflowID, Slug: "archived"})
	require.NoError(t, stores.Tasks.Archive(ctx, archived.ID))

	require.NoError(t, stores.Tasks.MarkDeleting(ctx, []string{task.ID, archived.ID, "not-an-id"}))

	live, err := stores.Tasks.List(ctx, task.WorkflowID, false)
	require.NoError(t, err)
	assert.Empty(t, live)
	gone, err := stores.Tasks.List(ctx, task.WorkflowID, true)
	require.NoError(t, err)
	assert.Empty(t, gone)
}

// TestLockUndeleted pins a task being deleted refusing the lock a joining agent takes.
func TestLockUndeleted(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	live := stores.SeedTask(t, taskstore.CreateParams{Slug: "live"})
	deleting := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: live.WorkflowID, Slug: "deleting"})
	require.NoError(t, stores.Tasks.MarkDeleting(ctx, []string{deleting.ID}))

	require.ErrorIs(t, stores.Tasks.LockUndeleted(ctx, deleting.ID), taskstore.ErrDeleting)
	require.NoError(t, stores.Tasks.LockUndeleted(ctx, live.ID))
}

// TestGetUndeletedForUpdate pins a task being deleted refusing the lock a joining writer takes, and a
// live one coming back as it stands.
func TestGetUndeletedForUpdate(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	live := stores.SeedTask(t, taskstore.CreateParams{Slug: "live"})
	deleting := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: live.WorkflowID, Slug: "deleting"})
	require.NoError(t, stores.Tasks.MarkDeleting(ctx, []string{deleting.ID}))

	_, err := stores.Tasks.GetUndeletedForUpdate(ctx, deleting.ID)
	require.ErrorIs(t, err, taskstore.ErrDeleting)

	got, err := stores.Tasks.GetUndeletedForUpdate(ctx, live.ID)
	require.NoError(t, err)
	assert.Equal(t, live.Slug, got.Slug)
}

// TestListUndeletedSlugs pins a task leaving the listing once a delete claims it, archived or not.
func TestListUndeletedSlugs(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	live := stores.SeedTask(t, taskstore.CreateParams{Slug: "live"})
	archived := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: live.WorkflowID, Slug: "archived"})
	require.NoError(t, stores.Tasks.Archive(ctx, archived.ID))
	deleting := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: live.WorkflowID, Slug: "deleting"})
	require.NoError(t, stores.Tasks.MarkDeleting(ctx, []string{deleting.ID}))

	got, err := stores.Tasks.ListUndeletedSlugs(ctx)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{live.ID: "live", archived.ID: "archived"}, got)
}
