package task_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	documentstore "komrad/internal/store/document"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
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
	worker := stores.SeedAgent(t, agentstore.Agent{Name: "worker", WorkflowID: live.WorkflowID, TaskID: &live.ID})
	orchestrator := stores.SeedAgent(t, agentstore.Agent{Name: "orchestrator", WorkflowID: live.WorkflowID, Role: agentstore.RoleOrchestrator})

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
			name:    "an agent on no task has no task",
			get:     func() (*taskstore.Task, error) { return stores.Tasks.GetByAgentID(ctx, orchestrator.ID) },
			wantErr: taskstore.ErrNoTask,
		},
		{
			name: "an id no agent holds has no task",
			get: func() (*taskstore.Task, error) {
				return stores.Tasks.GetByAgentID(ctx, "01a11e6f-af0b-7497-91fb-d3750f8cbef2")
			},
			wantErr: taskstore.ErrNoTask,
		},
		{
			name:   "an agent's task reads back by the agent's id",
			get:    func() (*taskstore.Task, error) { return stores.Tasks.GetByAgentID(ctx, worker.ID) },
			wantID: live.ID,
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

// TestArchiveIdle pins that only the named tasks no live agent works on are archived, and are the ones it returns.
func TestArchiveIdle(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	busy := stores.SeedTask(t, taskstore.CreateParams{Slug: "busy"})
	stores.SeedAgent(t, agentstore.Agent{Name: "worker", WorkflowID: busy.WorkflowID, TaskID: &busy.ID})
	done := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: busy.WorkflowID, Slug: "done"})
	finished := stores.SeedAgent(t, agentstore.Agent{Name: "finished", WorkflowID: busy.WorkflowID, TaskID: &done.ID})
	_, err := stores.Agents.Archive(ctx, []string{finished.ID})
	require.NoError(t, err)
	empty := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: busy.WorkflowID, Slug: "empty"})
	unnamed := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: busy.WorkflowID, Slug: "unnamed"})

	archived, err := stores.Tasks.ArchiveIdle(ctx, []string{busy.ID, done.ID, empty.ID})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{done.ID, empty.ID}, xslice.Map(archived, func(task taskstore.Task) string { return task.ID }))

	tests := []struct {
		name         string
		id           string
		wantArchived bool
	}{
		{name: "a task a live agent works on stays live", id: busy.ID},
		{name: "a task whose agents are all archived is archived", id: done.ID, wantArchived: true},
		{name: "a task no agent works on is archived", id: empty.ID, wantArchived: true},
		{name: "a task not named stays live", id: unnamed.ID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stores.Tasks.Get(ctx, tt.id)
			require.NoError(t, err)
			assert.Equal(t, tt.wantArchived, got.ArchivedAt != nil)
		})
	}
}

// TestArchiveIdleBesides pins that a task is archived only when no live agent but the one named works on it.
func TestArchiveIdleBesides(t *testing.T) {
	tests := []struct {
		name         string
		other        bool
		wantArchived bool
	}{
		{
			name:         "a task another live agent works on stays live",
			other:        true,
			wantArchived: false,
		},
		{
			name:         "a task only the named agent works on is archived",
			other:        false,
			wantArchived: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			ctx := t.Context()
			task := stores.SeedTask(t, taskstore.CreateParams{Slug: "work"})
			named := stores.SeedAgent(t, agentstore.Agent{Name: "joiner", WorkflowID: task.WorkflowID, TaskID: &task.ID})
			if tt.other {
				stores.SeedAgent(t, agentstore.Agent{Name: "other", WorkflowID: task.WorkflowID, TaskID: &task.ID})
			}

			require.NoError(t, stores.Tasks.ArchiveIdleBesides(ctx, task.ID, named.ID))

			got, err := stores.Tasks.Get(ctx, task.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantArchived, got.ArchivedAt != nil)
		})
	}
}

// TestRestore pins that restoring returns the named archived tasks to the sidebar and leaves every other task as it was.
func TestRestore(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	archived := stores.SeedTask(t, taskstore.CreateParams{Slug: "archived"})
	hide(t, stores, archived.ID)
	require.NoError(t, stores.Tasks.Archive(ctx, archived.ID))
	live := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: archived.WorkflowID, Slug: "live"})
	hide(t, stores, live.ID)
	unnamed := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: archived.WorkflowID, Slug: "unnamed"})
	require.NoError(t, stores.Tasks.Archive(ctx, unnamed.ID))

	require.NoError(t, stores.Tasks.Restore(ctx, []string{archived.ID, live.ID}))

	tests := []struct {
		name         string
		id           string
		wantArchived bool
		wantHidden   bool
	}{
		{name: "an archived task named is restored unhidden", id: archived.ID},
		{name: "a live task named stays live and hidden", id: live.ID, wantHidden: true},
		{name: "an archived task not named stays archived", id: unnamed.ID, wantArchived: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stores.Tasks.Get(ctx, tt.id)
			require.NoError(t, err)
			assert.Equal(t, tt.wantArchived, got.ArchivedAt != nil)
			assert.Equal(t, tt.wantHidden, got.Hidden)
		})
	}
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

