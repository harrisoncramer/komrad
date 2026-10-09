package task_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/store"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/lib/xslice"
)

// TestCreate pins which new tasks are refused.
func TestCreate(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	workflow := stores.SeedWorkflow(t, workflowstore.Config{Name: "komrad"})
	other := stores.SeedWorkflow(t, workflowstore.Config{Name: "other"})
	archived := stores.SeedTask(t, taskstore.CreateParams{
		WorkflowID: workflow.ID,
		Slug:       "archived-work",
	})
	require.NoError(t, stores.Tasks.Archive(ctx, archived.ID))

	tests := []struct {
		name    string
		params  taskstore.CreateParams
		wantErr error
	}{
		{
			name: "an empty slug is refused",
			params: taskstore.CreateParams{
				WorkflowID: workflow.ID,
				Title:      "Empty",
			},
			wantErr: taskstore.ErrBadSlug,
		},
		{
			name: "an uppercase slug is refused",
			params: taskstore.CreateParams{
				WorkflowID: workflow.ID,
				Slug:       "Record-Teardown",
				Title:      "Upper",
			},
			wantErr: taskstore.ErrBadSlug,
		},
		{
			name: "a slug with a doubled hyphen is refused",
			params: taskstore.CreateParams{
				WorkflowID: workflow.ID,
				Slug:       "record--teardown",
				Title:      "Doubled",
			},
			wantErr: taskstore.ErrBadSlug,
		},
		{
			name: "a slug an archived task holds is refused in any workflow",
			params: taskstore.CreateParams{
				WorkflowID: other.ID,
				Slug:       "archived-work",
				Title:      "Reused",
			},
			wantErr: store.ErrNameTaken,
		},
		{
			name: "a workflow that does not exist is not found",
			params: taskstore.CreateParams{
				WorkflowID: "01a11e6f-af0b-7497-91fb-d3750f8cbef2",
				Slug:       "orphan",
				Title:      "Orphan",
			},
			wantErr: store.ErrNotFound,
		},
		{
			name: "a kebab-case slug is created",
			params: taskstore.CreateParams{
				WorkflowID: workflow.ID,
				Slug:       "record-teardown",
				Title:      "Record teardown",
				Goal:       "Stop the flash on archive.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created, err := stores.Tasks.Create(ctx, tt.params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.params.Slug, created.Slug)
			assert.Equal(t, tt.params.Title, created.Title)
			assert.Equal(t, tt.params.Goal, created.Goal)
			assert.Equal(t, tt.params.WorkflowID, created.WorkflowID)
			assert.Nil(t, created.ArchivedAt)
		})
	}
}

// TestGet pins that a task reads back by its id or its slug, archived or not.
func TestGet(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	live := stores.SeedTask(t, taskstore.CreateParams{Slug: "live-work"})
	archived := stores.SeedTask(t, taskstore.CreateParams{Slug: "archived-work"})
	require.NoError(t, stores.Tasks.Archive(ctx, archived.ID))

	tests := []struct {
		name    string
		get     func() (*taskstore.Task, error)
		wantID  string
		wantErr error
	}{
		{
			name:    "an id that is not an id is not found",
			get:     func() (*taskstore.Task, error) { return stores.Tasks.Get(ctx, "not-an-id") },
			wantErr: store.ErrNotFound,
		},
		{
			name:    "an id no task holds is not found",
			get:     func() (*taskstore.Task, error) { return stores.Tasks.Get(ctx, "01a11e6f-af0b-7497-91fb-d3750f8cbef2") },
			wantErr: store.ErrNotFound,
		},
		{
			name:    "a slug no task holds is not found",
			get:     func() (*taskstore.Task, error) { return stores.Tasks.GetBySlug(ctx, "missing") },
			wantErr: store.ErrNotFound,
		},
		{
			name:   "a live task reads back by id",
			get:    func() (*taskstore.Task, error) { return stores.Tasks.Get(ctx, live.ID) },
			wantID: live.ID,
		},
		{
			name:   "an archived task reads back by slug",
			get:    func() (*taskstore.Task, error) { return stores.Tasks.GetBySlug(ctx, "archived-work") },
			wantID: archived.ID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.get()
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, got.ID)
		})
	}
}

// TestList pins that a workflow lists its live or its archived tasks, the newest first.
func TestList(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	workflow := stores.SeedWorkflow(t, workflowstore.Config{Name: "komrad"})
	stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "first"})
	stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "second"})
	done := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "done"})
	stores.SeedTask(t, taskstore.CreateParams{Slug: "elsewhere"})
	require.NoError(t, stores.Tasks.Archive(ctx, done.ID))

	tests := []struct {
		name       string
		workflowID string
		archived   bool
		want       []string
	}{
		{
			name:       "an id that is not an id lists nothing",
			workflowID: "not-an-id",
			want:       []string{},
		},
		{
			name:       "the live tasks list the newest first",
			workflowID: workflow.ID,
			want:       []string{"second", "first"},
		},
		{
			name:       "the archived tasks list on their own",
			workflowID: workflow.ID,
			archived:   true,
			want:       []string{"done"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stores.Tasks.List(ctx, tt.workflowID, tt.archived)
			require.NoError(t, err)
			assert.Equal(t, tt.want, xslice.Map(got, func(task taskstore.Task) string { return task.Slug }))
		})
	}
}

// TestArchive pins that archiving stamps a task once and refuses one that does not exist.
func TestArchive(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	task := stores.SeedTask(t, taskstore.CreateParams{Slug: "work"})

	require.ErrorIs(t, stores.Tasks.Archive(ctx, "01a11e6f-af0b-7497-91fb-d3750f8cbef2"), store.ErrNotFound)

	require.NoError(t, stores.Tasks.Archive(ctx, task.ID))
	first, err := stores.Tasks.Get(ctx, task.ID)
	require.NoError(t, err)
	require.NotNil(t, first.ArchivedAt)

	require.NoError(t, stores.Tasks.Archive(ctx, task.ID))
	second, err := stores.Tasks.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, first.ArchivedAt, second.ArchivedAt, "a second archive keeps the first stamp")
}

// TestIsSlugTaken pins that a slug stays taken once its task is archived.
func TestIsSlugTaken(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	task := stores.SeedTask(t, taskstore.CreateParams{Slug: "work"})
	require.NoError(t, stores.Tasks.Archive(ctx, task.ID))

	taken, err := stores.Tasks.IsSlugTaken(ctx, "work")
	require.NoError(t, err)
	assert.True(t, taken)

	taken, err = stores.Tasks.IsSlugTaken(ctx, "other")
	require.NoError(t, err)
	assert.False(t, taken)
}

// TestDeletingAWorkflowTakesItsTasks pins that a workflow's tasks never hold the workflow back from deletion.
func TestDeletingAWorkflowTakesItsTasks(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	task := stores.SeedTask(t, taskstore.CreateParams{Slug: "work"})

	require.NoError(t, stores.Workflows.Delete(ctx, task.WorkflowID))

	_, err := stores.Tasks.Get(ctx, task.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}