// TestListWatched covers the watched set read whole and by key.
func TestListWatched(t *testing.T) {
	tests := []struct {
		name      string
		seeds     []string
		archive   string
		keys      []string
		wantSlugs []string
	}{
		{
			name:      "an empty store lists nothing",
			wantSlugs: []string{},
		},
		{
			name:      "keys naming no task list nothing",
			seeds:     []string{"a"},
			keys:      []string{"00000000-0000-0000-0000-000000000000"},
			wantSlugs: []string{},
		},
		{
			name:      "an empty key list lists nothing",
			seeds:     []string{"a"},
			keys:      []string{},
			wantSlugs: []string{},
		},
		{
			name:      "nil keys list every task",
			seeds:     []string{"a", "b"},
			wantSlugs: []string{"a", "b"},
		},
		{
			name:      "an archived one stays in the whole set",
			seeds:     []string{"a", "b"},
			archive:   "b",
			wantSlugs: []string{"a", "b"},
		},
		{
			name:      "keys list only the tasks they name",
			seeds:     []string{"a", "b"},
			keys:      []string{"a"},
			wantSlugs: []string{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			ctx := t.Context()

			bySlug := make(map[string]string, len(tt.seeds))
			for _, slug := range tt.seeds {
				bySlug[slug] = stores.SeedTask(t, taskstore.CreateParams{Slug: slug}).ID
			}
			if tt.archive != "" {
				require.NoError(t, stores.Tasks.Archive(ctx, bySlug[tt.archive]))
			}

			keys := tt.keys
			if keys != nil {
				keys = xslice.Map(keys, func(key string) string {
					if id, ok := bySlug[key]; ok {
						return id
					}

					return key
				})
			}

			listed, err := stores.Tasks.ListWatched(ctx, keys)
			require.NoError(t, err)

			assert.ElementsMatch(t, tt.wantSlugs, xslice.Map(listed, func(task taskstore.Task) string { return task.Slug }))
		})
	}
}

// TestListWatchedDocuments pins the count of a task's documents across every agent on it and the
// time of its newest write.
func TestListWatchedDocuments(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	first := stores.SeedAgent(t, agentstore.Agent{Name: "first"})
	second := stores.SeedAgent(t, agentstore.Agent{Name: "second", TaskID: first.TaskID})
	elsewhere := stores.SeedAgent(t, agentstore.Agent{Name: "elsewhere"})

	documents := func(id string) taskstore.Task {
		listed, err := stores.Tasks.ListWatched(ctx, []string{id})
		require.NoError(t, err)
		require.Len(t, listed, 1)

		return listed[0]
	}

	task := documents(*first.TaskID)
	assert.Zero(t, task.Documents)
	assert.Nil(t, task.DocumentsUpdatedAt, "a task with no documents has no newest write")

	write := func(agentID, name string) *documentstore.Document {
		created, err := stores.Documents.Create(ctx, documentstore.CreateParams{AgentID: agentID, Name: name, Type: "RFC", Body: name})
		require.NoError(t, err)

		return created
	}
	write(first.ID, "plan")
	newest := write(second.ID, "notes")

	task = documents(*first.TaskID)
	assert.Equal(t, int32(2), task.Documents, "every agent on the task counts toward it")
	require.NotNil(t, task.DocumentsUpdatedAt)
	assert.True(t, task.DocumentsUpdatedAt.Equal(newest.UpdatedAt), "the newest write is the one that landed last")
	assert.Zero(t, documents(*elsewhere.TaskID).Documents, "another task's documents are not counted")
}

// TestUpdate pins which rewrites of a task are refused and that a field left unset keeps its value.
func TestUpdate(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	task := stores.SeedTask(t, taskstore.CreateParams{Slug: "work", Title: "Work", Goal: "It works."})
	other := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: task.WorkflowID, Slug: "other"})
	deleting := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: task.WorkflowID, Slug: "deleting"})
	require.NoError(t, stores.Tasks.MarkDeleting(ctx, []string{deleting.ID}))

	tests := []struct {
		name      string
		id        string
		params    taskstore.UpdateParams
		wantErr   error
		wantTitle string
		wantGoal  string
		wantNotes string
	}{
		{
			name:    "a slug that is not kebab-case is refused",
			id:      task.ID,
			params:  taskstore.UpdateParams{Slug: xptr.Ptr("Not Kebab")},
			wantErr: taskstore.ErrBadSlug,
		},
		{
			name:    "a slug another task holds is refused",
			id:      task.ID,
			params:  taskstore.UpdateParams{Slug: xptr.Ptr(other.Slug)},
			wantErr: store.ErrNameTaken,
		},
		{
			name:    "a ref that is not an id is not found",
			id:      "work",
			params:  taskstore.UpdateParams{Title: xptr.Ptr("Renamed")},
			wantErr: store.ErrNotFound,
		},
		{
			name:    "a task being deleted is not found",
			id:      deleting.ID,
			params:  taskstore.UpdateParams{Title: xptr.Ptr("Renamed")},
			wantErr: store.ErrNotFound,
		},
		{
			name:    "notes over a version that was never written are refused",
			id:      task.ID,
			params:  taskstore.UpdateParams{Notes: xptr.Ptr("Found it."), ReadNotesUpdatedAt: xptr.Ptr(time.Now())},
			wantErr: taskstore.ErrNotesChanged,
		},
		{
			name:      "only the fields set are rewritten",
			id:        task.ID,
			params:    taskstore.UpdateParams{Title: xptr.Ptr("Renamed"), Notes: xptr.Ptr("Found it.")},
			wantTitle: "Renamed",
			wantGoal:  "It works.",
			wantNotes: "Found it.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stores.Tasks.Update(ctx, tt.id, tt.params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, task.Slug, got.Slug)
			assert.Equal(t, tt.wantTitle, got.Title)
			assert.Equal(t, tt.wantGoal, got.Goal)
			assert.Equal(t, tt.wantNotes, got.Notes)
			assert.NotNil(t, got.NotesUpdatedAt)
		})
	}
}

// TestUpdateNotesVersion pins that a write of the notes must name the version it read.
func TestUpdateNotesVersion(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	task := stores.SeedTask(t, taskstore.CreateParams{Slug: "work"})

	first, err := stores.Tasks.Update(ctx, task.ID, taskstore.UpdateParams{Notes: xptr.Ptr("One.")})
	require.NoError(t, err)

	_, err = stores.Tasks.Update(ctx, task.ID, taskstore.UpdateParams{Notes: xptr.Ptr("Stale.")})
	require.ErrorIs(t, err, taskstore.ErrNotesChanged)

	second, err := stores.Tasks.Update(ctx, task.ID, taskstore.UpdateParams{Notes: xptr.Ptr("Two."), ReadNotesUpdatedAt: first.NotesUpdatedAt})
	require.NoError(t, err)
	assert.Equal(t, "Two.", second.Notes)
	assert.True(t, second.NotesUpdatedAt.After(*first.NotesUpdatedAt))
}

// TestSetHidden pins which tasks take a hide and which are refused.
func TestSetHidden(t *testing.T) {
	tests := []struct {
		name        string
		id          func(t *testing.T, stores *storetest.Stores) string
		hidden      bool
		wantChanged bool
		wantHidden  bool
	}{
		{
			name: "an archived task is refused",
			id: func(t *testing.T, stores *storetest.Stores) string {
				task := stores.SeedTask(t, taskstore.CreateParams{Slug: "archived"})
				require.NoError(t, stores.Tasks.Archive(t.Context(), task.ID))

				return task.ID
			},
			hidden: true,
		},
		{
			name: "a live task is hidden",
			id: func(t *testing.T, stores *storetest.Stores) string {
				return stores.SeedTask(t, taskstore.CreateParams{Slug: "live"}).ID
			},
			hidden:      true,
			wantChanged: true,
			wantHidden:  true,
		},
		{
			name: "a hidden task is shown again",
			id: func(t *testing.T, stores *storetest.Stores) string {
				task := stores.SeedTask(t, taskstore.CreateParams{Slug: "hidden"})
				hide(t, stores, task.ID)

				return task.ID
			},
			wantChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			id := tt.id(t, stores)

			changed, err := stores.Tasks.SetHidden(t.Context(), id, tt.hidden)
			require.NoError(t, err)
			assert.Equal(t, tt.wantChanged, changed)

			got, err := stores.Tasks.Get(t.Context(), id)
			require.NoError(t, err)
			assert.Equal(t, tt.wantHidden, got.Hidden)
		})
	}
}

// TestSetHiddenIgnoresAnIDNoRowCouldHold pins the guard that keeps a malformed id from Postgres.
func TestSetHiddenIgnoresAnIDNoRowCouldHold(t *testing.T) {
	changed, err := storetest.Open(t).Tasks.SetHidden(t.Context(), "not-a-uuid", true)
	require.NoError(t, err)
	assert.False(t, changed, "nothing was hidden")
}

func hide(t *testing.T, stores *storetest.Stores, id string) {
	t.Helper()

	changed, err := stores.Tasks.SetHidden(t.Context(), id, true)
	require.NoError(t, err)
	require.True(t, changed)
}
